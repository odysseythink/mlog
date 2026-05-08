# Unified Log Mode Design

## 背景

当前 mlog 同时提供两套独立的日志 API：

- **传统 API**：`Infof`、`Warningf`、`Errorf` 等 —— 走 `textPrintf` 路径，预格式化为 `[]byte` 后入 ring buffer
- **结构化 API**：`S().Info(msg, fields...)` —— 走 `Entry` 路径，异步 writer 中调用 `EncodeEntry` 编码

实际生产中，一个项目只会选择一种日志风格（结构化或 printf），不会混用。当前用 `S()` 作为结构化入口的搞法增加了 API 复杂度，且两套 API 在调用层完全独立，容易让使用者困惑。

## 目标

1. 去掉 `S()` 入口，统一日志调用方式
2. 通过运行时开关（flag）在启动时确定全局走结构化还是 printf 路径
3. 保留所有现有全局函数（`Info/Infof/Infoln` 等），在结构化模式下自动走 Entry 路径
4. 用 `mlog.With(fields...)` 替代 `S().With(fields...)`，支持绑定持久字段的子 logger

## 非目标

- 不支持同一进程内混用两种模式（flag 确定后全局统一）
- 不改变 ring buffer、async writer、Encoder 等底层管道
- 不增加 build tag 等编译期切换机制

## 架构设计

核心改动：新增运行时 **LogMode** 开关，全局决定底层走 `textPrintf` 还是 `Entry` 路径。

```
                    调用方
                      │
        ┌─────────────┼─────────────┐
        │             │             │
    Info/Infof   With(fields)   V(2).Info
        │             │             │
        ▼             ▼             ▼
   ┌─────────────────────────────────────┐
   │         全局 mode 判断               │
   │   ┌──────────┐    ┌──────────┐     │
   │   │ LogMode  │ or │ LogMode  │     │
   │   │ Printf   │    │Structured│     │
   │   └────┬─────┘    └────┬─────┘     │
   │        │               │            │
   │   textPrintf      Entry + Fields    │
   │        │               │            │
   │        └───────┬───────┘            │
   │                ▼                    │
   │           logEntry                  │
   │       (data []byte or entry *Entry) │
   └────────────────┬────────────────────┘
                    ▼
              Ring Buffer
                    │
                    ▼
            Async Writer
              EncodeEntry (structured 模式)
                    │
                    ▼
                 File
```

关键点：

- `printf` 模式（默认）：保持现有全部行为，`With` 的字段被忽略
- `structured` 模式：全局函数内部构造 `Entry`，走 `EncodeEntry` 路径
- 两种模式共享 ring buffer + async writer，改动集中在调用入口层
- mode 设置后不可变（首次日志输出后固定）

## API 变更

### 去掉的

- `S()` —— 完全删除，`With(fields...)` 取代其功能

### 新增的

```go
// LogMode 控制全局日志模式
type LogMode int8
const (
    LogModePrintf     LogMode = iota // 默认：传统 printf 风格
    LogModeStructured                 // 结构化：走 Entry + Encoder 路径
)

// SetLogMode 用于编程式设置（必须在任何日志输出前调用）
func SetLogMode(mode LogMode)

// With 返回一个携带持久字段的 logger，替代原来的 S().With()
func With(fields ...Field) *Logger
```

### 重构的（原 StructuredLogger → Logger）

```go
type Logger struct {
    fields []Field
}

func (l *Logger) With(fields ...Field) *Logger
func (l *Logger) Info(msg string, fields ...Field)
func (l *Logger) Infof(format string, args ...any)
func (l *Logger) Infoln(args ...any)
func (l *Logger) Debug(msg string, fields ...Field)
func (l *Logger) Debugf(format string, args ...any)
// Warning、Error、Fatal 同理
```

### 行为变更的（全局函数）

**`printf` 模式下：** 完全不变，和当前行为一致。

**`structured` 模式下：**

| 函数 | 结构化行为 |
|---|---|
| `Info(args...)` | args[0] 为 msg（string），args[1:] 中的 `Field` 提取到 entry.Fields |
| `Infof(format, args...)` | `fmt.Sprintf(format, args...)` 为 msg，无额外 fields |
| `Infoln(args...)` | 拼接为 msg，无额外 fields |
| `InfoContext(ctx, args...)` | 同 Info，ctx 用于 trace（存到 LogsinkMeta） |
| `InfoContextf(ctx, format, args...)` | 同 Infof |
| `InfoDepth/InfoDepthf` | 同 Info/Infof，depth 用于正确获取调用栈 |

所有 `ln` 和 `Depth` 变体同理。如果 `Info(args...)` 的 args[0] 不是 string，结构化模式下 panic。

### V 日志

`mlog.V(2).Info(...)` 同样根据 mode 分流：

- `printf`：现有行为
- `structured`：构造 Entry 走结构化路径

