package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/snowflake"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// AdminUserHandler 用户管理（管理员）。
type AdminUserHandler struct {
	mgr *storage.StoragePolicyManager
}

func NewAdminUserHandler(mgr *storage.StoragePolicyManager) *AdminUserHandler {
	return &AdminUserHandler{mgr: mgr}
}

// adminUserView 用户列表展示项，附带用户组名。
type adminUserView struct {
	model.User
	GroupName string `json:"group_name"`
}

func withGroupName(users []model.User, groups map[uint]string) []adminUserView {
	views := make([]adminUserView, 0, len(users))
	for _, u := range users {
		views = append(views, adminUserView{User: u, GroupName: groups[u.GroupID]})
	}
	return views
}

// List GET /api/admin/users
func (h *AdminUserHandler) List(c *gin.Context) {
	users, err := model.ListUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var groups []model.UserGroup
	_ = model.DB.Find(&groups).Error
	names := make(map[uint]string, len(groups))
	for _, g := range groups {
		names[g.ID] = g.Name
	}
	c.JSON(http.StatusOK, gin.H{"users": withGroupName(users, names)})
}

// Get GET /api/admin/users/:id
func (h *AdminUserHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	user, err := model.GetUserByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	view := adminUserView{User: *user}
	if user.GroupID != 0 {
		if g, err := model.GetUserGroupByID(user.GroupID); err == nil {
			view.GroupName = g.Name
		}
	}
	c.JSON(http.StatusOK, gin.H{"user": view})
}

type adminCreateUserRequest struct {
	Username string `json:"username" binding:"required,min=1,max=64"`
	Password string `json:"password" binding:"required,min=6"`
	GroupID  uint   `json:"group_id"`
	IsAdmin  bool   `json:"is_admin"`
}

// Create POST /api/admin/users
func (h *AdminUserHandler) Create(c *gin.Context) {
	var req adminCreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}
	req.Username = strings.TrimSpace(req.Username)

	groupID := req.GroupID
	if groupID == 0 {
		g, err := model.GetDefaultGroup()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请先添加用户组"})
			return
		}
		groupID = g.ID
	} else if _, err := model.GetUserGroupByID(groupID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "用户组不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
		return
	}

	user := &model.User{
		ID:           snowflake.Generate(),
		Username:     req.Username,
		PasswordHash: string(hash),
		IsAdmin:      req.IsAdmin,
		GroupID:      groupID,
	}
	if err := model.DB.Create(user).Error; err != nil {
		msg := err.Error()
		if strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "unique") || strings.Contains(msg, "Duplicate") {
			c.JSON(http.StatusConflict, gin.H{"error": "用户名已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": user})
}

type adminUpdateUserRequest struct {
	Username string `json:"username" binding:"required,min=1,max=64"`
	Password string `json:"password"` // 留空表示不修改
	GroupID  uint   `json:"group_id"`
	IsAdmin  *bool  `json:"is_admin"`
}

// Update PUT /api/admin/users/:id
func (h *AdminUserHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	var req adminUpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}
	req.Username = strings.TrimSpace(req.Username)

	user, err := model.GetUserByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if req.GroupID != 0 {
		if _, err := model.GetUserGroupByID(req.GroupID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "用户组不存在"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	user.Username = req.Username
	if req.GroupID != 0 {
		user.GroupID = req.GroupID
	}
	if req.IsAdmin != nil {
		user.IsAdmin = *req.IsAdmin
	}
	if req.Password != "" {
		if len(req.Password) < 6 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "密码至少 6 位"})
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
			return
		}
		user.PasswordHash = string(hash)
	}

	if err := model.DB.Save(user).Error; err != nil {
		msg := err.Error()
		if strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "unique") || strings.Contains(msg, "Duplicate") {
			c.JSON(http.StatusConflict, gin.H{"error": "用户名已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

// Delete DELETE /api/admin/users/:id —— 一并删除其文件记录；存储端对象需另行清理。
func (h *AdminUserHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	operatorID := c.GetInt64("user_id")
	if id == operatorID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不能删除当前登录的管理员账号"})
		return
	}
	if _, err := model.GetUserByID(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 先删存储端对象，再删数据库记录，避免留下孤儿对象
	var files []model.File
	if err := model.DB.Where("user_id = ? AND is_dir = ?", id, false).Find(&files).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for _, f := range files {
		driver, err := h.mgr.GetDriver(f.StoragePolicy)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("存储策略 %s 不可用，删除中止: %v", f.StoragePolicy, err)})
			return
		}
		if err := driver.Delete(f.StorageKey); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("删除存储对象失败: %v", err)})
			return
		}
	}

	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", id).Delete(&model.File{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&model.UploadSession{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&model.Share{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.User{}, id).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// ToggleBan PUT /api/admin/users/:id/ban - 切换封号状态
func (h *AdminUserHandler) ToggleBan(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 ID"})
		return
	}
	operatorID := c.GetInt64("user_id")
	if id == operatorID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不能封禁当前登录的管理员账号"})
		return
	}

	user, err := model.GetUserByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	user.Banned = !user.Banned
	if err := model.DB.Save(user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	status := "已解封"
	if user.Banned {
		status = "已封禁"
	}
	c.JSON(http.StatusOK, gin.H{"message": status, "banned": user.Banned})
}


