# workerpool：有界任务池与按 key 保序

[返回总览](../README.md) · [退出管理](../lifecycle/README.md) · [流式卡片](../feishu/card/README.md)

适合后台任务、批量请求以及同一卡片的连续更新。独立模块，Go 1.21+，标准库实现。

```sh
go get github.com/bkcarlos/goparts/workerpool@v0.1.0
```

## 普通任务池

以下函数等待全部已接纳任务完成，并把执行失败返回给调用者；不把 Submit 成功当成执行成功。

```go
package example

import (
    "context"
    "errors"
    "sync"
    "time"

    "github.com/bkcarlos/goparts/workerpool"
)

func RunJobs(ctx context.Context, jobs []workerpool.Task) error {
    var mu sync.Mutex
    var failures []error
    pool, err := workerpool.New(4, 32, func(event workerpool.Event) {
        if event.Err != nil {
            mu.Lock()
            failures = append(failures, event.Err)
            mu.Unlock()
        }
    })
    if err != nil { return err }
    var submitErr error
    for _, job := range jobs {
        if submitErr = pool.Submit(ctx, job); submitErr != nil { break }
    }
    pool.Close()
    // 请求取消后，仍给已接纳任务独立的退出预算。
    stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    waitErr := pool.Wait(stopCtx)
    mu.Lock()
    defer mu.Unlock()
    return errors.Join(submitErr, waitErr, errors.Join(failures...))
}
```

| 参数 / 方法 | 行为 |
| --- | --- |
| `New(workers, queueLen, observer...)` | workers 必须 >=1，queueLen >=0；没有隐式默认容量 |
| `Submit(ctx, task)` | 队列满时等待，Context 取消或池关闭时返回；不等待任务结果 |
| `Event` | 包含排队时间 Wait、执行耗时 Duration、任务错误 Err |
| `Close()` | 幂等，停止接纳新任务，允许已接纳任务排空 |
| `Wait(ctx)` | 等待关闭后的排空；自身超时不强制终止任务 |
| `ErrClosed` / `ErrPanic` | 关闭后拒绝提交 / 任务 panic 转换的错误 |

排队任务开始前会检查原提交 Context，已取消则跳过并通过 observer 报告。运行中的任务必须自己响应取消。
observer 同步运行，需及时返回、支持并发且不能 panic；不要在 observer 中阻塞整个任务池。

## 同 key 串行，不同 key 并发

`NewStream[string](4, 32, observer)` 创建按 key 调度的任务池，提交改为
`stream.Submit(ctx, cardID, task)`；Close/Wait 用法相同。同 key 按接纳顺序执行，
不同 key 可以同时执行，总在途任务上限为 `workers + queueLen`，空闲 key 自动清理。

适合一个卡片 ID 对应一条更新序列：Sequence 在任务实际执行时分配，再调用卡片更新接口。
并发 Submit 的先后以内部接纳顺序为准，不保证按调用方开始时间排序。

不要在任务内部同步向已满且包含自己的池提交任务；这种等待环无法由池自动解除。
工作池是进程内队列，不持久化任务，进程退出后不会自动重放。

## 验证

模块内执行 `GOWORK=off go test -race ./...`；仓库根目录 `make stress` 重复验证
排空、取消、关闭并发提交、panic、观察事件和同 key 顺序。
