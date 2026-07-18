package model

import (
	"fmt"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var DB *gorm.DB

// tablePrefix 全部业务表统一前缀，避免与同库其它应用冲突。
const tablePrefix = "cloudreve_"

func InitDB(cfg *config.Config) error {
	var dialector gorm.Dialector

	switch cfg.DB.Driver {
	case "sqlite":
		dialector = sqlite.Open(cfg.DB.DSN)
	case "postgres":
		dialector = postgres.Open(cfg.DB.DSN)
	default:
		return fmt.Errorf("不支持的数据库驱动: %s", cfg.DB.Driver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			TablePrefix: tablePrefix,
		},
	})
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}

	if err := db.AutoMigrate(&User{}, &File{}, &Share{}, &Setting{}, &StoragePolicy{}); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}

	DB = db
	return nil
}
