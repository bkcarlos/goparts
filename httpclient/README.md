# HTTP Client 独立模块

[返回模块总览](../README.md) · [统一错误与上报](../apperror/README.md)

模块名 `github.com/bkcarlos/goparts/httpclient`，Go 1.21+，仅依赖标准库。提供连接复用、超时、响应大小限制和 JSON 请求。

## 使用

```go
client, err := httpclient.New(httpclient.Config{
    Timeout: 5 * time.Second,
    MaxResponseBytes: 2 * 1024 * 1024,
    Headers: http.Header{"Authorization": []string{"Bearer " + token}},
})
if err != nil { return err }
defer client.CloseIdleConnections()

var result struct { ID int `json:"id"` }
resp, err := client.DoJSON(ctx, http.MethodGet, endpoint, nil, &result)
if err != nil { return err }
_ = resp.StatusCode
```

完整可运行示例：[examples/basic/main.go](examples/basic/main.go)，使用本地测试服务，不访问外部接口。

## 行为

### 配置参数

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `Timeout` | 10 秒 | 包含响应体读取；`0` 使用默认值，负数无效 |
| `MaxResponseBytes` | 4 MiB | 单次完整响应上限，含错误响应；`0` 使用默认值，负数及 `MaxInt64` 无效 |
| `Headers` | 空 | 初始化时复制，单次请求可覆盖同名请求头 |
| `Transport` | 模块创建的 Transport | 可注入自定义实现，需并发安全 |

`DefaultMaxResponseBytes` 仅用于未配置时的默认值，调用方可以覆盖：

```go
client, err := httpclient.New(httpclient.Config{
    MaxResponseBytes: 16 * 1024 * 1024, // 16 MiB
})
```

不填 `MaxResponseBytes` 或传 `0` 仍是 4 MiB，不表示无限制。`Config{}` 可以直接创建使用默认配置的客户端。

### 请求与响应

- 默认超时 10 秒，包含读取响应体的时间；调用方 Context 更早取消时立即响应取消。
- 默认最大响应体 4 MiB，包括错误响应。超过限制返回 `ErrResponseTooLarge`，只保留响应元数据，不返回截断内容。
- 默认创建自己的 HTTP Transport，配置代理、连接池、HTTP/2 和空闲连接超时；可通过 `Config.Transport` 替换。
- `Config.Headers` 在初始化时复制，单次请求的 `Request.Headers` 覆盖同名默认头。发送期间调用方不要并发修改请求 Header 或 Body。
- `Do` 读取完响应后关闭响应体。成功返回 `Response`；非 2xx 返回 `Response` 和 `*StatusError`，可通过返回的 Body 读取服务端错误详情。
- `DoJSON` 编码非 nil input，解码成功且非空的响应到 output。output 为 nil 时不解码；否则必须是非 nil 指针。空响应保留 output 原值。
- 不跟随重定向，3xx 同样作为状态错误返回；需要跳转时由调用方检查目标再请求。
- 没有应用层自动重试，标准 Transport 自带的连接重试语义仍适用。若组合独立 `retry` 模块，调用方必须判断幂等性，并为每次尝试重新创建请求体。
- 网络错误移除标准库附加的完整 URL，错误消息不附带响应内容。自定义 Transport/JSON 解码器产生的错误文本由其自身负责。
- Client 可并发使用；注入的 Transport 也必须支持并发。

需要单次请求头、原始字节或 Reader 请求体时使用：

```go
resp, err := client.Do(ctx, httpclient.Request{
    Method: http.MethodPost,
    URL: endpoint,
    Headers: http.Header{"Content-Type": []string{"application/json"}},
    Body: strings.NewReader(`{"name":"demo"}`),
})
```

错误可用 `errors.As(err, &statusErr)` 和 `errors.Is(err, context.DeadlineExceeded)` 等判断。`StatusError` 包含 StatusCode、Method 和独立复制的 Headers；API 的业务状态码由调用方解析。非 2xx 同时发生读取失败或响应超限时通过 `errors.Join` 保留两类错误。Headers 可能包含敏感数据，不自动输出到 Error 或统一上报字段。

`StatusError` 已实现统一错误接口，编码为 `httpclient.http_status`，上报字段包含 `http_status`。可直接传给 `apperror.Reporter.Capture`；`ErrResponseTooLarge` 等普通哨兵错误可在业务边界通过 `apperror.Wrap` 增加业务编码。适配方式见 [apperror](../apperror/README.md#已有模块适配)。

本模块将响应读入内存，适合常规 API 调用。大文件下载可使用独立 [download 模块](../download/README.md)，SSE 等流式响应使用 `net/http` 或对应客户端。自定义 Transport 若忽略请求 Context，模块无法强行中断它。

## Retry-After 与读请求重试

`StatusError.RetryDelay(now)` / `ParseRetryAfter(value, now)` 支持秒数和 HTTP 日期；过去日期返回零，非法值返回 false，超大秒数饱和到最大 Duration。只对明确可重复的读取组合 `retry`，默认不会重试写请求：

```go
r, err := retry.New(retry.Config{
    MaxAttempts: 3,
    RetryIf: func(err error) bool {
        var status *httpclient.StatusError
        return errors.As(err, &status) && status.Method == http.MethodGet &&
            (status.StatusCode == 429 || status.StatusCode == 503)
    },
    RetryAfter: func(err error) (time.Duration, bool) {
        var status *httpclient.StatusError
        if errors.As(err, &status) { return status.RetryDelay(time.Now()) }
        return 0, false
    },
})
if err != nil { return err }
err = r.Do(ctx, func(ctx context.Context) error {
    _, err := client.DoJSON(ctx, http.MethodGet, endpoint, nil, &result)
    return err
})
```

总等待预算通过 Context 设置。Retry-After 不决定是否重试；等待不会因抖动或本地 MaxDelay 变短。响应头包含服务器原值，避免整体写入日志。

## 独立运行

```sh
GOWORK=off go run ./examples/basic
GOWORK=off go test -race -cover ./...
GOWORK=off go vet ./...
```

在本模块目录执行。`CloseIdleConnections` 会作用于注入的共享 Transport，请在合适的资源生命周期结束时调用。安装与本地联调方式见[接入指南](../README.md#接入业务项目)。

### 观测与受控预览

`Config.Hooks.OnRequest` / `OnResponse` 同步接收独立快照，包含方法、耗时、
状态码、响应字节数和失败标记；不读取请求 Body、不记录查询参数。
头部只保留 Content-Type/Length、Accept、Retry-After、X-Request-Id。
钩子可对接日志和指标，需支持并发且及时返回。
默认 `StatusError` 不含 Body。若配置 `ErrorBodyBytes`，必须同时传入
`RedactBody func([]byte) []byte`；输入和输出都限长，结果放入 `BodyPreview`，
不会写入 Error()。截断的原文可能不是完整 JSON，脱敏函数必须对此安全处理。
