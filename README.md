# mlog

Go 结构化日志库，兼容 glog 风格 API，支持零分配热路径、可插拔编码器（JSON/logfmt/text）和异步写入。

## 特性

- **glog 兼容 API**：`Infof`、`Warningf`、`Errorf`、`Fatalf` 等，可直接替换 glog
- **结构化日志**：`S().Info(msg, fields...)` 零分配热路径（~40ns/op, 0 alloc）
- **可插拔编码器**：text（默认）、JSON Lines、logfmt，通过 `-log_encoder` 标志切换
- **异步写入**：基于 ring buffer 的批量异步 writer，不阻塞业务 goroutine
- **自动轮转**：按大小自动切割日志文件，支持符号链接
- **V 日志**：`-v` 和 `-vmodule` 控制细粒度日志级别
- **采样限速**：`-log_rate_limit` 控制 Debug/Info/Warning 输出速率
- **Context 传递**：`InfoContext`、`ErrorContext` 系列函数传递 trace context

## 安装

```bash
go get github.com/odysseythink/mlog
```

要求 Go 1.21+。

## 快速开始

### 基本用法（glog 风格）

```go
package main

import (
    "github.com/odysseythink/mlog"
)

func main() {
    defer mlog.Flush()

    mlog.Info("服务启动")
    mlog.Infof("监听端口: %d", 8080)
    mlog.Warning("配置项缺失，使用默认值")
    mlog.Errorf("连接失败: %v", err)
    // mlog.Fatalf("无法恢复的错误")  // 会输出堆栈并调用 os.Exit(255)
}
```

### 结构化日志

```go
package main

import (
    "github.com/odysseythink/mlog"
)

func main() {
    defer mlog.Flush()

    // 基本结构化日志
    mlog.S().Info("请求处理完成",
        mlog.String("method", "GET"),
        mlog.String("path", "/api/users"),
        mlog.Int("status", 200),
        mlog.Duration("elapsed", 12*time.Millisecond),
    )

    // 绑定持久字段
    logger := mlog.S().With(
        mlog.String("service", "user-api"),
        mlog.String("version", "1.0.0"),
    )
    logger.Info("用户登录", mlog.String("user_id", "abc123"))
    logger.Error("数据库超时", mlog.Err(err))
}
```

### V 日志（条件日志）

```go
// 只在 -v=2 及以上时输出
if mlog.V(2) {
    mlog.Info("详细调试信息")
}

// 链式调用
mlog.V(2).Infof("处理了 %d 条记录", count)
```

### 切换编码器

```go
// 编程方式切换
mlog.SetEncoder(mlog.NewJSONEncoder())

// 或通过命令行标志
// ./your_app -log_encoder=json
```

输出对比：

```
# text（默认）
[2026-04-30 10:00:00.123456][I][   1234][main.go:42] 请求处理完成 method=GET status=200

# json (-log_encoder=json)
{"ts":"2026-04-30T10:00:00.123456Z","level":"INFO","caller":"main.go:42","msg":"请求处理完成","method":"GET","status":200}

# logfmt (-log_encoder=logfmt)
ts=2026-04-30T10:00:00.123456Z level=INFO caller=main.go:42 msg="请求处理完成" method=GET status=200
```

## API 参考

### 日志函数

每个级别（Debug/Info/Warning/Error/Fatal）都有以下变体：

| 函数 | 说明 |
|---|---|
| `Info(args...)` | 输出 Info 级别日志 |
| `Infoln(args...)` | 空格分隔输出 |
| `Infof(format, args...)` | 格式化输出 |
| `InfoDepth(depth, args...)` | 指定调用栈深度 |
| `InfoDepthf(depth, format, args...)` | 指定深度 + 格式化 |
| `InfoContext(ctx, args...)` | 携带 context |
| `InfoContextf(ctx, format, args...)` | 携带 context + 格式化 |
| `InfoContextDepth(ctx, depth, args...)` | 携带 context + 深度 |
| `InfoContextDepthf(ctx, depth, format, args...)` | 全部参数 |

Fatal 变体额外输出堆栈跟踪并调用 `os.Exit(255)`。

### 结构化日志 API

```go
// 获取全局 StructuredLogger
logger := mlog.S()

// 绑定持久字段（返回新实例，不修改原对象）
reqLogger := logger.With(mlog.String("request_id", "abc"))

// 日志输出
logger.Info("消息", mlog.Int("key", 42))
logger.Warning("警告", mlog.Err(err))
logger.Error("错误", mlog.String("detail", "连接超时"))
logger.Fatal("致命错误")  // 调用 os.Exit
```

### 字段构造器

| 构造器 | 类型 | 示例 |
|---|---|---|
| `Int(key, val)` | int | `mlog.Int("status", 200)` |
| `Int64(key, val)` | int64 | `mlog.Int64("bytes", 1024)` |
| `Float64(key, val)` | float64 | `mlog.Float64("ratio", 0.95)` |
| `String(key, val)` | string | `mlog.String("method", "GET")` |
| `Bool(key, val)` | bool | `mlog.Bool("ok", true)` |
| `Duration(key, val)` | time.Duration | `mlog.Duration("elapsed", 5*time.Millisecond)` |
| `Err(err)` | error | `mlog.Err(err)` (key 固定为 `"error"`) |
| `Any(key, val)` | any | `mlog.Any("data", payload)` |

