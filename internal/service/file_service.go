package service

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type FileService struct {
	storageMgr *storage.StoragePolicyManager
}

func NewFileService(mgr *storage.StoragePolicyManager) *FileService {
	return &FileService{storageMgr: mgr}
}

// GetDriver 按策略名取驱动（handler 层探测驱动能力用）。
func (s *FileService) GetDriver(policy string) (storage.StorageDriver, error) {
	return s.storageMgr.GetDriver(policy)
}

// buildStorageKey 生成对象键：{basePath/}userID/uuid{.ext}，保留原文件扩展名便于在存储桶中识别与预览。
func (s *FileService) buildStorageKey(userID int64, policy string, fileName string) (string, error) {
	ext := strings.ToLower(filepath.Ext(fileName))
	if len(ext) > 16 || strings.ContainsAny(ext, "/\\?#%") {
		ext = ""
	}
	// userID 已经是雪花 ID
	key := fmt.Sprintf("%d/%s%s", userID, uuid.New().String(), ext)
	if info, ok := s.storageMgr.GetPolicyInfo(policy); ok && info.BasePath != "" {
		key = strings.Trim(info.BasePath, "/") + "/" + key
	}
	return key, nil
}

func (s *FileService) ListFiles(userID int64, parentID uint) ([]model.File, error) {
	var files []model.File
	err := model.DB.Where("user_id = ? AND parent_id = ?", userID, parentID).
		Order("is_dir DESC, name ASC").
		Find(&files).Error
	return files, err
}

// ListFilesByPolicy 跨目录列出用户在某存储策略下的全部文件（不含文件夹）。
func (s *FileService) ListFilesByPolicy(userID int64, policy string) ([]model.File, error) {
	var files []model.File
	err := model.DB.Where("user_id = ? AND storage_policy = ? AND is_dir = ?", userID, policy, false).
		Order("name ASC").
		Find(&files).Error
	return files, err
}

// ListFilesByMimePrefix 跨目录列出用户在指定 mime 类型前缀下的全部文件（如 image/% 或 video/%）。
func (s *FileService) ListFilesByMimePrefix(userID int64, mimePrefix string) ([]model.File, error) {
	var files []model.File
	err := model.DB.Where("user_id = ? AND is_dir = ? AND mime_type LIKE ?", userID, false, mimePrefix+"%").
		Order("updated_at DESC").
		Find(&files).Error
	return files, err
}

// SearchFiles 按文件名跨全部目录搜索用户文件；policy 非空时限定存储策略。
func (s *FileService) SearchFiles(userID int64, keyword string, policy string) ([]model.File, error) {
	query := model.DB.Where("user_id = ? AND is_dir = ?", userID, false)
	if policy != "" {
		query = query.Where("storage_policy = ?", policy)
	}

	var files []model.File
	err := query.Where("LOWER(name) LIKE ?", "%"+strings.ToLower(strings.TrimSpace(keyword))+"%").
		Order("name ASC").
		Find(&files).Error
	return files, err
}

func (s *FileService) Mkdir(userID int64, parentID uint, name string) (*model.File, error) {
	dir := &model.File{
		UserID:   userID,
		ParentID: parentID,
		Name:     name,
		IsDir:    true,
	}
	if err := model.DB.Create(dir).Error; err != nil {
		return nil, err
	}
	return dir, nil
}

