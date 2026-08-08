package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudreve-eo/cloudreve-eo/internal/logx"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// slowThreshold 慢 SQL 阈值：超过该耗时记 WARN。
const slowThreshold = 200 * time.Millisecond

// gormLogger 实现 gorm logger.Interface，把 GORM 内部日志（连接失败、
// 慢 SQL、SQL 错误）转发到 logx，保持 EdgeOne 单行 JSON 格式，
// 替代默认的 log 标准库纯文本输出。
type gormLogger struct{}

func (gormLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface { return gormLogger{} }

func (gormLogger) Info(_ context.Context, msg string, args ...any) {
	logx.With(logx.ModuleDB).Info(fmt.Sprintf(strings.TrimSpace(msg), args...))
}

func (gormLogger) Warn(_ context.Context, msg string, args ...any) {
	logx.With(logx.ModuleDB).Warn(fmt.Sprintf(strings.TrimSpace(msg), args...))
}

func (gormLogger) Error(_ context.Context, msg string, args ...any) {
	logx.With(logx.ModuleDB).Error(fmt.Sprintf(strings.TrimSpace(msg), args...))
}

// Trace 每次 SQL 执行的回调：错误记 ERROR（ErrRecordNotFound 是正常未命中，跳过），
// 超阈值记 WARN 慢 SQL，正常执行记 DEBUG。
func (gormLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()
	l := logx.With(logx.ModuleDB)
	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		l.Error("SQL 执行失败", "err", err.Error(), "sql", sql, "rows", rows, "elapsed_ms", elapsed.Milliseconds())
	case elapsed > slowThreshold:
		l.Warn("慢 SQL", "sql", sql, "rows", rows, "elapsed_ms", elapsed.Milliseconds())
	default:
		l.Debug("SQL", "sql", sql, "rows", rows, "elapsed_ms", elapsed.Milliseconds())
	}
}
