package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cloudreve-eo/cloudreve-eo/internal/service"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
	"github.com/gin-gonic/gin"
)

type FileHandler struct {
	fileService *service.FileService
}

func NewFileHandler(fs *service.FileService) *FileHandler {
	return &FileHandler{fileService: fs}
}

func (h *FileHandler) List(c *gin.Context) {
	userID := c.GetInt64("user_id")
	policy := c.Query("policy")
	mimePrefix := c.Query("mime_prefix")

	// search 非空时跨全部目录搜索；policy 非空则限定在该存储策略内。
	if keyword := strings.TrimSpace(c.Query("search")); keyword != "" {
		files, err := h.fileService.SearchFiles(userID, keyword, policy)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"files": files})
		return
	}

	// mime_prefix 非空时跨目录返回指定 mime 类型的全部文件（图片/视频分类）
	if mimePrefix != "" {
		files, err := h.fileService.ListFilesByMimePrefix(userID, mimePrefix)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"files": files})
		return
	}

	// policy 非空时跨目录返回该策略下全部文件；否则按目录浏览。
	if policy != "" {
		files, err := h.fileService.ListFilesByPolicy(userID, policy)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"files": files})
		return
	}

	parentID, _ := strconv.ParseUint(c.Query("parent_id"), 10, 32)
	files, err := h.fileService.ListFiles(userID, uint(parentID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"files": files})
}

type mkdirRequest struct {
	ParentID uint   `json:"parent_id"`
	Name     string `json:"name" binding:"required"`
}

func (h *FileHandler) Mkdir(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req mkdirRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dir, err := h.fileService.Mkdir(userID, req.ParentID, req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"file": dir})
}

type uploadRequest struct {
	FileName    string `json:"file_name" binding:"required"`
	ContentType string `json:"content_type" binding:"required"`
	ParentID    uint   `json:"parent_id"`
}

func (h *FileHandler) Upload(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req uploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	url, key, policy, err := h.fileService.GetUploadURL(userID, req.FileName, req.ContentType)
	if err != nil {
		// 如果驱动不支持预签名 URL（如 GitHub），返回标志让前端用服务端上传
		if strings.Contains(err.Error(), "不支持客户端直传") || strings.Contains(err.Error(), "服务端上传") {
			c.JSON(http.StatusOK, gin.H{
				"server_upload":  true,
				"storage_key":    key,
				"storage_policy": policy,
			})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"upload_url":     url,
		"storage_key":    key,
		"storage_policy": policy,
	})
}

// UploadServer 服务端直接上传（用于 GitHub 等不支持预签名 URL 的存储）
func (h *FileHandler) UploadServer(c *gin.Context) {
	userID := c.GetInt64("user_id")

	// 读取文件
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未找到文件"})
		return
	}

	storageKey := c.PostForm("storage_key")
	storagePolicy := c.PostForm("storage_policy")
	contentType := c.PostForm("content_type")
	parentIDStr := c.PostForm("parent_id")
	var parentID uint
	if parentIDStr != "" {
		id, _ := strconv.ParseUint(parentIDStr, 10, 32)
		parentID = uint(id)
	}

	if storageKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 storage_key"})
		return
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 打开文件
	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法打开文件"})
		return
	}
	defer src.Close()

	// 读取文件内容
	content := make([]byte, file.Size)
	if _, err := src.Read(content); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取文件失败"})
		return
	}

	// 上传到存储并创建文件记录
	result, err := h.fileService.UploadServer(userID, parentID, file.Filename, storageKey, content, file.Size, contentType, storagePolicy)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"file": result})
}

type uploadCallbackRequest struct {
	FileName      string `json:"file_name" binding:"required"`
	StorageKey    string `json:"storage_key" binding:"required"`
	StoragePolicy string `json:"storage_policy"`
	Size          int64  `json:"size"`
	MimeType      string `json:"mime_type"`
	ParentID      uint   `json:"parent_id"`
}

func (h *FileHandler) UploadCallback(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req uploadCallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	file, err := h.fileService.UploadCallback(userID, req.ParentID, req.FileName, req.StorageKey, req.Size, req.MimeType, req.StoragePolicy)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"file": file})
}

