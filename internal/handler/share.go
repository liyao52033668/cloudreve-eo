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

// ListMyShares GET /api/shares —— 列出当前用户的全部分享。
func (h *ShareHandler) ListMyShares(c *gin.Context) {
	userID := c.GetInt64("user_id")
	shares, err := h.shareService.List(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"shares": shares})
}

// Delete DELETE /api/shares/:id —— 取消分享。
func (h *ShareHandler) Delete(c *gin.Context) {
	userID := c.GetInt64("user_id")
	shareID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	if err := h.shareService.Delete(uint(shareID), userID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已取消分享"})
}

type shareFileRequest struct {
	FileIDs []uint `json:"file_ids" binding:"required"`
}

// AddFiles PUT /api/shares/:id/files —— 向分享中添加文件。
func (h *ShareHandler) AddFiles(c *gin.Context) {
	userID := c.GetInt64("user_id")
	shareID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var req shareFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.shareService.AddFiles(uint(shareID), userID, req.FileIDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已添加文件"})
}

// RemoveFiles DELETE /api/shares/:id/files —— 从分享中移除文件。
func (h *ShareHandler) RemoveFiles(c *gin.Context) {
	userID := c.GetInt64("user_id")
	shareID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	// DELETE 请求体：部分客户端/代理不支持 body，同时支持 ?ids=1,2,3
	var req shareFileRequest
	var fileIDs []uint
	if err := c.ShouldBindJSON(&req); err == nil && len(req.FileIDs) > 0 {
		fileIDs = req.FileIDs
	} else if idsQuery := c.Query("ids"); idsQuery != "" {
		ids, perr := parseIDList(idsQuery)
		if perr != nil || len(ids) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要移除的文件"})
			return
		}
		fileIDs = ids
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要移除的文件"})
		return
	}

	if err := h.shareService.RemoveFiles(uint(shareID), userID, fileIDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已移除文件"})
}

// DownloadZip GET /api/shares/:code/zip —— 分享文件打包下载。
// ids 为空下载全部根文件，非空下载选中项；公开路由，浏览器可直接导航原生下载。
func (h *ShareHandler) DownloadZip(c *gin.Context) {
	code := c.Param("code")
	password := c.Query("password")

	beforeWrite := func(fileName string) {
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename*=UTF-8''%s`, url.PathEscape(fileName)))
		c.Header("X-Accel-Buffering", "no") // 禁止边缘网关缓冲，下载框即时弹出
		if flusher, ok := c.Writer.(http.Flusher); ok {
			flusher.Flush() // 立即推送响应头，浏览器下载框先弹出
		}
	}

	var err error
	if idQuery := c.Query("ids"); idQuery != "" {
		ids, perr := parseIDList(idQuery)
		if perr != nil || len(ids) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要下载的文件"})
			return
		}
		err = h.shareService.DownloadSelected(code, password, ids, c.Writer, beforeWrite)
	} else {
		_, err = h.shareService.DownloadDir(code, password, c.Writer, beforeWrite)
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
}
