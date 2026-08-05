package service

import (
	"errors"
	"io"
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

// ensureInShare 校验 dirID 位于分享根 rootID 的子树内（沿父链上溯）。
func (s *ShareService) ensureInShare(userID, rootID, dirID uint) error {
	visited := make(map[uint]struct{})
	current := dirID
	for current != 0 {
		if current == rootID {
			return nil
		}
		if _, ok := visited[current]; ok {
			return errors.New("目录结构异常")
		}
		visited[current] = struct{}{}
		var f model.File
		if err := model.DB.Where("id = ? AND user_id = ?", current, userID).First(&f).Error; err != nil {
			return errors.New("目录不存在")
		}
		current = f.ParentID
	}
	return errors.New("目录不在分享范围内")
}

// ListChildren 返回分享目录中 parentID 下的文件列表（供分享页浏览目录）。
func (s *ShareService) ListChildren(code string, password string, parentID uint) ([]model.File, error) {
	share, root, err := s.GetByCode(code, password)
	if err != nil {
		return nil, err
	}
	if !root.IsDir {
		return nil, errors.New("该分享不是文件夹")
	}
	if parentID != root.ID {
		if err := s.ensureInShare(share.UserID, root.ID, parentID); err != nil {
			return nil, err
		}
	}

	var files []model.File
	if err := model.DB.Where("user_id = ? AND parent_id = ?", share.UserID, parentID).
		Order("is_dir DESC, name ASC").
		Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

// DownloadDir 将分享文件夹打包为 zip 并写入 w，返回建议的文件名。
func (s *ShareService) DownloadDir(code string, password string, w io.Writer) (string, error) {
	share, root, err := s.GetByCode(code, password)
	if err != nil {
		return "", err
	}
	if !root.IsDir {
		return "", errors.New("该分享不是文件夹")
	}

	entries, err := collectZipEntries(share.UserID, *root)
	if err != nil {
		return "", err
	}
	if err := writeZipTree(w, s.storageMgr, root.Name, entries); err != nil {
		return "", err
	}
	return root.Name + ".zip", nil
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
	url, err := driver.GenerateDownloadURL(file.StorageKey, file.Name, 30*time.Minute)
	if err != nil {
		return "", err
	}

	model.DB.Model(share).Update("views", share.Views+1)
	return url, nil
}

// GetChildDownloadURL 生成分享目录内某个文件的下载 URL（校验其在分享子树内）。
func (s *ShareService) GetChildDownloadURL(code string, password string, fileID uint) (string, error) {
	share, root, err := s.GetByCode(code, password)
	if err != nil {
		return "", err
	}
	if !root.IsDir {
		return "", errors.New("该分享不是文件夹")
	}
	if fileID == root.ID {
		return "", errors.New("文件夹请使用打包下载")
	}
	if err := s.ensureInShare(share.UserID, root.ID, fileID); err != nil {
		return "", err
	}

	var file model.File
	if err := model.DB.Where("id = ? AND user_id = ?", fileID, share.UserID).First(&file).Error; err != nil {
		return "", errors.New("文件不存在")
	}
	if file.IsDir {
		return "", errors.New("不能下载文件夹")
	}
	driver, err := s.storageMgr.GetDriver(file.StoragePolicy)
	if err != nil {
		return "", err
	}
	return driver.GenerateDownloadURL(file.StorageKey, file.Name, 30*time.Minute)
}
