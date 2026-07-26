package persist

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
)

type gitHubBackend struct {
	cfg    config.PersistGitHubConfig
	client *http.Client
}

func newGitHubBackend(cfg config.PersistGitHubConfig) *gitHubBackend {
	return &gitHubBackend{
		cfg:    cfg,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (b *gitHubBackend) Name() string { return "GitHub" }

func (b *gitHubBackend) contentsURL() string {
	return fmt.Sprintf("https://api.github.com/repos/%s/contents/%s",
		b.cfg.Repo, url.PathEscape(b.cfg.Path))
}

func (b *gitHubBackend) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+b.cfg.Token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "cloudreve-eo")
}

func (b *gitHubBackend) Download() ([]byte, bool, error) {
	req, err := http.NewRequest("GET", b.contentsURL()+"?ref="+url.QueryEscape(b.cfg.Branch), nil)
	if err != nil {
		return nil, false, err
	}
	b.setHeaders(req)
	// raw 媒体类型直接返回文件内容，绕过 Contents API 1MB JSON 限制
	req.Header.Set("Accept", "application/vnd.github.raw+json")
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
		return nil, false, fmt.Errorf("GitHub 下载失败 HTTP %d: %s", resp.StatusCode, body)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// currentSHA 获取远端文件当前 blob SHA（更新必需）；不存在时返回空串。
func (b *gitHubBackend) currentSHA() (string, error) {
	req, err := http.NewRequest("GET", b.contentsURL()+"?ref="+url.QueryEscape(b.cfg.Branch), nil)
	if err != nil {
		return "", err
	}
	b.setHeaders(req)
	// object 媒体类型只需元数据，避免拉取大文件内容
	req.Header.Set("Accept", "application/vnd.github.object+json")
	resp, err := b.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("GitHub 获取文件信息失败 HTTP %d: %s", resp.StatusCode, body)
	}
	var meta struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return "", err
	}
	return meta.SHA, nil
}

func (b *gitHubBackend) Upload(data []byte) error {
	sha, err := b.currentSHA()
	if err != nil {
		return err
	}
	payload := map[string]string{
		"message": "chore: sync cloudreve sqlite snapshot",
		"content": base64.StdEncoding.EncodeToString(data),
		"branch":  b.cfg.Branch,
	}
	if sha != "" {
		payload["sha"] = sha
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", b.contentsURL(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	b.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GitHub 上传失败 HTTP %d: %s", resp.StatusCode, respBody)
	}
	return nil
}
