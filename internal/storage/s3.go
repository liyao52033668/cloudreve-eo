package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// S3Driver 使用 AWS SDK v2 实现 S3 兼容存储驱动（含 MinIO 等）。
type S3Driver struct {
	client     *s3.Client
	bucket     string
	customHost string // 自定义下载/预览域名；空表示使用 Endpoint
	pathStyle  bool
}

// NewS3Driver 创建 S3 兼容存储驱动。
// endpoint 非空时使用自定义端点（MinIO/COS 等）。
// forcePathStyle 为 true 时使用 path-style（http://endpoint/bucket/key），
// false 时使用 virtual-hosted（http://bucket.endpoint/key）；MinIO 与部分私有 S3 通常需开启。
// customHost 非空时，生成的下载/预览 URL 将使用该自定义域名（如 COS / 七牛的 CDN 加速域名）。
func NewS3Driver(endpoint, region, bucket, accessKey, secretKey string, forcePathStyle bool, customHost string) (*S3Driver, error) {
	resolver := aws.EndpointResolverWithOptionsFunc(
		func(service, reg string, options ...interface{}) (aws.Endpoint, error) {
			if endpoint != "" {
				return aws.Endpoint{URL: endpoint}, nil
			}
			return aws.Endpoint{}, &aws.EndpointNotFoundError{}
		},
	)

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		),
		awsconfig.WithEndpointResolverWithOptions(resolver),
	)
	if err != nil {
		return nil, fmt.Errorf("加载 S3 配置失败: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = forcePathStyle
	})

	return &S3Driver{client: client, bucket: bucket, customHost: customHost, pathStyle: forcePathStyle}, nil
}

func (d *S3Driver) GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error) {
	presigner := s3.NewPresignClient(d.client)
	result, err := presigner.PresignPutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      aws.String(d.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(expire))
	if err != nil {
		return "", fmt.Errorf("生成上传 URL 失败: %w", err)
	}
	return result.URL, nil
}

func (d *S3Driver) GenerateDownloadURL(key string, fileName string, expire time.Duration) (string, error) {
	presigner := s3.NewPresignClient(d.client)
	input := &s3.GetObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(key),
	}
	// 使用自定义域名（CDN）时，不能设置 ResponseContentDisposition，
	// 因为 CDN 处理匿名 GET 请求时不支持覆盖响应头
	if fileName != "" && d.customHost == "" {
		// RFC 5987 编码，支持中文等非 ASCII 文件名
		input.ResponseContentDisposition = aws.String(
			fmt.Sprintf(`attachment; filename*=UTF-8''%s`, url.PathEscape(fileName)),
		)
	}
	result, err := presigner.PresignGetObject(context.Background(), input, s3.WithPresignExpires(expire))
	if err != nil {
		return "", fmt.Errorf("生成下载 URL 失败: %w", err)
	}
	if d.customHost != "" {
		return rewriteDownloadURL(result.URL, d.customHost, d.bucket, d.pathStyle)
	}
	return result.URL, nil
}

// rewriteDownloadURL 把预签名 URL 的主机替换为自定义域名（COS / 七牛等 CDN 加速域名），
// 保留签名 query 与路径。path-style 下原 URL 形如 https://endpoint/bucket/key，
// CDN 域名通常已绑定到 bucket，需去掉开头的 /bucket 前缀。
func rewriteDownloadURL(rawURL, customHost, bucket string, pathStyle bool) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("解析预签名 URL 失败: %w", err)
	}
	host, err := url.Parse(customHost)
	if err != nil {
		return "", fmt.Errorf("解析自定义域名失败: %w", err)
	}
	scheme := host.Scheme
	if scheme == "" {
		scheme = "https"
	}
	u.Scheme = scheme
	u.Host = host.Host
	if pathStyle && bucket != "" && strings.HasPrefix(u.Path, "/"+bucket) {
		u.Path = strings.TrimPrefix(u.Path, "/"+bucket)
	}
	return u.String(), nil
}

func (d *S3Driver) Delete(key string) error {
	_, err := d.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("删除对象失败: %w", err)
	}
	return nil
}

func (d *S3Driver) GetSize(key string) (int64, error) {	result, err := d.client.HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, fmt.Errorf("获取对象大小失败: %w", err)
	}
	if result.ContentLength == nil {
		return 0, fmt.Errorf("获取对象大小失败: ContentLength 为空")
	}
	return *result.ContentLength, nil
}

// Read 打开对象并返回内容流（文件夹打包下载用）。
func (d *S3Driver) Read(key string) (io.ReadCloser, error) {
	result, err := d.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("读取对象失败: %w", err)
	}
	return result.Body, nil
}

