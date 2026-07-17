package service

import (
	"errors"
	"fmt"
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

func (s *FileService) ListFiles(userID uint, parentID uint) ([]model.File, error) {
	var files []model.File
	err := model.DB.Where("user_id = ? AND parent_id = ?", userID, parentID).
		Order("is_dir DESC, name ASC").
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

func (s *FileService) GetUploadURL(userID uint, fileName string, contentType string) (string, string, error) {
	key := fmt.Sprintf("%d/%s", userID, uuid.New().String())
	driver := s.storageMgr.DefaultDriver()

	url, err := driver.GenerateUploadURL(key, contentType, 30*time.Minute)
	if err != nil {
		return "", "", err
	}
	return url, key, nil
}

func (s *FileService) UploadCallback(userID uint, parentID uint, fileName, storageKey string, size int64, mimeType string) (*model.File, error) {
	file := &model.File{
		UserID:        userID,
		ParentID:      parentID,
		Name:          fileName,
		IsDir:         false,
		Size:          size,
		MimeType:      mimeType,
		StorageKey:    storageKey,
		StoragePolicy: s.storageMgr.DefaultPolicy(),
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
		return nil, err
	}
	return file, nil
}

func (s *FileService) GetDownloadURL(userID uint, fileID uint) (string, error) {
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
	return driver.GenerateDownloadURL(file.StorageKey, 30*time.Minute)
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
