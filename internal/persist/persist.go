// Package persist 将 SQLite 数据库文件同步到远端（对象存储 / GitHub），
// 解决云函数等无状态环境本地磁盘不持久的问题。
package persist

import (
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
)

// Backend 远端存取接口。
type Backend interface {
	Name() string
	// Download 拉取远端数据库文件内容；远端不存在时返回 (nil, false, nil)。
	Download() (data []byte, found bool, err error)
	Upload(data []byte) error
}

// SnapshotFunc 生成数据库一致性快照到 dst 文件（如 SQLite 的 VACUUM INTO）。
type SnapshotFunc func(dst string) error

type Syncer struct {
	backend  Backend
	dbPath   string
	interval time.Duration
	snapshot SnapshotFunc
	lastHash [32]byte
}

// New 按配置创建 Syncer；Backend 为 local 时返回 nil（无需同步）。
func New(cfg *config.Config) (*Syncer, error) {
	var backend Backend
	var err error
	switch cfg.Persist.Backend {
	case "local":
		return nil, nil
	case "s3":
		backend, err = newS3Backend(cfg.Persist.S3)
	case "github":
		backend = newGitHubBackend(cfg.Persist.GitHub)
	default:
		return nil, fmt.Errorf("不支持的持久化后端: %s", cfg.Persist.Backend)
	}
	if err != nil {
		return nil, err
	}
	return &Syncer{
		backend:  backend,
		dbPath:   cfg.DB.DSN,
		interval: cfg.Persist.Interval,
	}, nil
}

// Restore 启动时从远端恢复数据库文件。
// 本地已存在时跳过（本地开发或热重启场景优先本地文件）；远端不存在时全新启动。
func (s *Syncer) Restore() error {
	if _, err := os.Stat(s.dbPath); err == nil {
		log.Printf("[persist] 本地已存在 %s，跳过远端恢复", s.dbPath)
		return nil
	}
	data, found, err := s.backend.Download()
	if err != nil {
		return fmt.Errorf("从 %s 恢复数据库失败: %w", s.backend.Name(), err)
	}
	if !found {
		log.Printf("[persist] %s 上无数据库快照，全新启动", s.backend.Name())
		return nil
	}
	if err := os.WriteFile(s.dbPath, data, 0o600); err != nil {
		return fmt.Errorf("写入数据库文件失败: %w", err)
	}
	s.lastHash = sha256.Sum256(data)
	log.Printf("[persist] 已从 %s 恢复数据库（%d 字节）", s.backend.Name(), len(data))
	return nil
}

// Start 启动后台定时同步。snapshot 用于生成一致性快照（数据库初始化后传入）。
func (s *Syncer) Start(snapshot SnapshotFunc) {
	s.snapshot = snapshot
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for range ticker.C {
			if err := s.SyncNow(); err != nil {
				log.Printf("[persist] 同步失败: %v", err)
			}
		}
	}()
	log.Printf("[persist] 已启用 %s 持久化，每 %s 同步一次", s.backend.Name(), s.interval)
}

// SyncNow 立即做一次快照并在内容变化时上传。
func (s *Syncer) SyncNow() error {
	tmp := s.dbPath + ".snapshot"
	_ = os.Remove(tmp) // VACUUM INTO 要求目标不存在
	if err := s.snapshot(tmp); err != nil {
		return fmt.Errorf("生成快照失败: %w", err)
	}
	defer os.Remove(tmp)
	data, err := os.ReadFile(tmp)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(data)
	if hash == s.lastHash {
		return nil
	}
	if err := s.backend.Upload(data); err != nil {
		return fmt.Errorf("上传到 %s 失败: %w", s.backend.Name(), err)
	}
	s.lastHash = hash
	log.Printf("[persist] 已同步数据库到 %s（%d 字节）", s.backend.Name(), len(data))
	return nil
}
