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
	ID            uint   `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	Endpoint      string `json:"endpoint"`
	Region        string `json:"region"`
	Bucket        string `json:"bucket"`
	AccessKey     string `json:"access_key"`
	SecretKeyHint string `json:"secret_key_hint"` // 仅提示是否已配置，不回显明文
	IsDefault     bool   `json:"is_default"`
	DefaultQuota  int64  `json:"default_quota"`
	CreatedAt     string `json:"created_at,omitempty"`
}

func toAdminView(p *model.StoragePolicy) adminPolicyView {
	hint := ""
	if p.SecretKey != "" {
		hint = "••••••••"
	}
	return adminPolicyView{
		ID:            p.ID,
		Name:          p.Name,
		Type:          p.Type,
		Endpoint:      p.Endpoint,
		Region:        p.Region,
		Bucket:        p.Bucket,
		AccessKey:     p.AccessKey,
		SecretKeyHint: hint,
		IsDefault:     p.IsDefault,
		DefaultQuota:  p.DefaultQuota,
		CreatedAt:     p.CreatedAt.Format("2006-01-02 15:04:05"),
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
	Name      string `json:"name" binding:"required,min=1,max=64"`
	Endpoint  string `json:"endpoint" binding:"required"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket" binding:"required"`
	AccessKey string `json:"access_key" binding:"required"`
	SecretKey string `json:"secret_key"`
	IsDefault    bool  `json:"is_default"`
	DefaultQuota int64 `json:"default_quota"`
}

// Create POST /api/admin/storage/policies
func (h *PolicyHandler) Create(c *gin.Context) {
	var req policyBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.SecretKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Secret Key 不能为空"})
		return
	}
	if req.Region == "" {
		req.Region = "us-east-1"
	}
	if req.DefaultQuota < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "默认配额不能为负数"})
		return
	}

	p := &model.StoragePolicy{
		Name:         req.Name,
		Type:         "s3",
		Endpoint:     strings.TrimSpace(req.Endpoint),
		Region:       strings.TrimSpace(req.Region),
		Bucket:       strings.TrimSpace(req.Bucket),
		AccessKey:    strings.TrimSpace(req.AccessKey),
		SecretKey:    req.SecretKey,
		IsDefault:    req.IsDefault,
		DefaultQuota: req.DefaultQuota,
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
	if req.Region == "" {
		req.Region = "us-east-1"
	}
	if req.DefaultQuota < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "默认配额不能为负数"})
		return
	}

	updates := &model.StoragePolicy{
		Name:         req.Name,
		Type:         "s3",
		Endpoint:     strings.TrimSpace(req.Endpoint),
		Region:       strings.TrimSpace(req.Region),
		Bucket:       strings.TrimSpace(req.Bucket),
		AccessKey:    strings.TrimSpace(req.AccessKey),
		SecretKey:    req.SecretKey,
		IsDefault:    req.IsDefault,
		DefaultQuota: req.DefaultQuota,
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
