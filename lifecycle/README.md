# Lifecycle 独立模块

[返回模块总览](../README.md) · [统一错误与上报](../apperror/README.md)

模块名 `github.com/bkcarlos/goparts/lifecycle`，Go 1.21+，仅依赖标准库。管理长期运行的后台任务、退出信号和资源清理。

## 使用

```go
m, err := lifecycle.New(lifecycle.Config{ShutdownTimeout: 10 * time.Second})
if err != nil { return err }

if err := m.Add("worker", func(ctx context.Context) error {
    return runWorker(ctx)
}); err != nil { return err }

if err := m.OnStop("database", func(ctx context.Context) error {
    return db.Close()
}); err != nil { return err }

return m.RunSignals(context.Background())
```

完整可运行示例：[examples/basic/main.go](examples/basic/main.go)。示例在 100ms 后自动退出，也支持提前用 Ctrl+C 退出。

## 生命周期

唯一配置项 `Config.ShutdownTimeout` 默认 10 秒，传 `0` 使用默认值，负数返回初始化错误。该预算是任务退出与全部清理共用的总时长，不是每个清理函数各有 10 秒。

1. 在 Run 前用 `Add` 注册长期任务、`OnStop` 注册清理函数；名称必须唯一。
2. `Run` 并发启动任务，等待父 Context 取消或任一任务返回，包括返回 nil。
3. 取消所有任务的 Context，等待任务退出。
4. 按注册顺序的逆序执行清理函数。

任务退出和全部清理共用一个新的超时预算，默认 10 秒。清理 Context 保留父 Context 的值，但不继承其取消状态或截止时间。

正常父 Context 取消或任务正常返回时，若清理成功则返回 nil。任务失败、清理失败会保留名称和错误链，通过 `errors.Join` 合并。任务因管理器取消而返回的取消错误被忽略，但与其他失败合并的错误会保留。任务/清理函数的 panic 会转为带名称的错误，以继续退出流程。

可在 `Run` 返回后使用 `errors.Is/As` 检查或统一上报。`apperror.Describe` 对合并错误只选择第一个带编码的分支；需要分别报告全部失败时由业务遍历分支。停机时原工作 Context 可能已取消，上报应使用业务明确设置期限的独立 Context。

`RunSignals` 默认处理 SIGINT 和 SIGTERM；传入信号列表可以替换默认值。函数返回时释放信号注册。不需要信号处理的场景直接调用 `Run(ctx)`。

每个 Manager 只能运行一次，启动后不能再注册任务或清理函数。没有任务时等待 Context 取消，再执行清理。

## 退出约束

任务必须主动响应 Context 取消，清理函数也应遵守截止时间。超时返回 `ErrShutdownTimeout`，尚未执行的清理会跳过；Go 无法强制终止忽略 Context 的 goroutine，它们可能继续运行。

清理函数在任务退出后执行。因此不能把“让任务退出所必需的操作”只放进 OnStop。例如 `http.Server.ListenAndServe` 不直接响应 Context，应在任务内部响应 ctx.Done 并调用 Server.Shutdown，使任务能够退出；OnStop 用于任务退出后的最终资源清理。

当前只提供停止协调，不包括自动重启、任务依赖排序、启动就绪检查或分布式调度。

## 独立运行

在本模块目录执行：

```sh
GOWORK=off go run ./examples/basic
GOWORK=off go test -race -cover ./...
GOWORK=off go vet ./...
```

安装与本地联调方式见[接入指南](../README.md#接入业务项目)。

### 停机阶段与预算

`OnQuiesce(name, fn)` 在任务取消之前逆序执行，用于停止新请求/新任务入队；
随后取消任务、等待退出，最后逆序执行 OnStop 关闭资源。
`Add` / `OnQuiesce` / `OnStop` 均可传 `WithStopTimeout(d)`；任务预算从
任务 context 被取消开始计算，hook 预算从调用开始计算，总 ShutdownTimeout 仍优先。
单个 hook 超时会记录错误并继续后续清理，总预算耗尽则结束等待。
Go 无法强杀 goroutine：超时返回后不配合 context 的函数仍可能运行，
业务必须避免其继续使用已清理资源。任务 context 保留父级值，取消由停机阶段控制。
