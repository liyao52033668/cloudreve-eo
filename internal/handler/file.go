package handler

import (
	"errors"
	"fmt"
	"io"
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
			resp := gin.H{
				"server_upload":  true,
				"storage_key":    key,
				"storage_policy": policy,
			}
			// 驱动支持分块中转时（百度/TeraBox），告知前端大文件走分块通道，
			// 避免整文件超过网关单次请求 body 上限（EdgeOne 为 6MB）。
			if driver, derr := h.fileService.GetDriver(policy); derr == nil {
				if _, ok := driver.(storage.ServerChunkedUploader); ok {
					resp["chunked"] = true
					resp["chunk_size"] = service.ServerChunkSize
				}
			}
			c.JSON(http.StatusOK, resp)
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

type chunkedInitRequest struct {
	FileName      string   `json:"file_name" binding:"required"`
	ContentType   string   `json:"content_type" binding:"required"`
	StorageKey    string   `json:"storage_key" binding:"required"`
	StoragePolicy string   `json:"storage_policy"`
	Size          int64    `json:"size" binding:"required"`
	ParentID      uint     `json:"parent_id"`
	BlockMD5s     []string `json:"block_md5s" binding:"required"`
}

// ChunkedInit POST /api/files/upload/chunked —— 初始化服务端中转分块上传（百度/TeraBox）。
func (h *FileHandler) ChunkedInit(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req chunkedInitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	contentType := req.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	session, err := h.fileService.InitChunkedUpload(userID, req.FileName, contentType, req.StorageKey, req.StoragePolicy, req.Size, req.ParentID, req.BlockMD5s)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": session})
}

// ChunkedUploadChunk POST /api/files/upload/chunked/chunk —— 上传单个块（multipart，单块 ≤5MB）。
func (h *FileHandler) ChunkedUploadChunk(c *gin.Context) {
	storageKey := c.PostForm("storage_key")
	storagePolicy := c.PostForm("storage_policy")
	uploadID := c.PostForm("upload_id") // Dropbox 首块时可为空（创建会话后经响应返回）
	partSeqStr := c.PostForm("part_seq")
	partSeq, err := strconv.Atoi(partSeqStr)
	if err != nil || partSeq < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少或无效的 part_seq"})
		return
	}
	if storageKey == "" || storagePolicy == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 storage_key/storage_policy"})
		return
	}
	file, err := c.FormFile("chunk")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未找到块数据"})
		return
	}
	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法打开块数据"})
		return
	}
	defer src.Close()
	data := make([]byte, file.Size)
	if _, err := src.Read(data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取块数据失败"})
		return
	}
	nextUploadID, err := h.fileService.UploadServerChunk(storageKey, storagePolicy, uploadID, partSeq, data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "upload_id": nextUploadID})
}

type chunkedCompleteRequest struct {
	StorageKey    string   `json:"storage_key" binding:"required"`
	StoragePolicy string   `json:"storage_policy" binding:"required"`
	UploadID      string   `json:"upload_id" binding:"required"`
	FileName      string   `json:"file_name" binding:"required"`
	ContentType   string   `json:"content_type"`
	Size          int64    `json:"size" binding:"required"`
	ParentID      uint     `json:"parent_id"`
	BlockMD5s     []string `json:"block_md5s" binding:"required"`
}

