# 飞书长连接事件

`github.com/bkcarlos/goparts/feishu/events` 通过 WebSocket 接收飞书事件和卡片回调，无需公网 HTTP 回调地址。支持应用凭据鉴权、二进制帧、分片重组、心跳、断线重连和事件应答。属于 Feishu module 下的独立子包，不导入官方 SDK 或暴露其 service 类型。

## 使用

在飞书开发者后台为自建应用启用机器人，配置事件与回调的接收方式为长连接，订阅所需事件，并申请、发布相关权限。长连接使用应用 App ID / App Secret，不是群 Webhook 地址或用户 access token；它不会自动替你创建订阅、申请权限或发送消息。

```go
import (
    "context"

    "github.com/bkcarlos/goparts/feishu/card"
    "github.com/bkcarlos/goparts/feishu/events"
)

dispatcher := events.NewDispatcher()
err := dispatcher.OnP2MessageReceive(func(ctx context.Context, e events.MessageEvent) error {
    // e.Header.EventID 用于幂等；e.Message.Content 是 JSON 字符串。
    // 将耗时工作可靠地入队，失败时返回错误。
    return handleMessage(ctx, e)
})
if err != nil { return err }

err = dispatcher.OnCardAction(func(ctx context.Context, e card.CallbackRequest) (*card.CallbackResponse, error) {
    // e.Event.Operator / Action / Context 与现有 card 子包的类型一致。
    return &card.CallbackResponse{
        Toast: &card.Toast{Type: "success", Content: "已受理"},
    }, nil
})
if err != nil { return err }

client, err := events.New(events.Config{
    AppID: appID, AppSecret: appSecret, Dispatcher: dispatcher,
    OnError: func(ctx context.Context, err error) { report(ctx, err) },
})
if err != nil { return err }
return client.Run(ctx)
```

| 注册方法 | 事件 |
| --- | --- |
| `OnP2MessageReceive` | `im.message.receive_v1`，消息接收 |
| `OnCardAction` | `card.action.trigger`，卡片按钮等交互，可返回 toast / card |
| `OnChatMemberBotAdded` | `im.chat.member.bot.added_v1`，机器人入群 |
| `On(eventType, Handler)` | 其他 schema 2.0 事件，使用 `Event.Data` 原始 JSON |

同一类型只允许注册一个处理器，重复注册会报错。未注册类型会成功应答并忽略。新连接中只支持 schema 2.0 事件及回调，不支持旧版 schema 1.0；需要更多字段时可用通用 `On` 自行解码。长连接回调在已鉴权的连接上接收，不走 `card.CallbackDecoder` 的 HTTP 签名/加密校验。

## 执行与重连

- `Run` 阻塞运行；一个 Client 同时只能有一个 Run，重复运行返回 `ErrAlreadyRunning`。取消 Context 后关闭连接，取消处理器 Context，等待自身工作协程退出。取消结束返回 `context.Canceled` / `DeadlineExceeded`，可以组合 `lifecycle` 管理。Run 结束后可以再次运行。
- 每次重连都重新获取连接地址和鉴权。默认自动重连，遵守服务端的次数、间隔和首次随机延迟；没有服务端配置时，重连间隔为 120 秒且不限次数。鉴权/权限等永久错误会直接返回；繁忙、限流、暂时网络错误可重连。
- `DisableReconnect` 可关闭重连，`MaxReconnectAttempts` 限制连续连接失败的重试次数（零不加本地限制），成功握手后重置计数。服务端明确不允许重连或限制更少次数时仍遵守服务端限制。
- 应用层 ping 立即发送，随后按照配置发送；pong 可更新服务端连接参数。连接连续无下行数据超过 `2 × PingInterval + 5 秒` 时断开并按策略重连。
- 处理器在每条连接内串行执行，收包与心跳独立运行，队列有上限；队列满时断开，未确认事件需依赖服务端重投。不会启动无限数量的处理协程。
- 处理器成功时返回 200 应答，错误、panic、超时或无法编码的回调响应返回 500 应答。卡片结果按飞书协议编码到应答的 base64 `data` 中。
- **不保证仅执行一次**：断线、失败或应答丢失都可能导致重复投递；按 `Header.EventID` 做业务幂等，卡片重投时复用已保存的回调结果。当前没有跨实例去重、持久化队列或广播分发。
- 飞书要求及时应答。回调应迅速完成，耗时任务可靠入队后再返回；不能只启动一个无人管理的 goroutine 就认定业务已完成。`HandlerTimeout` 只通过 Context 协作取消，不能强制停止不遵守 Context 的 Go 函数；不返回的处理器会阻塞退出。
- `OnConnected` / `OnError` 同步调用，可能从不同协程调用；需线程安全、快速返回并遵守 Context。钩子 panic 被隔离。应用事件数据和回调错误可能包含业务信息，由调用方选择记录内容。