// ResolveUserPolicy 解析用户上传使用的存储策略：取用户所属用户组绑定的策略。
// 多策略时随机挑选一个仍有剩余配额的策略；都满则返回配额不足。
func (s *FileService) ResolveUserPolicy(userID int64) (string, error) {
	group, err := s.UserGroupOf(userID)
	if err != nil {
		return "", err
	}
	names := group.PolicyNames()
	if len(names) == 0 {
		// 空列表表示跟随默认策略
		return s.storageMgr.ResolvePolicy("")
	}

	// 打乱顺序，实现随机挑选
	rand.Shuffle(len(names), func(i, j int) { names[i], names[j] = names[j], names[i] })

	var lastErr error
	for _, name := range names {
		resolved, err := s.storageMgr.ResolvePolicy(name)
		if err != nil {
			lastErr = err
			continue
		}
		// size=0 仅检查当前策略是否还有空间（已满则跳过）
		if err := s.checkQuota(userID, resolved, 0); err != nil {
			lastErr = err
			continue
		}
		return resolved, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", errors.New("用户组没有可用策略")
}

// UserGroupOf 返回用户所属的用户组；未分组或组不存在时返回默认组（仍无则报错）。
func (s *FileService) UserGroupOf(userID int64) (*model.UserGroup, error) {
	user, err := model.GetUserByID(userID)
	if err != nil {
		return nil, errors.New("用户不存在")
	}
	if user.GroupID != 0 {
		group, err := model.GetUserGroupByID(user.GroupID)
		if err == nil {
			return group, nil
		}
	}
	group, err := model.GetDefaultGroup()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("未配置用户组，请管理员先添加用户组")
		}
		return nil, err
	}
	return group, nil
}

// GetUploadURL 生成上传预签名 URL。存储策略由用户所属用户组决定。
// 返回 uploadURL, storageKey, resolvedPolicy。
func (s *FileService) GetUploadURL(userID int64, fileName string, contentType string) (string, string, string, error) {
	resolved, err := s.ResolveUserPolicy(userID)
	if err != nil {
		return "", "", "", err
	}
	driver, err := s.storageMgr.GetDriver(resolved)
	if err != nil {
		return "", "", "", err
	}

	key, err := s.buildStorageKey(userID, resolved, fileName)
	if err != nil {
		return "", "", "", err
	}
	url, err := driver.GenerateUploadURL(key, contentType, 30*time.Minute)
	if err != nil {
		// 即使生成 URL 失败，也返回 key 和 policy，以便服务端上传使用
		return "", key, resolved, err
	}
	return url, key, resolved, nil
}

// resolveCallbackPolicy 确定回调落库的存储策略。
// 上传时对象实际落地的策略才是权威来源；若调用方传入则校验后直接采用，
// 避免多策略随机挑选导致文件记录与对象实际位置不一致（进而预览/下载损坏）。
// policy 为空时回退到按用户组重新挑选（兼容旧行为）。
func (s *FileService) resolveCallbackPolicy(userID int64, policy string) (string, error) {
	if policy == "" {
		return s.ResolveUserPolicy(userID)
	}
	return s.storageMgr.ResolvePolicy(policy)
}

// UploadCallback 写入文件记录。policy 为上传时对象实际落地的存储策略，
// 传入则直接采用，保证记录与实际存储位置一致。
func (s *FileService) UploadCallback(userID int64, parentID uint, fileName, storageKey string, size int64, mimeType string, policy string) (*model.File, error) {
	resolved, err := s.resolveCallbackPolicy(userID, policy)
	if err != nil {
		return nil, err
	}

	if size < 0 {
		return nil, errors.New("文件大小无效")
	}
	if err := s.checkQuota(userID, resolved, size); err != nil {
		return nil, err
	}

	file := &model.File{
		UserID:        userID,
		ParentID:      parentID,
		Name:          fileName,
		IsDir:         false,
		Size:          size,
		MimeType:      mimeType,
		StorageKey:    storageKey,
		StoragePolicy: resolved,
	}

	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(file).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.User{}).Where("id = ?", userID).
			Update("storage_used", gorm.Expr("storage_used + ?", size)).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return file, nil
}