type multipartInitRequest struct {
	FileName    string `json:"file_name" binding:"required"`
	ContentType string `json:"content_type" binding:"required"`
	Size        int64  `json:"size" binding:"required"`
	ParentID    uint   `json:"parent_id"`
}

// MultipartInit POST /api/files/upload/multipart —— 创建分片上传会话。
func (h *FileHandler) MultipartInit(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req multipartInitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	session, err := h.fileService.InitMultipartUpload(userID, req.FileName, req.ContentType, req.Size, req.ParentID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": session})
}

// MultipartSessions GET /api/files/upload/multipart/sessions —— 列出可续传的会话。
func (h *FileHandler) MultipartSessions(c *gin.Context) {
	userID := c.GetInt64("user_id")
	sessions, err := h.fileService.ListMultipartSessions(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sessions": sessions})
}

type multipartResumeRequest struct {
	StorageKey string `json:"storage_key" binding:"required"`
}

// MultipartResume POST /api/files/upload/multipart/resume —— 恢复会话：返回已传分片与新签名 URL。
func (h *FileHandler) MultipartResume(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req multipartResumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	session, err := h.fileService.ResumeMultipartUpload(userID, req.StorageKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": session})
}

type multipartCompleteRequest struct {
	FileName   string                  `json:"file_name" binding:"required"`
	StorageKey string                  `json:"storage_key" binding:"required"`
	UploadID   string                  `json:"upload_id" binding:"required"`
	Size       int64                   `json:"size" binding:"required"`
	MimeType   string                  `json:"mime_type"`
	ParentID   uint                    `json:"parent_id"`
	Parts      []storage.CompletedPart `json:"parts" binding:"required"`
}

// MultipartComplete POST /api/files/upload/multipart/complete —— 合并分片并落库。
func (h *FileHandler) MultipartComplete(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req multipartCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	file, err := h.fileService.CompleteMultipartUpload(userID, req.ParentID, req.FileName, req.StorageKey, req.UploadID, req.Size, req.MimeType, req.Parts)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"file": file})
}

type multipartAbortRequest struct {
	StorageKey string `json:"storage_key" binding:"required"`
	UploadID   string `json:"upload_id" binding:"required"`
}

// MultipartAbort POST /api/files/upload/multipart/abort —— 取消分片上传。
func (h *FileHandler) MultipartAbort(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req multipartAbortRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.fileService.AbortMultipartUpload(userID, req.StorageKey, req.UploadID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已取消"})
}

// ListStoragePolicies GET /api/storage/policies
func (h *FileHandler) ListStoragePolicies(c *gin.Context) {
	policies := h.fileService.ListStoragePolicies()
	defaultName := ""
	for _, p := range policies {
		if p.IsDefault {
			defaultName = p.Name
			break
		}
	}
	c.JSON(http.StatusOK, gin.H{"policies": policies, "default": defaultName})
}

func (h *FileHandler) Download(c *gin.Context) {
	userID := c.GetInt64("user_id")
	fileID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	preview := c.Query("preview") == "1"

	url, err := h.fileService.GetDownloadURL(userID, uint(fileID), preview)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"download_url": url})
}

// DownloadDir GET /api/files/:id/zip —— 文件夹打包下载。
func (h *FileHandler) DownloadDir(c *gin.Context) {
	userID := c.GetInt64("user_id")
	fileID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	fileName, err := h.fileService.DownloadDir(userID, uint(fileID), c.Writer)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename*=UTF-8''%s`, url.PathEscape(fileName)))
}

func (h *FileHandler) Delete(c *gin.Context) {
	userID := c.GetInt64("user_id")
	fileID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	if err := h.fileService.Delete(userID, uint(fileID)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

type renameRequest struct {
	Name string `json:"name" binding:"required"`
}

func (h *FileHandler) Rename(c *gin.Context) {
	userID := c.GetInt64("user_id")
	fileID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var req renameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.fileService.Rename(userID, uint(fileID), req.Name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "重命名成功"})
}

type moveRequest struct {
	ParentID uint `json:"parent_id"`
}

func (h *FileHandler) Move(c *gin.Context) {
	userID := c.GetInt64("user_id")
	fileID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var req moveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.fileService.Move(userID, uint(fileID), req.ParentID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "移动成功"})
}
