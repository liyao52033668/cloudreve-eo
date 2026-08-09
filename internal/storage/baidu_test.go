package storage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newBaiduTestDriver 构造指向 mock 服务器的驱动，token 预置为未过期。
func newBaiduTestDriver(t *testing.T, tokenJSON string) *BaiduDriver {
	t.Helper()
	d, err := NewBaiduDriver("appid", "secret", "", "", tokenJSON)
	if err != nil {
		t.Fatalf("NewBaiduDriver: %v", err)
	}
	return d
}

func validTokenJSON() string {
	token := BaiduToken{
		AccessToken:    "at-1",
		RefreshToken:   "rt-1",
		AccessExpireAt: time.Now().Add(24 * time.Hour).Unix(),
	}
	raw, _ := json.Marshal(token)
	return string(raw)
}

// mockBaiduServer 按 method 参数分发业务 API。
func mockBaiduServer(t *testing.T, handler func(method string, w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/2.0/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "at-new",
				"refresh_token": "rt-new",
				"expires_in":    2592000,
			})
		default:
			handler(r.URL.Query().Get("method"), w, r)
		}
	}))
}

func TestNewBaiduDriverValidation(t *testing.T) {
	if _, err := NewBaiduDriver("", "s", "", "", ""); err == nil {
		t.Fatal("AppKey 为空应报错")
	}
	if _, err := NewBaiduDriver("k", "", "", "", ""); err == nil {
		t.Fatal("SecretKey 为空应报错")
	}
	d, err := NewBaiduDriver("k", "s", "", "", "")
	if err != nil {
		t.Fatalf("未授权驱动应可创建: %v", err)
	}
	if d.IsAuthorized() {
		t.Fatal("未授权驱动 IsAuthorized 应为 false")
	}
	if _, err := NewBaiduDriver("k", "s", "", "", "{bad json"); err == nil {
		t.Fatal("token JSON 格式错误应报错")
	}
}

