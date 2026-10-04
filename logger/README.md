# Logger 独立模块

[返回模块总览](../README.md) · [统一错误与上报](../apperror/README.md)

模块名：`github.com/bkcarlos/goparts/logger`，Go 1.21+，仅依赖标准库。该目录包含独立的 `go.mod`、实现、测试和示例，可以单独复制到其他仓库使用。

## 使用

```go
import "github.com/bkcarlos/goparts/logger"

// 在业务函数中初始化，err 由调用方处理。
l, err := logger.New(logger.Config{Service: "order-service"})
if err != nil { return err }
l.Info("订单创建成功", "order_id", 1001)
```

完整可运行代码见 [examples/basic/main.go](examples/basic/main.go)。

## 配置与请求字段

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `Format` | `json` | 可选 `json`、`text` |
| `Level` | INFO | 接收 `slog.Leveler`；传 `*slog.LevelVar` 支持动态调整 |
| `Writer` | `os.Stdout` | 输出由调用方负责关闭和刷新 |
| `AddSource` | `false` | 是否记录调用位置 |
| `Service` / `Environment` | 空 | 非空时附加为固定日志字段 |

`New` 返回 `*slog.Logger`，不修改 `slog.Default()`。

```go
var level slog.LevelVar // 默认 INFO
log, err := logger.New(logger.Config{
    Format: "text",
    Level: &level,
    Writer: os.Stdout,
})
if err != nil { return err }
level.Set(slog.LevelDebug)
log.With("module", "payment").DebugContext(ctx, "开始支付", "order_id", 1001)
```

- 默认 JSON、INFO、标准输出。使用标准 `slog` 的 `With`、`WithGroup` 和 `*Context` 方法。
- `logger.WithContext(ctx, log)` 绑定基础日志器，`logger.WithFields` 派生带字段的 Context，不修改父 Context。
- `logger.FromContext` 找不到绑定日志器时返回 `slog.Default()`。需要请求字段时从该 Context 获取日志器，仅调用基础日志器的 `InfoContext` 不会自动注入字段。
- `Writer` 由调用者管理关闭/刷新。文件轮转、采样、脱敏尚未实现；不要直接输出密钥或完整个人信息。
- 同一个日志器及其派生日志器可并发使用。多个独立日志器共享自定义 Writer 时，调用者应保证 Writer 并发安全。

## 与错误模块组合

若需要错误码、简短说明和统一字段，可将本日志器接入 `apperror.SinkFunc`。完整的两模块组合示例见[根目录接入指南](../README.md#接入业务项目)。

## 独立运行与验证

在本模块目录执行；关闭 workspace 后仍可编译、测试和运行：

```sh
GOWORK=off go run ./examples/basic
GOWORK=off go test -race -cover ./...
GOWORK=off go vet ./...
```

本模块可独立引入；安装与本地联调方式见[接入指南](../README.md#接入业务项目)。

参考：[Go slog 文档](https://pkg.go.dev/log/slog)。

### 脱敏、轮转与入口

`Config{Redact:true}` 对消息中的 Bearer 字符串和嵌套属性中的凭据键脱敏，
包括 With、Group、map/slice。也可用 `RedactingHandler` 包装已有 slog Handler。
脱敏无法识别任意业务字段的秘密，敏感数据仍应在写日志之前排除。
`NewRotatingWriter(FileConfig{Path:"app.log", MaxBytes:10<<20, Backups:3})`
提供进程内并发安全的轮转（单条超过上限返回错误，不支持多进程共享同一文件）。
通过 `Config.Writer` 注入，调用方负责 Close。
`InitGlobalLogger`、`CloseGlobalLogger` 和 Debug/Info/Warn/Error 是可选全局入口；
仅显式传入的 closer 转移所有权。`ToLevel` 解析 slog 级别。
`NewTraceWriter(w).Record(ctx, fields...)` 输出脱敏 JSON 行，可在 HTTP 钩子中调用；
默认不捕获请求体。历史 toolkits 的精确签名需要其源码，当前不声称兼容。
