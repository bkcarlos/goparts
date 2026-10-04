# ratelimit：令牌桶与熔断器

[LLM 使用指南](AI_GUIDE.md)：按当前版本查找能力、源码入口和调用约定。

[返回总览](../README.md) · [HTTP 中间件](../middleware/README.md) · [重试](../retry/README.md)

Go 1.21+，独立标准库模块。令牌桶限制调用频率；熔断器在连续故障后暂时拒绝调用。
两者都需在应用中创建并复用，不会自动应用到 HTTP、飞书或 LLM Client。

```sh
go get github.com/bkcarlos/goparts/ratelimit@v0.1.0
```

## 限流

```go
package example

import (
    "context"

    "github.com/bkcarlos/goparts/ratelimit"
)

func CallLimited(ctx context.Context, limiter ratelimit.RateLimiter,
    operation func(context.Context) error) error {
    if err := limiter.Wait(ctx); err != nil { return err }
    return operation(ctx)
}

func NewLimiter() (*ratelimit.Bucket, error) {
    return ratelimit.New(10, 20) // 每秒补充 10 个令牌，最大突发 20 个。
}
```

`Allow()` 不阻塞，令牌不足返回 false，适合返回 HTTP 429；`Wait(ctx)` 等待令牌或 Context 取消。
初始令牌为 burst 个，rate 必须为有限正数，burst 必须 >=1。不会启动后台补充 goroutine，
因此不需要 Close。RateLimiter 接口允许业务注入其他实现。

## 熔断

```go
package example

import (
    "context"
    "errors"
    "time"

    "github.com/bkcarlos/goparts/ratelimit"
)

func NewBreaker() (*ratelimit.CircuitBreaker, error) {
    return ratelimit.NewCircuitBreaker(ratelimit.BreakerConfig{
        Failures: 5,
        OpenTimeout: 30*time.Second,
        IsFailure: func(err error) bool {
            return err != nil && !errors.Is(err, context.Canceled)
        },
    })
}
```

用 `breaker.Do(ctx, operation)` 包裹调用，通过 `errors.Is(err, ratelimit.ErrOpen)` 区分熔断拒绝。
示例不把调用方取消计为故障；是否忽略业务错误、限流或超时，由应用的 IsFailure 决定。

| 状态 | 放行规则 | 下一步 |
| --- | --- | --- |
| Closed | 正常放行 | 连续故障达到 Failures 后进入 Open；成功清零计数 |
| Open | 返回 ErrOpen，不执行 operation | OpenTimeout 后允许进入 HalfOpen |
| HalfOpen | 同时只放行一个探测 | 成功关闭；失败重新打开 |

Failures=0 默认 5，OpenTimeout=0 默认 30 秒；负值无效。IsFailure 为空时非 nil 错误计为故障。
operation 的 panic 计为故障并继续向外传播。旧请求完成不会覆盖后续状态代次。

两种组件均为进程内状态；多副本各有自己的桶和熔断器。熔断不等于重试，若组合 retry，
应明确一次“重试整体”还是每次“单独尝试”计入熔断。

模块内执行 `GOWORK=off go test -race ./...`；根目录 `make stress` 重复验证竞争与状态切换。
