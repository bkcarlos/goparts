# 飞书卡片

[返回模块总览](../../README.md) · [Feishu 模块](../README.md) · [用户登录与文档](../user/README.md) · [LLM 流式回调](../../llm/README.md#流式对话)

`github.com/bkcarlos/goparts/feishu/card` 是 Feishu 模块内可单独导入的子包，只依赖标准库。卡片构建无需凭证；客户端接入企业自建应用机器人，自动获取并缓存 `tenant_access_token`。

| 能力 | API |
| --- | --- |
| JSON 2.0 卡片 | `NewCard`、`Markdown`、`Divider`、`Image`、`LinkButton`、`CallbackButton` |
| 模板卡片 | `Template`、`TemplateData`、`CreateTemplate` |
| 群聊 / 私聊发送 | `Client.Send`、`Chat`、`User` |
| 回复 / 更新消息 | `Client.Reply`、`Client.UpdateMessage` |
| 卡片实体 | `Client.Create`、`Reference`、`Client.Update` |
| 流式更新 | `Client.UpdateText`、`Client.Settings`、`Client.Finish` |
| HTTP 按钮回调 | `CallbackDecoder.Decode`、`CallbackResponse`、`RawResponseCard` |

## 构建与发送

应用客户端通过 `card.New(card.Config{...})` 初始化：

| 字段 | 默认值 / 要求 | 说明 |
| --- | --- | --- |
| `AppID` / `AppSecret` | 必填 | 企业自建应用凭据 |
| `BaseURL` | `https://open.feishu.cn/open-apis` | API 根地址，包含 `/open-apis` |
| `Timeout` | 15 秒 | 单个 HTTP 请求超时；`0` 使用默认值，负数无效 |
| `HTTPClient` | 新建客户端 | 复制配置，禁止重定向 |

`NewCard` 只构建 JSON，无需初始化客户端。响应上限固定为 2 MiB；回调解析器使用单独的 `CallbackConfig`，其 `MaxAge` 默认 5 分钟、请求体上限固定为 1 MiB。

```go
import "github.com/bkcarlos/goparts/feishu/card"

client, err := card.New(card.Config{AppID: appID, AppSecret: appSecret})
if err != nil { return err }

content := card.NewCard("发布通知").WithSummary("v1.0 已上线").Add(
    card.Markdown("**版本**：v1.0\n**状态**：部署成功"),
    card.Divider(),
    card.LinkButton("查看详情", "https://example.com/releases"),
    card.CallbackButton("确认", map[string]any{"release_id": "v1.0"}),
)
content.Header.Template = "green"

message, err := client.Send(ctx, card.Chat(chatID), content,
    card.SendOptions{UUID: "release-v1.0"})
if err != nil { return err }

// 私聊使用 card.User(openID)；其他 ID 使用 Receiver{Type: "email", ID: email} 等。
_, err = client.Reply(ctx, message.MessageID,
    card.NewCard("进度").Add(card.Markdown("发布检查通过")),
    card.ReplyOptions{InThread: true})
if err != nil { return err }

return client.UpdateMessage(ctx, message.MessageID,
    card.NewCard("已确认").Add(card.Markdown("发布已确认")))
```

`NewCard` 默认设置 `schema: 2.0` 和 `config.update_multi: true`，便于后续更新。Header、Config 可直接配置，`Element` 是可扩展的 map：例如 `button["type"] = "primary"`、`button["disabled"] = true`。复杂分栏、表单等可通过 `Add(map[string]any{...})` 或完整 JSON 对象 / `json.RawMessage` 接入。构建器不校验全部卡片 DSL；组件属性、元素数量和模板变量最终由飞书校验。图片使用已有 `image_key`，不包含图片上传。

已有 Webhook 可直接发送构建好的通知卡片：

```go
notice := card.NewCard("通知").Add(card.Markdown("服务正常"))
err := webhook.Send(ctx, feishu.Card(notice))
```

Webhook 保持原有 20 KiB 请求限制。需要按钮回调、私聊、更新或流式输出时，使用应用机器人客户端；Webhook 本身不能完成用户登录或卡片交互。

模板发送：

```go
content := card.Template(templateID, "1.0.0", map[string]any{"status": "完成"})
message, err := client.Send(ctx, card.Chat(chatID), content, card.SendOptions{})
```

模板需先在飞书卡片搭建工具中发布；省略版本时使用最新发布版本。`CreateTemplate` 接收 `TemplateData`，返回卡片实体 ID。

## LLM 流式卡片

```go
content := card.NewCard("AI 回答").WithStreaming(true).Add(
    card.Markdown("思考中…").WithID("answer"),
)
id, err := client.Create(ctx, content)
if err != nil { return err }
_, err = client.Send(ctx, card.Chat(chatID), card.Reference(id), card.SendOptions{})
if err != nil { return err }

// fullText 是当前累计全文。调用方将 LLM delta 合并后按需节流。
var sequence int32 = 1
if err := client.UpdateText(ctx, id, "answer", fullText,
    card.UpdateOptions{Sequence: sequence}); err != nil { return err }
sequence++
return client.Finish(ctx, id, "回答完成", card.UpdateOptions{Sequence: sequence})
```

- `UpdateText` 接收累计全文，不能直接传入单个 delta。Markdown 元素 ID 需以字母开头、只含字母/数字/下划线，最长 20 字符。
- 同一卡片的**所有**更新，包括文本、整卡和设置，必须串行执行并共用严格递增的 `Sequence`（1 到 2147483647）。组件不自动持久化序号，多实例业务需自行协调。
- `Finish` 关闭流式模式并设置摘要；它不会写入最后一段文字，结束前先提交最终全文。请求失败或被取消后，调用方仍应尝试用新的 Context 和更大序号关闭流式模式，并处理关闭失败。
- 超时可能发生在服务端已接受更新之后。新操作使用更大序号；对同一次请求重试时保留其 UUID、序号和内容，按飞书幂等规则处理。组件不自动重试。
- 每个卡片实体只支持发送一次，且发送/修改者必须是创建它的应用。需要发给多个会话时分别创建实体。卡片实体有效期为 14 天。
- 普通消息卡片更新同样限发送后 14 天，更新前后必须启用共享更新；单条消息更新限 5 QPS。飞书端仍会校验权限和限流。

完整的“Webhook 通知 → 创建实体 → 发送 → 累计更新 → 结束 → 回调解析”本地示例：

```sh
cd feishu
GOWORK=off go run ./examples/cards_mock
```

示例只访问 `httptest` 本地服务，不发真实飞书消息，也不调用 LLM 服务。业务可在自己的 LLM 流回调中累加文本后调用卡片接口，两模块不互相依赖。

## HTTP 回调

在飞书应用的回调配置中订阅 `card.action.trigger`，配置公网 HTTPS 回调地址和对应的 Verification Token / Encrypt Key。本子包提供 HTTP 回调和 URL 验证；无需公网地址时可使用独立的 [events 长连接子包](../events/README.md)，通过 `OnCardAction` 复用这里的回调类型。当前不支持旧版卡片回调协议。

```go
decoder, err := card.NewCallbackDecoder(card.CallbackConfig{
    AppID: appID,
    VerificationToken: verificationToken,
    EncryptKey: encryptKey, // 与开发者后台保持一致；未启用加密时为空
})
if err != nil { return err }

http.HandleFunc("/feishu/card", func(w http.ResponseWriter, r *http.Request) {
    event, err := decoder.Decode(r)
    if err != nil {
        http.Error(w, "invalid callback", http.StatusBadRequest)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    if event.Type == "url_verification" {
        _ = json.NewEncoder(w).Encode(map[string]string{"challenge": event.Challenge})
        return
    }

    // 使用 event.Header.EventID 做幂等和响应缓存。
    // 业务操作前校验 event.Event.Operator、目标资源和 action 的权限。
    // Action.Value / FormValue 保留为 json.RawMessage，按业务类型解析。
    response := card.CallbackResponse{
        Toast: &card.Toast{Type: "info", Content: "已收到操作"},
    }
    _ = json.NewEncoder(w).Encode(response)
})
```

回调应在 3 秒内响应，耗时任务交给业务任务队列。立即更新时可设置 `response.Card = card.RawResponseCard(newCard)`，或使用 `TemplateResponseCard`。返回普通 JSON，不要重定向。示例中的提示不代表业务操作已经成功。

解析器限制请求体为 1 MiB，检查 Verification Token、App ID、事件类型。配置 Encrypt Key 时，事件需通过 SHA-256 签名、时间戳检查（默认前后 5 分钟）和 AES-CBC 解密；URL 验证按官方协议允许无签名，但仍检查解密和 Verification Token。未配置 Encrypt Key 时按明文协议检查 Token，不具备签名时间窗口保护。解析器不记录或去重事件；业务需持久化幂等记录，并缓存响应以处理重投。Token、解密后的原始回调和凭证不要写入日志。

## 权限、错误和验证

`APIError` 已适配 [apperror 统一上报](../../apperror/README.md#已有模块适配)，编码为 `feishu.card.api_error`，字段包含 `http_status`、`upstream_code`、`request_id`。原始服务端 Message 保留在错误对象上，不自动放入上报记录。

企业自建应用需要启用机器人能力并发布配置。发送消息可开通 `im:message:send_as_bot`；更新可使用该权限或 `im:message:update` 等接口允许的权限；创建和更新实体另需 `cardkit:card:write`。机器人需在目标群内，私聊用户需处于应用可用范围。用户文档授权仍使用独立的 `feishu/user`，Cardkit 操作使用应用身份。

客户端默认每个 HTTP 请求超时 15 秒，支持 Context 取消和注入 HTTPClient/BaseURL（后者包含 `/open-apis`）。HTTPClient 被复制，禁止重定向，Token 仅缓存在内存；Token 获取并发合并，临近过期时刷新。不自动重试发送或更新，也不在 Token 失效后重放请求。

`errors.As(err, &apiErr)` 可取得 `*card.APIError` 的 `Code`、`Message`、`StatusCode` 和 `RequestID`；`Error()` 不包含原始响应。响应缺少必要字段返回 `ErrInvalidResponse`。响应上限 2 MiB；通用出站 JSON 对象保守限制 30 KiB，模板展开和样式膨胀仍以飞书校验为准。流式文本接口另按 100000 字符限制，最终卡片仍受平台容量限制。

```sh
GOWORK=off go test -race -cover ./...
GOWORK=off go vet ./...
```

测试覆盖请求协议、Token 缓存和刷新、并发与取消、业务错误和 HTTP 错误、无重试、重定向限制、卡片构建，以及回调验签、解密、伪造/过期请求和 URL 验证。尚未连接真实飞书应用进行端到端验证。

协议参考：[JSON 2.0](https://open.feishu.cn/document/feishu-cards/card-json-v2-structure)、[创建实体](https://open.feishu.cn/document/cardkit-v1/card/create)、[流式更新](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/streaming-updates-openapi-overview)、[更新消息卡片](https://open.feishu.cn/document/server-docs/im-v1/message-card/patch)、[卡片回调](https://open.feishu.cn/document/feishu-cards/card-callback-communication)、[官方回调协议实现参考](https://github.com/larksuite/oapi-sdk-go/blob/v3_main/event/event.go)。本包独立实现协议，没有引入官方 SDK 或 CLI 依赖。
