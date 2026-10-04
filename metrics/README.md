# metrics

仅 Counter.Add、Histogram.Record、Gauge.Set 三个接口及 Nop，无全局注册。
`metrics/otel` 适配 OpenTelemetry Meter，不替调用方安装 exporter/provider。
HTTP Hooks.OnResponse、retry.OnAttempt、cache 的 observer、workerpool 的 observer
可用闭包连接指标；业务组件不 import metrics，也不强制引入 OTel。
例如在 OnResponse 中按状态码调用 counter.Add(ctx,1,labels)，
按 Duration.Seconds() 调用 histogram.Record(ctx,seconds,labels)。
标签需控制基数，不放用户 ID、完整 URL 或凭据；回调需并发安全。
