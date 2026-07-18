package handler

import (
	"net/http"

	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type UserHandler struct {
	mgr *storage.StoragePolicyManager
}

func NewUserHandler(mgr *storage.StoragePolicyManager) *UserHandler {
	return &UserHandler{mgr: mgr}
}

type policyUsage struct {
	Name         string `json:"name"`
	IsDefault    bool   `json:"is_default"`
	DefaultQuota int64  `json:"default_quota"`
	Used         int64  `json:"used"`
}

func (h *UserHandler) Profile(c *gin.Context) {
	userID := c.GetUint("user_id")
	var user model.User
	if err := model.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	policies := make([]policyUsage, 0)
	if h.mgr != nil {
		for _, info := range h.mgr.ListPolicies() {
			var used int64
			_ = model.DB.Model(&model.File{}).
				Where("user_id = ? AND storage_policy = ? AND is_dir = ?", userID, info.Name, false).
				Select("COALESCE(SUM(size), 0)").Scan(&used).Error
			policies = append(policies, policyUsage{
				Name:         info.Name,
				IsDefault:    info.IsDefault,
				DefaultQuota: info.DefaultQuota,
				Used:         used,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"user":             user,
		"storage_policies": policies,
	})
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

func (h *UserHandler) ChangePassword(c *gin.Context) {
	userID := c.GetUint("user_id")
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user model.User
	if err := model.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "原密码错误"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
		return
	}

	if err := model.DB.Model(&user).Update("password_hash", string(hash)).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新密码失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "密码修改成功"})
}
