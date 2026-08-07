package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GroupHandler 用户组管理（管理员）。
type GroupHandler struct{}

func NewGroupHandler() *GroupHandler {
	return &GroupHandler{}
}

type groupBody struct {
	Name          string   `json:"name" binding:"required,min=1,max=64"`
	StoragePolicies []string `json:"storage_policies"` // 多选存储策略名称；空表示跟随默认策略
	MaxStorage    int64    `json:"max_storage"`      // 每用户最大容量（字节）；0 沿用所有策略的默认配额总和
	IsDefault     bool     `json:"is_default"`
}

func (h *GroupHandler) validate(req *groupBody) error {
	req.Name = strings.TrimSpace(req.Name)
	if req.MaxStorage < 0 {
		return errors.New("最大容量不能为负数")
	}
	if len(req.StoragePolicies) == 0 {
		// 空数组 = 跟随默认策略（保持兼容）
		return nil
	}
	for _, p := range req.StoragePolicies {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, err := model.GetStoragePolicyByName(p); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("存储策略不存在: " + p)
			}
			return err
		}
	}
	return nil
}

// List GET /api/admin/groups —— 全部用户组，附带用户数与已用容量合计。
func (h *GroupHandler) List(c *gin.Context) {
	views, err := model.ListUserGroups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"groups": views})
}

// Get GET /api/admin/groups/:id
func (h *GroupHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	g, err := model.GetUserGroupByID(uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户组不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"group": g})
}

// Create POST /api/admin/groups
func (h *GroupHandler) Create(c *gin.Context) {
	var req groupBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}
	if err := h.validate(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	g := &model.UserGroup{
		Name:          req.Name,
		StoragePolicy: req.StoragePolicy,
		MaxStorage:    req.MaxStorage,
		IsDefault:     req.IsDefault,
	}
	if err := model.CreateUserGroup(g); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "unique") || strings.Contains(msg, "Duplicate") {
			c.JSON(http.StatusConflict, gin.H{"error": "用户组名称已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"group": g})
}

// Update PUT /api/admin/groups/:id
func (h *GroupHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	var req groupBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}
	if err := h.validate(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := &model.UserGroup{
		Name:          req.Name,
		StoragePolicy: req.StoragePolicy,
		MaxStorage:    req.MaxStorage,
		IsDefault:     req.IsDefault,
	}
	if err := model.UpdateUserGroup(uint(id), updates); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户组不存在"})
			return
		}
		msg := err.Error()
		if strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "unique") || strings.Contains(msg, "Duplicate") {
			c.JSON(http.StatusConflict, gin.H{"error": "用户组名称已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
		return
	}
	g, _ := model.GetUserGroupByID(uint(id))
	c.JSON(http.StatusOK, gin.H{"group": g})
}

// Delete DELETE /api/admin/groups/:id —— 组内用户自动并入默认组。
func (h *GroupHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	if err := model.DeleteUserGroup(uint(id)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户组不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// SetDefault POST /api/admin/groups/:id/default
func (h *GroupHandler) SetDefault(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	if err := model.SetDefaultUserGroup(uint(id)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户组不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已设为默认用户组"})
}
