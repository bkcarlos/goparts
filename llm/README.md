# LLM 独立模块

[LLM 使用指南](AI_GUIDE.md)：按当前版本查找能力、源码入口和调用约定。

[返回模块总览](../README.md) · [统一错误与上报](../apperror/README.md) · [飞书流式卡片](../feishu/card/README.md#llm-流式卡片)

模块名 `github.com/bkcarlos/goparts/llm`，Go 1.21+。根包仅依赖标准库；可选 `llm/chat` 子包使用 JSON Schema 校验依赖，独立于其他公共模块。

面向 OpenAI 兼容协议提供普通对话、SSE 流式对话、函数工具调用的数据结构、JSON 输出参数和 Embedding 向量接口。服务地址、API Key 和模型名称由业务传入，不绑定具体模型或服务商。

## 选择接口

| 需求 | API | 说明 |
| --- | --- | --- |
| 常见兼容端点对话 | Chat / ChatStream | `/chat/completions`；流式结束标记 `[DONE]` |
| 向量 | Embeddings | `/embeddings`；显式选择 embedding 模型 |
| Responses 协议 | Responses / ResponsesStream | `/responses`；兼容 Chat 的服务商未必支持 |
| 多轮裁剪、工具校验、聚合输出 | [llm/chat](chat/README.md) | 可选应用层；不自动持久化或执行任意工具 |

```sh
go get github.com/bkcarlos/goparts/llm@v0.1.0
```

## 创建客户端

```go
client, err := llm.New(llm.Config{
    BaseURL: os.Getenv("LLM_BASE_URL"),
    APIKey: os.Getenv("LLM_API_KEY"),
    Model: os.Getenv("LLM_MODEL"),
    Timeout: 2 * time.Minute,
})
if err != nil { return err }
defer client.CloseIdleConnections()
```

`BaseURL` 是 API 根地址，模块追加 `/chat/completions` 或 `/embeddings`。例如 OpenAI 根地址为 `https://api.openai.com/v1`，也是缺省值。接入 DeepSeek、通义等服务时填入对应服务商文档给出的兼容根地址，包含其版本路径；不要填完整的 chat/completions 路径。本地免鉴权端点可以省略 API Key。

模型名称必须在 Config 或 ChatRequest 中明确指定；单次请求可覆盖默认聊天模型。Embedding 使用独立的模型参数。不同服务商/模型支持的工具、结构化输出、推理和向量能力不同，本模块不探测或自动转换这些能力。

## 配置参数

初始化配置一览：

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `BaseURL` | `DefaultBaseURL` | 包含版本路径的兼容 API 根地址 |
| `APIKey` | 空 | 业务传入；无鉴权本地服务可以省略 |
| `Model` | 空 | 默认聊天模型，也可在单次请求中指定 |
| `Timeout` | 2 分钟 | 覆盖完整请求和流式读取，`0` 使用默认值 |
| `MaxResponseBytes` | 8 MiB | 普通响应及 HTTP 错误响应上限，`0` 使用默认值 |
| `MaxEventBytes` | 1 MiB | 单个 SSE 行或事件上限，`0` 使用默认值 |
| `Headers` | 空 | 附加请求头，初始化时复制 |
| `HTTPClient` | 新建客户端 | 可注入；复制配置并禁止重定向 |

例如需要接收更大的普通响应时，可传 `MaxResponseBytes: 16 * 1024 * 1024`；这不会改变 SSE 的 `MaxEventBytes`。超时和大小参数不接受负数；大小参数的最大整数值也会因溢出保护被拒绝。

## 普通对话

```go
temperature := 0.0
response, err := client.Chat(ctx, llm.ChatRequest{
    Messages: []llm.Message{
        llm.System("请使用中文简短回答"),
        llm.User("什么是 Go 的 Context？"),
    },
    Temperature: &temperature,
})
if err != nil { return err }
fmt.Println(response.Text())
```

`Text()` 取 index 为 0 的 choice 的文字。完整结果包含 `Choices`、工具调用、拒绝信息、`FinishReason`、可选 `Usage` 和 `RequestID`。需要判断内容是否完整时检查 FinishReason，例如 `length` 表示达到生成上限。

多轮对话由业务维护 `Messages`，将前一次返回的 assistant Message 和新的 user Message 追加到下次请求。模块不共享或自动保存会话。

当前 Message.Content 支持文本；图片、音频和文件输入尚未封装。

## 流式对话

```go
err := client.ChatStream(ctx, llm.ChatRequest{
    Messages: []llm.Message{llm.User("介绍一下你的能力")},
    IncludeUsage: true,
}, func(chunk llm.ChatChunk) error {
    for _, choice := range chunk.Choices {
        if choice.Index == 0 {
            fmt.Print(choice.Delta.Content)
        }
    }
    if chunk.Usage != nil {
        // 记录本次请求的 token 用量。
    }
    return nil
})
```

回调同步执行，返回错误会停止读取并关闭响应体；错误链保留回调错误。回调本身应及时返回，网络超时不能强制打断阻塞的业务回调。

流式响应必须是 `text/event-stream`，支持 CRLF、注释心跳、多行 data 和超过 64 KiB 的单个片段。只有收到 `[DONE]` 才算成功；提前断流返回 `io.ErrUnexpectedEOF`。发生错误前可能已经产生部分输出，调用方应将其标记为不完整。

`IncludeUsage` 默认关闭，避免向不支持该参数的兼容端点发送 stream_options。开启后按服务端返回提供用量，可能出现 Choices 为空、仅包含 Usage 的片段；中断时不保证收到最终用量。

## 工具调用

```go
request := llm.ChatRequest{
    Messages: []llm.Message{llm.User("查询订单 42")},
    Tools: []llm.Tool{llm.FunctionTool(
        "lookup_order", "根据 ID 查询订单",
        json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"],"additionalProperties":false}`),
    )},
    ToolChoice: "auto",
}
response, err := client.Chat(ctx, request)
if err != nil { return err }
calls := response.Choices[0].Message.ToolCalls
```

根包只传递工具定义和模型返回的调用数据，不执行工具；可选 `llm/chat.ToolRegistry` 可校验参数并分发到业务显式注册的处理器。业务应按函数名白名单选择实现，解析并校验 `Function.Arguments`，执行后构造下一轮消息：

```go
request.Messages = append(request.Messages, response.Choices[0].Message)
// 针对 calls 中每个调用，校验并执行工具，分别追加对应结果：
request.Messages = append(request.Messages, llm.ToolResult(call.ID, resultText))
```

`call` 和 `resultText` 由业务工具执行流程提供。每个调用都应有对应 tool 结果。流式模式的函数参数是碎片，应按 **choice.Index + tool.Index** 分组拼接，完成后再解析 JSON，不能收到部分参数就执行。

## JSON 输出与扩展参数

`ResponseFormat` 支持 `json_object` 和 `json_schema` 参数。例如：

```go
request.ResponseFormat = &llm.ResponseFormat{Type: "json_object"}
```

请求中应包含生成 JSON 的指令，返回的 Content 仍是字符串，由业务 `json.Unmarshal` 并校验。支持结构化输出的模型可以配置 `JSONSchema{Name, Schema, Strict}`。

`Temperature`、`TopP`、token 上限和工具并行开关使用指针区分未设置和显式零值。`MaxCompletionTokens` 和兼容旧端点的 `MaxTokens` 二选一，不默认发送任何 token 上限或采样参数。

`Extra map[string]any` 用于顶层服务商扩展参数，如业务确认目标服务支持时设置 `enable_thinking`。Extra 不能覆盖 `model`、`messages`、`stream` 等内置字段，即使内置字段未设置也不允许覆盖。模块保留返回中的 `reasoning_content` 扩展字段。

## Embedding

```go
vectors, err := client.Embeddings(ctx, llm.EmbeddingRequest{
    Model: embeddingModel,
    Input: []string{"第一段文本", "第二段文本"},
})
if err != nil { return err }
for _, item := range vectors.Data {
    // item.Index 对应原始输入位置；服务端可能返回乱序数据。
    _ = item.Embedding
}
```

固定请求 float 编码，返回 `[]float64`。支持批量文本、可选 Dimensions 和用量统计；检查响应条数、索引和维度一致性。某个兼容服务不提供 Embedding 时，需要为提供向量能力的服务单独创建 Client。本模块不包含向量数据库、分词器或 RAG 流程。

## 超时与错误

- 默认整体超时两分钟，包含流式读取；调用方 Context 更早取消时优先生效。
- 普通响应/HTTP 错误响应默认上限 8 MiB；单个 SSE 行或事件默认上限 1 MiB，可分别配置。流式内容不整体累积到内存。
- 错误分类：`*APIError`、`ErrInvalidResponse`、`ErrResponseTooLarge`、`ErrEventTooLarge`、Context 错误、流式提前 EOF。
- APIError 保留 StatusCode、Code、Type、Message、RequestID 和原始 RetryAfter。`Error()` 不拼接服务端消息，避免把服务端回显的提示词写入普通错误日志；需要诊断时按业务数据规则使用 Message。
- 不记录 API Key、提示词和响应。标准网络错误移除附带 URL；自定义 Transport、JSON 编码器或回调产生的错误文本由调用方控制。
- HTTPClient 可注入，配置会复制，不修改调用方客户端；已有客户端 Timeout 仍然有效。禁止跟随重定向。
- 不实现应用层自动重试。超时或断流时服务端可能已经处理并计费，重试由业务决定。
- Client 可以并发使用。调用期间不要并发修改请求的消息、工具、Extra 等切片或 map。

错误判断示例：

```go
var apiErr *llm.APIError
if errors.As(err, &apiErr) {
    // 使用 apiErr.StatusCode / Code / RequestID 分类处理。
}
if errors.Is(err, context.DeadlineExceeded) {
    // 请求超时，送达/生成状态可能未知。
}
```

## 统一错误上报

`APIError` 可直接交给统一错误上报，编码为 `llm.api_error`，字段保留 HTTP 状态、上游编码、类型、Request ID 和 Retry-After；原始 Message 不自动进入上报记录。更多说明见 [apperror 适配文档](../apperror/README.md#已有模块适配)。

## 独立运行与验证

在本模块目录执行，前两个示例只访问本地模拟服务：

```sh
GOWORK=off go run ./examples/basic
GOWORK=off go run ./examples/stream
GOWORK=off go test -race -cover ./...
GOWORK=off go vet ./...
```

真实调用示例位于 `examples/live`。先自行设置 `LLM_BASE_URL`、`LLM_API_KEY`、`LLM_MODEL`，然后运行：

```sh
GOWORK=off go run ./examples/live -prompt '你好'
GOWORK=off go run ./examples/live -stream -prompt '你好'
```

这两个命令会向配置服务发送真实请求，可能产生费用。本次实现仅通过本地模拟服务验证，尚未做服务商真实联调。安装与本地联调方式见[接入指南](../README.md#接入业务项目)。

协议参考：[Chat Completions](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create)、[流式事件](https://developers.openai.com/api/reference/resources/chat/subresources/completions/streaming-events)、[工具调用](https://developers.openai.com/api/docs/guides/function-calling)、[Embedding](https://developers.openai.com/api/reference/resources/embeddings/methods/create)。此模块面向已选定的兼容协议；已提供独立的 Responses 接口（见下文）；Anthropic 原生接口不在当前范围内。

### Responses API

`client.Responses(ctx, ResponsesRequest{Input:"你好", PreviousResponseID:previous})`
走 `/responses`，模型默认沿用 Config.Model，可指定 Reasoning、Tools、Store、
MaxOutputTokens 和不覆盖内置字段的 Extra。Input 支持字符串或 []ResponseInput，
多模态内容可通过 Content / json.RawMessage 表达。
`ResponsesStream(ctx, req, func(ResponseEvent) error)` 保留完整 Raw 事件，支持
response.created、response.output_text.delta、工具参数事件及其他未来事件。
必须收到 response.completed 才成功；失败/incomplete 返回 ResponseStatusError，
提前 EOF 返回 io.ErrUnexpectedEOF；回调错误和 context 取消会停止读取。
仅普通 Chat 兼容并不代表供应商也实现 Responses，需要端点本身支持。
协议参考：[OpenAI 流式响应](https://developers.openai.com/api/docs/guides/streaming-responses)。

### llm/chat 应用层

Session 以完整 user 回合裁剪历史，保留开头 system/developer；支持消息数和自定义
token 计数预算。不会留下孤立 tool result，最新回合超预算返回 ErrBudget，不修改状态。
ToolRegistry.Register 编译 JSON Schema（禁止外部引用），Dispatch 先验证参数再调用
显式注册的 handler，返回 llm.ToolResult；handler 自行检查业务权限，不执行任意模型代码。
Accumulator 聚合全文，按字符数/定时间隔串行 flush；Close 发送尾部并停止计时器，
回调错误停止后续写入。SequenceAllocator[K].Next 为每个 key 分配递增序号，
搭配 workerpool.Stream 保证卡片更新发送顺序；仅在无在途调用后 Forget。


完整的 Responses 普通调用函数：

```go
package example

import (
    "context"

    "github.com/bkcarlos/goparts/llm"
)

func Respond(ctx context.Context, client *llm.Client, prompt, previousID string) (string, string, error) {
    response, err := client.Responses(ctx, llm.ResponsesRequest{
        Input: prompt,
        PreviousResponseID: previousID,
    })
    if err != nil { return "", "", err }
    return response.Text(), response.ID, nil
}
```

client 需事先配置 Model；previousID 为空表示不关联前次响应。是否支持服务端关联历史由端点决定，
它与本地 chat.Session 是两种状态管理方式，不能假定其中一种会自动同步另一种。
[llm/chat 接入文档](chat/README.md) 提供会话、工具处理和流式聚合的完整函数示例。
