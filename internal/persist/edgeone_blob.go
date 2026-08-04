package persist

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
)

// edgeOneBlobBackend 通过 Node 云函数 db-blob.js 存取 EdgeOne Blob。
// Blob SDK 仅有 Node 版本，故 Go 主程序以 HTTP 调用同站代理函数；
// 上传使用预签名 URL 直写 Blob，绕开云函数 6MB 请求体限制。
type edgeOneBlobBackend struct {
	cfg    config.PersistEdgeOneConfig
	client *http.Client
}

func newEdgeOneBlobBackend(cfg config.PersistEdgeOneConfig) *edgeOneBlobBackend {
	return &edgeOneBlobBackend{
		cfg:    cfg,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (b *edgeOneBlobBackend) Name() string { return "EdgeOne Blob" }

func (b *edgeOneBlobBackend) proxyURL() string { return b.cfg.BaseURL + "/db-blob" }

func (b *edgeOneBlobBackend) Download() ([]byte, bool, error) {
	req, err := http.NewRequest("GET", b.proxyURL(), nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+b.cfg.Secret)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, false, fmt.Errorf("EdgeOne Blob 下载失败 HTTP %d: %s", resp.StatusCode, body)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (b *edgeOneBlobBackend) Upload(data []byte) error {
	uploadURL, err := b.requestUploadURL()
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", uploadURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("EdgeOne Blob 上传失败 HTTP %d: %s", resp.StatusCode, body)
	}
	return nil
}

// requestUploadURL 向代理函数请求预签名上传 URL。
func (b *edgeOneBlobBackend) requestUploadURL() (string, error) {
	req, err := http.NewRequest("POST", b.proxyURL(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+b.cfg.Secret)
	resp, err := b.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("获取 EdgeOne Blob 上传地址失败 HTTP %d: %s", resp.StatusCode, body)
	}
	var result struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.URL == "" {
		return "", fmt.Errorf("EdgeOne Blob 代理未返回上传地址")
	}
	return result.URL, nil
}
