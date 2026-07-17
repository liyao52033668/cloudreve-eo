package handler

import (
	"net/http"
	"strconv"

	"github.com/cloudreve-eo/cloudreve-eo/internal/service"
	"github.com/gin-gonic/gin"
)

type FileHandler struct {
	fileService *service.FileService
}

func NewFileHandler(fs *service.FileService) *FileHandler {
	return &FileHandler{fileService: fs}
}

func (h *FileHandler) List(c *gin.Context) {
	userID := c.GetUint("user_id")
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
	userID := c.GetUint("user_id")
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
	userID := c.GetUint("user_id")
	var req uploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	url, key, err := h.fileService.GetUploadURL(userID, req.FileName, req.ContentType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"upload_url": url, "storage_key": key})
}

type uploadCallbackRequest struct {
	FileName   string `json:"file_name" binding:"required"`
	StorageKey string `json:"storage_key" binding:"required"`
	Size       int64  `json:"size" binding:"required"`
	MimeType   string `json:"mime_type"`
	ParentID   uint   `json:"parent_id"`
}

func (h *FileHandler) UploadCallback(c *gin.Context) {
	userID := c.GetUint("user_id")
	var req uploadCallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	file, err := h.fileService.UploadCallback(userID, req.ParentID, req.FileName, req.StorageKey, req.Size, req.MimeType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"file": file})
}

func (h *FileHandler) Download(c *gin.Context) {
	userID := c.GetUint("user_id")
	fileID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	url, err := h.fileService.GetDownloadURL(userID, uint(fileID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"download_url": url})
}

func (h *FileHandler) Delete(c *gin.Context) {
	userID := c.GetUint("user_id")
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
	userID := c.GetUint("user_id")
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
	ParentID uint `json:"parent_id" binding:"required"`
}

func (h *FileHandler) Move(c *gin.Context) {
	userID := c.GetUint("user_id")
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
