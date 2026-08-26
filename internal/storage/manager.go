package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/logx"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
)

// PolicyInfo 对外暴露的存储策略信息（不含密钥）。
type PolicyInfo struct {
	ID             uint   `json:"id,omitempty"`
	Name           string `json:"name"`
	Type           string `json:"type"` // s3 / github / terabox / filen / dropbox / baidu
	Bucket         string `json:"bucket,omitempty"`
	Endpoint       string `json:"endpoint,omitempty"`
	Region         string `json:"region,omitempty"`
	ForcePathStyle bool   `json:"force_path_style"`
	// CustomHost 自定义下载/预览域名；空表示使用 Endpoint。
	CustomHost string `json:"custom_host,omitempty"`
	// BasePath 对象键前缀，上传时拼到 storage_key 前面。
	BasePath string `json:"base_path,omitempty"`
	// ChunkSize 分片大小（字节）；0 表示使用默认值。
	ChunkSize    int64 `json:"chunk_size"`
	IsDefault    bool  `json:"is_default"`
	DefaultQuota int64 `json:"default_quota"`
	// Authorized 仅 TeraBox / 百度网盘类型：是否已完成 OAuth 授权。
	Authorized bool `json:"authorized"`
}

// StoragePolicyManager 管理多个存储策略及其对应驱动，支持从数据库热重载。
// 策略仅来自前端写入的数据库，无环境变量引导。
type StoragePolicyManager struct {
	mu            sync.RWMutex
	defaultDriver StorageDriver
	defaultPolicy string
	drivers       map[string]StorageDriver
	infos         map[string]PolicyInfo
	// proxySecret 动态提供签名密钥（JWT 轮转后自动跟随）；
	// proxyBaseURL 用于为无外链直链的驱动（如 Filen）签发服务端代理下载 URL。
	proxySecret  func() string
	proxyBaseURL string
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
		var driver StorageDriver
		var err error
		var authorized bool

		switch p.Type {
		case "github":
			driver, err = NewGitHubDriver(p.Endpoint, p.SecretKey, p.BasePath, p.CustomHost, p.Branch)
		case "dropbox":
			driver, err = NewDropboxDriver(p.SecretKey, p.BasePath)
		case "filen":
			var fd *FilenDriver
			fd, err = NewFilenDriver(p.AccessKey, p.SecretKey, p.BasePath)
			if err == nil {
				policyName := p.Name
				mgr := m
				// Filen 无外链直链：下载/预览 URL 指向带签名的服务端代理。
				fd.proxyURL = func(storageKey, attachment string) (string, error) {
					return mgr.SignProxyURL(policyName, storageKey, attachment, 30*time.Minute)
				}
			}
			driver = fd
		case "terabox":
			var tb *TeraBoxDriver
			tb, err = NewTeraBoxDriver(p.AccessKey, p.SecretKey, p.Region, p.Endpoint, p.OAuthToken)
			if err == nil {
				policyID := p.ID
				// token 刷新后持久化回数据库，保证进程重启后仍可用
				tb.onTokenRefreshed = func(token TeraBoxToken) {
					raw, marshalErr := json.Marshal(token)
					if marshalErr != nil {
						return
					}
					if saveErr := model.SetStoragePolicyOAuthToken(policyID, string(raw)); saveErr != nil {
						logx.Warn(logx.ModuleStorage, "保存 TeraBox token 失败", "policy", p.Name, "err", saveErr.Error())
					}
				}
				authorized = tb.IsAuthorized()
			}
			driver = tb
		case "baidu":
			var bd *BaiduDriver
			bd, err = NewBaiduDriver(p.AccessKey, p.SecretKey, p.Endpoint, p.BasePath, p.OAuthToken)
			if err == nil {
				policyID := p.ID
				// token 刷新后持久化回数据库，保证进程重启后仍可用
				bd.onTokenRefreshed = func(token BaiduToken) {
					raw, marshalErr := json.Marshal(token)
					if marshalErr != nil {
						return
					}
					if saveErr := model.SetStoragePolicyOAuthToken(policyID, string(raw)); saveErr != nil {
						logx.Warn(logx.ModuleStorage, "保存百度网盘 token 失败", "policy", p.Name, "err", saveErr.Error())
					}
				}
				// 百度 dlink 为临时短链：下载/预览 URL 指向带签名的服务端代理
				mgr := m
				bd.proxyURL = func(storageKey, attachment string) (string, error) {
					return mgr.SignProxyURL(p.Name, storageKey, attachment, 30*time.Minute)
				}
				authorized = bd.IsAuthorized()
			}
			driver = bd
		case "webdav":
			var wd *WebDAVDriver
			wd, err = NewWebDAVDriver(p.Endpoint, p.AccessKey, p.SecretKey, p.BasePath, p.CustomHost, p.WebDAVDirect)
			if err == nil {
				policyName := p.Name
				mgr := m
				// WebDAV 中转模式：下载/预览 URL 指向带签名的服务端代理；
				// 直连模式（direct=true）不注入，直接返回内嵌凭据的直连 URL。
				wd.proxyURL = func(storageKey, attachment string) (string, error) {
					return mgr.SignProxyURL(policyName, storageKey, attachment, 30*time.Minute)
				}
			}
			driver = wd
		default: // s3
			driver, err = NewS3Driver(p.Endpoint, p.Region, p.Bucket, p.AccessKey, p.SecretKey, p.ForcePathStyle, p.CustomHost)
		}

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
			CustomHost:     p.CustomHost,
			BasePath:       p.BasePath,
			ChunkSize:      p.ChunkSize,
			IsDefault:      p.IsDefault,
			DefaultQuota:   p.DefaultQuota,
			Authorized:     authorized,
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
		logx.Warn(logx.ModuleStorage, "部分存储策略初始化失败（已跳过）", "detail", strings.Join(loadErrs, "; "))
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

// SetProxySigner 配置服务端代理下载 URL 的签名密钥提供函数与基础地址。
// baseURL 形如 "/api/files/proxy"（相对）或完整地址；代理 handler 用同一密钥校验。
func (m *StoragePolicyManager) SetProxySigner(secretProvider func() string, baseURL string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.proxySecret = secretProvider
	m.proxyBaseURL = strings.TrimRight(baseURL, "/")
}

// SignProxyURL 为 Filen 等无外链驱动生成带签名的代理下载 URL。
// attachment 为下载时建议的文件名；空字符串表示内联预览。
// URL 形如 {baseURL}?policy=x&key=y&name=z&exp=ts&sig=hex(hmac(...))。
func (m *StoragePolicyManager) SignProxyURL(policy, storageKey, attachment string, expire time.Duration) (string, error) {
	m.mu.RLock()
	secretProvider := m.proxySecret
	base := m.proxyBaseURL
	m.mu.RUnlock()
	if secretProvider == nil || base == "" {
		return "", fmt.Errorf("代理下载未配置（缺少签名密钥或基础地址）")
	}
	secret := secretProvider()
	if secret == "" {
		return "", fmt.Errorf("代理下载未配置（签名密钥为空）")
	}

	exp := time.Now().Add(expire).Unix()
	q := fmt.Sprintf("policy=%s&key=%s&name=%s&exp=%d",
		urlQueryEscape(policy), urlQueryEscape(storageKey), urlQueryEscape(attachment), exp)
	sig := proxySignature(secret, policy, storageKey, attachment, exp)
	return base + "?" + q + "&sig=" + sig, nil
}

// VerifyProxyURL 校验代理下载 URL 的签名与有效期。
func (m *StoragePolicyManager) VerifyProxyURL(policy, storageKey, attachment, exp, sig string) error {
	m.mu.RLock()
	secretProvider := m.proxySecret
	m.mu.RUnlock()
	if secretProvider == nil {
		return fmt.Errorf("代理下载未配置")
	}
	secret := secretProvider()
	if secret == "" {
		return fmt.Errorf("代理下载未配置（签名密钥为空）")
	}
	expUnix, err := strconv.ParseInt(exp, 10, 64)
	if err != nil {
		return fmt.Errorf("无效的过期时间")
	}
	if time.Now().Unix() > expUnix {
		return fmt.Errorf("下载链接已过期")
	}
	expected := proxySignature(secret, policy, storageKey, attachment, expUnix)
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return fmt.Errorf("下载链接签名无效")
	}
	return nil
}

// proxySignature 计算 HMAC-SHA256 十六进制签名。
func proxySignature(secret, policy, key, attachment string, exp int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s|%s|%s|%d", policy, key, attachment, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// urlQueryEscape URL 查询值转义。
func urlQueryEscape(s string) string {
	return url.QueryEscape(s)
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
