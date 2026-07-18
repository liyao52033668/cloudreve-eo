package storage

import (
	"fmt"
	"sort"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
)

// PolicyInfo 对外暴露的存储策略信息（不含密钥）。
type PolicyInfo struct {
	Name      string `json:"name"`
	Type      string `json:"type"` // 当前固定 "s3"
	Bucket    string `json:"bucket,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
	IsDefault bool   `json:"is_default"`
}

// StoragePolicyManager 管理多个存储策略及其对应驱动。
type StoragePolicyManager struct {
	defaultDriver StorageDriver
	defaultPolicy string
	drivers       map[string]StorageDriver
	infos         map[string]PolicyInfo
}

// NewStoragePolicyManager 根据配置初始化可用的存储驱动。
// 仅支持 S3 兼容策略；可同时注册多套（来自 S3_POLICIES 或单套 S3_*）。
func NewStoragePolicyManager(cfg *config.Config) (*StoragePolicyManager, error) {
	mgr := &StoragePolicyManager{
		drivers:       make(map[string]StorageDriver),
		infos:         make(map[string]PolicyInfo),
		defaultPolicy: cfg.Storage.Default,
	}

	policies := cfg.S3List
	if len(policies) == 0 && cfg.S3.Bucket != "" {
		// 兼容仅填了 S3 字段、未走 Load 解析的测试构造
		p := cfg.S3
		if p.Name == "" {
			p.Name = "s3"
		}
		policies = []config.S3Config{p}
	}

	for _, p := range policies {
		name := p.Name
		if name == "" {
			name = "s3"
		}
		driver, err := NewS3Driver(p.Endpoint, p.Region, p.Bucket, p.AccessKey, p.SecretKey)
		if err != nil {
			return nil, fmt.Errorf("初始化 S3 策略 %q 失败: %w", name, err)
		}
		mgr.drivers[name] = driver
		mgr.infos[name] = PolicyInfo{
			Name:     name,
			Type:     "s3",
			Bucket:   p.Bucket,
			Endpoint: p.Endpoint,
		}
	}

	if len(mgr.drivers) == 0 {
		return nil, fmt.Errorf("未配置任何 S3 存储策略（请设置 S3_POLICIES 或 S3_BUCKET 等）")
	}

	// 默认策略：优先 DEFAULT_STORAGE；否则取第一个已注册策略
	if mgr.defaultPolicy == "" {
		mgr.defaultPolicy = firstPolicyName(mgr.drivers)
	}
	driver, ok := mgr.drivers[mgr.defaultPolicy]
	if !ok {
		return nil, fmt.Errorf("默认存储策略 %q 未配置", mgr.defaultPolicy)
	}
	mgr.defaultDriver = driver

	for name, info := range mgr.infos {
		info.IsDefault = name == mgr.defaultPolicy
		mgr.infos[name] = info
	}

	return mgr, nil
}

func firstPolicyName(drivers map[string]StorageDriver) string {
	names := make([]string, 0, len(drivers))
	for n := range drivers {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func (m *StoragePolicyManager) DefaultDriver() StorageDriver {
	return m.defaultDriver
}

func (m *StoragePolicyManager) DefaultPolicy() string {
	return m.defaultPolicy
}

func (m *StoragePolicyManager) GetDriver(policy string) (StorageDriver, error) {
	if policy == "" {
		policy = m.defaultPolicy
	}
	driver, ok := m.drivers[policy]
	if !ok {
		return nil, fmt.Errorf("存储策略 %s 不存在", policy)
	}
	return driver, nil
}

// ResolvePolicy 校验策略名；空字符串返回默认策略。
func (m *StoragePolicyManager) ResolvePolicy(policy string) (string, error) {
	if policy == "" {
		return m.defaultPolicy, nil
	}
	if _, ok := m.drivers[policy]; !ok {
		return "", fmt.Errorf("存储策略 %s 不存在", policy)
	}
	return policy, nil
}

// ListPolicies 返回已配置策略列表（默认策略排前，其余按名称排序）。
func (m *StoragePolicyManager) ListPolicies() []PolicyInfo {
	list := make([]PolicyInfo, 0, len(m.infos))
	for _, info := range m.infos {
		list = append(list, info)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].IsDefault != list[j].IsDefault {
			return list[i].IsDefault
		}
		return list[i].Name < list[j].Name
	})
	return list
}

// NewTestStoragePolicyManager 使用预置驱动构造管理器，供单测注入 mock。
func NewTestStoragePolicyManager(policy string, driver StorageDriver) *StoragePolicyManager {
	return &StoragePolicyManager{
		defaultDriver: driver,
		defaultPolicy: policy,
		drivers:       map[string]StorageDriver{policy: driver},
		infos: map[string]PolicyInfo{
			policy: {Name: policy, Type: "s3", IsDefault: true},
		},
	}
}

// NewTestStoragePolicyManagerMulti 注册多个 mock 策略，供多策略单测。
func NewTestStoragePolicyManagerMulti(defaultPolicy string, drivers map[string]StorageDriver) *StoragePolicyManager {
	infos := make(map[string]PolicyInfo, len(drivers))
	for name := range drivers {
		infos[name] = PolicyInfo{Name: name, Type: "s3", IsDefault: name == defaultPolicy}
	}
	return &StoragePolicyManager{
		defaultDriver: drivers[defaultPolicy],
		defaultPolicy: defaultPolicy,
		drivers:       drivers,
		infos:         infos,
	}
}
