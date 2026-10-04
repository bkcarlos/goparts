# middleware：标准 HTTP 中间件

[返回总览](../README.md) · [限流与熔断](../ratelimit/README.md) · [日志](../logger/README.md)

独立 Go 1.21 模块，采用 `func(http.Handler) http.Handler`，可以直接用于 net/http。

```sh
go get github.com/bkcarlos/goparts/middleware@v0.1.0
```

## 组合一个受保护的接口

```go
package example

import (
    "errors"
    "io"
    "log/slog"
    "net/http"

    "github.com/bkcarlos/goparts/middleware"
)

func NewHandler(log *slog.Logger, token string) http.Handler {
    api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        data, err := io.ReadAll(r.Body)
        if err != nil {
            var tooLarge *http.MaxBytesError
            if errors.As(err, &tooLarge) {
                http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
            } else {
                http.Error(w, "invalid body", http.StatusBadRequest)
            }
            return
        }
        w.Header().Set("Content-Type", "text/plain; charset=utf-8")
        _, _ = w.Write(data)
    })
    return middleware.Chain(api,
        middleware.RequestID(false),
        middleware.AccessLog(log, nil),
        middleware.Recover(nil),
        middleware.AuthBearer(middleware.StaticToken(token)),
        middleware.BodyLimit(1<<20),
    )
}
```

交给 `http.Server{Handler: NewHandler(log, token)}` 即可；监听地址、TLS、服务器退出仍由应用管理。
Chain 第一个中间件最外层，上面的顺序使正常请求、401、413 与恢复的 panic 都能进入访问日志。
StaticToken 的 expected 为空时拒绝全部请求，应用应在启动时确认配置有效。

## 能力与选择

| 中间件 | 行为 | 注意事项 |
| --- | --- | --- |
| `RequestID(false)` | 生成随机 ID，写入 Context 和响应头 | `ID(ctx)` 读取；只有显式 true 才接受经过字符/长度校验的传入 ID |
| `AccessLog(log, observer)` | 记录方法、路径、ID、状态、字节和耗时 | 不记录查询参数、请求头和正文；observer 需并发安全 |
| `Recover(onPanic)` | 普通 panic 转 500，并可通知回调 | 不向客户端暴露 panic 文本；已写响应不能重置；ErrAbortHandler 继续传播 |
| `BodyLimit(bytes)` | 预检查 Content-Length，并限制未知长度流 | 业务读取 Body 时仍需处理 MaxBytesError；负值为配置错误 |
| `AuthBearer(validator)` | 校验 Bearer token | validator 可动态读取轮换后的密钥；不提供用户数据库或 JWT 解析 |
| `Timeout(duration)` | 使用标准 TimeoutHandler 缓冲响应 | 不用于 SSE/WebSocket；任务仍需响应 Context 取消 |
| `CORS(config)` | 校验源和预检方法/请求头 | 有 Credentials 时不接受通配符源 |

AccessLog 保留 Flusher 能力，ResponseController 可通过 Unwrap 访问底层 Writer。
CORS 并不是认证：允许某个浏览器源不意味着该请求有业务权限。
如要让浏览器预检无需 Bearer token，把 CORS 放在 AuthBearer 外层；Origins、Methods、Headers
应列出实际允许的值，Header 名按不区分大小写匹配。

限流由独立 ratelimit 模块提供。业务 handler 可先调用 `Allow()`，不足时返回 429；
本模块不会隐式创建或共享令牌桶。

## 验证

本目录 `GOWORK=off go test -race ./...`。测试覆盖鉴权拒绝、CORS 预检、请求 ID 信任边界、
正文边界、SSE Flush、panic 恢复和访问日志的状态/字节统计。