// UploadServer 服务端直接上传(用于 GitHub 等不支持预签名 URL 的存储)。
// policy 为上传入口已解析的落地策略，传入则沿用，保证与 storage_key 生成时的策略一致。
func (s *FileService) UploadServer(userID int64, parentID uint, fileName, storageKey string, content []byte, size int64, mimeType string, policy string) (*model.File, error) {
	resolved, err := s.resolveCallbackPolicy(userID, policy)
	if err != nil {
		return nil, err
	}

	if size < 0 {
		return nil, errors.New("文件大小无效")
	}
	if err := s.checkQuota(userID, resolved, size); err != nil {
		return nil, err
	}

	driver, err := s.storageMgr.GetDriver(resolved)
	if err != nil {
		return nil, err
	}

	// 上传到存储
	if err := driver.UploadFile(storageKey, content); err != nil {
		return nil, fmt.Errorf("上传到存储失败: %w", err)
	}

	// 创建文件记录
	file := &model.File{
		UserID:        userID,
		ParentID:      parentID,
		Name:          fileName,
		IsDir:         false,
		Size:          size,
		MimeType:      mimeType,
		StorageKey:    storageKey,
		StoragePolicy: resolved,
	}

	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(file).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.User{}).Where("id = ?", userID).
			Update("storage_used", gorm.Expr("storage_used + ?", size)).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		// 上传成功但数据库失败,尝试删除文件
		_ = driver.Delete(storageKey)
		return nil, err
	}
	return file, nil
}

// ServerChunkSize 服务端中转分块上传的块大小。
// EdgeOne 网关限制单次请求 body ≤ 6MB，取 5MB 留出 multipart 头开销余量。
const ServerChunkSize = 5 << 20

// ServerChunkedSession 服务端中转分块上传会话（百度/TeraBox 等）。
// 无状态设计：upload_id 由客户端持有并在每块/complete 请求中携带，
// 不写 DB 会话（避免污染"未完成上传"列表，且该协议不支持按块查询恢复）。
type ServerChunkedSession struct {
	UploadID   string `json:"upload_id"`
	StorageKey string `json:"storage_key"`
	ChunkSize  int64  `json:"chunk_size"`
	FastUpload bool   `json:"fast_upload"` // 秒传命中，无需传块
}

// InitChunkedUpload 初始化服务端中转分块上传。
// storageKey/policy 沿用上传入口（getUploadURL）已解析的结果，保证策略与 key 一致；
// blockMD5s 为客户端按 chunk_size 切块计算的各块 MD5。
func (s *FileService) InitChunkedUpload(userID int64, fileName, contentType, storageKey, policy string, size int64, parentID uint, blockMD5s []string) (*ServerChunkedSession, error) {
	if size < 0 {
		return nil, errors.New("文件大小无效")
	}
	if storageKey == "" {
		return nil, errors.New("缺少 storage_key")
	}
	partCount := int((size + ServerChunkSize - 1) / ServerChunkSize)
	if partCount == 0 {
		partCount = 1
	}
	if len(blockMD5s) != partCount {
		return nil, fmt.Errorf("块 MD5 数量不匹配：期望 %d 个，收到 %d 个", partCount, len(blockMD5s))
	}
	resolved, err := s.resolveCallbackPolicy(userID, policy)
	if err != nil {
		return nil, err
	}
	if err := s.checkQuota(userID, resolved, size); err != nil {
		return nil, err
	}
	driver, err := s.storageMgr.GetDriver(resolved)
	if err != nil {
		return nil, err
	}
	uploader, ok := driver.(storage.ServerChunkedUploader)
	if !ok {
		return nil, errors.New("该存储不支持分块上传")
	}

	uploadID, fast, err := uploader.InitChunkedUpload(storageKey, size, blockMD5s)
	if err != nil {
		return nil, fmt.Errorf("预创建上传失败: %w", err)
	}
	if fast {
		// 秒传命中：直接创建文件记录，无需传块
		if _, err := s.createFileRecord(userID, parentID, fileName, storageKey, size, contentType, resolved, driver); err != nil {
			return nil, err
		}
	}
	return &ServerChunkedSession{
		UploadID:   uploadID,
		StorageKey: storageKey,
		ChunkSize:  ServerChunkSize,
		FastUpload: fast,
	}, nil
}

