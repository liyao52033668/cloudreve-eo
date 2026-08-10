// Package logx 提供统一的结构化日志。
//
// EdgeOne Makers 云函数自动采集 stdout/stderr，用户日志夹在平台
// START/END RequestId 之间，控制台按关键字检索。因此所有日志
// 经 slog.NewJSONHandler 以单行 JSON 输出到 stdout：
//
//	{"time":"2026-08-04T10:00:00Z","level":"INFO","module":"persist","msg":"已从 edgeone-blob 恢复数据库","bytes":1048576}
//
// 级别经 LOG_LEVEL 环境变量控制，默认 info（debug / info / warn / error）。
package logx

import (
	"log/slog"
	"os"
)

var level slog.LevelVar

// Setup 初始化全局 slog：JSON 单行输出到 stdout。
// levelStr 为空或无效时默认 info。应在进程启动最前面调用，
// 使 config.Load 之前的致命错误也能以统一格式落盘。
func Setup(levelStr string) {
	if err := level.UnmarshalText([]byte(levelStr)); err != nil {
		level.Set(slog.LevelInfo)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: &level})))
}

// 模块名：日志检索的关键字，与包名对应。
const (
	ModuleApp       = "app"       // 入口 api.go
	ModuleConfig    = "config"    // internal/config
	ModulePersist   = "persist"   // internal/persist
	ModuleStorage   = "storage"   // internal/storage
	ModuleDB        = "db"        // GORM 数据库日志
	ModuleAccessLog = "accesslog" // HTTP 请求访问日志
	ModuleHandler   = "handler"   // internal/handler
)

// With 派生带模块名的 logger。
func With(module string) *slog.Logger {
	return slog.Default().With(slog.String("module", module))
}

// Info / Warn 按模块输出。
func Info(module, msg string, args ...any) {
	With(module).Info(msg, args...)
}

func Warn(module, msg string, args ...any) {
	With(module).Warn(msg, args...)
}

// Error 输出错误日志，err 需经 Err 包装后以 k/v 形式传入。
func Error(module, msg string, args ...any) {
	With(module).Error(msg, args...)
}

// Err 把 error 包装为日志属性（字段名 err）。
func Err(err error) slog.Attr {
	return slog.Any("err", err)
}
