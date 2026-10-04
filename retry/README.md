# Retry 独立模块

[返回模块总览](../README.md) · [HTTP 请求](../httpclient/README.md) · [编码错误](../apperror/README.md)

模块名 `github.com/bkcarlos/goparts/retry`，Go 1.21+，仅依赖标准库。提供有界次数、指数退避、随机抖动和 Context 取消。

## 使用

```go
r, err := retry.New(retry.Config{
    MaxAttempts: 3,
    InitialDelay: 100 * time.Millisecond,
    MaxDelay: 2 * time.Second,
    Multiplier: 2,
    Jitter: 0.2,
    RetryIf: func(err error) bool {
        return errors.Is(err, ErrTemporary)
    },
})
if err != nil { return err }

err = r.Do(ctx, func(ctx context.Context) error {
    return fetchData(ctx)
})
```

`ErrTemporary`、`fetchData` 由业务定义。完整可运行示例：[examples/basic/main.go](examples/basic/main.go)。

## 行为

| 参数 | 默认值 / 语义 |
| --- | --- |
| `MaxAttempts` | 3，包含首次执行；设为 1 时只执行一次 |
| `InitialDelay` | 100ms，第一次重试前等待 |
| `MaxDelay` | 5s，包含抖动后的最大等待时间；不能小于 InitialDelay |
| `Multiplier` | 2，最小为 1 |
| `Jitter` | 0，关闭抖动；取值 0～1，0.2 表示围绕基础延迟上下浮动 20%，再受 MaxDelay 截断 |
| `RetryIf` | nil 时不重试；只有明确返回 true 的错误才会重试 |

每次尝试前检查 Context，等待期间也能取消。全部失败返回最后一次操作的原始错误，取消返回 `ctx.Err()`。最后一次失败后不会再次等待或调用 RetryIf。

Retrier 配置不可变，可以并发复用；RetryIf 和传入操作的并发安全由调用者保证。模块不捕获 panic。

业务操作必须正确使用传入 Context；若操作忽略 Context 并永久阻塞，重试模块无法强行打断。总执行时间由调用方 Context 约束。

使用 `apperror` 时，可在 `RetryIf` 中通过 `errors.Is(err, ErrTemporary)` 匹配预定义业务错误；包装后的错误链仍可识别。`RetryIf: nil` 即使配了 `MaxAttempts: 3` 也只执行一次。重试结束后在应用边界统一上报，避免每次尝试都重复发通知。

是否可重试取决于业务。尤其创建订单、发消息、扣款等有副作用操作，应先具备幂等机制。HTTP 请求重试时要在每次尝试中重建已消耗的 Body。

## 独立运行

在本模块目录执行：

```sh
GOWORK=off go run ./examples/basic
GOWORK=off go test -race -cover ./...
GOWORK=off go vet ./...
```

安装与本地联调方式见[接入指南](../README.md#接入业务项目)。
