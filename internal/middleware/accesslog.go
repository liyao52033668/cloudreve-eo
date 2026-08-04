package middleware

import (
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/logx"
	"github.com/gin-gonic/gin"
)

// AccessLog 以结构化 JSON 输出每个请求的访问日志，
// 替代 gin.Logger 的默认彩色文本（EdgeOne 控制台按关键字检索）。
// 5xx 记 ERROR，4xx 记 WARN，其余 INFO；跳过 404 避免静态资源探测刷屏。
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		if status == 404 {
			return
		}

		msg := c.Request.Method + " " + c.Request.URL.Path
		attrs := []any{
			"status", status,
			"latency_ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
			"bytes", c.Writer.Size(),
		}
		switch {
		case status >= 500:
			logx.Error(logx.ModuleAccessLog, msg, attrs...)
		case status >= 400:
			logx.Warn(logx.ModuleAccessLog, msg, attrs...)
		default:
			logx.Info(logx.ModuleAccessLog, msg, attrs...)
		}
	}
}
