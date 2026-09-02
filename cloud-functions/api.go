package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
	"github.com/cloudreve-eo/cloudreve-eo/internal/handler"
	"github.com/cloudreve-eo/cloudreve-eo/internal/logx"
	"github.com/cloudreve-eo/cloudreve-eo/internal/middleware"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/persist"
	"github.com/cloudreve-eo/cloudreve-eo/internal/service"
	"github.com/cloudreve-eo/cloudreve-eo/internal/snowflake"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
	"github.com/gin-gonic/gin"
)

func main() {
	// 结构化日志先于一切初始化：EdgeOne 平台按行采集 stdout，
	// 之后所有输出（含 config 错误）都是单行 JSON。
	logx.Setup(os.Getenv("LOG_LEVEL"))

	// gin ReleaseMode：禁止 debug 模式的路由注册表与彩色纯文本日志，
	// 所有日志统一走 logx 单行 JSON（EdgeOne 控制台按关键字检索）。
	gin.SetMode(gin.ReleaseMode)

	cfg, err := config.Load()
	if err != nil {
		logx.Error(logx.ModuleApp, "加载配置失败", logx.Err(err))
		os.Exit(1)
	}

	// SQLite 持久化：local（默认）返回 nil，s3/github/edgeone-blob 返回 Syncer
	syncer, err := persist.New(cfg)
	if err != nil {
		logx.Error(logx.ModuleApp, "初始化数据库持久化失败", logx.Err(err))
		os.Exit(1)
	}

	port := ":" + __edgeoneGetPort("9000")

	if cfg.LazyRestore() {
		// edgeone-blob 未显式配置地址：平台不注入站点域名，只能等首个请求
		// 从 Host 头推导后再恢复数据库，因此整个初始化推迟到首个请求。
		logx.Info(logx.ModuleApp, "Cloudreve-EO 启动中", "mode", "lazy-restore", "note", "首个请求时从 EdgeOne Blob 恢复数据库")
		if err := http.ListenAndServe(port, __edgeoneStripPrefix("/api", newLazyBootstrap(cfg, syncer))); err != nil {
			logx.Error(logx.ModuleApp, "启动服务失败", logx.Err(err))
			os.Exit(1)
		}
		return
	}

	engine, err := buildApp(cfg, syncer)
	if err != nil {
		logx.Error(logx.ModuleApp, "启动失败", logx.Err(err))
		os.Exit(1)
	}
	logx.Info(logx.ModuleApp, "Cloudreve-EO 启动中")
	if err := http.ListenAndServe(port, __edgeoneStripPrefix("/api", engine)); err != nil {
		logx.Error(logx.ModuleApp, "启动服务失败", logx.Err(err))
		os.Exit(1)
	}
}

