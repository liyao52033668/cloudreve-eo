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

	if cfg.JWT.Secret == "" {
		log.Fatal("JWT_SECRET 环境变量未设置")
	}

	if err := model.InitDB(cfg); err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}

	storageMgr, err := storage.NewStoragePolicyManager(cfg)
	if err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}

	authService := service.NewAuthService(cfg)
	fileService := service.NewFileService(storageMgr)
	shareService := service.NewShareService(storageMgr)

	authHandler := handler.NewAuthHandler(authService, cfg.JWT.Secret)
	fileHandler := handler.NewFileHandler(fileService)
	shareHandler := handler.NewShareHandler(shareService)
	userHandler := handler.NewUserHandler()

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

	protected := api.Group("")
	protected.Use(middleware.JWTAuth(cfg.JWT.Secret))
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

		shares := protected.Group("/shares")
		shares.POST("", shareHandler.Create)

		user := protected.Group("/user")
		user.GET("/profile", userHandler.Profile)
		user.PUT("/password", userHandler.ChangePassword)
	}

	publicShares := api.Group("/shares")
	publicShares.GET("/:code", shareHandler.Get)
	publicShares.GET("/:code/download", shareHandler.Download)

	fmt.Printf("Cloudreve-EO 启动中，端口: %s\n", cfg.Server.Port)
	if err := r.Run(":" + cfg.Server.Port); err != nil {
		log.Fatalf("启动服务失败: %v", err)
	}
}
