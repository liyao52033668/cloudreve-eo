package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DB      DBConfig
	Server  ServerConfig
	Persist PersistConfig
	Log     LogConfig
}

// LogConfig 结构化日志配置。
type LogConfig struct {
	// Level 日志级别：debug / info / warn / error，默认 info。
	Level string
}

type DBConfig struct {
	Driver string
	DSN    string
}

type ServerConfig struct {
	Port string
}

// PersistConfig SQLite 数据库文件持久化配置。
// EdgeOne 云函数等无状态环境的本地文件系统不持久，
// 可选择将 SQLite 文件同步到对象存储、GitHub 仓库或 EdgeOne Blob。
type PersistConfig struct {
	// Backend: local（默认，仅本地文件）| s3 | github | edgeone-blob
	Backend  string
	Interval time.Duration
	S3       PersistS3Config
	GitHub   PersistGitHubConfig
	EdgeOne  PersistEdgeOneConfig
	// EdgeOneBaseURL 非空时启动即恢复；为空（edgeone-blob 未显式配置地址）
	// 时恢复推迟到首个请求，从请求 Host 头自动推导。
	EdgeOneBaseURL string
}

// LazyRestore 持久化恢复无法在启动时完成（如 edgeone-blob 未显式配置地址、
// 需从首个请求的 Host 头推导站点域名），必须先放行请求、在请求内完成初始化。
func (c *Config) LazyRestore() bool {
	return c.Persist.Backend == "edgeone-blob" && c.Persist.EdgeOne.BaseURL == ""
}

type PersistS3Config struct {
	Endpoint       string
	Region         string
	Bucket         string
	AccessKey      string
	SecretKey      string
	Key            string
	ForcePathStyle bool
}

type PersistGitHubConfig struct {
	Token  string
	Repo   string // owner/repo
	Branch string
	Path   string
}

// PersistEdgeOneConfig 通过 Node 云函数 db-blob.js 存取 EdgeOne Blob。
// Blob SDK 仅有 Node 版本，Go 主程序以 HTTP 调用同站的代理函数。
type PersistEdgeOneConfig struct {
	// BaseURL 可选。留空时由首个请求的 Host 头自动推导（Go 与 Node
	// 函数共用同一对外域名，边缘按路径分发）。
	BaseURL string
	Secret  string // 与 Node 函数的 DB_PERSIST_EDGEONE_SECRET 一致的共享密钥
}

// Load 仅加载基础设施环境变量。
// JWT / 存储策略 / 默认配额等业务配置一律由前端写入数据库，不从环境变量引导。
// 数据库持久化后端必须先于数据库可用，因此同样走环境变量。
func Load() (*Config, error) {
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

	persist, err := loadPersist(dbDriver)
	if err != nil {
		return nil, err
	}

	return &Config{
		DB: DBConfig{
			Driver: dbDriver,
			DSN:    dbDSN,
		},
		Server: ServerConfig{
			Port: port,
		},
		Persist: persist,
		Log: LogConfig{
			Level: os.Getenv("LOG_LEVEL"),
		},
	}, nil
}

func loadPersist(dbDriver string) (PersistConfig, error) {
	backend := os.Getenv("DB_PERSIST")
	if backend == "" {
		backend = "local"
	}
	cfg := PersistConfig{Backend: backend, Interval: 60 * time.Second}
	if v := os.Getenv("DB_PERSIST_INTERVAL"); v != "" {
		sec, err := strconv.Atoi(v)
		if err != nil || sec < 5 {
			return cfg, fmt.Errorf("DB_PERSIST_INTERVAL 必须是不小于 5 的秒数: %q", v)
		}
		cfg.Interval = time.Duration(sec) * time.Second
	}

	switch backend {
	case "local":
		return cfg, nil
	case "s3":
		cfg.S3 = PersistS3Config{
			Endpoint:       os.Getenv("DB_PERSIST_S3_ENDPOINT"),
			Region:         getenvDefault("DB_PERSIST_S3_REGION", "auto"),
			Bucket:         os.Getenv("DB_PERSIST_S3_BUCKET"),
			AccessKey:      os.Getenv("DB_PERSIST_S3_ACCESS_KEY"),
			SecretKey:      os.Getenv("DB_PERSIST_S3_SECRET_KEY"),
			Key:            getenvDefault("DB_PERSIST_S3_KEY", "cloudreve.db"),
			ForcePathStyle: os.Getenv("DB_PERSIST_S3_PATH_STYLE") == "true",
		}
		if cfg.S3.Bucket == "" || cfg.S3.AccessKey == "" || cfg.S3.SecretKey == "" {
			return cfg, fmt.Errorf("DB_PERSIST=s3 需要设置 DB_PERSIST_S3_BUCKET / DB_PERSIST_S3_ACCESS_KEY / DB_PERSIST_S3_SECRET_KEY")
		}
	case "github":
		cfg.GitHub = PersistGitHubConfig{
			Token:  os.Getenv("DB_PERSIST_GITHUB_TOKEN"),
			Repo:   os.Getenv("DB_PERSIST_GITHUB_REPO"),
			Branch: getenvDefault("DB_PERSIST_GITHUB_BRANCH", "main"),
			Path:   getenvDefault("DB_PERSIST_GITHUB_PATH", "cloudreve.db"),
		}
		if cfg.GitHub.Token == "" || cfg.GitHub.Repo == "" {
			return cfg, fmt.Errorf("DB_PERSIST=github 需要设置 DB_PERSIST_GITHUB_TOKEN / DB_PERSIST_GITHUB_REPO（owner/repo）")
		}
	case "edgeone-blob":
		cfg.EdgeOne = PersistEdgeOneConfig{
			BaseURL: strings.TrimRight(os.Getenv("DB_PERSIST_EDGEONE_BASE_URL"), "/"),
			Secret:  os.Getenv("DB_PERSIST_EDGEONE_SECRET"),
		}
		if cfg.EdgeOne.Secret == "" {
			return cfg, fmt.Errorf("DB_PERSIST=edgeone-blob 需要设置 DB_PERSIST_EDGEONE_SECRET")
		}
		cfg.EdgeOneBaseURL = cfg.EdgeOne.BaseURL
	default:
		return cfg, fmt.Errorf("不支持的 DB_PERSIST 后端: %s（可选 local / s3 / github / edgeone-blob）", backend)
	}

	if dbDriver != "sqlite" {
		return cfg, fmt.Errorf("DB_PERSIST=%s 仅在 DB_DRIVER=sqlite 时有效", backend)
	}
	return cfg, nil
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
