package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/logx"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/service"
	"golang.org/x/net/webdav"
	"gorm.io/gorm"
)

// WebDAVHandler 对外提供 WebDAV 协议服务，让第三方客户端（Rclone、Windows 资源管理器等）
// 可以挂载访问用户的云盘文件。
type WebDAVHandler struct {
	fileService *service.FileService
}

func NewWebDAVHandler(fs *service.FileService) *WebDAVHandler {
	return &WebDAVHandler{fileService: fs}
}

// ServeHTTP 处理 WebDAV 请求。
// OPTIONS 不需要认证（WebDAV 标准探测），其余请求需 Basic Auth。
func (h *WebDAVHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// panic 兜底：任何内部 panic 都不能崩溃整个云函数（EdgeOne 会直接 500 页面），
	// 改为记录日志并返回 500 JSON，保证同实例其它请求不受影响。
	defer func() {
		if rec := recover(); rec != nil {
			logx.Error(logx.ModuleApp, "WebDAV handler panic",
				"method", r.Method,
				"path", r.URL.Path,
				"panic", fmt.Sprint(rec),
			)
			http.Error(w, "WebDAV 处理内部错误", http.StatusInternalServerError)
		}
	}()

	// 检查 WebDAV 全局开关
	enabled, err := model.IsWebDAVEnabled()
	if err != nil {
		logx.Error(logx.ModuleApp, "检查 WebDAV 开关失败", logx.Err(err))
		http.Error(w, "服务内部错误", http.StatusInternalServerError)
		return
	}
	if !enabled {
		http.Error(w, "WebDAV 服务未启用", http.StatusServiceUnavailable)
		return
	}

	// OPTIONS 是 WebDAV 客户端探测请求，无需认证即可返回能力头
	if r.Method == "OPTIONS" {
		w.Header().Set("Allow", "OPTIONS, LOCK, GET, HEAD, POST, DELETE, PROPPATCH, COPY, MOVE, UNLOCK, PROPFIND, PUT, MKCOL")
		w.Header().Set("DAV", "1, 2")
		w.Header().Set("MS-Author-Via", "DAV")
		return
	}

	// Basic Auth 认证
	username, password, ok := r.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="Cloudreve-EO WebDAV"`)
		http.Error(w, "需要认证", http.StatusUnauthorized)
		return
	}

	// 查找用户
	user, err := model.GetUserByUsername(username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, "用户名或密码错误", http.StatusUnauthorized)
			return
		}
		logx.Error(logx.ModuleApp, "查询用户失败", logx.Err(err))
		http.Error(w, "服务内部错误", http.StatusInternalServerError)
		return
	}

	// 验证 WebDAV 密码
	valid, err := model.VerifyWebDAVPassword(user.ID, password)
	if err != nil {
		logx.Error(logx.ModuleApp, "验证 WebDAV 密码失败", logx.Err(err))
		http.Error(w, "服务内部错误", http.StatusInternalServerError)
		return
	}
	if !valid {
		http.Error(w, "用户名或密码错误", http.StatusUnauthorized)
		return
	}

	// 检查用户是否被封禁
	if user.Banned {
		http.Error(w, "用户已被封禁", http.StatusForbidden)
		return
	}

	// 创建用户专属的 FileSystem
	fs := &userFileSystem{
		userID:      user.ID,
		fileService: h.fileService,
	}

	// 创建 WebDAV handler
	davHandler := &webdav.Handler{
		Prefix:     "/dav",
		FileSystem: fs,
		LockSystem: webdav.NewMemLS(),
		Logger: func(r *http.Request, err error) {
			if err != nil {
				logx.Warn(logx.ModuleApp, "WebDAV 请求错误",
					"method", r.Method,
					"path", r.URL.Path,
					"user", username,
					logx.Err(err),
				)
			}
		},
	}

	davHandler.ServeHTTP(w, r)
}

// userFileSystem 实现 webdav.FileSystem 接口，将 WebDAV 路径映射到用户的文件树。
type userFileSystem struct {
	userID      int64
	fileService *service.FileService
}

// resolvePath 将 WebDAV 路径解析为文件记录。
// 路径格式：/dir1/dir2/file.txt -> 逐级查找 parent_id 链
func (fs *userFileSystem) resolvePath(ctx context.Context, name string) (*model.File, error) {
	name = strings.Trim(name, "/")
	if name == "" {
		// 根目录，返回一个虚拟的文件记录
		return &model.File{
			ID:     0,
			UserID: fs.userID,
			Name:   "",
			IsDir:  true,
		}, nil
	}

	parts := strings.Split(name, "/")
	var parentID uint = 0

	for i, part := range parts {
		var file model.File
		err := model.DB.Where("user_id = ? AND parent_id = ? AND name = ?", fs.userID, parentID, part).First(&file).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, os.ErrNotExist
			}
			return nil, err
		}
		if i == len(parts)-1 {
			return &file, nil
		}
		if !file.IsDir {
			return nil, os.ErrNotExist
		}
		parentID = file.ID
	}
	return nil, os.ErrNotExist
}

// resolveParent 解析父目录 ID。
func (fs *userFileSystem) resolveParent(ctx context.Context, name string) (uint, error) {
	name = strings.Trim(name, "/")
	if name == "" {
		return 0, nil
	}

	dir := filepath.Dir(name)
	if dir == "." || dir == "" {
		return 0, nil
	}

	parts := strings.Split(dir, "/")
	var parentID uint = 0

	for _, part := range parts {
		var file model.File
		err := model.DB.Where("user_id = ? AND parent_id = ? AND name = ?", fs.userID, parentID, part).First(&file).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return 0, os.ErrNotExist
			}
			return 0, err
		}
		if !file.IsDir {
			return 0, os.ErrNotExist
		}
		parentID = file.ID
	}
	return parentID, nil
}

// Mkdir 创建目录。
func (fs *userFileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	name = strings.Trim(name, "/")
	if name == "" {
		return os.ErrExist
	}

	parentID, err := fs.resolveParent(ctx, name)
	if err != nil {
		return err
	}

	dirName := filepath.Base(name)

	// 检查是否已存在
	var existing model.File
	err = model.DB.Where("user_id = ? AND parent_id = ? AND name = ?", fs.userID, parentID, dirName).First(&existing).Error
	if err == nil {
		return os.ErrExist
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	_, err = fs.fileService.Mkdir(fs.userID, parentID, dirName)
	return err
}

// OpenFile 打开文件。
func (fs *userFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	name = strings.Trim(name, "/")

	// 检查文件是否存在
	file, err := fs.resolvePath(ctx, name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	// 如果是创建模式且文件不存在
	if flag&(os.O_CREATE|os.O_EXCL) == (os.O_CREATE | os.O_EXCL) {
		if err == nil {
			return nil, os.ErrExist
		}
		// 文件不存在，创建新文件
		return fs.createFile(ctx, name)
	}

	// 如果文件不存在
	if errors.Is(err, os.ErrNotExist) {
		if flag&os.O_CREATE != 0 {
			return fs.createFile(ctx, name)
		}
		return nil, os.ErrNotExist
	}

	// 文件存在
	if file.IsDir {
		return &dirFile{file: file, fs: fs}, nil
	}

	// 如果是写模式
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_TRUNC) != 0 {
		return &writableFile{file: file, fs: fs, flag: flag}, nil
	}

	// 只读模式
	return &readableFile{file: file, fs: fs}, nil
}

// createFile 创建新文件（用于 PUT 上传）。
func (fs *userFileSystem) createFile(ctx context.Context, name string) (webdav.File, error) {
	parentID, err := fs.resolveParent(ctx, name)
	if err != nil {
		return nil, err
	}

	fileName := filepath.Base(name)

	// 解析存储策略
	policy, err := fs.fileService.ResolveUserPolicy(fs.userID)
	if err != nil {
		return nil, err
	}

	// 生成 storage key
	storageKey, err := fs.fileService.BuildStorageKey(fs.userID, policy, fileName)
	if err != nil {
		return nil, err
	}

	return &writableFile{
		file: &model.File{
			UserID:        fs.userID,
			ParentID:      parentID,
			Name:          fileName,
			IsDir:         false,
			StorageKey:    storageKey,
			StoragePolicy: policy,
		},
		fs:   fs,
		flag: os.O_CREATE | os.O_WRONLY,
	}, nil
}

// RemoveAll 删除文件或目录。
func (fs *userFileSystem) RemoveAll(ctx context.Context, name string) error {
	name = strings.Trim(name, "/")
	if name == "" {
		return errors.New("不能删除根目录")
	}

	file, err := fs.resolvePath(ctx, name)
	if err != nil {
		return err
	}

	return fs.fileService.Delete(fs.userID, file.ID)
}

// Rename 重命名/移动文件或目录。
func (fs *userFileSystem) Rename(ctx context.Context, oldName, newName string) error {
	oldName = strings.Trim(oldName, "/")
	newName = strings.Trim(newName, "/")

	if oldName == "" || newName == "" {
		return errors.New("无效路径")
	}

	oldFile, err := fs.resolvePath(ctx, oldName)
	if err != nil {
		return err
	}

	newParentID, err := fs.resolveParent(ctx, newName)
	if err != nil {
		return err
	}

	newNameBase := filepath.Base(newName)

	// 如果父目录相同，只是重命名
	if oldFile.ParentID == newParentID {
		return fs.fileService.Rename(fs.userID, oldFile.ID, newNameBase)
	}

	// 否则是移动
	return fs.fileService.Move(fs.userID, oldFile.ID, newParentID)
}

// Stat 获取文件/目录信息。
func (fs *userFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	name = strings.Trim(name, "/")

	file, err := fs.resolvePath(ctx, name)
	if err != nil {
		return nil, err
	}

	return &fileInfo{file: file}, nil
}

// fileInfo 实现 os.FileInfo 接口。
type fileInfo struct {
	file *model.File
}

func (fi *fileInfo) Name() string {
	if fi.file.Name == "" {
		return "/"
	}
	return fi.file.Name
}

func (fi *fileInfo) Size() int64 {
	return fi.file.Size
}

func (fi *fileInfo) Mode() os.FileMode {
	if fi.file.IsDir {
		return 0755 | os.ModeDir
	}
	return 0644
}

func (fi *fileInfo) ModTime() time.Time {
	return fi.file.UpdatedAt
}

func (fi *fileInfo) IsDir() bool {
	return fi.file.IsDir
}

func (fi *fileInfo) Sys() interface{} {
	return nil
}

// readableFile 只读文件。
type readableFile struct {
	file   *model.File
	fs     *userFileSystem
	reader io.ReadCloser
	offset int64
}

func (f *readableFile) Close() error {
	if f.reader != nil {
		return f.reader.Close()
	}
	return nil
}

func (f *readableFile) Read(p []byte) (int, error) {
	if f.reader == nil {
		driver, err := f.fs.fileService.GetDriver(f.file.StoragePolicy)
		if err != nil {
			return 0, err
		}
		rc, err := driver.Read(f.file.StorageKey)
		if err != nil {
			return 0, err
		}
		f.reader = rc
	}
	return f.reader.Read(p)
}

func (f *readableFile) Seek(offset int64, whence int) (int64, error) {
	// 简单实现：重新打开流
	if f.reader != nil {
		f.reader.Close()
		f.reader = nil
	}
	driver, err := f.fs.fileService.GetDriver(f.file.StoragePolicy)
	if err != nil {
		return 0, err
	}
	rc, err := driver.Read(f.file.StorageKey)
	if err != nil {
		return 0, err
	}
	f.reader = rc
	f.offset = offset
	return offset, nil
}

func (f *readableFile) Readdir(count int) ([]os.FileInfo, error) {
	if !f.file.IsDir {
		return nil, errors.New("不是目录")
	}

	files, err := f.fs.fileService.ListFiles(f.fs.userID, f.file.ID)
	if err != nil {
		return nil, err
	}

	infos := make([]os.FileInfo, 0, len(files))
	for _, file := range files {
		infos = append(infos, &fileInfo{file: &file})
	}
	return infos, nil
}

func (f *readableFile) Stat() (os.FileInfo, error) {
	return &fileInfo{file: f.file}, nil
}

func (f *readableFile) Write(p []byte) (int, error) {
	return 0, errors.New("只读文件")
}

// writableFile 可写文件（用于 PUT 上传）。
type writableFile struct {
	file   *model.File
	fs     *userFileSystem
	flag   int
	buffer []byte
}

// maxWebDAVUploadSize WebDAV PUT 上传大小限制。
// EdgeOne 云函数网关限制单次请求 body ≤6MB，留 1MB 余量给 HTTP 头。
// 超过此大小的文件无法通过 WebDAV 上传，需用网页端（已实现分片上传）。
const maxWebDAVUploadSize = 5 << 20 // 5MB

func (f *writableFile) Close() error {
	// 检查文件大小
	if int64(len(f.buffer)) > maxWebDAVUploadSize {
		return fmt.Errorf("文件过大（%d 字节），WebDAV 上传限制 %d 字节。请使用网页端上传大文件", len(f.buffer), maxWebDAVUploadSize)
	}

	// 如果有数据，上传到存储
	if len(f.buffer) > 0 {
		driver, err := f.fs.fileService.GetDriver(f.file.StoragePolicy)
		if err != nil {
			return err
		}

		// 上传到存储
		if err := driver.UploadFile(f.file.StorageKey, f.buffer); err != nil {
			return err
		}

		// 创建文件记录
		mimeType := "application/octet-stream"
		ext := strings.ToLower(filepath.Ext(f.file.Name))
		if ext == ".txt" || ext == ".md" || ext == ".json" {
			mimeType = "text/plain"
		} else if ext == ".html" || ext == ".htm" {
			mimeType = "text/html"
		} else if ext == ".jpg" || ext == ".jpeg" {
			mimeType = "image/jpeg"
		} else if ext == ".png" {
			mimeType = "image/png"
		}

		_, err = f.fs.fileService.UploadCallback(
			f.fs.userID,
			f.file.ParentID,
			f.file.Name,
			f.file.StorageKey,
			int64(len(f.buffer)),
			mimeType,
			f.file.StoragePolicy,
		)
		if err != nil {
			// 回滚：删除存储对象
			_ = driver.Delete(f.file.StorageKey)
			return err
		}
	}
	return nil
}

func (f *writableFile) Read(p []byte) (int, error) {
	return 0, errors.New("不支持读取")
}

func (f *writableFile) Readdir(count int) ([]os.FileInfo, error) {
	return nil, errors.New("不是目录")
}

func (f *writableFile) Seek(offset int64, whence int) (int64, error) {
	return 0, errors.New("不支持 Seek")
}

func (f *writableFile) Stat() (os.FileInfo, error) {
	return &fileInfo{file: f.file}, nil
}

func (f *writableFile) Write(p []byte) (int, error) {
	f.buffer = append(f.buffer, p...)
	return len(p), nil
}

// dirFile 目录文件。
type dirFile struct {
	file *model.File
	fs   *userFileSystem
}

func (f *dirFile) Close() error {
	return nil
}

func (f *dirFile) Read(p []byte) (int, error) {
	return 0, errors.New("不能读取目录")
}

func (f *dirFile) Readdir(count int) ([]os.FileInfo, error) {
	files, err := f.fs.fileService.ListFiles(f.fs.userID, f.file.ID)
	if err != nil {
		return nil, err
	}

	infos := make([]os.FileInfo, 0, len(files))
	for _, file := range files {
		infos = append(infos, &fileInfo{file: &file})
	}
	return infos, nil
}

func (f *dirFile) Seek(offset int64, whence int) (int64, error) {
	return 0, errors.New("不支持 Seek")
}

func (f *dirFile) Stat() (os.FileInfo, error) {
	return &fileInfo{file: f.file}, nil
}

func (f *dirFile) Write(p []byte) (int, error) {
	return 0, errors.New("不能写入目录")
}
