package storage

import (
	"fmt"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
)

// StoragePolicyManager 管理多个存储策略及其对应驱动。
// Task 3 仅初始化 S3 驱动；EdgeOne 驱动在 Task 4 实现。
type StoragePolicyManager struct {
	defaultDriver StorageDriver
	defaultPolicy string
	drivers       map[string]StorageDriver
}

// NewStoragePolicyManager 根据配置初始化可用的存储驱动。
func NewStoragePolicyManager(cfg *config.Config) (*StoragePolicyManager, error) {
	mgr := &StoragePolicyManager{
		drivers:       make(map[string]StorageDriver),
		defaultPolicy: cfg.Storage.Default,
	}

	// 初始化 S3 驱动：默认策略为 s3，或已配置 Bucket 时注册
	if cfg.Storage.Default == "s3" || cfg.S3.Bucket != "" {
		driver, err := NewS3Driver(
			cfg.S3.Endpoint,
			cfg.S3.Region,
			cfg.S3.Bucket,
			cfg.S3.AccessKey,
			cfg.S3.SecretKey,
		)
		if err != nil {
			return nil, fmt.Errorf("初始化 S3 驱动失败: %w", err)
		}
		mgr.drivers["s3"] = driver
		if cfg.Storage.Default == "s3" {
			mgr.defaultDriver = driver
		}
	}

	// EdgeOne 驱动将在 Task 4 实现，此处暂不初始化

	if mgr.defaultDriver == nil {
		return nil, fmt.Errorf("默认存储策略 %s 未配置", cfg.Storage.Default)
	}

	return mgr, nil
}

func (m *StoragePolicyManager) DefaultDriver() StorageDriver {
	return m.defaultDriver
}

func (m *StoragePolicyManager) DefaultPolicy() string {
	return m.defaultPolicy
}

func (m *StoragePolicyManager) GetDriver(policy string) (StorageDriver, error) {
	driver, ok := m.drivers[policy]
	if !ok {
		return nil, fmt.Errorf("存储策略 %s 不存在", policy)
	}
	return driver, nil
}

// NewTestStoragePolicyManager 使用预置驱动构造管理器，供单测注入 mock，避免访问真实对象存储。
func NewTestStoragePolicyManager(policy string, driver StorageDriver) *StoragePolicyManager {
	return &StoragePolicyManager{
		defaultDriver: driver,
		defaultPolicy: policy,
		drivers:       map[string]StorageDriver{policy: driver},
	}
}
