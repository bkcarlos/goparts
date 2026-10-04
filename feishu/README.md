# Feishu 独立模块

[返回模块总览](../README.md) · [用户登录与文档](user/README.md) · [卡片操作](card/README.md) · [统一错误与上报](../apperror/README.md)

模块名：`github.com/bkcarlos/goparts/feishu`，Go 1.21+。`events` 子包使用 Gorilla WebSocket 和 Protobuf wire 编码，`user` 的口令密钥派生使用 PBKDF2 依赖；其余当前子包使用标准库。该目录包含独立的 `go.mod`、实现、测试和示例，可以单独复制到其他仓库使用。

常用入口按场景划分如下（业务 API 见文末）：

- 根包 `github.com/bkcarlos/goparts/feishu`：群自定义机器人 Webhook 通知。
- 子包 [`github.com/bkcarlos/goparts/feishu/user`](user/README.md)：设备授权登录、用户 token 刷新、可选加密会话存储，以及用户身份的 docx 读取、创建、追加和编辑。参考官方 CLI 的授权协议独立实现，没有完整引入 CLI。
- 子包 [`github.com/bkcarlos/goparts/feishu/card`](card/README.md)：JSON 2.0 卡片构建、模板、应用机器人群聊/私聊发送、回复与更新、LLM 流式卡片和 HTTP 按钮回调校验。
- 子包 [`github.com/bkcarlos/goparts/feishu/attachment`](attachment/README.md)：聊天附件上传和发送、云空间文件上传、文档素材上传，组合用户身份完成 docx 附件关联。
- 子包 [`github.com/bkcarlos/goparts/feishu/events`](events/README.md)：长连接鉴权、心跳、重连、消息/卡片/机器人入群事件分发和应答。

用户授权需要应用 App ID / App Secret 与用户同意，Webhook 地址本身不能登录、上传附件或操作文档。卡片发送和更新使用应用机器人身份；附件通过 TokenProvider 显式选择应用或用户身份。长连接使用应用凭据，事件订阅和权限需在开发者后台配置。

## 使用

在业务代码中 `import "github.com/bkcarlos/goparts/feishu"`，直接调用 `feishu.New(feishu.Config{...})` 创建客户端。配置由调用方传入，模块本身不读取环境变量。完整可运行代码见 [examples/basic/main.go](examples/basic/main.go)。

## 消息与错误处理

### Webhook 配置

| 字段 | 默认值 / 要求 | 说明 |
| --- | --- | --- |
| `WebhookURL` | 必填 | 自定义机器人的完整 Webhook 地址 |
| `Secret` | 空 | 启用机器人签名校验时传入对应密钥 |
| `Timeout` | 5 秒 | `0` 使用默认值，负数无效 |
| `HTTPClient` | 新建客户端 | 支持注入；复制配置，禁止重定向 |

本表针对根包 `feishu`。应用机器人卡片使用 `card.Config`；用户授权使用 `user.Config`，两者都需要应用凭据，不能使用 Webhook URL 替代。

### 消息构建


```go
bot, err := feishu.New(feishu.Config{WebhookURL: webhook, Secret: secret})
if err != nil { return err }

// 每次 Send 都返回错误，业务代码应检查。
if err := bot.SendText(ctx, "服务启动成功"); err != nil { return err }
if err := bot.SendMarkdown(ctx, "告警", "**错误率上升**，请检查服务"); err != nil { return err }

// 富文本：每个内层切片为一行。
if err := bot.Send(ctx, feishu.Post(map[string]feishu.PostContent{
    "zh_cn": {
        Title: "发布通知",
        Content: [][]feishu.PostElement{{
            {Tag: "text", Text: "发布完成 "},
            {Tag: "a", Text: "查看详情", Href: "https://example.com/releases"},
        }},
    },
})); err != nil { return err }

// 图片使用已获取的 image_key，本包不负责图片上传。
if err := bot.Send(ctx, feishu.Image(imageKey)); err != nil { return err }

// 复杂卡片可传入符合飞书协议的 JSON 对象。
if err := bot.Send(ctx, feishu.Card(cardJSON)); err != nil { return err }
```

