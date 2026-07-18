package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

// 确保 S3Driver 实现 StorageDriver 接口
var _ StorageDriver = (*S3Driver)(nil)
