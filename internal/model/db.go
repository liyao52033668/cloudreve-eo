package model

import (
	"fmt"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
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
		// glebarez/sqlite 基于 modernc.org/sqlite，纯 Go、无需 CGO。
		// EdgeOne / 交叉编译常为 CGO_ENABLED=0，不能使用 mattn/go-sqlite3。
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

	if err := db.AutoMigrate(&User{}, &UserGroup{}, &File{}, &Share{}, &Setting{}, &StoragePolicy{}, &UploadSession{}); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}

	// 手动添加 StoragePolicy 表的新增字段（如果不存在）
	// GORM AutoMigrate 在表已存在时可能不会添加新字段
	if db.Migrator().HasTable(&StoragePolicy{}) {
		if !db.Migrator().HasColumn(&StoragePolicy{}, "branch") {
			if err := db.Migrator().AddColumn(&StoragePolicy{}, "branch"); err != nil {
				return fmt.Errorf("添加 branch 字段失败: %w", err)
			}
		}
		if !db.Migrator().HasColumn(&StoragePolicy{}, "oauth_token") {
			if err := db.Migrator().AddColumn(&StoragePolicy{}, "OAuthToken"); err != nil {
				return fmt.Errorf("添加 oauth_token 字段失败: %w", err)
			}
		}
	}

	DB = db
	return nil
}

// SnapshotSQLite 用 VACUUM INTO 生成一致性快照文件（dst 必须不存在）。
func SnapshotSQLite(dst string) error {
	return DB.Exec("VACUUM INTO ?", dst).Error
}
