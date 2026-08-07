package storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/logx"
)

// GitHubDriver 使用 GitHub Contents API 实现存储驱动。
// 注意：GitHub 存储有诸多限制（单文件 100MB、不支持分片上传、无预签名 URL），
// 仅适合小规模文件存储场景。
type GitHubDriver struct {
	owner      string // 仓库所有者
	repo       string // 仓库名称
	branch     string // 分支
	token      string // Personal Access Token
	basePath   string // 存储路径前缀
	customHost string // 自定义域名（可选）
	client     *http.Client
}

// NewGitHubDriver 创建 GitHub 存储驱动。
// endpoint 格式：owner/repo 或 https://github.com/owner/repo
// basePath 为存储路径前缀，如 "files" 或 "cloudreve/uploads"
// branch 为分支名称，为空时默认使用 main
func NewGitHubDriver(endpoint, token, basePath, customHost, branch string) (*GitHubDriver, error) {
	// 解析 endpoint 获取 owner/repo
	owner, repo, err := parseGitHubEndpoint(endpoint)
	if err != nil {
		return nil, err
	}

	if token == "" {
		return nil, fmt.Errorf("GitHub Token 不能为空")
	}

	// 清理 basePath
	basePath = trimPath(basePath)

	// 分支默认为 main
	if branch == "" {
		branch = "main"
	}

	return &GitHubDriver{
		owner:      owner,
		repo:       repo,
		branch:     branch,
		token:      token,
		basePath:   basePath,
		customHost: customHost,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}, nil
}

func parseGitHubEndpoint(endpoint string) (owner, repo string, err error) {
	// 支持格式：owner/repo 或 https://github.com/owner/repo
	endpoint = trimPath(endpoint)
	if endpoint == "" {
		return "", "", fmt.Errorf("GitHub endpoint 不能为空")
	}

	// 如果是完整 URL
	if u, parseErr := url.Parse(endpoint); parseErr == nil && u.Host != "" {
		path := trimPath(u.Path)
		parts := splitPath(path)
		if len(parts) >= 2 {
			return parts[0], parts[1], nil
		}
	}

	// 简单格式：owner/repo
	parts := splitPath(endpoint)
	if len(parts) >= 2 {
		return parts[0], parts[1], nil
	}

	return "", "", fmt.Errorf("GitHub endpoint 格式错误，应为 owner/repo")
}

