package main

import (
	"context"
	"log"
	"log/slog"

	"github.com/bkcarlos/goparts/logger"
)

func main() {
	var level slog.LevelVar
	l, err := logger.New(logger.Config{
		Service:     "example",
		Environment: "local",
		Level:       &level,
	})
	if err != nil {
		log.Fatal(err)
	}
	ctx := logger.WithContext(context.Background(), l)
	ctx = logger.WithFields(ctx, "request_id", "demo-001")
	logger.FromContext(ctx).InfoContext(ctx, "日志模块初始化成功")
	level.Set(slog.LevelDebug)
	logger.FromContext(ctx).DebugContext(ctx, "动态调整日志级别", "module", "example")
}
