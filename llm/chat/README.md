# llm/chat：会话、工具分发和流式聚合

[LLM 客户端](../README.md) · [工作池](../../workerpool/README.md) · [飞书卡片](../../feishu/card/README.md)

这是 llm module 的子包，Go 1.21+，随 llm 一起发布；安装 `github.com/bkcarlos/goparts/llm@v0.1.0`。
根包负责请求与响应，这里提供可选应用逻辑。各对象独立创建，不绑定全局会话。

## 会话预算与工具消息

```go
package example

import (
    "github.com/bkcarlos/goparts/llm"
    "github.com/bkcarlos/goparts/llm/chat"
)

func NewConversation() (*chat.Session, error) {
    session, err := chat.NewSession(chat.SessionConfig{MaxMessages: 20})
    if err != nil { return nil, err }
    if err := session.Append(llm.System("请使用中文回答")); err != nil {
        return nil, err
    }
    return session, nil
}
```

每轮先 Append 用户消息，再把 `session.Messages()` 传给 Chat；处理成功后 Append assistant 消息。
每个业务会话使用自己的 Session。方法并发安全，但“Append → 请求模型 → Append”整个回合不是原子操作，
同一会话仍需业务串行调度，避免两轮响应交叉。

| 配置 / 方法 | 语义 |
| --- | --- |
| MaxMessages | 大于零时限制消息数，包含系统/工具消息；零不限制 |
| MaxTokens / CountTokens | 大于零时必须提供计数函数；SDK 不内置所有模型 tokenizer |
| Append | 按完整 user 回合裁剪最旧历史，保留开头 system/developer |
| Messages | 返回消息和 ToolCalls 切片副本 |
| Reset | 清空全部历史，包括系统提示 |
| ErrBudget | 最新回合加固定消息仍超预算；Append 失败不修改已有历史 |

工具结果必须匹配尚未完成的 ToolCall ID，不能重复提交结果；未完成所有调用前不能开始新的普通消息。
可以先 Append 带调用的 assistant 消息，再追加对应 tool 结果。Session 只在内存中，不保存到数据库。

## 注册并校验工具

```go
package example

import (
    "context"
    "encoding/json"

    "github.com/bkcarlos/goparts/llm/chat"
)

func RegisterLookup(registry *chat.ToolRegistry,
    lookup func(context.Context, int) (string, error)) error {
    schema := json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"],"additionalProperties":false}`)
    return registry.Register("lookup_order", "按 ID 查询订单", schema,
        func(ctx context.Context, raw json.RawMessage) (string, error) {
            var args struct { ID int `json:"id"` }
            if err := json.Unmarshal(raw, &args); err != nil { return "", err }
            return lookup(ctx, args.ID)
        })
}
```

ToolRegistry 零值可用。将 `registry.Tools()` 传给 `llm.ChatRequest.Tools`，拿到模型完整 ToolCall 后，
调用 `registry.Dispatch(ctx, call)`；成功返回一条 llm.ToolResult 消息，追加到下一轮请求。
如果响应含多个调用，应逐个处理，并限制业务工具执行轮数，避免无限调用循环。

Dispatch 在执行前验证名称、ID、调用类型、JSON 和 Schema；未知工具、尾随 JSON、参数不匹配不会运行 handler。
MaxArgumentsBytes=0 默认 1 MiB；工具定义的 Schema 最多 1 MiB，外部 Schema 引用被禁止。
MaxArgumentsBytes 应在开始使用前配置，不要并发修改。handler 负责权限、幂等性和副作用控制，参数通过校验不等于有权执行。
模型流中的参数片段需先拼完整再 Dispatch；这里不自动拼接工具调用增量，也不自动递归调用模型。

## 聚合流式输出

```go
package example

import (
    "context"
    "errors"
    "time"

    "github.com/bkcarlos/goparts/llm"
    "github.com/bkcarlos/goparts/llm/chat"
)

func StreamText(ctx context.Context, client *llm.Client, prompt string,
    update func(context.Context, string) error) (err error) {
    acc, err := chat.NewAccumulator(ctx, chat.AccumulatorConfig{
        Characters: 128,
        Interval: 500*time.Millisecond,
        MaxBytes: 1<<20,
        Flush: update,
    })
    if err != nil { return err }
    defer func() { err = errors.Join(err, acc.Close()) }()
    return client.ChatStream(ctx, llm.ChatRequest{
        Messages: []llm.Message{llm.User(prompt)},
    }, func(chunk llm.ChatChunk) error {
        for _, choice := range chunk.Choices {
            if choice.Index == 0 {
                if err := acc.Add(choice.Delta.Content); err != nil { return err }
            }
        }
        return nil
    })
}
```

Flush 接收**完整累计文本**，不是本次增量；更新卡片应替换对应元素文本。字符阈值按 rune，
MaxBytes 按 UTF-8 字节；Characters=0 默认 128，MaxBytes=0 默认 16 MiB，Interval=0 不定时刷新。
回调串行执行且不能重入同一个 Accumulator；应及时返回。首次回调错误会保留，后续 Add/Flush 继续返回错误。
Close 停止计时器并发送未刷出的尾部，需要检查返回值。网络流失败时上述函数也会尝试刷新已收文本，
业务应根据最终 error 标为“未完成”，不能把 Close 成功当作模型生成成功。

`SequenceAllocator[K]` 零值可用，每 key 从 1 递增。序号分配本身不保证网络发送顺序，
应搭配 workerpool.Stream，在实际执行更新的任务里 Next；所有在途更新完成后才 Forget。
跨实例操作同一卡片时，需要业务提供共享序号和串行协调。

验证：llm 目录 `GOWORK=off go test -race ./chat`；根目录 `make stress` 与 `make fuzz`
分别验证并发行为和工具参数解析。LLM 服务商的实际工具能力仍需独立验证。
