package main

import (
	"fmt"
	"os"
	"time"

	"github.com/odysseythink/mlog"
)

func main() {
	defer mlog.Flush()

	logDir := "/tmp/mlog_demo"
	os.MkdirAll(logDir, 0755)
	mlog.SetLogDir(logDir)

	fmt.Println("=== 当前模式日志输出 ===")
	fmt.Println()

	mlog.Info("服务启动完成")
	time.Sleep(100 * time.Millisecond)
	mlog.Infof("监听端口: %d", 8080)
	time.Sleep(100 * time.Millisecond)
	mlog.Info("请求处理完成",
		mlog.String("method", "GET"),
		mlog.String("path", "/api/users"),
		mlog.Int("status", 200),
	)

	logger := mlog.With(
		mlog.String("service", "user-api"),
		mlog.String("version", "1.0.0"),
	)
	time.Sleep(100 * time.Millisecond)
	logger.Info("用户登录", mlog.String("user_id", "abc123"))

	fmt.Println()
	fmt.Println("日志文件位置:", logDir)
}
