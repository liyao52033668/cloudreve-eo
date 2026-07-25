package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Driver 使用 AWS SDK v2 实现 S3 兼容存储驱动（含 MinIO 等）。
type S3Driver struct {
	client *s3.Client
	bucket string
}

// NewS3Driver 创建 S3 兼容存储驱动。
// endpoint 非空时使用自定义端点（MinIO/COS 等）。
// forcePathStyle 为 true 时使用 path-style（http://endpoint/bucket/key），
// false 时使用 virtual-hosted（http://bucket.endpoint/key）；MinIO 与部分私有 S3 通常需开启。
func NewS3Driver(endpoint, region, bucket, accessKey, secretKey string, forcePathStyle bool) (*S3Driver, error) {
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

	return &S3Driver{client: client, bucket: bucket}, nil
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

func (d *S3Driver) GenerateDownloadURL(key string, expire time.Duration) (string, error) {
	presigner := s3.NewPresignClient(d.client)
	result, err := presigner.PresignGetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expire))
	if err != nil {
		return "", fmt.Errorf("生成下载 URL 失败: %w", err)
	}
	return result.URL, nil
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

func (d *S3Driver) GetSize(key string) (int64, error) {
	result, err := d.client.HeadObject(context.Background(), &s3.HeadObjectInput{
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
		return fmt.Errorf("设置存储桶 CORS 失败: %w", err)
	}
	return nil
}

// 确保 S3Driver 实现 StorageDriver 接口
var _ StorageDriver = (*S3Driver)(nil)
