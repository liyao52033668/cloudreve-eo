package main

import (
	"fmt"
	"log"

	"github.com/cloudreve-eo/cloudreve-eo/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	fmt.Printf("Cloudreve-EO 启动中，端口: %s\n", cfg.Server.Port)
	// 后续任务会逐步填充：数据库初始化、路由注册、服务启动
}
