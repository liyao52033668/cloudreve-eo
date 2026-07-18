package config

import (
	"os"
)

type Config struct {
	DB     DBConfig
	Server ServerConfig
}

type DBConfig struct {
	Driver string
	DSN    string
}

type ServerConfig struct {
	Port string
}

// Load 仅加载基础设施环境变量。
// JWT / 存储策略 / 默认配额等业务配置一律由前端写入数据库，不从环境变量引导。
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

	return &Config{
		DB: DBConfig{
			Driver: dbDriver,
			DSN:    dbDSN,
		},
		Server: ServerConfig{
			Port: port,
		},
	}, nil
}
