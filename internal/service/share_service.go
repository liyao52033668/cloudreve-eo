package service

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strconv"
	"strings"
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

// Create 创建分享：支持单文件与多文件（fileIDs 至少一个）。
func (s *ShareService) Create(userID int64, fileIDs []uint, password string, expireAt *time.Time) (*model.Share, error) {
	if len(fileIDs) == 0 {
		return nil, errors.New("请选择要分享的文件")
	}
	var files []model.File
	if err := model.DB.Where("id IN ? AND user_id = ?", fileIDs, userID).Find(&files).Error; err != nil {
		return nil, err
	}
	if len(files) != len(fileIDs) {
		return nil, errors.New("文件不存在")
	}

	idStrs := make([]string, len(fileIDs))
	for i, id := range fileIDs {
		idStrs[i] = strconv.FormatUint(uint64(id), 10)
	}
	share := &model.Share{
		UserID:   userID,
		FileIDs:  strings.Join(idStrs, ","),
		Code:     generateCode(),
		Password: password,
		ExpireAt: expireAt,
	}
	if err := model.DB.Create(share).Error; err != nil {
		return nil, err
	}
	return share, nil
}

// RootFileIDs 解析分享的文件 ID 列表（兼容旧单文件分享 FileID 字段）。
func RootFileIDs(share *model.Share) ([]uint, error) {
	if share.FileIDs != "" {
		var ids []uint
		for _, part := range strings.Split(share.FileIDs, ",") {
			id, err := strconv.ParseUint(strings.TrimSpace(part), 10, 32)
			if err != nil || id == 0 {
				return nil, errors.New("分享数据异常")
			}
			ids = append(ids, uint(id))
		}
		if len(ids) == 0 {
			return nil, errors.New("分享数据异常")
		}
		return ids, nil
	}
	if share.FileID != 0 {
		return []uint{share.FileID}, nil
	}
	return nil, errors.New("分享数据异常")
}

// validateShare 校验分享存在、未过期、提取码正确，并累加浏览量。
func validateShare(code string, password string) (*model.Share, error) {
	var share model.Share
	if err := model.DB.Where("code = ?", code).First(&share).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("分享不存在")
		}
		return nil, err
	}
	if share.ExpireAt != nil && share.ExpireAt.Before(time.Now()) {
		return nil, errors.New("分享已过期")
	}
	if share.Password != "" && share.Password != password {
		return nil, errors.New("提取码错误")
	}
	model.DB.Model(&share).Update("views", share.Views+1)
	return &share, nil
}

// GetByCode 返回分享及其根文件列表（单文件分享长度为 1）。
func (s *ShareService) GetByCode(code string, password string) (*model.Share, []model.File, error) {
	share, err := validateShare(code, password)
	if err != nil {
		return nil, nil, err
	}
	ids, err := RootFileIDs(share)
	if err != nil {
		return nil, nil, err
	}
	var files []model.File
	if err := model.DB.Where("id IN ?", ids).Find(&files).Error; err != nil {
		return nil, nil, err
	}
	if len(files) == 0 {
		return nil, nil, errors.New("文件不存在")
	}
	return share, files, nil
}