字段构造器在栈上分配，不产生堆内存分配。

### 日志级别

```go
const (
    Severity_Debug   Severity = 0  // "DEBUG"
    Severity_Info    Severity = 1  // "INFO"
    Severity_Warning Severity = 2  // "WARNING"
    Severity_Error   Severity = 3  // "ERROR"
    Severity_Fatal   Severity = 4  // "FATAL"
)
```

### V 日志

```go
level := mlog.Level(2)
v := mlog.V(level)       // 返回 mlog.Verbose (bool)
v.Info("调试信息")       // 只在 -v >= 2 时输出
v.Infof("count=%d", n)   // 格式化版本
```

### 编码器

```go
// 实现自定义编码器
type Encoder interface {
    EncodeEntry(entry *Entry) []byte
    Clone() Encoder
}

// 内置编码器
mlog.SetEncoder(mlog.NewJSONEncoder())
mlog.SetEncoder(mlog.NewLogfmtEncoder())
mlog.SetEncoder(mlog.NewTextEncoder())  // 默认
```

### 日志文件管理

```go
mlog.SetLogDir("/var/log/myapp")     // 设置日志目录
mlog.SetMaxLogSize(500)              // 单文件最大 500MB
mlog.Flush()                         // 刷新缓冲区
mlog.Close()                         // 关闭所有日志文件

// 获取日志文件路径
names, err := mlog.Names(mlog.Severity_Info)
```

### 堆栈跟踪

```go
stack := mlog.StackdumpCaller(0)  // 当前 goroutine 堆栈
fmt.Println(stack.String())

text := mlog.CallerText(0)        // 堆栈文本
pcs := mlog.CallerPC(0)           // 程序计数器切片
```

## 命令行标志

| 标志 | 默认值 | 说明 |
|---|---|---|
| `-v` | 0 | V 日志级别 |
| `-vmodule` | "" | 按文件设置 V 级别，如 `file=2,directory/=1` |
| `-log_dir` | "" | 日志文件目录 |
| `-log_link` | "" | 额外的符号链接目录 |
| `-logtostderr` | false | 仅输出到 stderr |
| `-alsologtostderr` | false | 同时输出到文件和 stderr |
| `-stderrthreshold` | ERROR | 输出到 stderr 的最低级别 |
| `-log_backtrace_at` | "" | 在指定位置输出堆栈，如 `file.go:123` |
| `-log_encoder` | text | 编码器：text、json、logfmt |
| `-log_ring_size` | 4096 | Ring buffer 大小 |
| `-log_batch_size` | 64 | 批量写入大小 |
| `-log_rate_limit` | 0 | 每秒日志条数限制（0=不限） |
| `-logbuflevel` | INFO | 缓冲写入的最低级别 |

## 运行示例

```bash
# 编译运行
go run example/demo01/main.go -log_dir=/tmp/mlog -log_encoder=json -v=2

# 仅输出到 stderr
go run example/demo01/main.go -logtostderr

# 限速（每秒最多 100 条 Debug/Info/Warning）
go run example/demo01/main.go -log_rate_limit=100
```

## 架构

```
调用方
  │
  ├── Infof/Warningf/Errorf  (传统 API)
  │     │
  │     ▼
  │   textPrintf ──► data []byte ──► logEntry ──┐
  │                                               │
  ├── S().Info(msg, fields...)  (结构化 API)     │
  │     │                                        │
  │     ▼                                        ▼
  │   Entry + Fields ──► logEntry ──► Ring Buffer ──► AsyncWriter ──► File
  │
  └── Encoder.EncodeEntry()  (异步 writer 中调用)
```

- **Entry 池化**：`sync.Pool` 复用 Entry 和 Fields 切片（cap 16），避免热路径分配
- **双路写入**：传统 API 发送 `data []byte`，结构化 API 发送 `entry *Entry`，共享 ring buffer 和 async writer
- **批量异步**：async writer 从 ring buffer 批量读取，调用 `EncodeEntry` 编码后写入文件
- **ERROR 确认**：Error 及以上级别阻塞等待 ack，保证日志持久化可见

## 性能

Apple M4 Pro 上的 benchmark 结果：

| Benchmark | ns/op | allocs/op |
|---|---|---|
| FieldConstruction | 0.23 | 0 |
| StructuredInfo（完整热路径） | ~40 | 0 |
| EntryPool (get + put) | 7.2 | 0 |
| TextEncoderEncode | 103 | 1 |
| JSONEncoderEncode | 217 | 2 |

零分配热路径：`S().Info()` 的 Entry 构建、字段合并、ring buffer 推送全程无堆分配。

## 许可证

[添加许可证信息]
