package middleware

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"strings"

	"github.com/cloudreve-eo/cloudreve-eo/internal/logx"
	"github.com/gin-gonic/gin"
)

// Recovery 捕获 handler panic，以 logx 单行 JSON 记录错误与堆栈，
// 替代 gin.Recovery() 的彩色纯文本输出（EdgeOne 控制台按关键字检索）。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				// 客户端中断连接触发的 panic 不是服务端错误，降级为 DEBUG
				if isBrokenPipe(r) {
					logx.With(logx.ModuleAccessLog).Debug("连接中断",
						"err", errString(r),
						"path", c.Request.URL.Path,
					)
					c.Abort()
					return
				}

				stack := make([]byte, 4096)
				n := runtime.Stack(stack, false)
				logx.Error(logx.ModuleAccessLog, "panic 恢复",
					"err", errString(r),
					"path", c.Request.URL.Path,
					"method", c.Request.Method,
					"stack", string(stack[:n]),
				)
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}

// errString 把 recover 值统一为字符串。
func errString(v any) string {
	switch e := v.(type) {
	case error:
		return e.Error()
	case string:
		return e
	default:
		return fmt.Sprint(v)
	}
}

// isBrokenPipe 判断是否为客户端中断（broken pipe / connection reset）。
func isBrokenPipe(v any) bool {
	err, ok := v.(error)
	if !ok {
		return false
	}
	var ne *net.OpError
	if errors.As(err, &ne) {
		msg := strings.ToLower(ne.Err.Error())
		return strings.Contains(msg, "broken pipe") || strings.Contains(msg, "connection reset by peer")
	}
	return false
}
