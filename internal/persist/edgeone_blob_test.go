package persist

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
)

// newTestProxy 模拟 Node 云函数 db-blob.js：GET 下载、POST 返回预签名上传地址。
func newTestProxy(t *testing.T, secret string, stored *[]byte, uploadSrv *httptest.Server) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case "GET":
			if *stored == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write(*stored)
		case "POST":
			fmt.Fprintf(w, `{"url":"%s/upload"}`, uploadSrv.URL)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
}

func TestEdgeOneBlobBackend_Download(t *testing.T) {
	data := []byte("sqlite-snapshot")
	stored := &data
	uploadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer uploadSrv.Close()
	proxy := newTestProxy(t, "secret", stored, uploadSrv)
	defer proxy.Close()

	b := newEdgeOneBlobBackend(config.PersistEdgeOneConfig{BaseURL: proxy.URL, Secret: "secret"})
	got, found, err := b.Download()
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	if !found || string(got) != string(data) {
		t.Errorf("Download() = (%q, %v), want (%q, true)", got, found, data)
	}
}

func TestEdgeOneBlobBackend_DownloadNotFound(t *testing.T) {
	var data []byte
	stored := &data
	uploadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer uploadSrv.Close()
	proxy := newTestProxy(t, "secret", stored, uploadSrv)
	defer proxy.Close()

	b := newEdgeOneBlobBackend(config.PersistEdgeOneConfig{BaseURL: proxy.URL, Secret: "secret"})
	_, found, err := b.Download()
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	if found {
		t.Errorf("Download() found = true, want false")
	}
}

func TestEdgeOneBlobBackend_Upload(t *testing.T) {
	var uploaded []byte
	uploadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("上传请求方法 = %s, want PUT", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/octet-stream" {
			t.Errorf("上传 Content-Type = %q, want application/octet-stream", ct)
		}
		uploaded, _ = io.ReadAll(r.Body)
	}))
	defer uploadSrv.Close()
	var data []byte
	proxy := newTestProxy(t, "secret", &data, uploadSrv)
	defer proxy.Close()

	b := newEdgeOneBlobBackend(config.PersistEdgeOneConfig{BaseURL: proxy.URL, Secret: "secret"})
	data = []byte("new-snapshot")
	if err := b.Upload(data); err != nil {
		t.Fatalf("Upload() error: %v", err)
	}
	if string(uploaded) != string(data) {
		t.Errorf("上传内容 = %q, want %q", uploaded, data)
	}
}

func TestEdgeOneBlobBackend_Unauthorized(t *testing.T) {
	var data []byte
	uploadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer uploadSrv.Close()
	proxy := newTestProxy(t, "secret", &data, uploadSrv)
	defer proxy.Close()

	b := newEdgeOneBlobBackend(config.PersistEdgeOneConfig{BaseURL: proxy.URL, Secret: "wrong"})
	if _, _, err := b.Download(); err == nil {
		t.Errorf("Download() 密钥错误时应返回错误")
	}
	if err := b.Upload([]byte("x")); err == nil {
		t.Errorf("Upload() 密钥错误时应返回错误")
	}
}
