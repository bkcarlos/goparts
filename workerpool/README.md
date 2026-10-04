# workerpool

`New(workers, queueLen, observer...)` 创建有界任务池。
`Submit(ctx, func(context.Context) error)` 在满载时等待或响应取消，返回表示已接纳，
执行结果通过 observer 接收（排队时间、执行时间和 error）。任务 panic 转成 ErrPanic。
`Close()` 停止接纳并排空已接纳任务；`Wait(ctx)` 等待退出。
提交后的 context 仍属于调用方，取消后未开始任务会跳过；运行中任务必须配合取消。

`NewStream[K](workers, queueLen, observer...)` 按 key 保持接纳顺序，同 key 串行、
不同 key 并发；总在途上限为 workers+queueLen，key 空闲后自动释放。
适合逐张卡片的流式更新。Submit 并发时顺序以内部接纳顺序为准。
任务内不要同步提交到已经占满且包含自己的池，以免产生业务层等待环。
observer 同步执行，必须及时返回、不 panic，并支持并发。
