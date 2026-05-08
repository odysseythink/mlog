# Comprehensive Testing Design

## 背景

mlog 当前测试覆盖率仅 **45.8%**，大量新增代码（尤其是 `LogMode` 分支和 `Logger` 新方法）处于未测试状态。需要建立全面的单元测试和压力测试体系，确保：

1. 两种模式（printf / structured）下所有代码路径正确
2. 性能基线可量化、可追踪
3. 并发场景下无 race、无内存泄漏

## 目标

### 单元测试
- 核心模块覆盖率提升至 **90%+**
- 所有 `LogMode` 分支均有测试覆盖
- 所有 `Logger` 方法（含新加的 11 个 printf/ln 方法）均有测试
- 三种编码器（text/json/logfmt）覆盖所有字段类型

### 压力测试
- 全维度性能基准：吞吐量、延迟分位、内存分配、CPU 热点
- 并发梯度：1/4/8/16/32/64 goroutine
- 模式对比：printf vs structured
- 编码器对比：text vs json vs logfmt
- fields 数量对比：0/3/10 fields

## 非目标

- 不测试文件 sink 的 O_EXCL 冲突 bug（项目已有问题，不在本次修复范围）
- 不做集成测试（如多进程日志轮转、磁盘满等极端场景）
- 不做跨平台测试（聚焦 darwin/linux 通用路径）

## 架构

三个阶段分步推进，每阶段有独立质量门：

```
阶段 1: 单元测试补全 (覆盖率 45.8% → 90%+)
    │
    ├── 模块级测试（按文件分组）
    └── 质量门: go test -cover ≥ 90%

阶段 2: 压力测试框架 + 基础场景
    │
    ├── Benchmark 文件 + 基础吞吐/内存指标
    └── 质量门: 3 次运行变异 < 5%

阶段 3: 扩展压力测试
    │
    ├── 并发梯度 / 延迟分位 / CPU & 内存 profiling
    └── 质量门: 完整报告输出
```

## 阶段 1：单元测试补全

### 测试文件组织

| 测试文件 | 覆盖的源码文件 | 当前覆盖率 | 目标 |
|---|---|---|---|
| `mode_test.go`（已有） | `constants.go`, `mlog.go` mode 分支 | ~60% | 85%+ |
| `structured_test.go`（已有） | `structured.go` | ~40% | 90%+ |
| `encoder_test.go`（已有） | `encoder_*.go` | ~60% | 95%+ |
| `logsink_test.go`（已有） | `logsink.go` | ~50% | 90%+ |
| `ringbuffer_test.go`（已有） | `ringbuffer.go` | ~70% | 90%+ |
| `async_writer_test.go`（已有） | `async_writer.go` | ~60% | 85%+ |

### 各模块具体测试内容

#### mode_test.go — 补全局函数分支

新增测试覆盖：
- `infoStructured`：args 为空、args[0] 非 string、args 中含 Field、无 Field
- `infofStructured`：format + args 组合
- `infoLnStructured`：多参数拼接
- `infoContextStructured` / `ctxlogStructured`：ctx 传递、字段提取
- 所有 severity 的全局函数在两种模式下的路由：Debug/Debugf、Info/Infof、Warning/Warningf、Error/Errorf、Fatal/Fatalf、Exit/Exitf
- `Verbose` 方法在两种模式下的路由

#### structured_test.go — 补 Logger 方法

新增测试覆盖：
- `Debug(msg, fields)`、`Debugf(format, args)`、`Debugln(args)`
- `Infof(format, args)`、`Infoln(args)`
- `Warningf(format, args)`、`Warningln(args)`
- `Errorf(format, args)`、`Errorln(args)`
- `Fatalf(format, args)`、`Fatalln(args)`
- 以上方法在 **printf 模式** 和 **structured 模式** 下的行为验证

#### encoder_test.go — 补字段类型

当前已覆盖 `Int`, `Bool`, `Duration`。需补全：
- `Int64`、`Float64`、`String`、`Err`、`Any`
- 边界值：空字符串、nil error、负数、极大值
- JSON 转义：含引号、反斜杠、换行符的字符串
- logfmt 编码器的引号处理

#### logsink_test.go — 补 textPrintf

新增测试：
- `textPrintf` 各种 format + args 组合
- `LogsinkPrintf` 多 sink 场景
- `MaxLogMessageLen` 截断
- `backtraceAt` 触发堆栈
- rate limiter 拦截路径

#### ringbuffer_test.go — 补并发

新增测试：
- `tryPush` 满队列返回 false
- `drainBatch` 正确读取顺序
- 多 goroutine 并发 push/drain（race detector 通过）
- `close` 后 push 返回 false

#### async_writer_test.go — 补 writer 生命周期

新增测试：
- `writeBatch` 正确编码 entry + data 混合
- `flushBuf` 刷盘 + ack 信号
- `writerLoop` 正常关闭流程
- ERROR 级别 ack 超时路径

### 规避文件 sink bug 的策略