// buildApp 完成数据库恢复、初始化与全部路由装配。
// 启动路径直接调用；懒加载路径在首个请求内调用。
func buildApp(cfg *config.Config, syncer *persist.Syncer) (*gin.Engine, error) {
	// SQLite 持久化：s3/github/edgeone-blob 先从远端恢复再启动定时同步
	if syncer != nil {
		if err := syncer.Restore(); err != nil {
			return nil, fmt.Errorf("恢复数据库失败: %w", err)
		}
	}

	if err := model.InitDB(cfg); err != nil {
		return nil, fmt.Errorf("初始化数据库失败: %w", err)
	}

	// 初始化雪花 ID 生成器
	if err := snowflake.Init(0); err != nil {
		return nil, fmt.Errorf("初始化雪花 ID 生成器失败: %w", err)
	}

	// 默认用户组：新注册用户自动归入；老库升级时也保证存在
	if _, err := model.EnsureDefaultGroup(); err != nil {
		return nil, fmt.Errorf("初始化默认用户组失败: %w", err)
	}

	if syncer != nil {
		syncer.Start(model.SnapshotSQLite)
	}

	// JWT 主密钥：库中已有则加载，否则自动生成写入库
	jwtSecret, err := model.EnsureJWTSecret()
	if err != nil {
		return nil, fmt.Errorf("初始化 JWT 密钥失败: %w", err)
	}
	jwtSecrets := service.NewJWTSecretStore(jwtSecret)

	// 存储策略仅来自数据库；空则管理员在前端「存储策略」页添加
	storageMgr, err := storage.NewStoragePolicyManager()
	if err != nil {
		return nil, fmt.Errorf("初始化存储失败: %w", err)
	}
	if n := len(storageMgr.ListPolicies()); n == 0 {
		logx.Warn(logx.ModuleStorage, "尚未配置存储策略，请管理员在前端「存储策略」页面添加 S3 兼容策略")
	} else {
		logx.Info(logx.ModuleStorage, "已加载存储策略", "count", n, "default", storageMgr.DefaultPolicy())
	}

	authService := service.NewAuthService()
	fileService := service.NewFileService(storageMgr)
	shareService := service.NewShareService(storageMgr)

	// 无外链直链存储（如 Filen/百度）的代理下载 URL 签名器：
	// JWT 主密钥复用为签名密钥（传方法值，密钥轮转后自动跟随）。
	// baseURL 指向边缘函数流式中继 /api/files/stream：EdgeOne 云函数响应被平台
	// 整体缓冲且有 ~6MB 上限，大文件必须经边缘函数（ReadableStream）按 Range
	// 分段回调 /api/files/proxy 再流式拼流，才能完整送达浏览器。
	storageMgr.SetProxySigner(jwtSecrets.Get, "/api/files/stream")

	authHandler := handler.NewAuthHandler(authService, jwtSecrets)
	fileHandler := handler.NewFileHandler(fileService)
	shareHandler := handler.NewShareHandler(shareService)
	userHandler := handler.NewUserHandler(storageMgr)
	settingHandler := handler.NewSettingHandler(jwtSecrets)
	policyHandler := handler.NewPolicyHandler(storageMgr, jwtSecrets.Get)
	groupHandler := handler.NewGroupHandler()
	adminUserHandler := handler.NewAdminUserHandler(storageMgr)
	webdavHandler := handler.NewWebDAVHandler(fileService)

	// gin.New 手动装配中间件：访问日志与 panic 恢复均走结构化单行 JSON，
	// 保持 EdgeOne 控制台按关键字检索的格式。
	r := gin.New()
	// WebDAV 服务：使用 Basic Auth 而非 JWT，须在其他中间件（含 CORS/JWTAuth）之前接管。
	// 外部访问 /api/dav/...，EdgeOne 剥离 /api 后此处收到 /dav/...。
	r.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/dav") {
			webdavHandler.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}
		c.Next()
	})
	r.Use(__edgeonePagesMiddleware())
	r.Use(middleware.AccessLog(), middleware.Recovery())

	r.Use(func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Credentials", "true")
		} else {
			c.Header("Access-Control-Allow-Origin", "*")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		c.Header("Access-Control-Max-Age", "86400")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// EdgeOne Makers：入口文件名 api.go 决定 URL 前缀 /api，
	// 请求到达 Gin 前会剥离 /api。因此路由不要再写 /api 前缀。
	// 前端仍访问 /api/auth/register、/api/files 等。
	auth := r.Group("/auth")
	auth.POST("/register", authHandler.Register)
	auth.POST("/login", authHandler.Login)

	// 公开站点信息（注册开关等，无需登录）
	r.GET("/site", settingHandler.GetPublicSite)

	// 无外链直链存储（如 Filen）的代理下载：无登录态，URL 携带 HMAC 签名校验
	r.GET("/files/proxy", fileHandler.ProxyDownload)
	// 生产环境该路径由边缘函数流式中继处理（edge-functions/api/files/stream.js）；
	// 此处注册同逻辑兜底，供本地开发（makers dev 可能不运行边缘函数）使用。
	r.GET("/files/stream", fileHandler.ProxyDownload)

	// 百度网盘 OAuth 回调（redirect_uri 固定路径，state 携带签名的策略 ID）
	r.GET("/oauth/baidu/callback", policyHandler.BaiduOAuthCallback)

	protected := r.Group("")
	protected.Use(middleware.JWTAuth(jwtSecrets))
	{
		files := protected.Group("/files")
		files.GET("", fileHandler.List)
		files.POST("/mkdir", fileHandler.Mkdir)
		files.POST("/upload", fileHandler.Upload)
		files.POST("/upload/server", fileHandler.UploadServer)
		files.POST("/upload/callback", fileHandler.UploadCallback)
		files.POST("/upload/multipart", fileHandler.MultipartInit)
		files.GET("/upload/multipart/sessions", fileHandler.MultipartSessions)
		files.POST("/upload/multipart/resume", fileHandler.MultipartResume)
		files.POST("/upload/multipart/complete", fileHandler.MultipartComplete)
		files.POST("/upload/multipart/abort", fileHandler.MultipartAbort)
		// 服务端中转分块上传（百度/TeraBox）：网关限单请求 body ≤6MB，
		// 客户端切 5MB 块逐块提交，服务端逐块转发存储端 superfile2。
		files.POST("/upload/chunked", fileHandler.ChunkedInit)
		files.POST("/upload/chunked/chunk", fileHandler.ChunkedUploadChunk)
		files.POST("/upload/chunked/complete", fileHandler.ChunkedComplete)
		// 批量操作（须注册在 /:id 系列之前，否则 "batch" 会被当作 :id）
		files.POST("/batch/delete", fileHandler.BatchDelete)
		files.POST("/batch/move", fileHandler.BatchMove)
		// GET + ?token= 鉴权：浏览器可直接导航，原生下载管理器接管 zip 流
		files.GET("/batch/download", fileHandler.BatchDownloadZip)
		files.GET("/:id/download", fileHandler.Download)
		files.GET("/:id/content", fileHandler.Content)
		files.GET("/:id/zip", fileHandler.DownloadDir)
		files.DELETE("/:id", fileHandler.Delete)
		files.PUT("/:id/rename", fileHandler.Rename)
		files.PUT("/:id/move", fileHandler.Move)

		// 用户侧：列出可用策略（上传选择，不含密钥）
		protected.GET("/storage/policies", policyHandler.ListPublic)

		shares := protected.Group("/shares")
		shares.POST("", shareHandler.Create)

		user := protected.Group("/user")
		user.GET("/profile", userHandler.Profile)
		user.PUT("/password", userHandler.ChangePassword)
		user.GET("/webdav", userHandler.GetWebDAVStatus)
		user.PUT("/webdav-password", userHandler.SetWebDAVPassword)
		user.GET("/webdav/password", userHandler.GetWebDAVPassword)

		// 管理员：参数设置 + 存储策略 CRUD
		admin := protected.Group("")
		admin.Use(middleware.RequireAdmin())
		{
			admin.GET("/settings/security", settingHandler.GetSecurity)
			admin.POST("/settings/security/rotate-jwt", settingHandler.RotateJWTSecret)
			admin.PUT("/settings/register", settingHandler.UpdateRegister)
			admin.GET("/settings/webdav", settingHandler.GetWebDAV)
			admin.PUT("/settings/webdav", settingHandler.UpdateWebDAV)

			adminPolicies := admin.Group("/admin/storage/policies")
			{
				adminPolicies.GET("", policyHandler.ListAdmin)
				adminPolicies.POST("", policyHandler.Create)
				adminPolicies.GET("/:id", policyHandler.GetAdmin)
				adminPolicies.PUT("/:id", policyHandler.Update)
				adminPolicies.DELETE("/:id", policyHandler.Delete)
				adminPolicies.POST("/:id/default", policyHandler.SetDefault)
				adminPolicies.POST("/:id/cors", policyHandler.SetCORS)
				// TeraBox OAuth 授权
				adminPolicies.GET("/:id/terabox/auth-url", policyHandler.TeraBoxAuthURL)
				adminPolicies.POST("/:id/terabox/auth-code", policyHandler.TeraBoxAuthByCode)
				adminPolicies.POST("/:id/terabox/devicecode", policyHandler.TeraBoxDeviceCode)
				adminPolicies.POST("/:id/terabox/auth-status", policyHandler.TeraBoxAuthStatus)
				// 百度网盘 OAuth 授权
				adminPolicies.GET("/:id/baidu/auth-url", policyHandler.BaiduAuthURL)
				adminPolicies.POST("/:id/baidu/auth-code", policyHandler.BaiduAuthByCode)
			}

			adminGroups := admin.Group("/admin/groups")
			{
				adminGroups.GET("", groupHandler.List)
				adminGroups.POST("", groupHandler.Create)
				adminGroups.GET("/:id", groupHandler.Get)
				adminGroups.PUT("/:id", groupHandler.Update)
				adminGroups.DELETE("/:id", groupHandler.Delete)
				adminGroups.POST("/:id/default", groupHandler.SetDefault)
			}

			adminUsers := admin.Group("/admin/users")
			{
				adminUsers.GET("", adminUserHandler.List)
				adminUsers.POST("", adminUserHandler.Create)
				adminUsers.GET("/:id", adminUserHandler.Get)
				adminUsers.PUT("/:id", adminUserHandler.Update)
				adminUsers.DELETE("/:id", adminUserHandler.Delete)
				adminUsers.PUT("/:id/ban", adminUserHandler.ToggleBan)
			}
		}
	}

	publicShares := r.Group("/shares")
	publicShares.GET("/:code", shareHandler.Get)
	publicShares.GET("/:code/files", shareHandler.List)
	publicShares.GET("/:code/files/:id/download", shareHandler.DownloadChild)
	publicShares.GET("/:code/download", shareHandler.Download)
	publicShares.GET("/:code/zip", shareHandler.DownloadZip)

	return r, nil
}