func splitPath(path string) []string {
	var parts []string
	for _, p := range splitString(path, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func splitString(s, sep string) []string {
	if s == "" {
		return nil
	}
	result := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if string(s[i]) == sep {
			if i > start {
				result = append(result, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

func trimPath(path string) string {
	start := 0
	for start < len(path) && path[start] == '/' {
		start++
	}
	end := len(path)
	for end > start && path[end-1] == '/' {
		end--
	}
	return path[start:end]
}

func (d *GitHubDriver) buildPath(key string) string {
	// key 已经包含 basePath（由 FileService.buildStorageKey 拼接），不需要重复添加
	return key
}

func (d *GitHubDriver) apiURL(path string) string {
	escapedPath := url.PathEscape(path)
	return fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", d.owner, d.repo, escapedPath)
}

func (d *GitHubDriver) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "token "+d.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "CloudreveEO")
}

// getFileSHA 获取文件的 SHA（用于更新/删除）
func (d *GitHubDriver) getFileSHA(path string) (string, error) {
	req, err := http.NewRequestWithContext(context.Background(), "GET", d.apiURL(path), nil)
	if err != nil {
		return "", err
	}
	d.setHeaders(req)

	resp, err := d.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil // 文件不存在
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("获取文件信息失败: HTTP %d", resp.StatusCode)
	}

	var result struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.SHA, nil
}

// GenerateUploadURL GitHub 不支持预签名 URL，返回错误。
// 实际上传需要通过服务端代理。
func (d *GitHubDriver) GenerateUploadURL(key string, contentType string, expire time.Duration) (string, error) {
	return "", fmt.Errorf("GitHub 存储不支持客户端直传，请使用服务端上传")
}

// GenerateDownloadURL 生成下载 URL。
// 如果有 customHost，使用自定义域名；否则使用 raw.githubusercontent.com。
func (d *GitHubDriver) GenerateDownloadURL(key string, fileName string, expire time.Duration) (string, error) {
	path := d.buildPath(key)

	if d.customHost != "" {
		// 使用自定义域名
		u, err := url.Parse(d.customHost)
		if err != nil {
			return "", fmt.Errorf("解析自定义域名失败: %w", err)
		}
		u.Path = "/" + url.PathEscape(path)
		return u.String(), nil
	}

	// 使用 GitHub raw URL
	escapedPath := url.PathEscape(path)
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s",
		d.owner, d.repo, d.branch, escapedPath), nil
}

// Delete 删除文件。
func (d *GitHubDriver) Delete(key string) error {
	path := d.buildPath(key)

	// 获取文件 SHA
	sha, err := d.getFileSHA(path)
	if err != nil {
		return err
	}
	if sha == "" {
		return nil // 文件不存在，视为删除成功
	}

	payload := map[string]string{
		"message": "Delete file",
		"sha":     sha,
		"branch":  d.branch,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(context.Background(), "DELETE", d.apiURL(path), bytes.NewReader(body))
	if err != nil {
		return err
	}
	d.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		logx.Error(logx.ModuleStorage, "删除文件失败", logx.Err(err), "key", key)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		err = fmt.Errorf("删除文件失败: HTTP %d, %s", resp.StatusCode, string(bodyBytes))
		logx.Error(logx.ModuleStorage, "删除文件失败", logx.Err(err), "key", key)
		return err
	}
	logx.Info(logx.ModuleStorage, "文件已删除", "key", key)
	return nil
}

// GetSize 获取文件大小。
func (d *GitHubDriver) GetSize(key string) (int64, error) {
	path := d.buildPath(key)

	req, err := http.NewRequestWithContext(context.Background(), "GET", d.apiURL(path), nil)
	if err != nil {
		return 0, err
	}
	d.setHeaders(req)

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return 0, fmt.Errorf("文件不存在")
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("获取文件信息失败: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Size int64 `json:"size"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	return result.Size, nil
}

// Read 读取文件内容。
func (d *GitHubDriver) Read(key string) (io.ReadCloser, error) {
	downloadURL, err := d.GenerateDownloadURL(key, "", 0)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(context.Background(), "GET", downloadURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("下载文件失败: HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// InitMultipartUpload GitHub 不支持分片上传。
func (d *GitHubDriver) InitMultipartUpload(key string, contentType string) (string, error) {
	return "", fmt.Errorf("GitHub 存储不支持分片上传")
}

// GenerateUploadPartURL GitHub 不支持分片上传。
func (d *GitHubDriver) GenerateUploadPartURL(key string, uploadID string, partNumber int32, expire time.Duration) (string, error) {
	return "", fmt.Errorf("GitHub 存储不支持分片上传")
}

// CompleteMultipartUpload GitHub 不支持分片上传。
func (d *GitHubDriver) CompleteMultipartUpload(key string, uploadID string, parts []CompletedPart) error {
	return fmt.Errorf("GitHub 存储不支持分片上传")
}

// AbortMultipartUpload GitHub 不支持分片上传。
func (d *GitHubDriver) AbortMultipartUpload(key string, uploadID string) error {
	return fmt.Errorf("GitHub 存储不支持分片上传")
}

// ListUploadedParts GitHub 不支持分片上传。
func (d *GitHubDriver) ListUploadedParts(key string, uploadID string) ([]CompletedPart, error) {
	return nil, fmt.Errorf("GitHub 存储不支持分片上传")
}

// SetBucketCORS GitHub 不支持 CORS 配置。
func (d *GitHubDriver) SetBucketCORS() error {
	return fmt.Errorf("GitHub 存储不支持 CORS 配置")
}

// UploadFile 直接上传文件到 GitHub（服务端代理）。
func (d *GitHubDriver) UploadFile(key string, content []byte) error {
	path := d.buildPath(key)

	// 检查文件是否存在，获取 SHA
	sha, err := d.getFileSHA(path)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"message": "Upload file",
		"content": base64.StdEncoding.EncodeToString(content),
		"branch":  d.branch,
	}
	if sha != "" {
		payload["sha"] = sha
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(context.Background(), "PUT", d.apiURL(path), bytes.NewReader(body))
	if err != nil {
		return err
	}
	d.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		logx.Error(logx.ModuleStorage, "上传文件失败", logx.Err(err), "key", key)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		err = fmt.Errorf("上传文件失败: HTTP %d, %s", resp.StatusCode, string(bodyBytes))
		logx.Error(logx.ModuleStorage, "上传文件失败", logx.Err(err), "key", key)
		return err
	}
	logx.Info(logx.ModuleStorage, "文件已上传", "key", key)
	return nil
}
