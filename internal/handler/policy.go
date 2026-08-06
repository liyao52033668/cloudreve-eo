package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// PolicyHandler 存储策略管理（管理员）与用户列表。
type PolicyHandler struct {
	mgr *storage.StoragePolicyManager
}

func NewPolicyHandler(mgr *storage.StoragePolicyManager) *PolicyHandler {
	return &PolicyHandler{mgr: mgr}
}

// ListPublic GET /api/storage/policies —— 用户上传时选择，不含密钥。
func (h *PolicyHandler) ListPublic(c *gin.Context) {
	list := h.mgr.ListPolicies()
	c.JSON(http.StatusOK, gin.H{
		"policies": list,
		"default":  h.mgr.DefaultPolicy(),
	})
}

// adminPolicyView 管理端展示（密钥脱敏）。
type adminPolicyView struct {
	ID             uint   `json:"id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	Endpoint       string `json:"endpoint"`
	Region         string `json:"region"`
	Bucket         string `json:"bucket"`
	AccessKey      string `json:"access_key"`
	SecretKeyHint  string `json:"secret_key_hint"` // 仅提示是否已配置，不回显明文
	ForcePathStyle bool   `json:"force_path_style"`
	CustomHost     string `json:"custom_host"`
	BasePath       string `json:"base_path"`
	Branch         string `json:"branch"`
	ChunkSize      int64  `json:"chunk_size"`
	IsDefault      bool   `json:"is_default"`
	DefaultQuota   int64  `json:"default_quota"`
	CreatedAt      string `json:"created_at,omitempty"`
}

func toAdminView(p *model.StoragePolicy) adminPolicyView {
	hint := ""
	if p.SecretKey != "" {
		hint = "••••••••"
	}
	return adminPolicyView{
		ID:             p.ID,
		Name:           p.Name,
		Type:           p.Type,
		Endpoint:       p.Endpoint,
		Region:         p.Region,
		Bucket:         p.Bucket,
		AccessKey:      p.AccessKey,
		SecretKeyHint:  hint,
		ForcePathStyle: p.ForcePathStyle,
		CustomHost:     p.CustomHost,
		BasePath:       p.BasePath,
		Branch:         p.Branch,
		ChunkSize:      p.ChunkSize,
		IsDefault:      p.IsDefault,
		DefaultQuota:   p.DefaultQuota,
		CreatedAt:      p.CreatedAt.Format("2006-01-02 15:04:05"),
	}
}

// ListAdmin GET /api/admin/storage/policies
func (h *PolicyHandler) ListAdmin(c *gin.Context) {
	list, err := model.ListStoragePolicies()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	views := make([]adminPolicyView, 0, len(list))
	for i := range list {
		views = append(views, toAdminView(&list[i]))
	}
	c.JSON(http.StatusOK, gin.H{"policies": views})
}

// GetAdmin GET /api/admin/storage/policies/:id —— 编辑用，含完整密钥（仅管理员）。
func (h *PolicyHandler) GetAdmin(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	p, err := model.GetStoragePolicyByID(uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "策略不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"policy": p})
}

type policyBody struct {
	Name           string `json:"name" binding:"required,min=1,max=64"`
	Type           string `json:"type"` // s3 或 github
	Endpoint       string `json:"endpoint"`
	Region         string `json:"region"`
	Bucket         string `json:"bucket"`
	AccessKey      string `json:"access_key"`
	SecretKey      string `json:"secret_key"`
	ForcePathStyle *bool  `json:"force_path_style"`
	CustomHost     string `json:"custom_host"`
	BasePath       string `json:"base_path"`
	Branch         string `json:"branch"` // GitHub 分支
	ChunkSize      int64  `json:"chunk_size"`
	IsDefault      bool   `json:"is_default"`
	DefaultQuota   int64  `json:"default_quota"`
}

// Create POST /api/admin/storage/policies
func (h *PolicyHandler) Create(c *gin.Context) {
	var req policyBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}
	req.Name = strings.TrimSpace(req.Name)

	policyType := "s3"
	if req.Type != "" {
		policyType = req.Type
	}

	// 按类型校验必填字段
	if policyType == "github" {
		if req.Endpoint == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "GitHub 仓库地址不能为空"})
			return
		}
		if req.SecretKey == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "GitHub Token 不能为空"})
			return
		}
	} else {
		// S3 类型
		if req.SecretKey == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Secret Key 不能为空"})
			return
		}
		if req.Endpoint == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Endpoint 不能为空"})
			return
		}
		if req.Bucket == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Bucket 不能为空"})
			return
		}
		if req.AccessKey == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Access Key 不能为空"})
			return
		}
	}

	if req.Region == "" {
		req.Region = "us-east-1"
	}
	if req.DefaultQuota < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "默认配额不能为负数"})
		return
	}
	if req.ChunkSize != 0 && req.ChunkSize < 5<<20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "分片大小非 0 时至少为 5MB（S3 协议要求）"})
		return
	}

	forcePath := true
	if req.ForcePathStyle != nil {
		forcePath = *req.ForcePathStyle
	}
	p := &model.StoragePolicy{
		Name:           req.Name,
		Type:           policyType,
		Endpoint:       strings.TrimSpace(req.Endpoint),
		Region:         strings.TrimSpace(req.Region),
		Bucket:         strings.TrimSpace(req.Bucket),
		AccessKey:      strings.TrimSpace(req.AccessKey),
		SecretKey:      req.SecretKey,
		ForcePathStyle: forcePath,
		CustomHost:     normalizeCustomHost(req.CustomHost),
		BasePath:       normalizeBasePath(req.BasePath),
		Branch:         strings.TrimSpace(req.Branch),
		ChunkSize:      req.ChunkSize,
		IsDefault:      req.IsDefault,
		DefaultQuota:   req.DefaultQuota,
	}
	if err := model.CreateStoragePolicy(p); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "unique") || strings.Contains(msg, "Duplicate") {
			c.JSON(http.StatusConflict, gin.H{"error": "策略名称已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
		return
	}
	if err := h.mgr.ReloadFromDB(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存成功但热加载失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"policy": toAdminView(p)})
}

// Update PUT /api/admin/storage/policies/:id
func (h *PolicyHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	var req policyBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}
	req.Name = strings.TrimSpace(req.Name)

	policyType := "s3"
	if req.Type != "" {
		policyType = req.Type
	}

	// 按类型校验必填字段
	if policyType == "github" {
		if req.Endpoint == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "GitHub 仓库地址不能为空"})
			return
		}
		// SecretKey 在编辑时可以为空（表示不修改）
	} else {
		// S3 类型
		if req.Endpoint == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Endpoint 不能为空"})
			return
		}
		if req.Bucket == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Bucket 不能为空"})
			return
		}
		if req.AccessKey == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Access Key 不能为空"})
			return
		}
		// SecretKey 在编辑时可以为空（表示不修改）
	}

	if req.Region == "" {
		req.Region = "us-east-1"
	}
	if req.DefaultQuota < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "默认配额不能为负数"})
		return
	}
	if req.ChunkSize != 0 && req.ChunkSize < 5<<20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "分片大小非 0 时至少为 5MB（S3 协议要求）"})
		return
	}

	forcePath := true
	if req.ForcePathStyle != nil {
		forcePath = *req.ForcePathStyle
	}
	updates := &model.StoragePolicy{
		Name:           req.Name,
		Type:           policyType,
		Endpoint:       strings.TrimSpace(req.Endpoint),
		Region:         strings.TrimSpace(req.Region),
		Bucket:         strings.TrimSpace(req.Bucket),
		AccessKey:      strings.TrimSpace(req.AccessKey),
		SecretKey:      req.SecretKey,
		ForcePathStyle: forcePath,
		CustomHost:     normalizeCustomHost(req.CustomHost),
		BasePath:       normalizeBasePath(req.BasePath),
		Branch:         strings.TrimSpace(req.Branch),
		ChunkSize:      req.ChunkSize,
		IsDefault:      req.IsDefault,
		DefaultQuota:   req.DefaultQuota,
	}
	if err := model.UpdateStoragePolicy(uint(id), updates, req.SecretKey != ""); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "策略不存在"})
			return
		}
		msg := err.Error()
		if strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "unique") || strings.Contains(msg, "Duplicate") {
			c.JSON(http.StatusConflict, gin.H{"error": "策略名称已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
		return
	}
	if err := h.mgr.ReloadFromDB(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存成功但热加载失败: " + err.Error()})
		return
	}
	p, _ := model.GetStoragePolicyByID(uint(id))
	c.JSON(http.StatusOK, gin.H{"policy": toAdminView(p)})
}

// Delete DELETE /api/admin/storage/policies/:id
func (h *PolicyHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	if err := model.DeleteStoragePolicy(uint(id)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "策略不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.mgr.ReloadFromDB(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除成功但热加载失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// SetDefault POST /api/admin/storage/policies/:id/default
func (h *PolicyHandler) SetDefault(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	if err := model.SetDefaultStoragePolicy(uint(id)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "策略不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.mgr.ReloadFromDB(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "设置成功但热加载失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已设为默认策略"})
}

// SetCORS POST /api/admin/storage/policies/:id/cors —— 为策略对应存储桶写入浏览器直传 CORS 规则。
func (h *PolicyHandler) SetCORS(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	p, err := model.GetStoragePolicyByID(uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "策略不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	driver, err := h.mgr.GetDriver(p.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := driver.SetBucketCORS(); err != nil {
		if errors.Is(err, storage.ErrBucketCORSNotSupported) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "存储桶 CORS 已配置"})
}

// normalizeCustomHost 清理自定义域名：去首尾空白与末尾斜杠。
func normalizeCustomHost(h string) string {
	return strings.TrimRight(strings.TrimSpace(h), "/")
}

// normalizeBasePath 去掉首尾 / 与多余空白；禁止 .. 等路径穿越。
func normalizeBasePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	// 统一为正斜杠、折叠重复斜杠，并拒绝包含 .. 的路径
	parts := strings.Split(p, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			// 丢弃上级引用，避免对象键逃出前缀目录
			continue
		}
		clean = append(clean, part)
	}
	return strings.Join(clean, "/")
}