发送行为：

- 默认整体超时 5 秒；可通过 `Config.Timeout` 设置，调用方 Context 的更早截止时间同样生效。
- 签名按当前 Unix 秒生成；未设置 Secret 时不发送签名字段。
- 请求体上限 20 KiB；响应体读取上限 1 MiB。
- 同时识别当前 `code/msg` 和历史 `StatusCode/StatusMessage`，HTTP 200 内的业务失败也返回错误。缺少状态码或无效 JSON 不算成功。
- 可注入 `HTTPClient`；复制客户端配置，不修改调用方客户端。禁止跟随重定向。HTTP 地址用于本地测试或可信代理，生产飞书地址应使用 HTTPS。
- 默认单次尝试，不自动重试、不排队、不限流。超时并不代表没有送达，自动重试可能重复发送。调用方需遵守飞书频控；大量告警建议增加聚合、去重和限流层。
- 普通网络错误移除 `net/http` 附带的请求 URL，避免错误日志直接暴露 Webhook token；自定义 Transport 的错误文本仍由其自身负责。
- 卡片、富文本数据应符合飞书协议，发送期间不要并发修改其中的 map/slice。关键词、IP 白名单等限制仍由飞书端校验。

错误分类：

```go
err := bot.SendText(ctx, "通知内容")
var apiErr *feishu.APIError
var httpErr *feishu.HTTPError
switch {
case err == nil:
    // 成功
case errors.Is(err, context.DeadlineExceeded):
    // 超时，送达状态可能未知
case errors.Is(err, context.Canceled):
    // 调用被取消
case errors.As(err, &apiErr):
    // 查看 apiErr.Code / apiErr.Message
case errors.As(err, &httpErr):
    // 查看 httpErr.StatusCode
default:
    // 编码、网络或响应解析错误
}
```

## 统一错误上报

