package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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
	// FileID 兼容旧版单文件分享请求；FileIDs 优先
	FileID   uint   `json:"file_id"`
	FileIDs  []uint `json:"file_ids"`
	Password string `json:"password"`
	ExpireAt string `json:"expire_at"`
}

func (h *ShareHandler) Create(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req createShareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fileIDs := req.FileIDs
	if len(fileIDs) == 0 && req.FileID != 0 {
		fileIDs = []uint{req.FileID}
	}
	if len(fileIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要分享的文件"})
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

	share, err := h.shareService.Create(userID, fileIDs, req.Password, expireAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"share": share})
}

func (h *ShareHandler) Get(c *gin.Context) {
	code := c.Param("code")
	password := c.Query("password")

	share, files, err := h.shareService.GetByCode(code, password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"share": share, "files": files})
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

// List GET /api/shares/:code/files —— 分享文件列表（浏览目录用）。
// parent_id 缺省 0 表示顶层，返回分享的全部根文件。
func (h *ShareHandler) List(c *gin.Context) {
	code := c.Param("code")
	password := c.Query("password")
	parentID, _ := strconv.ParseUint(c.Query("parent_id"), 10, 32)

	files, err := h.shareService.ListChildren(code, password, uint(parentID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"files": files})
}

// DownloadSelectedZip POST /api/shares/:code/zip —— 分享内选中文件打包下载。
func (h *ShareHandler) DownloadSelectedZip(c *gin.Context) {
	code := c.Param("code")
	password := c.Query("password")
	var req struct {
		IDs []uint `json:"ids" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要下载的文件"})
		return
	}

	// zip 流式输出：响应头必须在首次写入前设置（首次写入即 flush 头部，事后设置无效）
	if err := h.shareService.DownloadSelected(code, password, req.IDs, c.Writer, func(fileName string) {
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename*=UTF-8''%s`, url.PathEscape(fileName)))
	}); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
}

// DownloadChild GET /api/shares/:code/files/:id/download —— 分享内单个文件下载。
func (h *ShareHandler) DownloadChild(c *gin.Context) {
	code := c.Param("code")
	password := c.Query("password")
	fileID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	url, err := h.shareService.GetChildDownloadURL(code, password, uint(fileID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"download_url": url})
}

// DownloadZip GET /api/shares/:code/zip —— 分享全部文件打包下载。
func (h *ShareHandler) DownloadZip(c *gin.Context) {
	code := c.Param("code")
	password := c.Query("password")

	// zip 流式输出：响应头必须在首次写入前设置（首次写入即 flush 头部，事后设置无效）
	_, err := h.shareService.DownloadDir(code, password, c.Writer, func(fileName string) {
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename*=UTF-8''%s`, url.PathEscape(fileName)))
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
}