// createFileRecord 创建文件记录并增加用户已用存储（上传成功后落库共用）。
func (s *FileService) createFileRecord(userID int64, parentID uint, fileName, storageKey string, size int64, mimeType, policy string, driver storage.StorageDriver) (*model.File, error) {
	file := &model.File{
		UserID:        userID,
		ParentID:      parentID,
		Name:          fileName,
		IsDir:         false,
		Size:          size,
		MimeType:      mimeType,
		StorageKey:    storageKey,
		StoragePolicy: policy,
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(file).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.User{}).Where("id = ?", userID).
			Update("storage_used", gorm.Expr("storage_used + ?", size)).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		// 落库失败，尝试删除存储端文件避免孤儿对象
		_ = driver.Delete(storageKey)
		return nil, err
	}
	return file, nil
}

// UploadServerChunk 上传单个块（服务端转发给存储端）。
// 无状态：upload_id 由客户端携带；返回后续块应继续使用的 upload_id
//（Dropbox 首块创建会话后经此返回真实会话 ID）。
// 单块 ≤5MB，满足网关单次请求 body ≤6MB 的限制。
func (s *FileService) UploadServerChunk(storageKey, policy, uploadID string, partSeq int, data []byte) (string, error) {
	if len(data) > ServerChunkSize {
		return "", fmt.Errorf("块大小 %d 超过上限 %d", len(data), ServerChunkSize)
	}
	// uploadID 允许为空：Dropbox 首块时创建会话；其余驱动 Init 已返回会话 ID。
	driver, err := s.storageMgr.GetDriver(policy)
	if err != nil {
		return "", err
	}
	uploader, ok := driver.(storage.ServerChunkedUploader)
	if !ok {
		return "", errors.New("该存储不支持分块上传")
	}
	offset := int64(partSeq) * ServerChunkSize
	return uploader.UploadChunk(storageKey, uploadID, partSeq, offset, data)
}

// CompleteServerChunkedUpload 合并分块并创建文件记录。
// 无状态：upload_id 与文件元数据由客户端携带。
func (s *FileService) CompleteServerChunkedUpload(
	userID int64, storageKey, policy, uploadID string,
	fileName, contentType string, size int64, parentID uint,
	blockMD5s []string,
) (*model.File, error) {
	if uploadID == "" {
		return nil, errors.New("缺少 upload_id")
	}
	partCount := int((size + ServerChunkSize - 1) / ServerChunkSize)
	if partCount == 0 {
		partCount = 1
	}
	if len(blockMD5s) != partCount {
		return nil, fmt.Errorf("块 MD5 数量不匹配：期望 %d 个，收到 %d 个", partCount, len(blockMD5s))
	}
	driver, err := s.storageMgr.GetDriver(policy)
	if err != nil {
		return nil, err
	}
	uploader, ok := driver.(storage.ServerChunkedUploader)
	if !ok {
		return nil, errors.New("该存储不支持分块上传")
	}
	if err := uploader.CompleteChunkedUpload(storageKey, uploadID, size, blockMD5s); err != nil {
		return nil, fmt.Errorf("合并上传失败: %w", err)
	}
	return s.createFileRecord(userID, parentID, fileName, storageKey, size, contentType, policy, driver)
}

// checkQuota 校验用户新增 size 字节后是否超出配额。
// 若用户组 MaxStorage > 0，按组总容量校验（跨策略合计已用）；
// 若 MaxStorage == 0，按当前策略 default_quota 校验该策略已用。
func (s *FileService) checkQuota(userID int64, resolvedPolicy string, size int64) error {
	info, ok := s.storageMgr.GetPolicyInfo(resolvedPolicy)
	if !ok {
		return fmt.Errorf("存储策略 %s 不存在", resolvedPolicy)
	}
	group, err := s.UserGroupOf(userID)
	if err != nil {
		return err
	}

	// 组级总容量优先
	if group.MaxStorage > 0 {
		var used int64
		if err := model.DB.Model(&model.File{}).
			Where("user_id = ? AND is_dir = ?", userID, false).
			Select("COALESCE(SUM(size), 0)").Scan(&used).Error; err != nil {
			return fmt.Errorf("统计已用容量失败: %w", err)
		}
		if used+size > group.MaxStorage {
			return errors.New("存储配额不足")
		}
		return nil
	}

	// 未设组容量：使用该策略默认配额；若策略配额也为 0 视为不限
	quota := info.DefaultQuota
	if quota <= 0 {
		return nil
	}
	var used int64
	if err := model.DB.Model(&model.File{}).
		Where("user_id = ? AND storage_policy = ? AND is_dir = ?", userID, resolvedPolicy, false).
		Select("COALESCE(SUM(size), 0)").Scan(&used).Error; err != nil {
		return fmt.Errorf("统计已用容量失败: %w", err)
	}
	if used+size > quota {
		return errors.New("存储配额不足")
	}
	return nil
}