项目中已有的文件 sink 在测试中会因 `O_EXCL` 冲突崩溃。测试中将使用：

```go
// 禁用文件 sink，避免 O_EXCL 冲突
origTextSinks := mlog.TextSinks
mlog.TextSinks = nil
defer func() { mlog.TextSinks = origTextSinks }()
```

对于必须测试文件输出的 case，使用 `mlog.SetLogDir` + 唯一目录 + 每次 1 秒间隔。

### 质量门

```bash
go test ./... -race -coverprofile=/tmp/cover.out
go tool cover -func=/tmp/cover.out | grep "total:"  # ≥ 90%
```

## 阶段 2：压力测试框架 + 基础场景

### Benchmark 文件组织

| 文件 | 内容 |
|---|---|
| `bench_print_test.go` | printf 模式基准测试 |
| `bench_structured_test.go` | structured 模式基准测试 |

### 测试场景（每种模式 × 3 种编码器）

```go
// printf 模式
func BenchmarkPrintInfo(b *testing.B)
func BenchmarkPrintInfof(b *testing.B)
func BenchmarkPrintInfoln(b *testing.B)

// structured 模式
func BenchmarkStructInfo(b *testing.B)       // 无 fields
func BenchmarkStructInfo3Fields(b *testing.B) // 3 个 fields
func BenchmarkStructInfo10Fields(b *testing.B)// 10 个 fields
func BenchmarkStructInfof(b *testing.B)
```

每种场景通过 `-log_encoder=text/json/logfmt` flag 切换编码器。

### 指标采集

```bash
# 标准 Go benchmark 指标
go test -bench=. -benchmem -count=5

# 输出：ns/op, B/op, allocs/op
```

### 质量门

- 连续 5 次运行，ns/op 变异系数 < 5%
- 单 goroutine 下 structured Info 0 fields 保持 ~40ns/op, 0 allocs

## 阶段 3：扩展压力测试（全维度）

### 并发梯度测试

```go
// bench_concurrency_test.go
func BenchmarkPrintConcurrency(b *testing.B)   // 1/4/8/16/32/64 goroutine
func BenchmarkStructConcurrency(b *testing.B)
```

使用 `b.RunParallel` + 自定义 goroutine 数控制。

### 延迟分位测试

```go
// bench_latency_test.go
func BenchmarkLatencyPrintInfo(b *testing.B)
func BenchmarkLatencyStructInfo(b *testing.B)
```

使用 `testing.B` 的 `Timer` + 自定义 histogram 采集 P50/P90/P99：
- 预热 1 秒
- 采样 5 秒
- 输出：P50/P90/P99 延迟（ns）

### CPU & 内存 Profiling

```bash
# CPU
go test -bench=BenchmarkStructInfo -cpuprofile=cpu.prof
go tool pprof -top cpu.prof

# 内存
go test -bench=BenchmarkStructInfo -memprofile=mem.prof
go tool pprof -top mem.prof
```

### 对比矩阵

| 维度 | 取值 |
|---|---|
| 模式 | printf / structured |
| 编码器 | text / json / logfmt |
| 并发 goroutine | 1, 4, 8, 16, 32, 64 |
| fields 数量 | 0, 3, 10 |
| 日志函数 | Info, Infof, Infoln |

总计 `2 × 3 × 6 × 3 × 3 = 324` 个组合。实际精选核心组合（约 30-40 个 benchmark case）避免冗余。

### 报告输出

最终输出 `BENCHMARK_REPORT.md`，包含：
- 所有场景的表格数据（ns/op, B/op, allocs/op）
- 延迟分位图（P50/P90/P99）
- CPU hotspot top 10
- 内存分配 hotspot top 10

### 质量门

- 所有 benchmark 稳定运行 5 次无 panic
- 结构化模式 0 fields 热路径保持 0 allocs
- 报告生成成功

## 文件变更预估

| 文件 | 变更 |
|---|---|
| `mode_test.go` | 新增全局函数分支测试 |
| `structured_test.go` | 新增 Logger 方法测试 |
| `encoder_test.go` | 补全字段类型测试 |
| `logsink_test.go` | 补 textPrintf / sink 测试 |
| `ringbuffer_test.go` | 补并发场景测试 |
| `async_writer_test.go` | 补生命周期测试 |
| `bench_print_test.go` | 新增 printf 模式 benchmark |
| `bench_structured_test.go` | 新增 structured 模式 benchmark |
| `bench_concurrency_test.go` | 新增并发梯度 benchmark |
| `bench_latency_test.go` | 新增延迟分位 benchmark |
| `BENCHMARK_REPORT.md` | 新增性能报告 |

## 风险评估

| 风险 | 缓解 |
|---|---|
| 文件 sink O_EXCL bug 导致测试不稳定 | 测试中禁用 TextSinks 或使用唯一目录 |
| benchmark 结果受机器负载影响 | 预热 + 多次运行取中位数 + 标注测试环境 |
| 大量测试增加 CI 时间 | 单元测试 `-short` 模式，benchmark 单独跑 |