// ChunkedComplete POST /api/files/upload/chunked/complete —— 合并分块并创建文件记录。
func (h *FileHandler) ChunkedComplete(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req chunkedCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	contentType := req.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	file, err := h.fileService.CompleteServerChunkedUpload(
		userID, req.StorageKey, req.StoragePolicy, req.UploadID,
		req.FileName, contentType, req.Size, req.ParentID, req.BlockMD5s,
	)
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

// ProxyDownload GET /api/files/proxy —— 无外链直链存储（如 Filen）的服务端代理下载。
// URL 携带 HMAC 签名，无需登录态，签名校验通过后按策略读取并流式输出。
func (h *FileHandler) ProxyDownload(c *gin.Context) {
	policy := c.Query("policy")
	storageKey := c.Query("key")
	attachment := c.Query("name")
	exp := c.Query("exp")
	sig := c.Query("sig")
	if policy == "" || storageKey == "" || exp == "" || sig == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不完整"})
		return
	}

	// 解析 Range 请求头（大文件分段下载/断点续传）
	start, end, hasRange := parseProxyRange(c.GetHeader("Range"))

	rc, mime, size, ranged, err := h.fileService.ProxyRead(policy, storageKey, attachment, exp, sig, start, end)
	if err != nil {
		if errors.Is(err, storage.ErrRangeNotSatisfiable) {
			c.JSON(http.StatusRequestedRangeNotSatisfiable, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	defer rc.Close()

	if attachment != "" {
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename*=UTF-8''%s`, url.PathEscape(attachment)))
	} else {
		c.Header("Content-Disposition", "inline")
	}
	c.Header("Content-Type", mime)
	c.Header("Cache-Control", "no-store")
	c.Header("Accept-Ranges", "bytes")
	// 禁止边缘网关缓冲响应：否则整个文件缓冲完才发给浏览器，
	// 下载框延迟与文件大小成正比（平台文档：流式响应必须带此头）
	c.Header("X-Accel-Buffering", "no")

	status := http.StatusOK
	if hasRange && ranged && size >= 0 {
		// 驱动按段读取成功：206 + Content-Range。
		// end 超出文件大小时按 RFC 7233 截断到末尾。
		rangeEnd := end
		if rangeEnd < 0 || rangeEnd >= size {
			rangeEnd = size - 1
		}
		c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, rangeEnd, size))
		c.Header("Content-Length", fmt.Sprintf("%d", rangeEnd-start+1))
		status = http.StatusPartialContent
	} else if size >= 0 {
		c.Header("Content-Length", fmt.Sprintf("%d", size))
	}
	c.Status(status)
	// 立即冲刷状态与响应头：浏览器先收到 attachment 头、下载框即时弹出，
	// 不必等上游（百度等）返回第一个字节
	if flusher, ok := c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
	if _, err := io.Copy(c.Writer, rc); err != nil {
		// 已开始写响应体，无法再返回 JSON，仅记录
		return
	}
}

// parseProxyRange 解析 HTTP Range 头，仅支持单个范围（分段下载场景）。
// 支持 bytes=start-end / bytes=start- / bytes=-suffix 三种形式；
// 非法或无该头时 hasRange=false（回退整文件下载）。
func parseProxyRange(header string) (start, end int64, hasRange bool) {
	if header == "" {
		return 0, -1, false
	}
	spec := strings.TrimPrefix(strings.TrimSpace(header), "bytes=")
	spec = strings.TrimSpace(spec)
	if spec == "" || strings.Contains(spec, ",") {
		return 0, -1, false // 多范围不支持，回退整文件
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, -1, false
	}
	startStr, endStr := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if startStr == "" {
		// 后缀范围 bytes=-N：最后 N 字节。调用方需 size 才能换算，这里标记 start=-suffix
		suffix, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || suffix <= 0 {
			return 0, -1, false
		}
		return -suffix, -1, true
	}
	s, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil || s < 0 {
		return 0, -1, false
	}
	if endStr == "" {
		return s, -1, true
	}
	e, err := strconv.ParseInt(endStr, 10, 64)
	if err != nil || e < s {
		return 0, -1, false
	}
	return s, e, true
}

// DownloadDir GET /api/files/:id/zip —— 文件夹打包下载。
// 浏览器可直接导航到本 URL（?token= 鉴权），原生下载管理器接管流式下载。
func (h *FileHandler) DownloadDir(c *gin.Context) {
	userID := c.GetInt64("user_id")
	fileID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	// zip 流式输出：响应头必须在首次写入前设置（首次写入即 flush 头部，事后设置无效）
	_, err := h.fileService.DownloadDir(userID, uint(fileID), c.Writer, func(fileName string) {
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename*=UTF-8''%s`, url.PathEscape(fileName)))
		c.Header("X-Accel-Buffering", "no") // 禁止边缘网关缓冲，下载框即时弹出
		if flusher, ok := c.Writer.(http.Flusher); ok {
			flusher.Flush() // 立即推送响应头，浏览器下载框先弹出
		}
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
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

type batchRequest struct {
	IDs      []uint `json:"ids" binding:"required,min=1"`
	ParentID uint   `json:"parent_id"` // 批量移动目标文件夹；0 表示根目录
}

// BatchDelete POST /api/files/batch/delete —— 批量删除文件/文件夹（含后代）。
func (h *FileHandler) BatchDelete(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req batchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要删除的文件"})
		return
	}
	if err := h.fileService.BatchDelete(userID, req.IDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// BatchMove POST /api/files/batch/move —— 批量移动文件/文件夹到同一目标文件夹。
func (h *FileHandler) BatchMove(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req batchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要移动的文件"})
		return
	}
	if err := h.fileService.BatchMove(userID, req.IDs, req.ParentID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "移动成功"})
}

// BatchDownloadZip POST /api/files/batch/download —— 批量打包下载（zip 流式输出）。
// BatchDownloadZip GET /api/files/batch/download?ids=1,2,3 —— 批量打包下载。
// GET + ?token= 鉴权，浏览器可直接导航到本 URL，原生下载管理器接管流式下载。
func (h *FileHandler) BatchDownloadZip(c *gin.Context) {
	userID := c.GetInt64("user_id")
	ids, err := parseIDList(c.Query("ids"))
	if err != nil || len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要下载的文件"})
		return
	}
	// zip 流式输出：响应头必须在首次写入前设置（首次写入即 flush 头部，事后设置无效）
	_, err = h.fileService.BatchDownloadZip(userID, ids, c.Writer, func(fileName string) {
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename*=UTF-8''%s`, url.PathEscape(fileName)))
		c.Header("X-Accel-Buffering", "no") // 禁止边缘网关缓冲，下载框即时弹出
		if flusher, ok := c.Writer.(http.Flusher); ok {
			flusher.Flush() // 立即推送响应头，浏览器下载框先弹出
		}
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
}

// parseIDList 解析逗号分隔的文件 ID 列表（如 "1,2,3"）。
func parseIDList(s string) ([]uint, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var ids []uint
	for _, part := range strings.Split(s, ",") {
		id, err := strconv.ParseUint(strings.TrimSpace(part), 10, 32)
		if err != nil || id == 0 {
			return nil, fmt.Errorf("无效的文件 ID: %s", part)
		}
		ids = append(ids, uint(id))
	}
	return ids, nil
}