// InitMultipartUpload 发起分片上传，返回 uploadID。
func (d *S3Driver) InitMultipartUpload(key string, contentType string) (string, error) {
	result, err := d.client.CreateMultipartUpload(context.Background(), &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(d.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("发起分片上传失败: %w", err)
	}
	if result.UploadId == nil {
		return "", fmt.Errorf("发起分片上传失败: UploadId 为空")
	}
	return *result.UploadId, nil
}

// GenerateUploadPartURL 为指定分片生成预签名 PUT URL。
func (d *S3Driver) GenerateUploadPartURL(key string, uploadID string, partNumber int32, expire time.Duration) (string, error) {
	presigner := s3.NewPresignClient(d.client)
	result, err := presigner.PresignUploadPart(context.Background(), &s3.UploadPartInput{
		Bucket:     aws.String(d.bucket),
		Key:        aws.String(key),
		UploadId:   aws.String(uploadID),
		PartNumber: aws.Int32(partNumber),
	}, s3.WithPresignExpires(expire))
	if err != nil {
		return "", fmt.Errorf("生成分片上传 URL 失败: %w", err)
	}
	return result.URL, nil
}

// CompleteMultipartUpload 合并分片，完成上传。
func (d *S3Driver) CompleteMultipartUpload(key string, uploadID string, parts []CompletedPart) error {
	completed := make([]types.CompletedPart, 0, len(parts))
	for _, p := range parts {
		completed = append(completed, types.CompletedPart{
			PartNumber: aws.Int32(p.PartNumber),
			ETag:       aws.String(p.ETag),
		})
	}
	_, err := d.client.CompleteMultipartUpload(context.Background(), &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(d.bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completed,
		},
	})
	if err != nil {
		return fmt.Errorf("合并分片失败: %w", err)
	}
	return nil
}

// AbortMultipartUpload 取消分片上传，清理已上传分片。
func (d *S3Driver) AbortMultipartUpload(key string, uploadID string) error {
	_, err := d.client.AbortMultipartUpload(context.Background(), &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(d.bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	if err != nil {
		return fmt.Errorf("取消分片上传失败: %w", err)
	}
	return nil
}

// ListUploadedParts 列出已上传的分片（自动翻页取全量）。
func (d *S3Driver) ListUploadedParts(key string, uploadID string) ([]CompletedPart, error) {
	var parts []CompletedPart
	var marker *string
	for {
		result, err := d.client.ListParts(context.Background(), &s3.ListPartsInput{
			Bucket:           aws.String(d.bucket),
			Key:              aws.String(key),
			UploadId:         aws.String(uploadID),
			PartNumberMarker: marker,
		})
		if err != nil {
			return nil, fmt.Errorf("查询已上传分片失败: %w", err)
		}
		for _, p := range result.Parts {
			if p.PartNumber == nil || p.ETag == nil {
				continue
			}
			parts = append(parts, CompletedPart{PartNumber: *p.PartNumber, ETag: *p.ETag})
		}
		if result.IsTruncated == nil || !*result.IsTruncated {
			break
		}
		marker = result.NextPartNumberMarker
	}
	return parts, nil
}

// SetBucketCORS 写入允许浏览器直传（PUT/GET 等）的宽松 CORS 规则。
// ETag 必须显式暴露，否则浏览器分片上传拿不到各分片的 ETag。
func (d *S3Driver) SetBucketCORS() error {
	_, err := d.client.PutBucketCors(context.Background(), &s3.PutBucketCorsInput{
		Bucket: aws.String(d.bucket),
		CORSConfiguration: &types.CORSConfiguration{
			CORSRules: []types.CORSRule{
				{
					AllowedOrigins: []string{"*"},
					AllowedMethods: []string{"GET", "PUT", "POST", "DELETE", "HEAD"},
					AllowedHeaders: []string{"*"},
					ExposeHeaders:  []string{"ETag"},
					MaxAgeSeconds:  aws.Int32(3600),
				},
			},
		},
	})
	if err != nil {
		// 部分 S3 兼容服务（如 EdgeOne 对象存储）不实现 PutBucketCors 管理 API，
		// 此时无法通过 SDK 配置跨域，需引导用户到控制台手动配置。
		var respErr *smithyhttp.ResponseError
		if errors.As(err, &respErr) && respErr.Response.StatusCode == http.StatusMethodNotAllowed {
			return fmt.Errorf("%w（HTTP 405 MethodNotAllowed），请在对象存储控制台手动配置跨域规则：放行 GET/PUT/POST/DELETE/HEAD，并暴露 ETag 响应头", ErrBucketCORSNotSupported)
		}
		return fmt.Errorf("设置存储桶 CORS 失败: %w", err)
	}
	return nil
}

// UploadFile S3 驱动不支持服务端直接上传，应使用预签名 URL 客户端直传。
// UploadFile 服务端直接上传文件到 S3（WebDAV 等服务端场景使用）。
func (d *S3Driver) UploadFile(key string, content []byte) error {
	_, err := d.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(content),
	})
	if err != nil {
		return fmt.Errorf("上传对象失败: %w", err)
	}
	return nil
}

// 确保 S3Driver 实现 StorageDriver 接口
var _ StorageDriver = (*S3Driver)(nil)
