# goparts

面向 Go 服务的公共组件集合：日志、配置、HTTP、重试、生命周期、LLM、飞书、统一错误处理、对象存储与下载。**每个模块有独立的 `go.mod`，按需引用、分别初始化**，不需要创建全局 SDK 实例。

当前要求 **Go 1.21+**，十个模块的生产代码互不导入。`config` 使用 `go.yaml.in/yaml/v3`；`feishu/events` 使用 Gorilla WebSocket 和 Protobuf wire 编码；`storage/aliyun` 使用阿里云 OSS Go SDK v2。其余模块只使用标准库，`storage` 根包也不导入云 SDK。`go.work` 用于本仓库开发，业务项目可以单独引入任意模块。

仓库：[bkcarlos/goparts](https://github.com/bkcarlos/goparts)。各模块使用 `github.com/bkcarlos/goparts/<模块名>` 导入路径；当前尚未创建版本标签。

## 导航

- [模块选型](#模块选型)
- [快速开始](#快速开始)
- [接入业务项目](#接入业务项目)
- [配置与默认值](#配置与默认值)
- [统一错误与上报](#统一错误与上报)
- [飞书与 LLM 组合](#飞书与-llm-组合)
- [示例入口](#示例入口)
- [开发与验证](#开发与验证)
- [目录与发布](#目录与发布)
- [远程 issues 与待完善事项](#远程-issues-与待完善事项)
- [常见问题](#常见问题)

## 模块选型

| 需要的能力 | 导入路径 | 主要入口 | 接入文档 |
| --- | --- | --- | --- |
| JSON / 文本日志、动态级别、请求字段 | `github.com/bkcarlos/goparts/logger` | `New`、`WithContext`、`WithFields` | [logger](logger/README.md) |
| 默认值、JSON / YAML 文件、环境变量、启动校验 | `github.com/bkcarlos/goparts/config` | `Load[T]` | [config](config/README.md) |
| HTTP / JSON 请求、连接复用、响应大小限制 | `github.com/bkcarlos/goparts/httpclient` | `New`、`Do`、`DoJSON` | [httpclient](httpclient/README.md) |
| 指数退避、抖动、按错误判断是否重试 | `github.com/bkcarlos/goparts/retry` | `New`、`Do` | [retry](retry/README.md) |
| 后台任务、退出信号、逆序资源清理 | `github.com/bkcarlos/goparts/lifecycle` | `New`、`Add`、`OnStop`、`RunSignals` | [lifecycle](lifecycle/README.md) |
| 兼容协议对话、SSE、工具调用数据、Embedding | `github.com/bkcarlos/goparts/llm` | `New`、`Chat`、`ChatStream`、`Embeddings` | [llm](llm/README.md) |
| 群自定义机器人 Webhook 通知 | `github.com/bkcarlos/goparts/feishu` | `New`、`SendText`、`SendMarkdown`、`Send` | [feishu](feishu/README.md) |
| 用户授权登录、会话刷新、docx 读写 | `github.com/bkcarlos/goparts/feishu/user` | `New`、`StartLogin`、`CompleteLogin` | [用户身份](feishu/user/README.md) |
| 卡片构建、发送、回复、更新、按钮回调 | `github.com/bkcarlos/goparts/feishu/card` | `NewCard`、`New`、`Send`、`UpdateText` | [卡片](feishu/card/README.md) |
| 聊天附件、云空间文件、文档素材上传 | `github.com/bkcarlos/goparts/feishu/attachment` | `New`、`UploadChatPath`、`UploadDrivePath`、`UploadMediaPath` | [附件](feishu/attachment/README.md) |
| 长连接消息、卡片和机器人入群事件 | `github.com/bkcarlos/goparts/feishu/events` | `NewDispatcher`、`New`、`Run` | [长连接](feishu/events/README.md) |
| 编码错误、错误链、业务扩展、统一上报 | `github.com/bkcarlos/goparts/apperror` | `New`、`Wrap`、`Describe`、`NewReporter` | [apperror](apperror/README.md) |
| 通用对象存储、阿里云 OSS 适配 | `github.com/bkcarlos/goparts/storage` | `New`、`PutFile`、`Get`、`List`、`PresignGet` | [storage](storage/README.md) |
| HTTP / 任意数据源流式下载与校验 | `github.com/bkcarlos/goparts/download` | `New`、`NewHTTPSource`、`Fetch` | [download](download/README.md) |

`feishu/user`、`feishu/card`、`feishu/attachment` 和 `feishu/events` 是同一个 Feishu module 下的独立子包，使用各自的配置和客户端。模块间通过参数、回调和小接口组合：例如业务加载配置后传给客户端，把 LLM 流交给卡片接口，把错误记录交给日志或通知出口。

## 快速开始

在仓库根目录执行以下本地示例，无需外部凭据：

```sh
# 日志与请求字段
(cd logger && GOWORK=off go run ./examples/basic)

# 错误编码、业务扩展和本地日志上报
(cd apperror && GOWORK=off go run ./examples/basic)

# LLM 流式响应：仅连接本地模拟服务
(cd llm && GOWORK=off go run ./examples/stream)

# 飞书卡片：本地模拟构建、发送、流式更新和回调
(cd feishu && GOWORK=off go run ./examples/cards_mock)
```

检查全部模块及跨模块错误上报：

```sh
make check
```

飞书真实发送、用户授权和 LLM 服务商调用的入口单独列在[示例入口](#示例入口)中。

## 接入业务项目

在已经初始化 Go module 的业务项目中，按需安装：

```sh
go get github.com/bkcarlos/goparts/logger@latest
go get github.com/bkcarlos/goparts/apperror@latest
```

仓库为私有时，需先配置有访问权限的 Git 凭据，并将 `github.com/bkcarlos/goparts` 加入 `GOPRIVATE`（保留已有条目）。例如仅为本次命令设置：

```sh
GOPRIVATE=github.com/bkcarlos/goparts go get github.com/bkcarlos/goparts/logger@latest
```

尚无版本标签时，Go 会解析为对应提交的伪版本。以下本地 `replace` 方式适合修改组件和业务联调。

以同时使用日志和错误组件为例，在业务项目的 `go.mod` 中增加以下内容，保留原有 `module`、`go` 及其他依赖：

```go
require (
    github.com/bkcarlos/goparts/logger v0.0.0
    github.com/bkcarlos/goparts/apperror v0.0.0
)

replace github.com/bkcarlos/goparts/logger => /absolute/path/to/goparts/logger
replace github.com/bkcarlos/goparts/apperror => /absolute/path/to/goparts/apperror
```

将路径改为本地实际目录，然后创建 `main.go`：

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/bkcarlos/goparts/apperror"
    "github.com/bkcarlos/goparts/logger"
)

var ErrOrderQuery = apperror.New("order.query_failed", "订单查询失败")

func main() {
    if err := run(); err != nil {
        log.Fatal(err)
    }
}

func run() error {
    l, err := logger.New(logger.Config{Service: "order-service"})
    if err != nil {
        return err
    }
    reporter, err := apperror.NewReporter(apperror.ReporterConfig{
        Sink: apperror.SinkFunc(func(ctx context.Context, record apperror.Record) error {
            l.ErrorContext(ctx, record.Message,
                "code", record.Code, "fields", record.Fields)
            return nil
        }),
    })
    if err != nil {
        return err
    }

    // 模拟底层错误；真实业务替换为数据库或接口调用结果。
    businessErr := ErrOrderQuery.Wrap(os.ErrNotExist,
        apperror.WithFields(apperror.Fields{"order_id": "42"}))
    return reporter.Capture(context.Background(), businessErr,
        apperror.Fields{"request_id": "demo-001"})
}
```

在业务项目中运行：

```sh
GOWORK=off go mod tidy
GOWORK=off go run .
```

这里 `v0.0.0` 配合本地 `replace` 使用，不代表已经发布的版本。引入飞书时只需配置 `github.com/bkcarlos/goparts/feishu` 这个模块，即可使用其根包、`user`、`card`、`attachment` 和 `events` 子包。

## 配置与默认值

客户端配置由调用方传入；各组件不会自动加载业务配置文件。`config` 模块提供可选的加载流程：**结构体 default 标签 → JSON / YAML 文件 → env 标签 → 校验**。`.yaml` / `.yml` 自动使用 YAML，也可显式传入 `FormatYAML`；YAML 时长可写为 `5s`。读取环境变量的示例程序和组件本身是分开的。

| 配置项 | 默认值 | 覆盖方式 |
| --- | --- | --- |
| 日志格式 / 级别 / 输出 | JSON / INFO / stdout | `logger.Config.Format`、`Level`、`Writer` |
| HTTP 请求超时 / 响应上限 | 10 秒 / 4 MiB | `httpclient.Config.Timeout`、`MaxResponseBytes` |
| LLM 请求超时 / 普通响应上限 | 2 分钟 / 8 MiB | `llm.Config.Timeout`、`MaxResponseBytes` |
| LLM 单个 SSE 行或事件上限 | 1 MiB | `llm.Config.MaxEventBytes` |
| 飞书 Webhook 请求超时 | 5 秒 | `feishu.Config.Timeout` |
| 飞书用户 / 卡片 API 单次请求超时 | 15 秒 | 对应子包的 `Config.Timeout` |
| 飞书附件操作超时 / 文件上限 / 响应上限 | 2 分钟 / 30 MiB / 2 MiB | `attachment.Config`；云空间和素材接口仍限制 20 MiB |
| 重试次数 / 初始等待 / 最大等待 | 3 次 / 100ms / 5 秒 | `retry.Config`；未配置 `RetryIf` 时不重试 |
| 生命周期关闭预算 | 10 秒 | `lifecycle.Config.ShutdownTimeout` |
| 上报完整错误文本 | 不包含 | `apperror.ReporterConfig.IncludeDetail` |

可配置的超时和大小参数传 `0` 时使用默认值，例如：

```go
client, err := httpclient.New(httpclient.Config{
    Timeout: 5 * time.Second,
    MaxResponseBytes: 16 * 1024 * 1024, // 自定义 16 MiB
})
```

`DefaultMaxResponseBytes` 只是默认值，不会覆盖显式配置。该规则不意味着所有零值都合法：飞书需要对应凭据，用户授权需显式配置 Scopes，Reporter 必须提供 Sink，LLM 聊天模型需在配置或请求中指定。构造参数的完整规则见各模块文档。

固定上限也需与可配置默认值区分：当前配置文件上限为 1 MiB；飞书 Webhook 请求上限为 20 KiB、响应上限为 1 MiB；用户 API 响应上限为 8 MiB、卡片 API 响应上限为 2 MiB。这些限制目前未暴露为 Config 字段。

## 统一错误与上报

建议在领域层定义稳定编码和简短说明，在调用处附加上下文，在应用边界上报一次：

```go
var ErrPayment = apperror.New("payment.failed", "支付失败")

// Wrap(nil, ...) 返回 nil；底层错误保留在 Unwrap 链中。
wrapped := ErrPayment.Wrap(err,
    apperror.WithFields(apperror.Fields{"order_id": orderID}))

if errors.Is(wrapped, ErrPayment) {
    // 按错误码分类处理。
}
```

业务需要携带额外类型信息时，可以嵌入 `*apperror.Base`；已有错误类型可以实现 `ErrorInfo() (code, message string)`，并按需实现 `ErrorFields() map[string]string`。通过 `errors.As` 仍可取得原始具体类型。

HTTP、LLM 和飞书的结构化错误已适配这些接口，可以直接传给 `Reporter.Capture`，无需先转换类型，也不会让它们依赖 `apperror`。普通错误默认归为 `common.unknown`，Context 取消和超时有对应通用编码；业务可包装成更具体的分类。

`Capture` 的返回值表示**上报是否失败**，不替代原始业务错误。Reporter 同步调用注入的 Sink，不自动重试、去重或启动后台队列。默认记录包含 `code`、`message`、`fields`；完整 `err.Error()` 仅在开启 `IncludeDetail` 后写入 `detail`。

详细示例见 [错误定义、嵌入扩展和 Sink 组合](apperror/README.md)，可将 Sink 接到 `logger`、飞书 Webhook 或自己的监控服务。

## 飞书与 LLM 组合

| 场景 | 包 | 身份 / 配置 | 使用范围 |
| --- | --- | --- | --- |
| 群通知 | `feishu` | Webhook URL，可选签名密钥 | 文本、图片、富文本、通知卡片 |
| 用户文档操作 | `feishu/user` | App ID / Secret + 用户设备授权 | 该用户已有权限范围内的 docx 读写 |
| 应用卡片交互 | `feishu/card` | App ID / Secret，获取 tenant token | 群聊/私聊卡片、更新、流式输出、HTTP 回调 |
| 附件上传 | `feishu/attachment` | 注入应用或用户的 AccessToken 方法 | 聊天上传和发送、云空间上传、文档素材上传 |
| 长连接事件 | `feishu/events` | App ID / Secret，开发者后台订阅事件 | 消息、卡片交互、机器人入群及通用 schema 2.0 事件 |

Webhook 地址不能替代用户登录。用户授权模块借鉴官方 CLI 的协议流程独立实现，没有完整引入 CLI；卡片模块也不依赖官方大 SDK。

LLM 模块可配置兼容协议的 BaseURL 和模型名；对接 DeepSeek、通义等兼容端点时，具体模型能力仍由服务商决定。应用层可将 `ChatStream` 的 delta 累加成全文，再调用卡片 `UpdateText`。同一卡片的更新需串行执行、共用递增 Sequence，最后调用 `Finish` 关闭流式模式。

接入步骤见 [用户登录与文档](feishu/user/README.md)、[卡片与流式更新](feishu/card/README.md)、[LLM 对话与流式回调](llm/README.md)。这些能力已通过本地模拟服务验证，尚未完成真实飞书应用和模型服务商的端到端联调。

## 示例入口

下列命令均在对应模块目录中执行，链接指向可运行源码。

| 模块 / 场景 | 命令 | 执行效果 |
| --- | --- | --- |
| [日志](logger/examples/basic/main.go) | `GOWORK=off go run ./examples/basic` | 本地 JSON 日志、请求字段、动态级别 |
| [配置](config/examples/basic/main.go) | `GOWORK=off go run ./examples/basic` | 默认值与环境变量加载 |
| [YAML 配置](config/examples/basic/config.yaml) | `GOWORK=off go run ./examples/basic -config ./examples/basic/config.yaml` | YAML、可读时长、环境变量覆盖 |
| [HTTP](httpclient/examples/basic/main.go) | `GOWORK=off go run ./examples/basic` | 本地 HTTP 服务与 JSON 请求 |
| [重试](retry/examples/basic/main.go) | `GOWORK=off go run ./examples/basic` | 模拟临时失败后成功 |
| [生命周期](lifecycle/examples/basic/main.go) | `GOWORK=off go run ./examples/basic` | 后台任务启动后自动退出 |
| [错误处理](apperror/examples/basic/main.go) | `GOWORK=off go run ./examples/basic` | 嵌入业务类型与本地上报 |
| [LLM 普通对话](llm/examples/basic/main.go) | `GOWORK=off go run ./examples/basic` | 本地模拟服务 |
| [LLM 流式对话](llm/examples/stream/main.go) | `GOWORK=off go run ./examples/stream` | 本地模拟 SSE |
| [飞书用户文档](feishu/examples/userdocs_mock/main.go) | `GOWORK=off go run ./examples/userdocs_mock` | 本地模拟授权、刷新和文档读写 |
| [飞书卡片](feishu/examples/cards_mock/main.go) | `GOWORK=off go run ./examples/cards_mock` | 本地模拟发送、更新和回调 |
| [飞书附件](feishu/examples/attachments_mock/main.go) | `GOWORK=off go run ./examples/attachments_mock` | 本地模拟聊天上传/发送、用户云空间上传/文档附件关联 |
| [对象存储](storage/examples/basic/main.go) | `GOWORK=off go run ./examples/basic` | 本地模拟阿里云上传、读取，使用通用 Client |
| [通用下载](download/examples/basic/main.go) | `GOWORK=off go run ./examples/basic` | 本地 HTTP 下载和 SHA-256 计算 |

真实调用入口需要显式配置并执行：

| 示例 | 准备 | 行为 |
| --- | --- | --- |
| [飞书 Webhook](feishu/examples/basic/main.go) | `FEISHU_WEBHOOK_URL`，可选 `FEISHU_SECRET` | `go run ./examples/basic`；未配置 URL 时跳过，配置后发送真实群通知 |
| [飞书用户文档](feishu/examples/userdocs/main.go) | `FEISHU_APP_ID`、`FEISHU_APP_SECRET`、`FEISHU_TOKEN_KEY`，对应应用权限 | `go run ./examples/userdocs login`；写入需显式执行 create / append / update，详见子包文档 |
| [飞书长连接](feishu/examples/events/main.go) | `FEISHU_APP_ID`、`FEISHU_APP_SECRET`，后台事件订阅和权限 | `go run ./examples/events`；未配置凭据时跳过，配置后真实连接并应答事件 |
| [LLM 服务商](llm/examples/live/main.go) | `LLM_MODEL`，对应端点的 `LLM_BASE_URL` / `LLM_API_KEY` | `go run ./examples/live -prompt '你好'`；会发出真实模型请求，可能计费 |

环境变量由这些示例读取，业务使用 SDK 时仍通过 Config 传参。

## 开发与验证

仓库根目录没有 `go.mod`，不要直接执行 `go test ./...`。可使用：

```sh
make test         # 各模块关闭 workspace，执行 go test -race -cover
make vet          # 各模块关闭 workspace，执行 go vet
make integration  # 使用 workspace 验证跨模块错误上报
make check        # 执行以上全部检查
```

`-race` 需要当前平台支持竞态检测，并具备相应 CGO / C 编译工具链。`make integration` 需要启用本仓库的 `go.work`；若环境中设置了 `GOWORK=off`，运行前取消该覆盖或显式指定此文件。

单独验证一个模块：

```sh
cd apperror
GOWORK=off go test -race -cover ./...
GOWORK=off go vet ./...
GOWORK=off go list -m all
```

测试使用本地模拟服务，不需要真实飞书凭据或模型 API Key；测试成功表示本地协议和行为检查通过。跨模块测试位于 [tests/error_reporting_test.go](tests/error_reporting_test.go)。

## 目录与发布

```text
goparts/
├── logger/       # 每个模块均有 go.mod、README、实现、测试与 examples
├── config/
├── httpclient/
├── retry/
├── lifecycle/
├── llm/
├── apperror/
├── storage/      # 通用接口，aliyun/ 为首个供应商适配器
├── download/     # 通过 Source 接入 HTTP 或对象存储，不依赖云 SDK
├── feishu/
│   ├── go.mod
│   ├── user/     # 用户授权和文档子包，共用 feishu 的 go.mod
│   ├── card/     # 卡片子包，共用 feishu 的 go.mod
│   ├── attachment/ # 附件上传子包，共用 feishu 的 go.mod
│   ├── events/   # 长连接事件子包，共用 feishu 的 go.mod
│   └── examples/
├── tests/        # workspace 跨模块集成测试
├── go.work
└── Makefile
```

每个模块分别管理版本。在当前多模块仓库中发布时，标签需包含模块目录前缀，例如 `logger/v0.1.0`、`feishu/v0.1.0`；Feishu 的 user/card/attachment/events 子包随 feishu 模块一起发布。当前只推送源码，尚未创建发布标签。`go.work` 和本地 `replace` 不会替代已发布模块的依赖声明。

## 远程 issues 与待完善事项

以下是 2026-10-04 对远程三个未关闭 issues 与当前代码的核对。每个 issue 都包含多项需求，部分完成不代表整个 issue 已解决；代码提交与验收应继续按独立功能分批进行。

### 已完成或已有基础

| 需求 | 当前状态 |
| --- | --- |
| YAML 配置（#2） | 已支持 `.yaml/.yml`、格式选择、严格字段校验和时长字符串；多文件合并、embed、嵌套指针遍历仍未实现 |
| HTTP 标准状态错误及统一上报（#1/#2） | 已有 StatusCode、Method、独立 Headers、Retry-After 解析和 `apperror` 适配；受控 Body 预览尚未实现 |
| Retry-After 与跨模块组合（#1/#2） | 已支持服务端最短等待、GET 重试示例，以及 HTTP + retry + apperror + logger 集成测试 |
| 对象存储与下载（#3） | 已有通用 Backend、阿里云普通/分片上传、范围读取/分页/签名，以及供应商无关的单流下载、校验和原子发布；尚无其他供应商适配、目录批量上传和下载断点续传 |
| 飞书附件上传（#1） | 已有聊天、Drive 文件、文档素材上传；docx 文件块关联已实现，多维表记录附件字段回填仍需业务 API |
| 飞书长连接（#1） | 已有鉴权、心跳、重连、分片、消息/卡片/机器人入群事件和应答；跨实例去重与真实应用联调仍待补齐 |
| 自定义配置校验（#1） | 已支持顶层 `Validate()`；通用标签校验器和嵌套 Validator 遍历尚未实现 |

### 迁移接入需求：[#1 add new feat](https://github.com/bkcarlos/goparts/issues/1)

| 待完善项 | 缺口与边界 |
| --- | --- |
| Bitable / Wiki / Contact | 表、记录批量操作/搜索、字段 CRUD；Wiki 节点解析；按邮箱批量查询用户 ID。附件上传本身已在 `attachment` 中实现 |
| JSON 1.0 卡片 | 需要独立旧版构造器或明确范围的迁移工具；现有构造器是 JSON 2.0 |
| LLM Responses | 尚无 `/responses` 请求模型、previous_response_id、reasoning 与对应 SSE 事件解析 |
| 多用户身份与存储 | `user.Client` / `TokenStore` 按单账号使用；需多用户索引、会话选择及安全的刷新协调，不能把用户鉴权失败静默替换为应用身份 |
| HTTP 重试与诊断 | 读请求组合示例和 StatusError 元数据已补齐；受控 Body 预览待实现，写请求仍需显式判断幂等 |
| 配置、生命周期、组合错误 | 通用 URL/枚举/正则校验，任务独立退出预算/分阶段清理，`errors.Join` 全分支错误描述 |
| 稳定发布 | 多模块版本标签和可固定版本的接入示例 |

### 通用组件能力：[#2 add new feat2](https://github.com/bkcarlos/goparts/issues/2)

| 建议阶段 | 待完善项 |
| --- | --- |
| 优先补现有模块 | Retry-After 和 HTTP 错误透传已完成；剩余 `apperror` 的 HTTP/退出码映射与 `logger` 字段脱敏 |
| 服务基础组件 | 独立 `middleware`（RequestID、访问日志、Recover、Timeout、CORS、Bearer、BodyLimit），限流与熔断，有界任务池与按 key 顺序执行 |
| 缓存与多实例 | TTL/文件缓存、Load-or-fetch；飞书 TokenCache、刷新协调和回调去重。缓存接口本身不能代替跨进程刷新锁 |
| 配置增强 | embed 默认层、多文件合并/注册表、嵌套指针和嵌套 Validate；YAML 加载已经完成 |
| LLM 应用层 | 会话裁剪、工具注册与校验/调度、流式文本聚合与节流、卡片 Sequence 分配及串行更新 |
| 可观测性 | metrics 接口与可选适配器、HTTP 请求/响应钩子、受控 trace 输出；日志/trace 需配合脱敏 |
| 使用便利性 | 可注入重试随机源、用户登录轮询回调、结构化错误属性；保持现有 Go 1.21 基线与错误接口兼容 |
| 工程建设 | LICENSE、CI 模块矩阵和漏洞检查、CHANGELOG / CONTRIBUTING / 版本标签；HTTP + retry + apperror + logger、storage + download 组合测试已增加 |

### 下游 common 替换：[#3 add new feat3](https://github.com/bkcarlos/goparts/issues/3)

该 issue 的重点是制品和部署能力，与当前服务组件的定位不同。对象存储已按供应商无关接口落地，下载也已独立封装。仍缺少：

- **存储与发布**：对象存储目录批量操作/其他供应商适配、版本发布/更新、制品库客户端；阿里云分片与签名已支持。
- **连接与文件传输**：SSH/SFTP、多主机和连接池、下载 Range 分片调度与断点续传；单流下载和 OSS 范围读取已支持。
- **通用数据与文件工具**：持久化缓存、并发安全/有序 Map、文件树过滤、压缩/格式化/跨平台磁盘空间等工具。
- **旧 API 兼容**：logger 的全局函数/旧接口/文件输出，retry 的函数式入口与预设，errx Registry/guidance 到 apperror 的迁移层。

建议先保留下游的制品/部署包，按调用方实际需要逐项迁移；不应为了同名替换直接将所有业务专用 API 加入通用模块。旧 API 的兼容范围需要结合下游源码确认。

下一批建议补 **Bitable / Wiki / Contact**、**LLM Responses** 和 **日志脱敏/错误码映射**；每批包含实现、针对性测试、README 和独立提交。对象存储当前按需求只实现阿里云，其他供应商通过同一 Backend 接口扩展。上述待办不替代真实服务端的联调验收。

## 常见问题

- **按需引入**：只引入所需 module；Feishu 子包共享一个 module，其余模块彼此独立。
- **重试行为**：HTTP、LLM 和飞书不提供应用层自动重试。需要时组合 `retry`，并显式配置 `RetryIf`。发消息、创建文档、模型生成等操作超时后可能已被执行，应由业务判断是否可以重复。
- **通知出口**：`apperror` 只调用你注入的 Sink，不自动发飞书消息。连接飞书或监控平台由业务决定。
- **当前边界**：尚未实现配置热更新、文件日志轮转和 LLM 会话存储。配置加载返回业务结构体；日志 Writer 由业务管理；LLM 对话历史由调用方维护。

后续可按实际需要增加 `middleware`、`ratelimit` 和有界并发任务模块，继续保持独立配置与依赖。
