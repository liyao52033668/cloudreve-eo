package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DB      DBConfig
	JWT     JWTConfig
	S3      S3Config
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

type S3Config struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
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

	return &Config{
		DB: DBConfig{
			Driver: dbDriver,
			DSN:    dbDSN,
		},
		JWT: JWTConfig{
			Secret: os.Getenv("JWT_SECRET"),
		},
		S3: S3Config{
			Endpoint:  os.Getenv("S3_ENDPOINT"),
			Region:    os.Getenv("S3_REGION"),
			Bucket:    os.Getenv("S3_BUCKET"),
			AccessKey: os.Getenv("S3_ACCESS_KEY"),
			SecretKey: os.Getenv("S3_SECRET_KEY"),
		},
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
