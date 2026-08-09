package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 服务端中转分块上传的本地缓冲临时文件。
// GitHub（整文件单次 PUT）与 Filen（SDK 从流读取）无法跨请求流式转发，
// 各块先按序追加到临时文件，complete 时整体提交后删除。
const (
	chunkBufferPrefix = "cloudreve-chunk-"
	chunkBufferSuffix = ".part"
	// 超过该时长未完成的缓冲文件视为孤儿（实例重启遗留），Init 时顺带清理。
	chunkBufferStaleAfter = 24 * time.Hour
)

// newChunkUploadID 生成随机上传会话 ID（同时作为临时文件名，无需内存映射）。
func newChunkUploadID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成上传会话 ID 失败: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// chunkTempPath 由会话 ID 得到临时缓冲文件路径。
func chunkTempPath(uploadID string) string {
	return filepath.Join(os.TempDir(), chunkBufferPrefix+uploadID+chunkBufferSuffix)
}

// createChunkBuffer 创建空的缓冲临时文件。
func createChunkBuffer(uploadID string) error {
	f, err := os.Create(chunkTempPath(uploadID))
	if err != nil {
		return fmt.Errorf("创建上传缓冲失败: %w", err)
	}
	return f.Close()
}

// appendChunkBuffer 按序追加一块数据到缓冲文件。
func appendChunkBuffer(uploadID string, data []byte) error {
	f, err := os.OpenFile(chunkTempPath(uploadID), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return fmt.Errorf("上传会话已失效（缓冲丢失），请重新上传")
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("写入上传缓冲失败: %w", err)
	}
	return nil
}

// openChunkBuffer 打开缓冲文件供整体提交，调用方负责关闭。
func openChunkBuffer(uploadID string) (*os.File, error) {
	f, err := os.Open(chunkTempPath(uploadID))
	if err != nil {
		return nil, fmt.Errorf("上传会话已失效（缓冲丢失），请重新上传")
	}
	return f, nil
}

// removeChunkBuffer 删除缓冲文件（complete/放弃时调用；不存在时静默）。
func removeChunkBuffer(uploadID string) {
	_ = os.Remove(chunkTempPath(uploadID))
}

// sweepStaleChunkBuffers 清理超期未完成的缓冲文件（实例重启后的孤儿）。
// 仅在分块上传 Init 时顺带执行，开销可忽略。
func sweepStaleChunkBuffers() {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-chunkBufferStaleAfter)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, chunkBufferPrefix) || !strings.HasSuffix(name, chunkBufferSuffix) {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(os.TempDir(), name))
	}
}
