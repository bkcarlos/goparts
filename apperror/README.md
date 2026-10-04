# AppError 独立模块

[返回模块总览](../README.md) · [日志与错误组合示例](../README.md#接入业务项目) · [可运行示例](examples/basic/main.go)

模块名 `github.com/bkcarlos/goparts/apperror`，Go 1.21+，仅依赖标准库。支持稳定错误码、简短说明、原始错误链、结构化字段，以及可注入的统一上报出口。可单独复制使用。

## 定义和使用

常用入口：

| 场景 | API | 返回值 / 行为 |
| --- | --- | --- |
| 定义编码错误 | `New(code, message, options...)` | `*Base`，可作为共享定义 |
| 附加本次调用字段 | `definition.With(WithFields(fields))` | 新 `*Base`，不修改原定义 |
| 包装底层错误 | `definition.Wrap(err, options...)` / `Wrap(err, code, message, options...)` | `error`；输入 nil 则返回 nil |
| 取得编码 | `CodeOf(err)` | 选择最外层编码；nil 返回空字符串 |
| 构建上报快照 | `Describe(err)` | `*Record`；nil 返回 nil |
| 扩展业务类型 | 嵌入 `*Base` 或实现 `Coded` | 保留标准错误链和具体类型 |
| 提交上报 | `Reporter.Capture(ctx, err, fields...)` | 返回上报失败；不替代业务错误 |

错误码使用字符串，既支持 `order.not_found` 这样的业务命名，也支持 `"10001"` 这样的数字编码。建议按领域分配唯一编码；修改说明文案不影响错误识别。

```go
var (
    ErrOrderNotFound = apperror.New("order.not_found", "订单不存在")
    ErrOrderQuery = apperror.New("order.query_failed", "订单查询失败")
)

func loadOrder(ctx context.Context, id string) error {
    err := queryDatabase(ctx, id)
    if err != nil {
        return ErrOrderQuery.Wrap(err,
            apperror.WithFields(apperror.Fields{"order_id": id}))
    }
    return nil
}

// 无底层错误时，直接返回带上下文的新副本。
return ErrOrderNotFound.With(
    apperror.WithFields(apperror.Fields{"order_id": "42"}),
)
```

也可以不预定义错误，直接包装：

```go
return apperror.Wrap(err, "payment.failed", "支付失败",
    apperror.WithFields(apperror.Fields{"order_id": id}),
)
```

`Wrap(nil, ...)` 和 `ErrOrderQuery.Wrap(nil)` 都返回真正的 `nil error`。`New`、`With` 返回 `*Base`；`Wrap` 返回 `error`，避免成功路径中返回带类型的 nil 指针。创建失败错误请用 `New` 或 `With`，不要使用 `Wrap(nil)`。

`Base` 的字段不对外暴露。`With`、`Wrap` 不修改共享定义，`WithFields` 在创建选项时复制输入，字段读取和上报也返回独立快照。字段是 `map[string]string`，适合订单号、请求 ID、状态码、操作名等。不要在调用过程中并发修改输入 map；错误链里的外部错误对象也应保持稳定。

字段覆盖顺序：底层错误字段 → 预定义错误字段 → 本次调用选项 → 上报附加字段。同名字段以后者为准。

## 识别错误及保留原始类型

```go
if errors.Is(err, ErrOrderNotFound) {
    // 按稳定错误码处理，不依赖文案或字段。
}

var pathErr *os.PathError
if errors.As(err, &pathErr) {
    // 原始具体错误仍可获取。
}

if errors.Is(err, context.DeadlineExceeded) {
    // 原始错误链也继续支持标准库哨兵错误。
}

code := apperror.CodeOf(err)
record := apperror.Describe(err) // {code, message, fields} 快照；nil error 返回 nil
```

`Base.Is` 按错误码匹配目标，因此两个不同实例只要编码相同就可匹配；`common.unknown` 不做编码匹配。原始指针匹配和错误链查找仍由标准库处理。对外部错误可以使用 `CodeOf`，外部类型不需要实现 `Is`。

`CodeOf` / `Describe` 选择错误链中最外层的 `Coded` 错误。对于 `errors.Join`，按标准库 `errors.As` 的深度优先顺序选择第一个带编码错误，仅取该错误自己的字段，不合并兄弟分支。需要分别上报所有分支时，由业务逐个调用。

没有带编码错误时，取消与超时分别映射为 `common.canceled`、`common.deadline_exceeded`；其他错误为 `common.unknown`，简短说明为 `unclassified error`。空编码归为 `common.unknown`，空说明使用 `unspecified error`；`CodeOf(nil)` 返回空字符串。

## 业务扩展：嵌入或组合

Go 通过结构体嵌入和接口组合扩展行为。推荐嵌入 `*apperror.Base`：

```go
type OrderError struct {
    *apperror.Base
    OrderID string
}

// 如果需要把业务字段纳入统一上报，覆盖 ErrorFields。
func (e *OrderError) ErrorFields() map[string]string {
    fields := e.Base.ErrorFields()
    if fields == nil { fields = map[string]string{} }
    fields["order_id"] = e.OrderID
    return fields
}

func newOrderError(id string) error {
    return &OrderError{Base: ErrOrderNotFound.With(), OrderID: id}
}

var _ error = (*OrderError)(nil)
var _ apperror.Coded = (*OrderError)(nil)
```

`Error`、`Unwrap`、`Is`、`ErrorInfo` 等方法自动提升；仍可用 `errors.As(err, &orderErr)` 获取业务类型。`With` 返回的是新的 `*Base`，构造业务错误时将它放入外层结构体即可。无需为每种业务错误重复写错误链逻辑。

已有自定义错误不必嵌入，只需实现结构化接口：

```go
type Coded interface {
    error
    ErrorInfo() (code, message string)
}

type FieldProvider interface {
    ErrorFields() map[string]string
}
```

`ErrorInfo` 使用方法而非导出字段，所以不会与已有的 `Code`、`Message`、`ErrorCode` 字段冲突。`FieldProvider` 可选。各模块可直接实现这些方法，无需导入 `apperror`。

## 统一上报

`ReporterConfig.Sink` 必填；`IncludeDetail` 默认 `false`。组件没有全局默认上报器，也没有内置超时，截止时间由 `Capture` 的 Context 控制。

上报点创建一个 `Reporter`，通过 `Sink` 对接日志、飞书或其他监控服务：

```go
reporter, err := apperror.NewReporter(apperror.ReporterConfig{
    Sink: apperror.SinkFunc(func(ctx context.Context, record apperror.Record) error {
        // logger 可使用标准 slog，或项目中的日志组件。
        logger.ErrorContext(ctx, record.Message,
            "code", record.Code, "fields", record.Fields)
        return nil
    }),
    IncludeDetail: false, // 默认值；true 时附加完整 err.Error()
})
if err != nil { return err }

if reportErr := reporter.Capture(ctx, businessErr,
    apperror.Fields{"request_id": requestID, "service": "orders"},
); reportErr != nil {
    // 这是上报失败；业务原始错误仍是 businessErr。
    // 在本地记录上报失败，避免递归交给同一个 Reporter。
}
return businessErr
```

飞书出口可通过函数组合，业务侧负责提供已经初始化的 Webhook 客户端：

```go
sink := apperror.SinkFunc(func(ctx context.Context, record apperror.Record) error {
    data, err := json.Marshal(record)
    if err != nil { return err }
    return bot.SendText(ctx, string(data))
})
```

上报记录可以直接序列化为 JSON：

```json
{
  "code": "order.query_failed",
  "message": "订单查询失败",
  "fields": {"order_id": "42", "request_id": "req-1"}
}
```

`Capture` 对 nil 错误不执行任何操作；每次非 nil 调用同步执行一次 Sink，保留传入 Context，不自动重试、去重或启动协程。并发复用 Reporter 时 Sink 也需支持并发，并遵守 Context。上报超时通过调用方的 Context 控制；需要队列、限流、聚合或多个出口时，在业务中组合 Sink。

建议底层只包装并返回错误，在 HTTP 入口、任务边界等位置上报一次，避免沿错误链重复通知。`Record.Message` 使用简短说明；完整错误文本默认不附带。`Base.Error()` 本身包含底层 cause；启用 `IncludeDetail` 后它也会进入 `Record.Detail`。字段和短说明由调用方决定，不会自动做敏感信息过滤。

## 已有模块适配

| 原有类型 | 统一编码 |
| --- | --- |
| `httpclient.StatusError` | `httpclient.http_status` |
| `storage.Error` | `storage.operation_failed` |
| `download.HTTPError` | `download.http_failed` |
| `llm.APIError` | `llm.api_error` |
| `feishu.APIError` | `feishu.webhook_api_error` |
| `feishu.HTTPError` | `feishu.http_status` |
| `card.APIError` | `feishu.card.api_error` |
| `attachment.APIError` | `feishu.attachment.api_error` |
| `events.APIError` | `feishu.events.api_error` |
| `user.APIError` | `feishu.user.api_error` |
| `user.OAuthError` | `feishu.user.oauth_error` |
| `user.ScopeError` | `feishu.user.missing_scope` |

这些错误可直接传给 `Capture`，保留原有类型、字段及 `Error()` 行为。HTTP 状态、上游错误码、Request ID 等进入结构化字段；上游原始 Message / Description 不自动加入上报。模块内部的普通校验错误和其他哨兵错误仍按未分类错误处理，可在业务边界包装成明确的业务编码。组件不将业务编码强制映射成 HTTP 状态，接口层自行决定响应。

## 独立运行与验证

```sh
cd apperror
GOWORK=off go run ./examples/basic
GOWORK=off go test -race -cover ./...
GOWORK=off go vet ./...
```

示例只输出本地日志，不发送外部通知。根目录 `make check` 还会运行跨模块集成测试，验证原有错误可直接上报、包装后仍可恢复原类型，且默认记录不包含上游原始错误文案。

安装与本地联调方式见[接入指南](../README.md#接入业务项目)。