// DefaultMultipartChunkSize 默认分片大小（字节），与 Cloudreve S3 策略默认值一致。
// 策略可通过 chunk_size 字段覆盖。
const DefaultMultipartChunkSize int64 = 25 << 20 // 25MB

// minMultipartChunkSize S3 协议要求除最后一片外每片至少 5MB。
const minMultipartChunkSize int64 = 5 << 20

// maxMultipartParts S3 协议单对象最大分片数。
const maxMultipartParts = 10000

// multipartSessionTTL 分片会话有效期；预签名 URL 按此签发，过期后需重新获取。
const multipartSessionTTL = 6 * time.Hour

// resolveChunkSize 返回策略生效的分片大小。
func (s *FileService) resolveChunkSize(resolvedPolicy string) int64 {
	if info, ok := s.storageMgr.GetPolicyInfo(resolvedPolicy); ok && info.ChunkSize >= minMultipartChunkSize {
		return info.ChunkSize
	}
	return DefaultMultipartChunkSize
}

// MultipartSession 分片上传会话信息，返回给前端。
type MultipartSession struct {
	UploadID      string   `json:"upload_id"`
	StorageKey    string   `json:"storage_key"`
	StoragePolicy string   `json:"storage_policy"`
	ChunkSize     int64    `json:"chunk_size"`
	PartURLs      []string `json:"part_urls"`
	// UploadedParts 已上传的分片（断点续传时非空），对应 PartURLs 下标 part_number-1 可跳过。
	UploadedParts []storage.CompletedPart `json:"uploaded_parts,omitempty"`
	FileName      string                  `json:"file_name,omitempty"`
	Size          int64                   `json:"size,omitempty"`
	ParentID      uint                    `json:"parent_id"`
}

// InitMultipartUpload 创建分片上传会话：生成对象键、发起 multipart、
// 按分片数量预签名各分片 URL，并持久化会话以支持断点续传。
// 存储策略由用户所属用户组决定。
func (s *FileService) InitMultipartUpload(userID int64, fileName, contentType string, size int64, parentID uint) (*MultipartSession, error) {
	if size <= 0 {
		return nil, errors.New("文件大小无效")
	}
	resolved, err := s.ResolveUserPolicy(userID)
	if err != nil {
		return nil, err
	}
	if err := s.checkQuota(userID, resolved, size); err != nil {
		return nil, err
	}
	driver, err := s.storageMgr.GetDriver(resolved)
	if err != nil {
		return nil, err
	}

	chunkSize := s.resolveChunkSize(resolved)
	partCount := int((size + chunkSize - 1) / chunkSize)
	if partCount > maxMultipartParts {
		return nil, fmt.Errorf("文件过大：分片数 %d 超过上限 %d，请调大策略分片大小", partCount, maxMultipartParts)
	}

	key, err := s.buildStorageKey(userID, resolved, fileName)
	if err != nil {
		return nil, err
	}

	uploadID, err := driver.InitMultipartUpload(key, contentType)
	if err != nil {
		return nil, err
	}

	urls, err := s.presignParts(driver, key, uploadID, partCount)
	if err != nil {
		_ = driver.AbortMultipartUpload(key, uploadID)
		return nil, err
	}

	if err := model.CreateUploadSession(&model.UploadSession{
		UserID:        userID,
		UploadID:      uploadID,
		StorageKey:    key,
		StoragePolicy: resolved,
		FileName:      fileName,
		ContentType:   contentType,
		Size:          size,
		ChunkSize:     chunkSize,
		ParentID:      parentID,
		ExpiresAt:     time.Now().Add(multipartSessionTTL),
	}); err != nil {
		_ = driver.AbortMultipartUpload(key, uploadID)
		return nil, fmt.Errorf("保存上传会话失败: %w", err)
	}

	return &MultipartSession{
		UploadID:      uploadID,
		StorageKey:    key,
		StoragePolicy: resolved,
		ChunkSize:     chunkSize,
		PartURLs:      urls,
		FileName:      fileName,
		Size:          size,
		ParentID:      parentID,
	}, nil
}

