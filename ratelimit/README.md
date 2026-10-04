# ratelimit

`New(ratePerSecond, burst)` 创建并发安全内存令牌桶，`Allow()` 立即尝试，
`Wait(ctx)` 可取消等待。RateLimiter 接口可由 Redis 等适配器实现。
`NewCircuitBreaker(BreakerConfig{Failures:5, OpenTimeout:30*time.Second})`
提供 closed/open/half_open 三态。通过 `Do(ctx, operation)` 调用；连续失败打开，
等待后仅放行一个探测请求，成功关闭，失败重新打开。旧一代请求完成不会覆盖新状态。
可注入 IsFailure 区分业务错误与故障；回调须并发安全。
这是进程内保护；不隐式重试、不更改飞书/LLM 客户端的请求行为。