// ensureInShare 校验 dirID 位于某个分享根 rootID 的子树内（沿父链上溯）。
func (s *ShareService) ensureInShare(userID int64, rootIDs []uint, dirID uint) error {
	rootSet := make(map[uint]struct{}, len(rootIDs))
	for _, id := range rootIDs {
		rootSet[id] = struct{}{}
	}
	visited := make(map[uint]struct{})
	current := dirID
	for current != 0 {
		if _, ok := rootSet[current]; ok {
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

// ListChildren 返回分享目录中 parentID 下的文件列表。
// parentID == 0 表示顶层：直接返回分享的根文件列表。
func (s *ShareService) ListChildren(code string, password string, parentID uint) ([]model.File, error) {
	share, err := validateShare(code, password)
	if err != nil {
		return nil, err
	}
	rootIDs, err := RootFileIDs(share)
	if err != nil {
		return nil, err
	}
	if parentID == 0 {
		var files []model.File
		if err := model.DB.Where("id IN ?", rootIDs).
			Order("is_dir DESC, name ASC").
			Find(&files).Error; err != nil {
			return nil, err
		}
		return files, nil
	}
	if err := s.ensureInShare(share.UserID, rootIDs, parentID); err != nil {
		return nil, err
	}

	var parent model.File
	if err := model.DB.Where("id = ? AND user_id = ?", parentID, share.UserID).First(&parent).Error; err != nil {
		return nil, errors.New("目录不存在")
	}
	if !parent.IsDir {
		return nil, errors.New("该分享不是文件夹")
	}

	var files []model.File
	if err := model.DB.Where("user_id = ? AND parent_id = ?", share.UserID, parentID).
		Order("is_dir DESC, name ASC").
		Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

// DownloadDir 将分享的全部根文件打包为 zip 并写入 w，返回建议的文件名。
// beforeWrite 在开始写流前被调用（此时设置响应头才有效：首次写入即 flush 头部）。
func (s *ShareService) DownloadDir(code string, password string, w io.Writer, beforeWrite func(fileName string)) (string, error) {
	share, err := validateShare(code, password)
	if err != nil {
		return "", err
	}
	rootIDs, err := RootFileIDs(share)
	if err != nil {
		return "", err
	}
	var roots []model.File
	if err := model.DB.Where("id IN ?", rootIDs).Find(&roots).Error; err != nil {
		return "", err
	}
	if len(roots) == 0 {
		return "", errors.New("文件不存在")
	}
	entries, err := collectBatchZipEntries(share.UserID, roots)
	if err != nil {
		return "", err
	}
	fileName := "批量下载.zip"
	if len(roots) == 1 {
		fileName = roots[0].Name + ".zip"
	}
	if beforeWrite != nil {
		beforeWrite(fileName)
	}
	if err := writeZipTree(w, s.storageMgr, "", entries); err != nil {
		return "", err
	}
	return fileName, nil
}

// DownloadSelected 将分享内选中的文件/文件夹打包为 zip 写入 w（校验均在分享根子树内）。
func (s *ShareService) DownloadSelected(code string, password string, fileIDs []uint, w io.Writer, beforeWrite func(fileName string)) error {
	if len(fileIDs) == 0 {
		return errors.New("未选择任何文件")
	}
	share, err := validateShare(code, password)
	if err != nil {
		return err
	}
	rootIDs, err := RootFileIDs(share)
	if err != nil {
		return err
	}
	rootSet := make(map[uint]struct{}, len(rootIDs))
	for _, id := range rootIDs {
		rootSet[id] = struct{}{}
	}
	for _, id := range fileIDs {
		if _, ok := rootSet[id]; ok {
			continue
		}
		if err := s.ensureInShare(share.UserID, rootIDs, id); err != nil {
			return err
		}
	}

	var roots []model.File
	if err := model.DB.Where("id IN ? AND user_id = ?", fileIDs, share.UserID).Find(&roots).Error; err != nil {
		return err
	}
	if len(roots) != len(fileIDs) {
		return errors.New("文件不存在")
	}
	entries, err := collectBatchZipEntries(share.UserID, roots)
	if err != nil {
		return err
	}
	fileName := "批量下载.zip"
	if len(roots) == 1 {
		fileName = roots[0].Name + ".zip"
	}
	if beforeWrite != nil {
		beforeWrite(fileName)
	}
	return writeZipTree(w, s.storageMgr, "", entries)
}

func (s *ShareService) GetDownloadURL(code string, password string) (string, error) {
	share, err := validateShare(code, password)
	if err != nil {
		return "", err
	}
	rootIDs, err := RootFileIDs(share)
	if err != nil {
		return "", err
	}
	if len(rootIDs) != 1 {
		return "", errors.New("多文件分享请使用打包下载")
	}
	var file model.File
	if err := model.DB.Where("id = ? AND user_id = ?", rootIDs[0], share.UserID).First(&file).Error; err != nil {
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

// GetChildDownloadURL 生成分享内某个文件的下载 URL（校验其在分享根子树内或是根文件本身）。
func (s *ShareService) GetChildDownloadURL(code string, password string, fileID uint) (string, error) {
	share, err := validateShare(code, password)
	if err != nil {
		return "", err
	}
	rootIDs, err := RootFileIDs(share)
	if err != nil {
		return "", err
	}

	isRoot := false
	for _, id := range rootIDs {
		if id == fileID {
			isRoot = true
			break
		}
	}
	if !isRoot {
		if err := s.ensureInShare(share.UserID, rootIDs, fileID); err != nil {
			return "", err
		}
	}

	var file model.File
	if err := model.DB.Where("id = ? AND user_id = ?", fileID, share.UserID).First(&file).Error; err != nil {
		return "", errors.New("文件不存在")
	}
	if file.IsDir {
		return "", fmt.Errorf("文件夹请使用打包下载")
	}
	driver, err := s.storageMgr.GetDriver(file.StoragePolicy)
	if err != nil {
		return "", err
	}
	return driver.GenerateDownloadURL(file.StorageKey, file.Name, 30*time.Minute)
}
