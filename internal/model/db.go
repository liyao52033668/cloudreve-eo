package model

import (
	"fmt"
	"strings"

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
		// SQL 日志走 logx 单行 JSON（EdgeOne 控制台按关键字检索）
		Logger: gormLogger{},
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
		// secret_key 早期为 varchar(255)，Dropbox 等 OAuth token 超长，需升级为 text。
		// AutoMigrate 不会变更已存在列的类型，这里显式迁移。
		if err := migrateSecretKeyType(db); err != nil {
			return fmt.Errorf("迁移 secret_key 字段类型失败: %w", err)
		}
	}

	DB = db
	return nil
}

// migrateSecretKeyType 将 secret_key 列升级为 text（早期版本为 varchar(255)，
// Dropbox 等 OAuth access token 超长会写入失败）。仅 Postgres 需要显式变更；
// SQLite 不强制 varchar 长度可跳过。已是 text 时为幂等空操作。
func migrateSecretKeyType(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	cols, err := db.Migrator().ColumnTypes(&StoragePolicy{})
	if err != nil {
		return err
	}
	for _, c := range cols {
		if c.Name() != "secret_key" {
			continue
		}
		if strings.Contains(strings.ToLower(c.DatabaseTypeName()), "text") {
			return nil // 已是 text，无需变更
		}
		table := tablePrefix + "storage_policies"
		return db.Exec(fmt.Sprintf("ALTER TABLE %s ALTER COLUMN secret_key TYPE text", table)).Error
	}
	return nil
}

// SnapshotSQLite 用 VACUUM INTO 生成一致性快照文件（dst 必须不存在）。
func SnapshotSQLite(dst string) error {
	return DB.Exec("VACUUM INTO ?", dst).Error
}