func (s *FileService) presignParts(driver storage.StorageDriver, key, uploadID string, partCount int) ([]string, error) {
	urls := make([]string, 0, partCount)
	for i := 1; i <= partCount; i++ {
		u, err := driver.GenerateUploadPartURL(key, uploadID, int32(i), multipartSessionTTL)
		if err != nil {
			return nil, err
		}
		urls = append(urls, u)
	}
	return urls, nil
}

// ListMultipartSessions 列出用户未过期的上传会话（前端展示可续传任务）。
func (s *FileService) ListMultipartSessions(userID int64) ([]model.UploadSession, error) {
	return model.ListUploadSessions(userID)
}

// ResumeMultipartUpload 恢复会话：查询存储端已上传分片，重新预签名全部分片 URL。
func (s *FileService) ResumeMultipartUpload(userID int64, storageKey string) (*MultipartSession, error) {
	sess, err := model.GetUploadSession(userID, storageKey)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("上传会话不存在或已完成")
		}
		return nil, err
	}
	driver, err := s.storageMgr.GetDriver(sess.StoragePolicy)
	if err != nil {
		return nil, err
	}

	uploaded, err := driver.ListUploadedParts(sess.StorageKey, sess.UploadID)
	if err != nil {
		// 存储端会话已失效（过期被清理等），删除本地记录
		_ = model.DeleteUploadSession(userID, storageKey)
		return nil, fmt.Errorf("会话已失效，请重新上传: %w", err)
	}

	partCount := int((sess.Size + sess.ChunkSize - 1) / sess.ChunkSize)
	urls, err := s.presignParts(driver, sess.StorageKey, sess.UploadID, partCount)
	if err != nil {
		return nil, err
	}

	// 刷新会话有效期
	sess.ExpiresAt = time.Now().Add(multipartSessionTTL)
	_ = model.DB.Save(sess).Error

	return &MultipartSession{
		UploadID:      sess.UploadID,
		StorageKey:    sess.StorageKey,
		StoragePolicy: sess.StoragePolicy,
		ChunkSize:     sess.ChunkSize,
		PartURLs:      urls,
		UploadedParts: uploaded,
		FileName:      sess.FileName,
		Size:          sess.Size,
		ParentID:      sess.ParentID,
	}, nil
}

// CompleteMultipartUpload 合并分片并写入文件记录，随后清理会话。
// 存储策略以会话记录为准（分片实际写入的位置），不随用户当前所属组重新挑选。
func (s *FileService) CompleteMultipartUpload(userID int64, parentID uint, fileName, storageKey, uploadID string, size int64, mimeType string, parts []storage.CompletedPart) (*model.File, error) {
	if len(parts) == 0 {
		return nil, errors.New("分片列表为空")
	}
	sess, err := model.GetUploadSession(userID, storageKey)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("上传会话不存在或已完成")
		}
		return nil, err
	}
	driver, err := s.storageMgr.GetDriver(sess.StoragePolicy)
	if err != nil {
		return nil, err
	}
	if err := driver.CompleteMultipartUpload(storageKey, uploadID, parts); err != nil {
		return nil, err
	}
	file, err := s.UploadCallback(userID, parentID, fileName, storageKey, size, mimeType, sess.StoragePolicy)
	if err != nil {
		return nil, err
	}
	_ = model.DeleteUploadSession(userID, storageKey)
	return file, nil
}

