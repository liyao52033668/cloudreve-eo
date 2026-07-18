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

	// JWT 主密钥：库中已有 > 环境变量引导写入 > 自动生成
	jwtSecret, err := model.EnsureJWTSecret(cfg.JWT.Secret)
	if err != nil {
		log.Fatalf("初始化 JWT 密钥失败: %v", err)
	}
	if cfg.JWT.Secret == "" {
		log.Printf("JWT 主密钥已从数据库加载或自动生成（无需环境变量）")
	}
	jwtSecrets := service.NewJWTSecretStore(jwtSecret)

	storageMgr, err := storage.NewStoragePolicyManager(cfg)
	if err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}

	authService := service.NewAuthService(cfg)
	fileService := service.NewFileService(storageMgr)
	shareService := service.NewShareService(storageMgr)

	authHandler := handler.NewAuthHandler(authService, jwtSecrets)
	fileHandler := handler.NewFileHandler(fileService)
	shareHandler := handler.NewShareHandler(shareService)
	userHandler := handler.NewUserHandler()
	settingHandler := handler.NewSettingHandler(jwtSecrets)

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

		protected.GET("/storage/policies", fileHandler.ListStoragePolicies)

		shares := protected.Group("/shares")
		shares.POST("", shareHandler.Create)

		user := protected.Group("/user")
		user.GET("/profile", userHandler.Profile)
		user.PUT("/password", userHandler.ChangePassword)

		// 管理员：参数设置（JWT 主密钥、注册开关）
		admin := protected.Group("")
		admin.Use(middleware.RequireAdmin())
		{
			admin.GET("/settings/security", settingHandler.GetSecurity)
			admin.POST("/settings/security/rotate-jwt", settingHandler.RotateJWTSecret)
			admin.PUT("/settings/register", settingHandler.UpdateRegister)
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
