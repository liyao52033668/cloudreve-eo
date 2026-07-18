package storage

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
)

// PolicyInfo 对外暴露的存储策略信息（不含密钥）。
type PolicyInfo struct {
	ID             uint   `json:"id,omitempty"`
	Name           string `json:"name"`
	Type           string `json:"type"` // 当前固定 "s3"
	Bucket         string `json:"bucket,omitempty"`
	Endpoint       string `json:"endpoint,omitempty"`
	Region         string `json:"region,omitempty"`
	ForcePathStyle bool   `json:"force_path_style"`
	// BasePath 对象键前缀，上传时拼到 storage_key 前面。
	BasePath     string `json:"base_path,omitempty"`
	IsDefault    bool   `json:"is_default"`
	DefaultQuota int64  `json:"default_quota"`
}

// StoragePolicyManager 管理多个存储策略及其对应驱动，支持从数据库热重载。
// 策略仅来自前端写入的数据库，无环境变量引导。
type StoragePolicyManager struct {
	mu            sync.RWMutex
	defaultDriver StorageDriver
	defaultPolicy string
	drivers       map[string]StorageDriver
	infos         map[string]PolicyInfo
}

// NewStoragePolicyManager 从数据库加载策略；库为空时管理器为空，管理员需在前端添加。
func NewStoragePolicyManager() (*StoragePolicyManager, error) {
	mgr := &StoragePolicyManager{
		drivers: make(map[string]StorageDriver),
		infos:   make(map[string]PolicyInfo),
	}
	if err := mgr.ReloadFromDB(); err != nil {
		return nil, err
	}
	return mgr, nil
}

// ReloadFromDB 从数据库重新加载全部策略并重建驱动（热更新）。
// 库中无策略时不报错，仅清空运行时映射。
func (m *StoragePolicyManager) ReloadFromDB() error {
	list, err := model.ListStoragePolicies()
	if err != nil {
		return fmt.Errorf("读取存储策略失败: %w", err)
	}

	drivers := make(map[string]StorageDriver, len(list))
	infos := make(map[string]PolicyInfo, len(list))
	var defaultName string
	var defaultDriver StorageDriver

	// 各策略相互独立：某一条初始化失败不影响其它策略加载。
	var loadErrs []string
	for _, p := range list {
		driver, err := NewS3Driver(p.Endpoint, p.Region, p.Bucket, p.AccessKey, p.SecretKey, p.ForcePathStyle)
		if err != nil {
			loadErrs = append(loadErrs, fmt.Sprintf("%s: %v", p.Name, err))
			continue
		}
		drivers[p.Name] = driver
		infos[p.Name] = PolicyInfo{
			ID:             p.ID,
			Name:           p.Name,
			Type:           p.Type,
			Bucket:         p.Bucket,
			Endpoint:       p.Endpoint,
			Region:         p.Region,
			ForcePathStyle: p.ForcePathStyle,
			BasePath:       p.BasePath,
			IsDefault:      p.IsDefault,
			DefaultQuota:   p.DefaultQuota,
		}
		if p.IsDefault {
			defaultName = p.Name
			defaultDriver = driver
		}
	}
	if len(loadErrs) > 0 {
		// 全部失败时返回错误；部分失败则继续，仅记录信息到 error 链的最后返回 nil 让服务可启动。
		if len(drivers) == 0 {
			return fmt.Errorf("全部存储策略初始化失败: %s", strings.Join(loadErrs, "; "))
		}
		// 部分成功：保留可用策略。失败项不进入 drivers，前端仍可在管理页看到并修正。
		fmt.Printf("部分存储策略初始化失败（已跳过）: %s\n", strings.Join(loadErrs, "; "))
	}

	if defaultName == "" && len(drivers) > 0 {
		// 默认策略未成功加载时，取已加载中名称排序第一的作为运行时默认
		names := make([]string, 0, len(drivers))
		for name := range drivers {
			names = append(names, name)
		}
		sort.Strings(names)
		defaultName = names[0]
		defaultDriver = drivers[defaultName]
		info := infos[defaultName]
		info.IsDefault = true
		infos[defaultName] = info
	}

	m.mu.Lock()
	m.drivers = drivers
	m.infos = infos
	m.defaultPolicy = defaultName
	m.defaultDriver = defaultDriver
	m.mu.Unlock()
	return nil
}

func (m *StoragePolicyManager) DefaultDriver() StorageDriver {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.defaultDriver
}

func (m *StoragePolicyManager) DefaultPolicy() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.defaultPolicy
}

func (m *StoragePolicyManager) GetDriver(policy string) (StorageDriver, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if policy == "" {
		policy = m.defaultPolicy
	}
	if policy == "" {
		return nil, fmt.Errorf("未配置任何存储策略，请管理员在「存储策略」中添加")
	}
	driver, ok := m.drivers[policy]
	if !ok {
		return nil, fmt.Errorf("存储策略 %s 不存在", policy)
	}
	return driver, nil
}

// ResolvePolicy 校验策略名；空字符串返回默认策略。
func (m *StoragePolicyManager) ResolvePolicy(policy string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if policy == "" {
		if m.defaultPolicy == "" {
			return "", fmt.Errorf("未配置任何存储策略，请管理员在「存储策略」中添加")
		}
		return m.defaultPolicy, nil
	}
	if _, ok := m.drivers[policy]; !ok {
		return "", fmt.Errorf("存储策略 %s 不存在", policy)
	}
	return policy, nil
}

// ListPolicies 返回已配置策略列表（默认策略排前，其余按名称排序）。
func (m *StoragePolicyManager) ListPolicies() []PolicyInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

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

// GetPolicyInfo 返回策略公开信息；不存在时 ok=false。
func (m *StoragePolicyManager) GetPolicyInfo(policy string) (PolicyInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if policy == "" {
		policy = m.defaultPolicy
	}
	info, ok := m.infos[policy]
	return info, ok
}

// NewTestStoragePolicyManager 使用预置驱动构造管理器，供单测注入 mock。
func NewTestStoragePolicyManager(policy string, driver StorageDriver) *StoragePolicyManager {
	return &StoragePolicyManager{
		defaultDriver: driver,
		defaultPolicy: policy,
		drivers:       map[string]StorageDriver{policy: driver},
		infos: map[string]PolicyInfo{
			// 单测默认给足够大的配额，避免无关用例因配额失败
			policy: {Name: policy, Type: "s3", IsDefault: true, DefaultQuota: 1 << 40},
		},
	}
}

// NewTestStoragePolicyManagerMulti 注册多个 mock 策略，供多策略单测。
func NewTestStoragePolicyManagerMulti(defaultPolicy string, drivers map[string]StorageDriver) *StoragePolicyManager {
	infos := make(map[string]PolicyInfo, len(drivers))
	for name := range drivers {
		infos[name] = PolicyInfo{Name: name, Type: "s3", IsDefault: name == defaultPolicy, DefaultQuota: 1 << 40}
	}
	return &StoragePolicyManager{
		defaultDriver: drivers[defaultPolicy],
		defaultPolicy: defaultPolicy,
		drivers:       drivers,
		infos:         infos,
	}
}

// NewTestStoragePolicyManagerWithQuota 单测用：指定策略配额。
func NewTestStoragePolicyManagerWithQuota(policy string, driver StorageDriver, quota int64) *StoragePolicyManager {
	return &StoragePolicyManager{
		defaultDriver: driver,
		defaultPolicy: policy,
		drivers:       map[string]StorageDriver{policy: driver},
		infos: map[string]PolicyInfo{
			policy: {Name: policy, Type: "s3", IsDefault: true, DefaultQuota: quota},
		},
	}
}