// AbortMultipartUpload 取消分片上传，清理存储端分片与本地会话。
// 存储端策略以会话记录为准，不受用户当前所属组影响。
func (s *FileService) AbortMultipartUpload(userID int64, storageKey, uploadID string) error {
	sess, err := model.GetUploadSession(userID, storageKey)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // 会话已不存在，视为已清理
		}
		return err
	}
	driver, err := s.storageMgr.GetDriver(sess.StoragePolicy)
	if err != nil {
		return err
	}
	if err := driver.AbortMultipartUpload(storageKey, uploadID); err != nil {
		return err
	}
	_ = model.DeleteUploadSession(userID, storageKey)
	return nil
}

// GetDownloadURL 生成下载/预览 URL。preview 为 true 时内联展示（图片预览等），否则强制附件下载。
func (s *FileService) GetDownloadURL(userID int64, fileID uint, preview bool) (string, error) {
	var file model.File
	if err := model.DB.Where("id = ? AND user_id = ?", fileID, userID).First(&file).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", errors.New("文件不存在")
		}
		return "", err
	}
	if file.IsDir {
		return "", errors.New("不能下载文件夹")
	}

	driver, err := s.storageMgr.GetDriver(file.StoragePolicy)
	if err != nil {
		return "", err
	}
	attachmentName := file.Name
	if preview {
		attachmentName = ""
	}
	return driver.GenerateDownloadURL(file.StorageKey, attachmentName, 30*time.Minute)
}

// ProxyRead 校验代理下载 URL 签名后，返回文件内容流、MIME 类型与文件总大小。
// 供无外链直链的存储（如 Filen / 百度网盘）经服务端中转下载/预览，调用方负责关闭返回的流。
// start/end 为闭区间字节范围（0,-1 表示整文件）：驱动支持 RangeReader 时按段读取，
// 返回 ranged=true；不支持时忽略范围整文件读取（HTTP 语义上以 200 响应即可）。
// 大小为 -1 表示查询失败/未知。
func (s *FileService) ProxyRead(policy, storageKey, attachment, exp, sig string, start, end int64) (io.ReadCloser, string, int64, bool, error) {
	if err := s.storageMgr.VerifyProxyURL(policy, storageKey, attachment, exp, sig); err != nil {
		return nil, "", -1, false, err
	}
	driver, err := s.storageMgr.GetDriver(policy)
	if err != nil {
		return nil, "", -1, false, err
	}

	// 后缀范围（bytes=-N，start 为负的 suffix）需先查大小换算
	if start < 0 {
		if size, serr := driver.GetSize(storageKey); serr == nil && size > 0 {
			start = size + start
			if start < 0 {
				start = 0
			}
			end = size - 1
		} else {
			start, end = 0, -1 // 大小未知，回退整文件
		}
	}

	wantRange := start > 0 || end >= 0
	var rc io.ReadCloser
	ranged := false
	if wantRange {
		if rr, ok := driver.(storage.RangeReader); ok {
			rc, err = rr.ReadRange(storageKey, start, end)
			ranged = err == nil
		}
	}
	if !ranged {
		rc, err = driver.Read(storageKey)
	}
	if err != nil {
		return nil, "", -1, false, err
	}

	// 尽力查 MIME 类型（预览时浏览器按此渲染），查不到则按流处理
	mime := "application/octet-stream"
	var file model.File
	if err := model.DB.Where("storage_policy = ? AND storage_key = ? AND is_dir = ?", policy, storageKey, false).
		First(&file).Error; err == nil && file.MimeType != "" {
		mime = file.MimeType
	}

	size, err := driver.GetSize(storageKey)
	if err != nil {
		size = -1
	}
	return rc, mime, size, ranged, nil
}