根包的 `APIError` / `HTTPError`，以及 user、card、attachment、events 子包的结构化 API 错误，都可直接传给 `apperror.Reporter.Capture`。统一编码与字段见 [错误类型适配表](../apperror/README.md#已有模块适配)。原有的 `errors.Is/As` 和错误类型保持可用。

## 独立运行与验证

在本模块目录执行：

```sh
GOWORK=off go run ./examples/basic
GOWORK=off go test -race -cover ./...
GOWORK=off go vet ./...
```

示例未设置 `FEISHU_WEBHOOK_URL` 时跳过发送；设置后会发出一条实际群通知。开启签名校验时还需设置 `FEISHU_SECRET`。

测试使用本地 HTTP 测试服务器及模拟 Transport，不连接真实飞书群。模块可在关闭 workspace 后独立编译和测试，不依赖日志模块。

用户登录与文档操作的本地示例：`GOWORK=off go run ./examples/userdocs_mock`。真实登录入口和应用权限准备见 [用户身份接入文档](user/README.md)。

卡片构建、流式更新和回调的本地示例：`GOWORK=off go run ./examples/cards_mock`。接口使用与权限准备见 [卡片接入文档](card/README.md)。

附件上传、发送和文档关联的本地示例：`GOWORK=off go run ./examples/attachments_mock`。接口与大小限制见 [附件接入文档](attachment/README.md)。

长连接本地验证：`GOWORK=off go test -race ./events`。真实连接入口为 `examples/events`，权限、订阅和执行语义见 [长连接接入文档](events/README.md)。

安装与本地联调方式见[接入指南](../README.md#接入业务项目)。

协议参考：[飞书自定义机器人指南](https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot)。

### 业务 API 与身份

新增独立子包 bitable（表 CRUD、批量记录创建/更新/搜索、字段 CRUD）、wiki
（Wiki 节点与真实 Bitable token 解析）、contact（邮箱/手机批量查询用户 ID）。
统一 `Config{TokenProvider, BaseURL, Timeout, MaxResponseBytes, HTTPClient}`，
默认响应限 4 MiB；分页显式传 PageSize/PageToken，结果含 HasMore/PageToken。
字段类型和复杂筛选使用自己的类型或 RawMessage，不引入飞书完整 SDK。
Drive/media 上传继续使用 attachment 包。接口写入只尝试一次，不隐式重放。

先安装整个 Feishu module，子包不分别发布：

```sh
go get github.com/bkcarlos/goparts/feishu@v0.1.0
```

下面的函数从 Wiki 链接解析多维表格，并显式遍历表分页。tokenProvider 可传
`user.Client.AccessToken`、`card.Client.AccessToken`，或多用户 Manager.AccessToken；
使用 Manager 时在传入 Context 中用 user.AsUser 选择用户，不能省略身份后假定自动回退。

```go
package example

import (
    "context"
    "errors"

    "github.com/bkcarlos/goparts/feishu/bitable"
    "github.com/bkcarlos/goparts/feishu/wiki"
)

func TablesFromWiki(ctx context.Context, tokenProvider func(context.Context) (string, error),
    link string) ([]bitable.Table, error) {
    resolver, err := wiki.New(wiki.Config{TokenProvider: tokenProvider})
    if err != nil { return nil, err }
    appToken, err := resolver.ResolveBitable(ctx, link)
    if err != nil { return nil, err }
    client, err := bitable.New(bitable.Config{TokenProvider: tokenProvider})
    if err != nil { return nil, err }
    var tables []bitable.Table
    cursor := ""
    seen := map[string]bool{}
    for {
        page, err := client.ListTables(ctx, appToken, bitable.Page{
            PageSize: 100, PageToken: cursor,
        })
        if err != nil { return nil, err }
        tables = append(tables, page.Items...)
        if !page.HasMore { return tables, nil }
        if page.PageToken == "" || seen[page.PageToken] {
            return nil, errors.New("non-advancing Feishu page token")
        }
        cursor = page.PageToken
        seen[cursor] = true
    }
}
```

| 子包 | 常用方法 | 输入要点 |
| --- | --- | --- |
| bitable | ListTables / CreateTable / UpdateTable / DeleteTable | app token；操作已有表还需 table ID |
| bitable | Search / BatchCreate / BatchUpdate | batch 1..500 条，字段不能为空；更新需 record ID |
| bitable | ListFields / CreateField / UpdateField / DeleteField | 创建/更新提供字段名和正整数类型 |
| wiki | GetNode / Resolve / ResolveBitable | token 或 HTTPS Wiki URL；ResolveBitable 检查节点确为多维表格 |
| contact | BatchGetID | emails 和 mobiles 各最多 50，至少有一项；ID 类型为 open_id/user_id/union_id |
| card/legacy | New / Div / Action / Button / JSON | 构建 JSON 1.0 卡片；不自动转换成 JSON 2.0 |
| dedup | NewMemory / Do | 同 key 合并执行并重放成功结果，失败可重试；TTL 和容量必须正数 |

Bitable PageSize 为 0..500；0 不发送 page_size。分页未自动耗尽，应用应保留 PageToken 原值。
TokenProvider 每次请求显式获取身份凭据，返回错误后不会悄悄换成应用身份。
业务 API 默认超时 15 秒、响应上限 4 MiB，可通过 Config 覆盖；Context 更早的取消仍生效。

Scopes 和资源授权由调用者在飞书后台开通。Wiki URL 只提取 token，不向 URL 主机发凭据。
官方协议参考：[Bitable](https://open.feishu.cn/document/server-docs/docs/bitable-v1/app-table-record/search)、
[Wiki](https://open.feishu.cn/document/server-docs/docs/wiki-v2/space-node/get_node)、
[Contact](https://open.feishu.cn/document/server-docs/contact-v3/user/batch_get_id)。
