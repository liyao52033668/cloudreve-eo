package model

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// UserGroup 用户组：绑定存储策略与本组用户的最大容量。
type UserGroup struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Name string `gorm:"uniqueIndex;size:64;not null" json:"name"`
	// StoragePolicies 多选策略名称，数据库以逗号分隔文本存储。
	StoragePolicies string `gorm:"type:text" json:"-"`
	// MaxStorage 组内每个用户的最大容量（字节）；0 表示沿用所有策略的默认配额总和。
	MaxStorage int64     `gorm:"not null;default:0" json:"max_storage"`
	IsDefault  bool      `gorm:"not null;default:false" json:"is_default"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// SplitStoragePolicies 把逗号分隔字符串拆成策略名列表。
func SplitStoragePolicies(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{})
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// JoinStoragePolicies 把策略名列表编码成逗号分隔字符串。
func JoinStoragePolicies(policies []string) string {
	cleaned := SplitStoragePolicies(strings.Join(policies, ","))
	return strings.Join(cleaned, ",")
}

// PolicyNames 返回用户组绑定的策略名列表。
func (g *UserGroup) PolicyNames() []string {
	if g == nil {
		return []string{}
	}
	return SplitStoragePolicies(g.StoragePolicies)
}

// EnsureDefaultGroup 保证存在默认用户组（新注册用户自动归入），返回组 ID。
func EnsureDefaultGroup() (uint, error) {
	var g UserGroup
	err := DB.Where("is_default = ?", true).First(&g).Error
	if err == nil {
		return g.ID, nil
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		return tx.Create(&UserGroup{
			Name:      "默认用户组",
			IsDefault: true,
		}).Error
	}); err != nil {
		return 0, err
	}
	err = DB.Where("is_default = ?", true).First(&g).Error
	if err != nil {
		return 0, err
	}
	return g.ID, nil
}

// GetDefaultGroup 返回默认用户组；无默认时取最早一条，均不存在则报错。
func GetDefaultGroup() (*UserGroup, error) {
	var g UserGroup
	err := DB.Where("is_default = ?", true).First(&g).Error
	if err == nil {
		return &g, nil
	}
	err = DB.Order("id ASC").First(&g).Error
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// GetUserGroupByID 按 ID 查询用户组。
func GetUserGroupByID(id uint) (*UserGroup, error) {
	var g UserGroup
	if err := DB.First(&g, id).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

// UserGroupView 列表/详情对外视图：storage_policies 以数组返回给前端。
type UserGroupView struct {
	ID              uint      `json:"id"`
	Name            string    `json:"name"`
	StoragePolicies []string  `json:"storage_policies"`
	MaxStorage      int64     `json:"max_storage"`
	IsDefault       bool      `json:"is_default"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	UserCount       int64     `json:"user_count"`
	StorageUsed     int64     `json:"storage_used"`
}

// ToUserGroupView 把模型转成前端可用视图。
func ToUserGroupView(g UserGroup, userCount, storageUsed int64) UserGroupView {
	return UserGroupView{
		ID:              g.ID,
		Name:            g.Name,
		StoragePolicies: g.PolicyNames(),
		MaxStorage:      g.MaxStorage,
		IsDefault:       g.IsDefault,
		CreatedAt:       g.CreatedAt,
		UpdatedAt:       g.UpdatedAt,
		UserCount:       userCount,
		StorageUsed:     storageUsed,
	}
}

func ListUserGroups() ([]UserGroupView, error) {
	var groups []UserGroup
	if err := DB.Order("is_default DESC, id ASC").Find(&groups).Error; err != nil {
		return nil, err
	}

	views := make([]UserGroupView, 0, len(groups))
	for _, g := range groups {
		var cnt int64
		DB.Model(&User{}).Where("group_id = ?", g.ID).Count(&cnt)
		var used int64
		DB.Model(&User{}).Where("group_id = ?", g.ID).
			Select("COALESCE(SUM(storage_used), 0)").Scan(&used)
		views = append(views, ToUserGroupView(g, cnt, used))
	}
	return views, nil
}

// CreateUserGroup 新建用户组；首条自动设为默认。
func CreateUserGroup(g *UserGroup) error {
	var count int64
	if err := DB.Model(&UserGroup{}).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		g.IsDefault = true
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if g.IsDefault {
			if err := tx.Model(&UserGroup{}).Where("is_default = ?", true).Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Create(g).Error
	})
}

// UpdateUserGroup 更新用户组（按 ID）。
func UpdateUserGroup(id uint, updates *UserGroup) error {
	existing, err := GetUserGroupByID(id)
	if err != nil {
		return err
	}
	existing.Name = updates.Name
	existing.StoragePolicies = updates.StoragePolicies
	existing.MaxStorage = updates.MaxStorage

	return DB.Transaction(func(tx *gorm.DB) error {
		if updates.IsDefault {
			if err := tx.Model(&UserGroup{}).Where("is_default = ?", true).Update("is_default", false).Error; err != nil {
				return err
			}
			existing.IsDefault = true
		}
		return tx.Save(existing).Error
	})
}

// DeleteUserGroup 删除用户组；组内用户迁移至默认组，若删的是默认则另选一默认。
func DeleteUserGroup(id uint) error {
	existing, err := GetUserGroupByID(id)
	if err != nil {
		return err
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&UserGroup{}, id).Error; err != nil {
			return err
		}

		// 选出接管用户的组：优先剩余默认组，否则取最早一条
		var fallback UserGroup
		if err := tx.Where("is_default = ?", true).First(&fallback).Error; err != nil {
			if err := tx.Order("id ASC").First(&fallback).Error; err != nil {
				// 已无任何用户组，组内用户归零（运行时兜底用系统默认策略）
				return tx.Model(&User{}).Where("group_id = ?", id).Update("group_id", 0).Error
			}
		}
		if err := tx.Model(&User{}).Where("group_id = ?", id).Update("group_id", fallback.ID).Error; err != nil {
			return err
		}
		if existing.IsDefault && !fallback.IsDefault {
			return tx.Model(&fallback).Update("is_default", true).Error
		}
		return nil
	})
}

// SetDefaultUserGroup 将指定用户组设为默认。
func SetDefaultUserGroup(id uint) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var g UserGroup
		if err := tx.First(&g, id).Error; err != nil {
			return err
		}
		if err := tx.Model(&UserGroup{}).Where("is_default = ?", true).Update("is_default", false).Error; err != nil {
			return err
		}
		return tx.Model(&g).Update("is_default", true).Error
	})
}
