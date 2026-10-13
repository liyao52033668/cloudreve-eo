package model

import (
	"fmt"
	"strings"
	"time"

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

	// 配置连接池：Supabase session pooler 限制 15 个并发连接，
	// 必须显式设置上限避免高并发时超限（EMAXCONNSESSION）。
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("获取底层数据库连接失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(10)              // 最大打开连接数，留 5 个余量
	sqlDB.SetMaxIdleConns(5)               // 最大空闲连接数
	sqlDB.SetConnMaxLifetime(30 * time.Minute) // 连接最大存活时间，避免长时间占用

	// PostgreSQL 迁移修复：SQLite 迁移过来的数据中，时间戳列可能存有空字符串 ""，
	// AutoMigrate 尝试 ALTER COLUMN TYPE timestamptz 时 PostgreSQL 无法将 "" 转为时间戳。
	// 在 AutoMigrate 之前将这些空字符串清理为 NULL。
	if db.Dialector.Name() == "postgres" {
		if err := fixEmptyTimestamps(db); err != nil {
			return fmt.Errorf("清理时间戳脏数据失败: %w", err)
		}
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
		if !db.Migrator().HasColumn(&StoragePolicy{}, "webdav_direct") {
			if err := db.Migrator().AddColumn(&StoragePolicy{}, "WebDAVDirect"); err != nil {
				return fmt.Errorf("添加 webdav_direct 字段失败: %w", err)
			}
		}
		if !db.Migrator().HasColumn(&StoragePolicy{}, "cloudreve_api_enabled") {
			if err := db.Migrator().AddColumn(&StoragePolicy{}, "CloudreveAPIEnabled"); err != nil {
				return fmt.Errorf("添加 cloudreve_api_enabled 字段失败: %w", err)
			}
		}
		if !db.Migrator().HasColumn(&StoragePolicy{}, "cloudreve_api_url") {
			if err := db.Migrator().AddColumn(&StoragePolicy{}, "CloudreveAPIURL"); err != nil {
				return fmt.Errorf("添加 cloudreve_api_url 字段失败: %w", err)
			}
		}
		if !db.Migrator().HasColumn(&StoragePolicy{}, "cloudreve_user") {
			if err := db.Migrator().AddColumn(&StoragePolicy{}, "CloudreveUser"); err != nil {
				return fmt.Errorf("添加 cloudreve_user 字段失败: %w", err)
			}
		}
		if !db.Migrator().HasColumn(&StoragePolicy{}, "cloudreve_pass") {
			if err := db.Migrator().AddColumn(&StoragePolicy{}, "CloudrevePass"); err != nil {
				return fmt.Errorf("添加 cloudreve_pass 字段失败: %w", err)
			}
		}
		// secret_key 早期为 varchar(255)，Dropbox 等 OAuth token 超长，需升级为 text。
		// AutoMigrate 不会变更已存在列的类型，这里显式迁移。
		if err := migrateSecretKeyType(db); err != nil {
			return fmt.Errorf("迁移 secret_key 字段类型失败: %w", err)
		}
	}

	// Share 表新增 file_ids（多文件分享）：旧库补列，新分享写入该字段
	if db.Migrator().HasTable(&Share{}) && !db.Migrator().HasColumn(&Share{}, "file_ids") {
		if err := db.Migrator().AddColumn(&Share{}, "file_ids"); err != nil {
			return fmt.Errorf("添加 shares.file_ids 字段失败: %w", err)
		}
	}

	// PostgreSQL 序列修复：从 SQLite 迁移过来的表，自增主键的序列可能未正确设置
	if db.Dialector.Name() == "postgres" {
		if err := fixPostgresSequences(db); err != nil {
			return fmt.Errorf("修复数据库序列失败: %w", err)
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

// fixEmptyTimestamps 清理 PostgreSQL 中时间戳列的空字符串为 NULL。
// SQLite 对列类型不做强制约束，早期版本可能将空字符串写入时间戳字段；
// PostgreSQL 的 timestamptz 不接受空字符串，AutoMigrate 变更列类型时会报错。
// 此函数在 AutoMigrate 之前调用，仅处理已存在的表和列，幂等安全。
func fixEmptyTimestamps(db *gorm.DB) error {
	// 需要清理的表和时间戳列（使用结构体让 GORM 自动解析表名）
	type col struct {
		model  interface{}
		column string
	}
	cols := []col{
		{model: &Share{}, column: "expire_at"},
	}
	for _, c := range cols {
		if !db.Migrator().HasTable(c.model) {
			continue
		}
		if !db.Migrator().HasColumn(c.model, c.column) {
			continue
		}
		// 检查列的实际数据库类型：如果已经是时间戳类型则无需清理
		colTypes, err := db.Migrator().ColumnTypes(c.model)
		if err != nil {
			continue
		}
		isText := false
		for _, ct := range colTypes {
			if ct.Name() == c.column {
				dbType := strings.ToLower(ct.DatabaseTypeName())
				// 文本类型（varchar/text）才可能存有空字符串，需要清理
				if strings.Contains(dbType, "char") || strings.Contains(dbType, "text") {
					isText = true
				}
				break
			}
		}
		if !isText {
			continue // 已是 timestamp/timestamptz 等类型，跳过
		}
		// 解析出实际表名（含前缀）
		stmt := &gorm.Statement{DB: db}
		_ = stmt.Parse(c.model)
		table := stmt.Schema.Table
		colName := c.column
		// 将空字符串和仅含空白的值设为 NULL
		if err := db.Exec(
			fmt.Sprintf("UPDATE %s SET %s = NULL WHERE TRIM(%s) = ''", table, colName, colName),
		).Error; err != nil {
			return err
		}
	}
	return nil
}

// fixPostgresSequences 修复 PostgreSQL 自增主键的序列。
// 从 SQLite 迁移过来的表，主键列可能缺少 DEFAULT nextval('sequence') 设置，
// 导致插入新记录时 ID 为 NULL，违反非空约束。
// 此函数为每个表的 id 列确保序列存在并正确关联，幂等安全。
func fixPostgresSequences(db *gorm.DB) error {
	// 需要修复序列的模型列表
	models := []interface{}{&User{}, &UserGroup{}, &File{}, &Share{}, &Setting{}, &StoragePolicy{}, &UploadSession{}}

	for _, model := range models {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			continue
		}
		table := stmt.Schema.Table
		pkCol := "id"

		// 检查表是否存在
		if !db.Migrator().HasTable(table) {
			continue
		}

		// 检查 id 列是否有 DEFAULT 值（序列）
		var hasDefault bool
		err := db.Raw(`
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = ? AND column_name = 'id'
				AND column_default IS NOT NULL
			)`, table).Scan(&hasDefault).Error
		if err != nil {
			continue
		}

		if !hasDefault {
			// 创建序列（如果不存在）
			seqName := table + "_id_seq"
			if err := db.Exec(fmt.Sprintf(`CREATE SEQUENCE IF NOT EXISTS "%s"`, seqName)).Error; err != nil {
				return fmt.Errorf("创建序列 %s 失败: %w", seqName, err)
			}

			// 设置列的 DEFAULT 为 nextval
			if err := db.Exec(fmt.Sprintf(`
				ALTER TABLE "%s" ALTER COLUMN "%s" SET DEFAULT nextval('%s')`,
				table, pkCol, seqName)).Error; err != nil {
				return fmt.Errorf("设置 %s.%s 默认值失败: %w", table, pkCol, err)
			}

			// 关联序列到列（这样删除列时序列也会删除）
			if err := db.Exec(fmt.Sprintf(`
				ALTER SEQUENCE "%s" OWNED BY "%s"."%s"`,
				seqName, table, pkCol)).Error; err != nil {
				// 这个失败不致命，继续
				continue
			}
		}

		// 将序列的当前值设置为表中 max(id)，确保新插入的 ID 不会冲突
		var maxID int64
		if err := db.Raw(fmt.Sprintf(`SELECT COALESCE(MAX(id), 0) FROM "%s"`, table)).Scan(&maxID).Error; err != nil {
			continue
		}
		if maxID > 0 {
			seqName := table + "_id_seq"
			if err := db.Exec(fmt.Sprintf(`SELECT setval('%s', %d)`, seqName, maxID)).Error; err != nil {
				return fmt.Errorf("设置序列 %s 值失败: %w", seqName, err)
			}
		}
	}
	return nil
}

// SnapshotSQLite 用 VACUUM INTO 生成一致性快照文件（dst 必须不存在）。
func SnapshotSQLite(dst string) error {
	return DB.Exec("VACUUM INTO ?", dst).Error
}