func TestNormalizeBaiduRootDir(t *testing.T) {
	cases := map[string]string{
		"":                  "/apps/cloudreve-eo",
		"mydir":             "/mydir",
		"/mydir/":           "/mydir",
		"a/b/":              "/a/b",
	}
	for in, want := range cases {
		if got := normalizeBaiduRootDir(in); got != want {
			t.Errorf("normalizeBaiduRootDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBaiduRootPath(t *testing.T) {
	d := newBaiduTestDriver(t, validTokenJSON())
	d.rootDir = "/apps/cloudreve-eo"
	if got := d.rootPath("123/abc.jpg"); got != "/apps/cloudreve-eo/123/abc.jpg" {
		t.Errorf("rootPath = %q", got)
	}
	if got := d.rootPath("/123/abc.jpg"); got != "/apps/cloudreve-eo/123/abc.jpg" {
		t.Errorf("rootPath 带前导斜杠 = %q", got)
	}
}

func TestBaiduTokenRefreshOnExpiry(t *testing.T) {
	d := newBaiduTestDriver(t, "")
	srv := mockBaiduServer(t, func(method string, w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()
	d.authURL = srv.URL + "/oauth/2.0/token"

	// 置入即将过期的 token，currentToken 应触发刷新
	d.mu.Lock()
	d.token = BaiduToken{AccessToken: "at-old", RefreshToken: "rt-old",
		AccessExpireAt: time.Now().Add(1 * time.Minute).Unix()}
	d.loaded = true
	d.mu.Unlock()

	var refreshed atomic.Bool
	d.onTokenRefreshed = func(token BaiduToken) { refreshed.Store(true) }

	token, err := d.currentToken()
	if err != nil {
		t.Fatalf("currentToken: %v", err)
	}
	if token != "at-new" {
		t.Errorf("token = %q, want at-new", token)
	}
	if !refreshed.Load() {
		t.Error("onTokenRefreshed 回调未触发")
	}
	d.mu.Lock()
	if d.token.RefreshToken != "rt-new" {
		t.Errorf("refresh_token 未更新: %q", d.token.RefreshToken)
	}
	d.mu.Unlock()
}

func TestBaiduTokenInvalidRetriesWithRefresh(t *testing.T) {
	d := newBaiduTestDriver(t, validTokenJSON())
	var calls atomic.Int32
	srv := mockBaiduServer(t, func(method string, w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 && r.URL.Query().Get("access_token") == "at-1" {
			_ = json.NewEncoder(w).Encode(map[string]any{"error_code": 110, "error_msg": "token expired"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"errno": 0})
	})
	defer srv.Close()
	d.panURL = srv.URL
	d.authURL = srv.URL + "/oauth/2.0/token"

	if _, err := d.baiduCall(t.Context(), http.MethodGet, d.panURL, baiduFileRoute, "list", nil, nil); err != nil {
		t.Fatalf("token 失效后应刷新重试成功: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("请求次数 = %d, want 2", calls.Load())
	}
}

func TestBaiduRateLimit429Retry(t *testing.T) {
	d := newBaiduTestDriver(t, validTokenJSON())
	var calls atomic.Int32
	srv := mockBaiduServer(t, func(method string, w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"errno": 0})
	})
	defer srv.Close()
	d.panURL = srv.URL

	if _, err := d.baiduCall(t.Context(), http.MethodGet, d.panURL, baiduFileRoute, "list", nil, nil); err != nil {
		t.Fatalf("429 后应重试成功: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("请求次数 = %d, want 2", calls.Load())
	}
}

func TestBaiduUploadFileMultipart(t *testing.T) {
	d := newBaiduTestDriver(t, validTokenJSON())
	var parts atomic.Int32
	srv := mockBaiduServer(t, func(method string, w http.ResponseWriter, r *http.Request) {
		switch method {
		case "list":
			// 目录存在检查
			_ = json.NewEncoder(w).Encode(map[string]any{"list": []any{}})
		case "precreate":
			r.ParseForm()
			if !strings.Contains(r.Form.Get("block_list"), ",") {
				// 小文件单分片
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"return_type": 1,
				"uploadid":    "up-1",
				"block_list":  []int{0},
			})
		case "locateupload":
			_ = json.NewEncoder(w).Encode(map[string]any{"host": ""})
		case "upload":
			parts.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"md5": "x"})
		case "create":
			r.ParseForm()
			if r.Form.Get("uploadid") != "up-1" {
				t.Error("create 缺少 uploadid")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"fs_id": 1, "path": r.Form.Get("path")})
		default:
			t.Errorf("未知 method: %s", method)
		}
	})
	defer srv.Close()
	d.panURL = srv.URL
	d.pcsURL = srv.URL

	// 预置上传域名缓存为 mock 服务器地址，跳过 locateupload 调用
	d.hostMu.Lock()
	d.uploadHost = srv.URL
	d.hostAt = time.Now()
	d.hostMu.Unlock()

	if err := d.UploadFile("123/file.txt", []byte("hello baidu")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if parts.Load() != 1 {
		t.Errorf("分片上传次数 = %d, want 1", parts.Load())
	}
}

func TestBaiduDeleteNotFoundTreatedAsSuccess(t *testing.T) {
	d := newBaiduTestDriver(t, validTokenJSON())
	srv := mockBaiduServer(t, func(method string, w http.ResponseWriter, r *http.Request) {
		if method == "filemanager" {
			_ = json.NewEncoder(w).Encode(map[string]any{"errno": 0,
				"list": []map[string]any{{"path": "/x", "errno": -9}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"errno": 0})
	})
	defer srv.Close()
	d.panURL = srv.URL

	if err := d.Delete("123/missing.txt"); err != nil {
		t.Fatalf("errno -9 应视为删除成功: %v", err)
	}
}

func TestBaiduGenerateUploadURLRejected(t *testing.T) {
	d := newBaiduTestDriver(t, validTokenJSON())
	if _, err := d.GenerateUploadURL("k", "", time.Minute); err == nil {
		t.Fatal("百度网盘不支持客户端直传，GenerateUploadURL 应报错")
	}
	if _, err := d.InitMultipartUpload("k", ""); err == nil {
		t.Fatal("InitMultipartUpload 应报错")
	}
}

func TestBaiduDownloadViaProxy(t *testing.T) {
	d := newBaiduTestDriver(t, validTokenJSON())
	d.proxyURL = func(storageKey, attachment string) (string, error) {
		return "/api/files/proxy?key=" + storageKey, nil
	}
	u, err := d.GenerateDownloadURL("123/a.jpg", "a.jpg", time.Minute)
	if err != nil {
		t.Fatalf("GenerateDownloadURL: %v", err)
	}
	if !strings.Contains(u, "key=123/a.jpg") {
		t.Errorf("代理 URL = %q", u)
	}

	// 未注入 proxyURL 时应报错
	d2 := newBaiduTestDriver(t, validTokenJSON())
	if _, err := d2.GenerateDownloadURL("k", "", time.Minute); err == nil {
		t.Fatal("proxyURL 未配置时应报错")
	}
}

func TestBaiduUnauthorizedOperations(t *testing.T) {
	d := newBaiduTestDriver(t, "") // 未授权
	if _, err := d.GetSize("k"); err != errBaiduUnauthorized {
		t.Errorf("GetSize 未授权错误 = %v", err)
	}
	if err := d.Delete("k"); err != errBaiduUnauthorized {
		t.Errorf("Delete 未授权错误 = %v", err)
	}
	if _, err := d.Read("k"); err != errBaiduUnauthorized {
		t.Errorf("Read 未授权错误 = %v", err)
	}
}
