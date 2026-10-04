# metrics：可替换的指标接口

[LLM 使用指南](AI_GUIDE.md)：按当前版本查找能力、源码入口和调用约定。

[返回总览](../README.md) · [HTTP 钩子](../httpclient/README.md) · [工作池事件](../workerpool/README.md)

Go 1.21+。根包只定义指标接口和 Nop，可选 `metrics/otel` 连接 OpenTelemetry Meter。
没有全局注册，也不会自行创建 exporter、监听端口或发送数据。

```sh
go get github.com/bkcarlos/goparts/metrics@v0.1.0
```

## 用接口隔离业务和后端

```go
package example

import (
    "context"
    "time"

    "github.com/bkcarlos/goparts/metrics"
)

func ObserveCall(ctx context.Context, counter metrics.Counter,
    latency metrics.Histogram, operation func(context.Context) error) error {
    start := time.Now()
    err := operation(ctx)
    outcome := "ok"
    if err != nil { outcome = "error" }
    labels := metrics.Labels{"operation": "lookup", "outcome": outcome}
    counter.Add(ctx, 1, labels)
    latency.Record(ctx, time.Since(start).Seconds(), labels)
    return err
}
```

未启用监控时可给两个参数都传 `metrics.Nop{}`，无需在每个业务调用处判断 nil。
指标单位由调用方统一约定，示例延迟使用秒；Labels 使用有界集合，避免用户 ID 或完整 URL。

| 接口 | 方法 | 用途 |
| --- | --- | --- |
| Counter | `Add(ctx, int64, Labels)` | 请求数、错误数；通常传正增量 |
| Histogram | `Record(ctx, float64, Labels)` | 延迟、队列等待时间、大小分布 |
| Gauge | `Set(ctx, float64, Labels)` | 当前连接数、队列长度等瞬时值 |

## OpenTelemetry 适配

```go
package example

import (
    "github.com/bkcarlos/goparts/metrics"
    partsotel "github.com/bkcarlos/goparts/metrics/otel"
    otelmetric "go.opentelemetry.io/otel/metric"
)

func NewRequestCounter(meter otelmetric.Meter) (metrics.Counter, error) {
    return partsotel.Counter(meter, "app.requests")
}
```

业务负责初始化 SDK MeterProvider、Reader 和 Exporter，拿到 Meter 后传给这里。
Histogram 和 Gauge 分别由 `partsotel.Histogram`、`partsotel.Gauge` 创建，创建错误应在启动阶段处理。
关闭 Exporter/Provider 前先停止产生业务指标，具体 Flush/Shutdown 由所用后端管理。

HTTP 的 Hooks、retry.OnAttempt、cache observer 和 workerpool observer 都能通过闭包接入。
这些回调可能并发执行；注入的实现应并发安全并及时返回。避免为每次请求创建新指标对象。

验证：模块内 `GOWORK=off go test -race ./...`，OTel 测试使用本地 SDK Reader，不发送到监控服务。
