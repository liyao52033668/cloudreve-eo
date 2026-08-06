package service

import (
	"archive/zip"
	"io"

	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
)

// zipEntry 待写入 zip 的一个条目；isDir 时仅 relPath 有意义。
type zipEntry struct {
	relPath string
	isDir   bool
	file    model.File
}

// collectZipEntries 递归收集目录下的全部子项（保持目录结构）。
func collectZipEntries(userID int64, root model.File) ([]zipEntry, error) {
	entries := []zipEntry{{relPath: root.Name, isDir: true, file: root}}
	queue := []zipEntry{{relPath: root.Name, isDir: true, file: root}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		var children []model.File
		if err := model.DB.Where("user_id = ? AND parent_id = ?", userID, cur.file.ID).
			Order("name ASC").Find(&children).Error; err != nil {
			return nil, err
		}
		for _, child := range children {
			e := zipEntry{relPath: cur.relPath + "/" + child.Name, file: child}
			if child.IsDir {
				e.isDir = true
				queue = append(queue, e)
			}
			entries = append(entries, e)
		}
	}
	return entries, nil
}

// writeZipFile 将单个文件内容流式写入 zip。
func writeZipFile(zw *zip.Writer, mgr *storage.StoragePolicyManager, e zipEntry) error {
	driver, err := mgr.GetDriver(e.file.StoragePolicy)
	if err != nil {
		return err
	}
	rc, err := driver.Read(e.file.StorageKey)
	if err != nil {
		return err
	}
	defer rc.Close()

	hdr := &zip.FileHeader{Name: e.relPath, Method: zip.Deflate}
	hdr.SetModTime(e.file.UpdatedAt)
	fw, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(fw, rc)
	return err
}

// writeZipTree 将已收集的条目写入 zip 流并返回建议的文件名。
func writeZipTree(w io.Writer, mgr *storage.StoragePolicyManager, rootName string, entries []zipEntry) error {
	zw := zip.NewWriter(w)
	for _, e := range entries {
		if e.isDir {
			if _, err := zw.Create(e.relPath + "/"); err != nil {
				return err
			}
			continue
		}
		if err := writeZipFile(zw, mgr, e); err != nil {
			return err
		}
	}
	return zw.Close()
}
