# middleware

独立 Go 1.21 模块，标准 `func(http.Handler) http.Handler`，不依赖框架。

```go
handler := middleware.Chain(api,
    middleware.RequestID(false),
    middleware.AccessLog(log, nil),
    middleware.Recover(nil),
    middleware.AuthBearer(middleware.StaticToken(token)),
    middleware.BodyLimit(1<<20),
)
```

Chain 的第一个中间件在最外层。RequestID 默认生成随机 ID，显式 true 才接受
经过字符/长度校验的来访 ID；`ID(ctx)` 读取。AccessLog 支持 slog 和观测回调，
不收集头部、查询参数和 Body；ResponseController 可通过 Unwrap 到底层响应。
Recover 隐藏 panic 内容，可选回调用于报告；已发送的响应无法改写状态。
Timeout 使用标准 TimeoutHandler 缓冲响应，不适用于 SSE/WebSocket。
BodyLimit 限制 Content-Length 和未知长度流，业务读取 Body 时需处理 MaxBytesError。
AuthBearer 可注入动态校验器；StaticToken 常量时间比较。CORS 必须显式列出源、
方法、请求头；带凭据不接受通配符源，拒绝不允许的预检。
