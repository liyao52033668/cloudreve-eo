package handler

import (
	"net/http"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/service"
	"github.com/gin-gonic/gin"
)

type ShareHandler struct {
	shareService *service.ShareService
}

func NewShareHandler(ss *service.ShareService) *ShareHandler {
	return &ShareHandler{shareService: ss}
}

type createShareRequest struct {
	FileID   uint   `json:"file_id" binding:"required"`
	Password string `json:"password"`
	ExpireAt string `json:"expire_at"`
}

func (h *ShareHandler) Create(c *gin.Context) {
	userID := c.GetUint("user_id")
	var req createShareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var expireAt *time.Time
	if req.ExpireAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpireAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "过期时间格式错误"})
			return
		}
		expireAt = &t
	}

	share, err := h.shareService.Create(userID, req.FileID, req.Password, expireAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"share": share})
}

func (h *ShareHandler) Get(c *gin.Context) {
	code := c.Param("code")
	password := c.Query("password")

	share, file, err := h.shareService.GetByCode(code, password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"share": share, "file": file})
}

func (h *ShareHandler) Download(c *gin.Context) {
	code := c.Param("code")
	password := c.Query("password")

	url, err := h.shareService.GetDownloadURL(code, password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"download_url": url})
}
