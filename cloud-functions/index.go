package main

import (
	"fmt"
	"log"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
	"github.com/cloudreve-eo/cloudreve-eo/internal/handler"
	"github.com/cloudreve-eo/cloudreve-eo/internal/middleware"
	"github.com/cloudreve-eo/cloudreve-eo/internal/model"
	"github.com/cloudreve-eo/cloudreve-eo/internal/service"
	"github.com/cloudreve-eo/cloudreve-eo/internal/storage"
	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	if err := model.InitDB(cfg); err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}

	// JWT 主密钥：库中已有则加载，否则自动生成写入库
	jwtSecret, err := model.EnsureJWTSecret()
	if err != nil {
		log.Fatalf("初始化 JWT 密钥失败: %v", err)
	}
	jwtSecrets := service.NewJWTSecretStore(jwtSecret)

	// 存储策略仅来自数据库；空则管理员在前端「存储策略」页添加
	storageMgr, err := storage.NewStoragePolicyManager()
	if err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}
	if n := len(storageMgr.ListPolicies()); n == 0 {
		log.Printf("尚未配置存储策略，请管理员在前端「存储策略」页面添加 S3 兼容策略")
	} else {
		log.Printf("已加载 %d 个存储策略，默认: %s", n, storageMgr.DefaultPolicy())
	}

	authService := service.NewAuthService()
	fileService := service.NewFileService(storageMgr)
	shareService := service.NewShareService(storageMgr)

	authHandler := handler.NewAuthHandler(authService, jwtSecrets)
	fileHandler := handler.NewFileHandler(fileService)
	shareHandler := handler.NewShareHandler(shareService)
	userHandler := handler.NewUserHandler(storageMgr)
	settingHandler := handler.NewSettingHandler(jwtSecrets)
	policyHandler := handler.NewPolicyHandler(storageMgr)

	r := gin.Default()

	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	api := r.Group("/api")

	auth := api.Group("/auth")
	auth.POST("/register", authHandler.Register)
	auth.POST("/login", authHandler.Login)

	// 公开站点信息（注册开关等，无需登录）
	api.GET("/site", settingHandler.GetPublicSite)

	protected := api.Group("")
	protected.Use(middleware.JWTAuth(jwtSecrets))
	{
		files := protected.Group("/files")
		files.GET("", fileHandler.List)
		files.POST("/mkdir", fileHandler.Mkdir)
		files.POST("/upload", fileHandler.Upload)
		files.POST("/upload/callback", fileHandler.UploadCallback)
		files.GET("/:id/download", fileHandler.Download)
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

		// 管理员：参数设置 + 存储策略 CRUD
		admin := protected.Group("")
		admin.Use(middleware.RequireAdmin())
		{
			admin.GET("/settings/security", settingHandler.GetSecurity)
			admin.POST("/settings/security/rotate-jwt", settingHandler.RotateJWTSecret)
			admin.PUT("/settings/register", settingHandler.UpdateRegister)

			adminPolicies := admin.Group("/admin/storage/policies")
			{
				adminPolicies.GET("", policyHandler.ListAdmin)
				adminPolicies.POST("", policyHandler.Create)
				adminPolicies.GET("/:id", policyHandler.GetAdmin)
				adminPolicies.PUT("/:id", policyHandler.Update)
				adminPolicies.DELETE("/:id", policyHandler.Delete)
				adminPolicies.POST("/:id/default", policyHandler.SetDefault)
			}
		}
	}

	publicShares := api.Group("/shares")
	publicShares.GET("/:code", shareHandler.Get)
	publicShares.GET("/:code/download", shareHandler.Download)

	// EdgeOne Makers 构建时会注入端口适配；本地 makers dev 也按此约定监听。
	// 平台文档推荐标准写法 r.Run(":9000")，勿改为独立进程式启动。
	fmt.Printf("Cloudreve-EO 启动中\n")
	if err := r.Run(":9000"); err != nil {
		log.Fatalf("启动服务失败: %v", err)
	}
}