## 实现细节

### mode 设置与不变性

```go
var (
    logMode     atomic.Int32 // 0=printf, 1=structured
    modeSetOnce sync.Once
)

func SetLogMode(mode LogMode) {
    modeSetOnce.Do(func() {
        logMode.Store(int32(mode))
    })
}

func getMode() LogMode {
    return LogMode(logMode.Load())
}
```

`-log_mode` flag 在 `init()` 中解析并调用 `SetLogMode`。任何日志输出（包括 `getMode()` 的第一次调用）都会触发 flag 的 lazy init，之后模式不可变。

### 全局函数分流（以 Info 系列为例）

```go
func Info(args ...any) {
    if getMode() == LogModeStructured {
        infoStructured(1, args...)
    } else {
        InfoDepth(1, args...)
    }
}

func Infof(format string, args ...any) {
    if getMode() == LogModeStructured {
        msg := fmt.Sprintf(format, args...)
        globalLogger.log(Severity_Info, msg, nil)
    } else {
        logf(1, Severity_Info, false, noStack, format, args...)
    }
}
```

`infoStructured` 负责从 `args...` 中提取 msg 和 fields，调用 `globalLogger.log`（无绑定字段的全局 logger 实例）。

### Logger 实现

```go
var globalLogger = &Logger{}

func With(fields ...Field) *Logger {
    return globalLogger.With(fields...)
}

func (l *Logger) With(fields ...Field) *Logger {
    merged := make([]Field, 0, len(l.fields)+len(fields))
    merged = append(merged, l.fields...)
    merged = append(merged, fields...)
    return &Logger{fields: merged}
}

func (l *Logger) Info(msg string, fields ...Field) {
    if getMode() == LogModeStructured {
        l.log(Severity_Info, msg, fields)
    } else {
        // printf 模式：忽略 fields，直接输出 msg
        InfoDepth(1, msg)
    }
}
```

`Logger.log` 复用现有的 `StructuredLogger.log` 逻辑，构造 Entry → `structuredEmit`。

### 编码器

编码器选择（text/json/logfmt）在两种模式下都可用：

- `printf` 模式：输出格式由 `textPrintf` 的硬编码格式决定
- `structured` 模式：由 `Encoder.EncodeEntry` 决定

### Context 传递

`InfoContext` / `InfoContextf` 系列在结构化模式下，context 仍通过 `LogsinkMeta` 传递。由于 `structuredEmit` 当前不处理 Context，这是已知限制，不在本次改动范围内。

## 测试策略

1. **默认模式测试**：现有全部测试在 `printf` 模式下继续通过（零回归）
2. **模式切换测试**：`SetLogMode(LogModeStructured)` 后验证 `Info/Infof` 走 Entry 路径
3. **混合调用测试**：`With(fields).Infof("msg")` 在两种模式下的行为
4. **V 日志模式测试**：`V(2).Info` 在结构化模式下正确分流
5. **边界测试**：`Info` 传入非 string 首参数在结构化模式下的 panic 行为

## 性能

- `printf` 模式：和现有完全一致，无额外开销
- `structured` 模式：全局函数多一次 `atomic.Load` + 分支判断，纳秒级开销
- `Logger` 方法：比原来 `StructuredLogger` 多一次 mode 判断，同样纳秒级

## 文件变更预估

| 文件 | 变更 |
|---|---|
| `constants.go` 或新文件 | 新增 `LogMode` 类型、flag、`SetLogMode` |
| `mlog.go` | 全局函数增加 mode 分支；V 日志方法增加分支 |
| `structured.go` | `StructuredLogger` 重命名为 `Logger`；删除 `S()`；新增 `With()` |
| `mlog_flags.go` | 注册 `-log_mode` flag |
| `*_test.go` | 新增结构化模式下的全局函数测试 |

## 迁移指南

### 旧代码

```go
mlog.S().Info("请求完成", mlog.String("path", "/api"))
logger := mlog.S().With(mlog.String("svc", "user"))
logger.Info("用户登录", mlog.String("id", "123"))
```

### 新代码

```go
mlog.Info("请求完成", mlog.String("path", "/api"))  // 需 -log_mode=structured
logger := mlog.With(mlog.String("svc", "user"))
logger.Info("用户登录", mlog.String("id", "123"))
```

printf 风格代码完全不变：

```go
mlog.Infof("监听端口: %d", 8080)
```

## 风险评估

| 风险 | 缓解 |
|---|---|
| 全局函数增加分支导致性能微降 | `atomic.Load` 开销可忽略，printf 模式下分支可预测 |
| `Info(args...)` 结构化模式下类型不安全 | 文档明确说明 args[0] 必须为 string，否则 panic；生产代码通常不会错用 |
| 去掉 `S()` 破坏现有用户 | 这是有意为之的 breaking change，配合版本号升级 |