// newLazyBootstrap 懒加载自举：首个请求到达时从其 Host 头推导站点地址，
// 完成数据库恢复与全部初始化，之后所有请求直接走已构建好的路由。
// 初始化失败返回 503，下个请求重试。
func newLazyBootstrap(cfg *config.Config, syncer *persist.Syncer) http.Handler {
	var engine atomic.Pointer[gin.Engine]
	var mu sync.Mutex

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if eng := engine.Load(); eng != nil {
			eng.ServeHTTP(w, req)
			return
		}

		mu.Lock()
		defer mu.Unlock()
		if eng := engine.Load(); eng != nil {
			eng.ServeHTTP(w, req)
			return
		}

		baseURL := baseURLFromRequest(req)
		logx.Info(logx.ModulePersist, "从请求推导站点地址", "base_url", baseURL)
		syncer.SetBaseURL(baseURL)

		eng, err := buildApp(cfg, syncer)
		if err != nil {
			logx.Error(logx.ModuleApp, "延迟初始化失败", logx.Err(err))
			http.Error(w, "服务初始化中，请稍后重试", http.StatusServiceUnavailable)
			return
		}
		engine.Store(eng)
		eng.ServeHTTP(w, req)
	})
}

// baseURLFromRequest 从请求推导站点地址（Go 与 Node 函数共用同一对外域名）。
// 边缘终结 TLS 后转发，协议优先取 X-Forwarded-Proto；本地开发无该头时用 http。
func baseURLFromRequest(r *http.Request) string {
	scheme := "https"
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = strings.TrimSpace(strings.Split(p, ",")[0])
	} else if strings.HasPrefix(r.Host, "localhost") || strings.HasPrefix(r.Host, "127.0.0.1") {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

// __edgeoneGetPort 从环境变量 PORT 获取端口，如果未设置则使用默认值
// 由 EdgeOne Makers CLI 自动注入
func __edgeoneGetPort(defaultPort string) string {
	if port := os.Getenv("PORT"); port != "" {
		return port
	}
	return defaultPort
}

// __edgeoneStripPrefix 类似 http.StripPrefix，但确保 strip 后空路径变为 "/"
// 避免框架收到空路径后做 301 redirect
// 由 EdgeOne Makers CLI 自动注入
func __edgeoneStripPrefix(prefix string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, prefix)
		rp := strings.TrimPrefix(r.URL.RawPath, prefix)
		if len(p) < len(r.URL.Path) && (r.URL.RawPath == "" || len(rp) < len(r.URL.RawPath)) {
			if p == "" {
				p = "/"
			}
			if rp == "" && r.URL.RawPath != "" {
				rp = "/"
			}
			r2 := *r
			urlCopy := *r.URL
			urlCopy.Path = p
			urlCopy.RawPath = rp
			r2.URL = &urlCopy
			h.ServeHTTP(w, &r2)
		} else {
			http.NotFound(w, r)
		}
	})
}