// DownloadDir 将用户文件夹打包为 zip 并写入 w，返回建议的文件名。
func (s *FileService) DownloadDir(userID int64, fileID uint, w io.Writer) (string, error) {
	var root model.File
	if err := model.DB.Where("id = ? AND user_id = ?", fileID, userID).First(&root).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", errors.New("文件不存在")
		}
		return "", err
	}
	if !root.IsDir {
		return "", errors.New("只能打包下载文件夹")
	}

	entries, err := collectZipEntries(userID, root)
	if err != nil {
		return "", err
	}
	if err := writeZipTree(w, s.storageMgr, root.Name, entries); err != nil {
		return "", err
	}
	return root.Name + ".zip", nil
}

func (s *FileService) Delete(userID int64, fileID uint) error {
	var file model.File
	if err := model.DB.Where("id = ? AND user_id = ?", fileID, userID).First(&file).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("文件不存在")
		}
		return err
	}

	return model.DB.Transaction(func(tx *gorm.DB) error {
		// BFS 收集自身及全部后代
		toDelete := []model.File{file}
		queue := []uint{}
		if file.IsDir {
			queue = append(queue, file.ID)
		}
		for len(queue) > 0 {
			var children []model.File
			if err := tx.Where("parent_id IN ? AND user_id = ?", queue, userID).Find(&children).Error; err != nil {
				return err
			}
			queue = queue[:0]
			for _, c := range children {
				toDelete = append(toDelete, c)
				if c.IsDir {
					queue = append(queue, c.ID)
				}
			}
		}

		var freedSize int64
		ids := make([]uint, 0, len(toDelete))
		for _, f := range toDelete {
			ids = append(ids, f.ID)
			if f.IsDir {
				continue
			}
			driver, err := s.storageMgr.GetDriver(f.StoragePolicy)
			if err != nil {
				return err
			}
			if err := driver.Delete(f.StorageKey); err != nil {
				return fmt.Errorf("删除存储对象失败: %w", err)
			}
			freedSize += f.Size
		}
		if freedSize > 0 {
			if err := tx.Model(&model.User{}).Where("id = ?", userID).
				Update("storage_used", gorm.Expr("storage_used - ?", freedSize)).Error; err != nil {
				return err
			}
		}
		return tx.Where("id IN ?", ids).Delete(&model.File{}).Error
	})
}

func (s *FileService) Rename(userID int64, fileID uint, newName string) error {
	result := model.DB.Model(&model.File{}).
		Where("id = ? AND user_id = ?", fileID, userID).
		Update("name", newName)
	if result.RowsAffected == 0 {
		return errors.New("文件不存在")
	}
	return result.Error
}

func (s *FileService) Move(userID int64, fileID uint, newParentID uint) error {
	var file model.File
	if err := model.DB.Where("id = ? AND user_id = ?", fileID, userID).First(&file).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("文件不存在")
		}
		return err
	}
	if file.ParentID == newParentID {
		return nil
	}
	if file.ID == newParentID {
		return errors.New("不能移动到自身")
	}

	if newParentID != 0 {
		var parent model.File
		if err := model.DB.Where("id = ? AND user_id = ? AND is_dir = ?", newParentID, userID, true).First(&parent).Error; err != nil {
			return errors.New("目标文件夹不存在")
		}

		// 文件夹不能移入自己的子目录，否则会形成循环目录结构。
		if file.IsDir {
			visited := make(map[uint]struct{})
			current := parent
			for {
				if current.ID == file.ID {
					return errors.New("不能将文件夹移动到其子目录")
				}
				if current.ParentID == 0 {
					break
				}
				if _, ok := visited[current.ID]; ok {
					return errors.New("目标目录结构异常")
				}
				visited[current.ID] = struct{}{}
				if err := model.DB.Where("id = ? AND user_id = ? AND is_dir = ?", current.ParentID, userID, true).First(&current).Error; err != nil {
					return errors.New("目标文件夹路径不存在")
				}
			}
		}
	}

	return model.DB.Model(&model.File{}).
		Where("id = ? AND user_id = ?", fileID, userID).
		Update("parent_id", newParentID).Error
}

// ListStoragePolicies 返回当前可用的存储策略（供上传时选择）。
func (s *FileService) ListStoragePolicies() []storage.PolicyInfo {
	return s.storageMgr.ListPolicies()
}
