//go:build !edgeone

package main

import "github.com/gin-gonic/gin"

// __edgeonePagesMiddleware EdgeOne 平台构建时注入的中间件（本地开发 stub）。
// 本地开发时返回空中间件，EdgeOne 环境由平台提供实际实现。
func __edgeonePagesMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}
