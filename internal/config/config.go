package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DB      DBConfig
	JWT     JWTConfig
	S3      S3Config   // 单套 S3 兼容配置（与 S3_POLICIES 二选一，兼容旧环境变量）
	S3List  []S3Config // 解析后的全部 S3 策略列表（至少包含一套时非空）
	EdgeOne EdgeOneConfig
	Server  ServerConfig
	Storage StorageConfig
}

type DBConfig struct {
	Driver string
	DSN    string
}

type JWTConfig struct {
	Secret string
}

// S3Config 描述一套 S3 兼容存储策略。
// Name 为策略标识，写入 files.storage_policy，上传时按此选择。
type S3Config struct {
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

type EdgeOneConfig struct {
	Bucket    string
	SecretID  string
	SecretKey string
}

type ServerConfig struct {
	Port string
}

type StorageConfig struct {
	Default      string
	DefaultQuota int64
}

func Load() (*Config, error) {
	quota := int64(1073741824)
	if q := os.Getenv("DEFAULT_QUOTA"); q != "" {
		v, err := strconv.ParseInt(q, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid DEFAULT_QUOTA: %w", err)
		}
		quota = v
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbDriver := os.Getenv("DB_DRIVER")
	if dbDriver == "" {
		dbDriver = "sqlite"
	}

	dbDSN := os.Getenv("DB_DSN")
	if dbDSN == "" {
		dbDSN = "cloudreve.db"
	}

	defaultStorage := os.Getenv("DEFAULT_STORAGE")
	if defaultStorage == "" {
		defaultStorage = "s3"
	}

	singleS3 := S3Config{
		Name:      "s3",
		Endpoint:  os.Getenv("S3_ENDPOINT"),
		Region:    os.Getenv("S3_REGION"),
		Bucket:    os.Getenv("S3_BUCKET"),
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
	}

	s3List, err := loadS3Policies(singleS3)
	if err != nil {
		return nil, err
	}

	// 多策略时若未显式设置 DEFAULT_STORAGE，默认用列表第一项
	if os.Getenv("DEFAULT_STORAGE") == "" && len(s3List) > 0 {
		defaultStorage = s3List[0].Name
	}

	return &Config{
		DB: DBConfig{
			Driver: dbDriver,
			DSN:    dbDSN,
		},
		JWT: JWTConfig{
			Secret: os.Getenv("JWT_SECRET"),
		},
		S3:     singleS3,
		S3List: s3List,
		EdgeOne: EdgeOneConfig{
			Bucket:    os.Getenv("EDGEONE_BUCKET"),
			SecretID:  os.Getenv("EDGEONE_SECRET_ID"),
			SecretKey: os.Getenv("EDGEONE_SECRET_KEY"),
		},
		Server: ServerConfig{
			Port: port,
		},
		Storage: StorageConfig{
			Default:      defaultStorage,
			DefaultQuota: quota,
		},
	}, nil
}

// loadS3Policies 解析 S3_POLICIES JSON 数组；未设置时回退到单套 S3_*（bucket 非空才纳入）。
//
// S3_POLICIES 示例：
//
//	[
//	  {"name":"minio","endpoint":"http://127.0.0.1:9001","region":"us-east-1","bucket":"a","access_key":"ak","secret_key":"sk"},
//	  {"name":"cos","endpoint":"https://cos.ap-guangzhou.myqcloud.com","region":"ap-guangzhou","bucket":"b","access_key":"ak","secret_key":"sk"}
//	]
func loadS3Policies(legacy S3Config) ([]S3Config, error) {
	raw := strings.TrimSpace(os.Getenv("S3_POLICIES"))
	if raw == "" {
		if legacy.Bucket != "" {
			return []S3Config{legacy}, nil
		}
		return nil, nil
	}

	var list []S3Config
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, fmt.Errorf("invalid S3_POLICIES: %w", err)
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("invalid S3_POLICIES: empty array")
	}

	seen := make(map[string]struct{}, len(list))
	for i := range list {
		name := strings.TrimSpace(list[i].Name)
		if name == "" {
			return nil, fmt.Errorf("invalid S3_POLICIES: item %d missing name", i)
		}
		if list[i].Bucket == "" {
			return nil, fmt.Errorf("invalid S3_POLICIES: policy %q missing bucket", name)
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("invalid S3_POLICIES: duplicate name %q", name)
		}
		seen[name] = struct{}{}
		list[i].Name = name
	}
	return list, nil
}
