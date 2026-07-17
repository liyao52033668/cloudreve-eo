package service

import (
	"errors"
	"math/rand"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
	"gorm.io/gorm"
)

type ShareService struct {
	storageMgr *storage.StoragePolicyManager
}

func NewShareService(mgr *storage.StoragePolicyManager) *ShareService {
	return &ShareService{storageMgr: mgr}
}

func generateCode() string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func (s *ShareService) Create(userID uint, fileID uint, password string, expireAt *time.Time) (*model.Share, error) {
	var file model.File
	if err := model.DB.Where("id = ? AND user_id = ?", fileID, userID).First(&file).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("文件不存在")
		}
		return nil, err
	}

	share := &model.Share{
		UserID:   userID,
		FileID:   fileID,
		Code:     generateCode(),
		Password: password,
		ExpireAt: expireAt,
	}
	if err := model.DB.Create(share).Error; err != nil {
		return nil, err
	}
	return share, nil
}

func (s *ShareService) GetByCode(code string, password string) (*model.Share, *model.File, error) {
	var share model.Share
	if err := model.DB.Where("code = ?", code).First(&share).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, errors.New("分享不存在")
		}
		return nil, nil, err
	}

	if share.ExpireAt != nil && share.ExpireAt.Before(time.Now()) {
		return nil, nil, errors.New("分享已过期")
	}

	if share.Password != "" && share.Password != password {
		return nil, nil, errors.New("提取码错误")
	}

	var file model.File
	if err := model.DB.First(&file, share.FileID).Error; err != nil {
		return nil, nil, errors.New("文件不存在")
	}

	model.DB.Model(&share).Update("views", share.Views+1)
	return &share, &file, nil
}

func (s *ShareService) GetDownloadURL(code string, password string) (string, error) {
	share, file, err := s.GetByCode(code, password)
	if err != nil {
		return "", err
	}
	if file.IsDir {
		return "", errors.New("不能下载文件夹")
	}

	driver, err := s.storageMgr.GetDriver(file.StoragePolicy)
	if err != nil {
		return "", err
	}
	url, err := driver.GenerateDownloadURL(file.StorageKey, 30*time.Minute)
	if err != nil {
		return "", err
	}

	model.DB.Model(share).Update("views", share.Views+1)
	return url, nil
}
