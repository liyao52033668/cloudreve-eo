package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// StoragePolicy 存储策略（S3 兼容），由管理员在前端配置，持久化到数据库。
type StoragePolicy struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;size:64;not null" json:"name"`
	Type      string    `gorm:"size:16;not null;default:s3" json:"type"` // 当前仅 s3
	Endpoint  string    `gorm:"size:512;not null" json:"endpoint"`
	Region    string    `gorm:"size:64" json:"region"`
	Bucket    string    `gorm:"size:255;not null" json:"bucket"`
	AccessKey string    `gorm:"size:255;not null" json:"access_key"`
	SecretKey string    `gorm:"size:255;not null" json:"secret_key"`
	IsDefault    bool      `gorm:"not null;default:false" json:"is_default"`
	// DefaultQuota 该策略下每个用户的默认配额（字节）；0 表示未配置/不可用。
	DefaultQuota int64     `gorm:"not null;default:0" json:"default_quota"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ListStoragePolicies 全部策略，默认排前。
func ListStoragePolicies() ([]StoragePolicy, error) {
	var list []StoragePolicy
	err := DB.Order("is_default DESC, name ASC").Find(&list).Error
	return list, err
}

// GetStoragePolicyByName 按名称查询。
func GetStoragePolicyByName(name string) (*StoragePolicy, error) {
	var p StoragePolicy
	if err := DB.Where("name = ?", name).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// GetStoragePolicyByID 按 ID 查询。
func GetStoragePolicyByID(id uint) (*StoragePolicy, error) {
	var p StoragePolicy
	if err := DB.First(&p, id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// GetDefaultStoragePolicy 默认策略；无默认时取第一条。
func GetDefaultStoragePolicy() (*StoragePolicy, error) {
	var p StoragePolicy
	err := DB.Where("is_default = ?", true).First(&p).Error
	if err == nil {
		return &p, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	err = DB.Order("id ASC").First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreateStoragePolicy 新建策略；若是首条或指定默认则设为默认。
func CreateStoragePolicy(p *StoragePolicy) error {
	if p.Type == "" {
		p.Type = "s3"
	}
	var count int64
	if err := DB.Model(&StoragePolicy{}).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		p.IsDefault = true
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		if p.IsDefault {
			if err := tx.Model(&StoragePolicy{}).Where("is_default = ?", true).Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Create(p).Error
	})
}

// UpdateStoragePolicy 更新策略（按 ID）。SecretKey 为空字符串时表示不修改。
func UpdateStoragePolicy(id uint, updates *StoragePolicy, updateSecret bool) error {
	existing, err := GetStoragePolicyByID(id)
	if err != nil {
		return err
	}

	existing.Name = updates.Name
	existing.Endpoint = updates.Endpoint
	existing.Region = updates.Region
	existing.Bucket = updates.Bucket
	existing.AccessKey = updates.AccessKey
	existing.DefaultQuota = updates.DefaultQuota
	if updateSecret && updates.SecretKey != "" {
		existing.SecretKey = updates.SecretKey
	}
	if updates.Type != "" {
		existing.Type = updates.Type
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		if updates.IsDefault {
			if err := tx.Model(&StoragePolicy{}).Where("is_default = ?", true).Update("is_default", false).Error; err != nil {
				return err
			}
			existing.IsDefault = true
		}
		return tx.Save(existing).Error
	})
}

// DeleteStoragePolicy 删除策略；若删的是默认则将最早一条设为默认。
func DeleteStoragePolicy(id uint) error {
	existing, err := GetStoragePolicyByID(id)
	if err != nil {
		return err
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&StoragePolicy{}, id).Error; err != nil {
			return err
		}
		if !existing.IsDefault {
			return nil
		}
		var next StoragePolicy
		if err := tx.Order("id ASC").First(&next).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil // 已无策略
			}
			return err
		}
		return tx.Model(&next).Update("is_default", true).Error
	})
}

// SetDefaultStoragePolicy 将指定策略设为默认。
func SetDefaultStoragePolicy(id uint) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var p StoragePolicy
		if err := tx.First(&p, id).Error; err != nil {
			return err
		}
		if err := tx.Model(&StoragePolicy{}).Where("is_default = ?", true).Update("is_default", false).Error; err != nil {
			return err
		}
		return tx.Model(&p).Update("is_default", true).Error
	})
}
