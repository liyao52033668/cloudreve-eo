package service

import (
	"errors"
	"fmt"
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

// buildStorageKey 生成对象键：{basePath/}userID/uuid{.ext}，保留原文件扩展名便于在存储桶中识别与预览。
func (s *FileService) buildStorageKey(userID uint, policy string, fileName string) string {
	ext := strings.ToLower(filepath.Ext(fileName))
	if len(ext) > 16 || strings.ContainsAny(ext, "/\\?#%") {
		ext = ""
	}
	key := fmt.Sprintf("%d/%s%s", userID, uuid.New().String(), ext)
	if info, ok := s.storageMgr.GetPolicyInfo(policy); ok && info.BasePath != "" {
		key = strings.Trim(info.BasePath, "/") + "/" + key
	}
	return key
}

func (s *FileService) ListFiles(userID uint, parentID uint) ([]model.File, error) {
	var files []model.File
	err := model.DB.Where("user_id = ? AND parent_id = ?", userID, parentID).
		Order("is_dir DESC, name ASC").
		Find(&files).Error
	return files, err
}

// ListFilesByPolicy 跨目录列出用户在某存储策略下的全部文件（不含文件夹）。
func (s *FileService) ListFilesByPolicy(userID uint, policy string) ([]model.File, error) {
	var files []model.File
	err := model.DB.Where("user_id = ? AND storage_policy = ? AND is_dir = ?", userID, policy, false).
		Order("name ASC").
		Find(&files).Error
	return files, err
}

func (s *FileService) Mkdir(userID uint, parentID uint, name string) (*model.File, error) {
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

// GetUploadURL 生成上传预签名 URL。
// policy 为空时使用默认策略；返回 uploadURL, storageKey, resolvedPolicy。
func (s *FileService) GetUploadURL(userID uint, fileName string, contentType string, policy string) (string, string, string, error) {
	resolved, err := s.storageMgr.ResolvePolicy(policy)
	if err != nil {
		return "", "", "", err
	}
	driver, err := s.storageMgr.GetDriver(resolved)
	if err != nil {
		return "", "", "", err
	}

	key := s.buildStorageKey(userID, resolved, fileName)
	url, err := driver.GenerateUploadURL(key, contentType, 30*time.Minute)
	if err != nil {
		return "", "", "", err
	}
	return url, key, resolved, nil
}

// UploadCallback 写入文件记录。policy 必须与获取上传 URL 时使用的策略一致。
func (s *FileService) UploadCallback(userID uint, parentID uint, fileName, storageKey string, size int64, mimeType string, policy string) (*model.File, error) {
	resolved, err := s.storageMgr.ResolvePolicy(policy)
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

// checkQuota 校验用户在指定策略下新增 size 字节后是否超出配额。
func (s *FileService) checkQuota(userID uint, resolvedPolicy string, size int64) error {
	info, ok := s.storageMgr.GetPolicyInfo(resolvedPolicy)
	if !ok {
		return fmt.Errorf("存储策略 %s 不存在", resolvedPolicy)
	}
	var used int64
	if err := model.DB.Model(&model.File{}).
		Where("user_id = ? AND storage_policy = ? AND is_dir = ?", userID, resolvedPolicy, false).
		Select("COALESCE(SUM(size), 0)").Scan(&used).Error; err != nil {
		return fmt.Errorf("统计已用容量失败: %w", err)
	}
	if used+size > info.DefaultQuota {
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
func (s *FileService) InitMultipartUpload(userID uint, fileName, contentType string, size int64, parentID uint, policy string) (*MultipartSession, error) {
	if size <= 0 {
		return nil, errors.New("文件大小无效")
	}
	resolved, err := s.storageMgr.ResolvePolicy(policy)
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

	key := s.buildStorageKey(userID, resolved, fileName)

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
func (s *FileService) ListMultipartSessions(userID uint) ([]model.UploadSession, error) {
	return model.ListUploadSessions(userID)
}

// ResumeMultipartUpload 恢复会话：查询存储端已上传分片，重新预签名全部分片 URL。
func (s *FileService) ResumeMultipartUpload(userID uint, storageKey string) (*MultipartSession, error) {
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
func (s *FileService) CompleteMultipartUpload(userID uint, parentID uint, fileName, storageKey, uploadID string, size int64, mimeType string, policy string, parts []storage.CompletedPart) (*model.File, error) {
	resolved, err := s.storageMgr.ResolvePolicy(policy)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, errors.New("分片列表为空")
	}
	driver, err := s.storageMgr.GetDriver(resolved)
	if err != nil {
		return nil, err
	}
	if err := driver.CompleteMultipartUpload(storageKey, uploadID, parts); err != nil {
		return nil, err
	}
	file, err := s.UploadCallback(userID, parentID, fileName, storageKey, size, mimeType, resolved)
	if err != nil {
		return nil, err
	}
	_ = model.DeleteUploadSession(userID, storageKey)
	return file, nil
}

// AbortMultipartUpload 取消分片上传，清理存储端分片与本地会话。
func (s *FileService) AbortMultipartUpload(userID uint, storageKey, uploadID string, policy string) error {
	resolved, err := s.storageMgr.ResolvePolicy(policy)
	if err != nil {
		return err
	}
	driver, err := s.storageMgr.GetDriver(resolved)
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
func (s *FileService) GetDownloadURL(userID uint, fileID uint, preview bool) (string, error) {
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

func (s *FileService) Delete(userID uint, fileID uint) error {
	var file model.File
	if err := model.DB.Where("id = ? AND user_id = ?", fileID, userID).First(&file).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("文件不存在")
		}
		return err
	}

	return model.DB.Transaction(func(tx *gorm.DB) error {
		if file.IsDir {
			var count int64
			tx.Model(&model.File{}).Where("parent_id = ? AND user_id = ?", fileID, userID).Count(&count)
			if count > 0 {
				return errors.New("文件夹不为空")
			}
		} else {
			driver, err := s.storageMgr.GetDriver(file.StoragePolicy)
			if err != nil {
				return err
			}
			if err := driver.Delete(file.StorageKey); err != nil {
				return fmt.Errorf("删除存储对象失败: %w", err)
			}
			if err := tx.Model(&model.User{}).Where("id = ?", userID).
				Update("storage_used", gorm.Expr("storage_used - ?", file.Size)).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&file).Error
	})
}

func (s *FileService) Rename(userID uint, fileID uint, newName string) error {
	result := model.DB.Model(&model.File{}).
		Where("id = ? AND user_id = ?", fileID, userID).
		Update("name", newName)
	if result.RowsAffected == 0 {
		return errors.New("文件不存在")
	}
	return result.Error
}

func (s *FileService) Move(userID uint, fileID uint, newParentID uint) error {
	if newParentID != 0 {
		var parent model.File
		if err := model.DB.Where("id = ? AND user_id = ? AND is_dir = ?", newParentID, userID, true).First(&parent).Error; err != nil {
			return errors.New("目标文件夹不存在")
		}
	}
	result := model.DB.Model(&model.File{}).
		Where("id = ? AND user_id = ?", fileID, userID).
		Update("parent_id", newParentID)
	if result.RowsAffected == 0 {
		return errors.New("文件不存在")
	}
	return result.Error
}

// ListStoragePolicies 返回当前可用的存储策略（供上传时选择）。
func (s *FileService) ListStoragePolicies() []storage.PolicyInfo {
	return s.storageMgr.ListPolicies()
}