## 配置

| 字段 | 默认值 / 含义 |
| --- | --- |
| `AppID` / `AppSecret` / `Dispatcher` | 必填 |
| `BaseURL` | `https://open.feishu.cn`，不带 `/open-apis`；Lark 可传对应源站 |
| `ConnectTimeout` | 10 秒，分别限制发现地址请求和 WebSocket 握手 |
| `WriteTimeout` | 5 秒，限制单次心跳或应答写入 |
| `HandlerTimeout` | 2.5 秒，限制处理器 Context |
| `PingInterval` / `ReconnectInterval` | 零使用服务端配置；缺失时均为 120 秒；非零覆盖间隔，最大 24 小时 |
| `MaxFrameBytes` | 4 MiB，单个 WebSocket 消息上限 |
| `MaxEventBytes` | 8 MiB，单个重组事件上限 |
| `MaxResponseBytes` | 1 MiB，地址发现响应及回调结果 JSON 上限（base64 编码前） |
| `QueueSize` | 64，等待处理事件数，不含正在处理的一个事件 |
| `HTTPClient` / `Dialer` | 可选，复制配置；HTTP 重定向禁用，Dialer 的 TLS 配置也复制 |

大小、队列和超时字段为零时使用默认值，负值无效。分片最多 128 片、64 个未完成消息，总素材缓冲不超过 `2 × MaxEventBytes`；不完整分片 5 秒过期，下次数据帧到达时清理。队列中的完整事件另计内存，按业务载荷调小 QueueSize 和大小上限。

不处理额外的 payload 压缩或加密。HTTPS 地址发现不会接受降级的 `ws://` 连接地址。不要记录带临时凭据的 WebSocket URL；`TransportError` 文本只说明失败操作，`Unwrap` 保留底层错误供 `errors.Is/As` 判断。`APIError` 保留状态、业务 code、原始 Message 和 RequestID，但默认文本与上报字段不输出原始 Message。

`APIError` 的统一编码是 `feishu.events.api_error`，可直接传入 [apperror](../../apperror/README.md#已有模块适配)；普通传输或处理器错误可由业务包装成自己的错误码。

## 示例与验证

从 `feishu` 模块目录运行：

```sh
# 本地 WebSocket 模拟服务，覆盖心跳、分片、事件、卡片应答、重连和取消
GOWORK=off go test -race ./events

# 未配置凭据时跳过；配置后真实连接，订阅并应答事件
GOWORK=off go run ./examples/events
```

真实示例读取 `FEISHU_APP_ID` 和 `FEISHU_APP_SECRET`，Ctrl+C 退出。没有替用户执行真实应用联调；权限、订阅和业务幂等仍需在实际环境验证。

传输依赖 `github.com/gorilla/websocket v1.5.3`；二进制字段使用 `google.golang.org/protobuf/encoding/protowire v1.36.0`，保留 Go 1.21 支持。协议参考 [飞书官方 SDK 的 ws 实现](https://github.com/larksuite/oapi-sdk-go/tree/99927aa13e271ea9fe03591204aad7bc6a2d869c/ws) 和 [事件订阅配置](https://open.feishu.cn/document/server-docs/event-subscription-guide/event-subscription-configure-/request-url-configuration-case)。

`Config.Deduper` 可注入 `feishu/dedup.Store`。去重键为 AppID+EventID，
仅保存成功、未超时且限长的回调响应，重投递返回相同 ACK Data；失败仍可重试。
默认 nil 保持原行为。内存实现适合同进程；多实例须共享分布式实现。
