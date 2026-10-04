# goparts

面向 Go 服务的公共组件集合：日志、配置、HTTP、重试、生命周期、LLM、飞书、统一错误处理、对象存储与下载。**每个模块有独立的 `go.mod`，按需引用、分别初始化**，不需要创建全局 SDK 实例。

基础模块要求 **Go 1.21+**；独立 `ssh` 和可选 `cache/redis` 模块要求 **Go 1.26+**，以使用当前依赖版本。完整仓库开发需要 Go 1.26+，建议使用受支持版本的最新补丁。共有 23 个独立模块，生产组件通过结构接口和回调组合，不需要统一 SDK 实例。`go.work` 仅用于本仓库开发。

第三方依赖按功能隔离：config 的 YAML、feishu 的 WebSocket/Protobuf/PBKDF2、llm/chat 的 JSON Schema、storage/aliyun 的 OSS SDK、metrics/otel 的 OTel、ssh 的 SSH/SFTP、cache/redis 的 Redis 客户端。具体版本见各模块 go.mod。

仓库：[bkcarlos/goparts](https://github.com/bkcarlos/goparts)，采用 [MIT](LICENSE)。模块发布使用 `<module>/v0.1.0` 标签；安装时写 `github.com/bkcarlos/goparts/<module>@v0.1.0`。发布状态与验收见 [FEATURES.md](FEATURES.md)。

## 导航

- [模块选型](#模块选型)
- [按场景组合](#按场景组合)
- [快速开始](#快速开始)
- [接入业务项目](#接入业务项目)
- [安装与版本管理](#安装与版本管理)
- [配置与默认值](#配置与默认值)
- [统一错误与上报](#统一错误与上报)
- [飞书与 LLM 组合](#飞书与-llm-组合)
- [对象存储与通用下载](#对象存储与通用下载)
- [并发与资源管理](#并发与资源管理)
- [示例入口](#示例入口)
- [开发与验证](#开发与验证)
- [目录与发布](#目录与发布)
- [远程 issues 与验收范围](#远程-issues-与验收范围)
- [常见问题](#常见问题)

## 模块选型

| 需要的能力 | 导入路径 | 主要入口 | 接入文档 |
| --- | --- | --- | --- |
| JSON / 文本日志、动态级别、请求字段 | `github.com/bkcarlos/goparts/logger` | `New`、`WithContext`、`WithFields` | [logger](logger/README.md) |
| 默认值、JSON / YAML 文件、环境变量、启动校验 | `github.com/bkcarlos/goparts/config` | `Load[T]` | [config](config/README.md) |
| HTTP / JSON 请求、连接复用、响应大小限制 | `github.com/bkcarlos/goparts/httpclient` | `New`、`Do`、`DoJSON` | [httpclient](httpclient/README.md) |
| 指数退避、抖动、按错误判断是否重试 | `github.com/bkcarlos/goparts/retry` | `New`、`Do` | [retry](retry/README.md) |
| 后台任务、退出信号、逆序资源清理 | `github.com/bkcarlos/goparts/lifecycle` | `New`、`Add`、`OnStop`、`RunSignals` | [lifecycle](lifecycle/README.md) |
| 兼容协议对话、SSE、工具调用数据、Embedding | `github.com/bkcarlos/goparts/llm` | `Chat`、`Responses`、`ResponsesStream`、`chat` | [llm](llm/README.md) |
| 群自定义机器人 Webhook 通知 | `github.com/bkcarlos/goparts/feishu` | `New`、`SendText`、`SendMarkdown`、`Send` | [feishu](feishu/README.md) |
| 用户授权登录、会话刷新、docx 读写 | `github.com/bkcarlos/goparts/feishu/user` | `New`、`StartLogin`、`CompleteLogin` | [用户身份](feishu/user/README.md) |
| 卡片构建、发送、回复、更新、按钮回调 | `github.com/bkcarlos/goparts/feishu/card` | `NewCard`、`New`、`Send`、`UpdateText` | [卡片](feishu/card/README.md) |
| 聊天附件、云空间文件、文档素材上传 | `github.com/bkcarlos/goparts/feishu/attachment` | `New`、`UploadChatPath`、`UploadDrivePath`、`UploadMediaPath` | [附件](feishu/attachment/README.md) |
| 长连接消息、卡片和机器人入群事件 | `github.com/bkcarlos/goparts/feishu/events` | `NewDispatcher`、`New`、`Run` | [长连接](feishu/events/README.md) |
| 多维表格记录、表与字段管理 | `github.com/bkcarlos/goparts/feishu/bitable` | `ListTables`、`Search`、`BatchCreate`、`BatchUpdate` | [飞书业务 API](feishu/README.md#业务-api-与身份) |
| Wiki 链接解析、通讯录 ID 查询 | `github.com/bkcarlos/goparts/feishu/wiki`、`contact` | `ResolveBitable`、`BatchGetID` | [飞书业务 API](feishu/README.md#业务-api-与身份) |
| 编码错误、错误链、业务扩展、统一上报 | `github.com/bkcarlos/goparts/apperror` | `New`、`Wrap`、`Describe`、`NewReporter` | [apperror](apperror/README.md) |
| 通用对象存储、阿里云 OSS 适配 | `github.com/bkcarlos/goparts/storage` | `New`、`PutFile`、`Get`、`List`、`PresignGet` | [storage](storage/README.md) |
| HTTP / 任意数据源流式下载与校验 | `github.com/bkcarlos/goparts/download` | `Fetch`、`FetchRanges`、`RangeSource` | [download](download/README.md) |
| 请求 ID、访问日志、认证、请求体限制 | `github.com/bkcarlos/goparts/middleware` | `Chain`、`RequestID`、`AuthBearer` | [middleware](middleware/README.md) |
| 令牌桶、三态熔断 | `github.com/bkcarlos/goparts/ratelimit` | `New`、`NewCircuitBreaker` | [ratelimit](ratelimit/README.md) |
| 有界队列、按 key 保序并发 | `github.com/bkcarlos/goparts/workerpool` | `New`、`NewStream[K]` | [workerpool](workerpool/README.md) |
| TTL/文件缓存、合并并发 miss | `github.com/bkcarlos/goparts/cache` | `NewMemory`、`NewFile`、`Loader` | [cache](cache/README.md) |
| 可选 Redis 适配器 | `github.com/bkcarlos/goparts/cache/redis` | `New` | [Redis](cache/redis/README.md) |
| 持久化最后更新时间 | `github.com/bkcarlos/goparts/persistcache` | `New[K]`、`ShouldUpdate` | [persistcache](persistcache/README.md) |
| Counter/Histogram/Gauge 与 OTel | `github.com/bkcarlos/goparts/metrics` | `Nop`、`otel` 子包 | [metrics](metrics/README.md) |
| 线程安全普通/有序 Map | `github.com/bkcarlos/goparts/safemap` | `New`、`NewOrdered` | [safemap](safemap/README.md) |
| 文件树与过滤器组合 | `github.com/bkcarlos/goparts/filetree` | `GetFileTree`、`Combine` | [filetree](filetree/README.md) |
| 格式化、归档、磁盘空间 | `github.com/bkcarlos/goparts/utils` | `CreateZip`、`diskspace` | [utils](utils/README.md) |
| SSH/SFTP、多主机与池 | `github.com/bkcarlos/goparts/ssh` | `NewSSHClient`、`NewPool` | [ssh](ssh/README.md) |
| 版本发布、下载、更新和回滚 | `github.com/bkcarlos/goparts/version` | `Publisher`、`Updater`、`Rollback` | [version](version/README.md) |
| 通用制品目录与校验下载 | `github.com/bkcarlos/goparts/artifact` | `PackageManager`、`NewHTTPBackend` | [artifact](artifact/README.md) |

`feishu/user`、`feishu/card`、`feishu/attachment` 和 `feishu/events` 是同一个 Feishu module 下的独立子包，使用各自的配置和客户端。模块间通过参数、回调和小接口组合：例如业务加载配置后传给客户端，把 LLM 流交给卡片接口，把错误记录交给日志或通知出口。

## 按场景组合

根据当前需求安装对应模块；不必一次引入全部 23 个模块。

| 应用场景 | 建议起步模块 | 后续按需加入 |
| --- | --- | --- |
| HTTP 服务 | `config` + `logger` + `middleware` + `apperror` | `lifecycle` 管理退出，`metrics` 接业务监控 |
| 外部 API 调用 | `httpclient` | `retry` 处理可重复请求，`ratelimit` 控制本地请求速率与熔断 |
| 飞书通知工具 | `feishu` 根包 | `apperror` 统一告警字段，`ratelimit` 避免集中发送 |
| 飞书机器人 | `feishu/events` + `feishu/card` | `feishu/user` 操作文档，`attachment` 上传附件，`bitable` 操作表格 |
| AI 对话机器人 | `llm` + `llm/chat` | `workerpool` 控制并发、按卡片保序，`feishu/card` 展示结果 |
| 定时同步 / CLI | `config` + `persistcache` + `filetree` | `cache` 缓存结果，`utils` 归档和检查磁盘空间 |
| 文件和制品分发 | `storage` + `download` | `version` 管理版本清单，`artifact` 对接已有制品目录，`ssh` 部署到主机 |

`cache` 保存业务值及 TTL；`persistcache` 只保存“某个 key 最后更新的时间”，适合判断是否要重新同步。
`version` 管理你发布的版本和更新流程；`artifact` 查询已有制品平台；两者不是同一个发布协议。
`ratelimit` 是显式调用的限流器/熔断器，`middleware` 不会自动替接口应用这些策略。

## 快速开始

如果要先体验组件，可克隆仓库（完整 workspace 需要 Go 1.26+）：

```sh
git clone https://github.com/bkcarlos/goparts.git
cd goparts
```

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
go get github.com/bkcarlos/goparts/logger@v0.1.0
go get github.com/bkcarlos/goparts/apperror@v0.1.0
```

创建 `main.go`（完整示例，仅写本地日志）：

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

## 安装与版本管理

仓库根目录没有 `go.mod`，安装单位是独立模块。例如在新项目中：

```sh
mkdir my-service
cd my-service
go mod init example.com/my-service
go get github.com/bkcarlos/goparts/logger@v0.1.0
go get github.com/bkcarlos/goparts/feishu@v0.1.0
go get github.com/bkcarlos/goparts/llm@v0.1.0
```

安装完成后，`go.mod` 记录依赖版本，`go.sum` 记录校验信息，建议一并提交。
代码中的 import 不携带版本号。已有业务项目无需再次执行 `go mod init`。

| 使用内容 | 安装的 module | 对应 Git 标签 |
| --- | --- | --- |
| `logger` | `github.com/bkcarlos/goparts/logger@v0.1.0` | `logger/v0.1.0` |
| `feishu/card`、`user`、`events`、`bitable` 等 | `github.com/bkcarlos/goparts/feishu@v0.1.0` | `feishu/v0.1.0` |
| `storage/aliyun` | `github.com/bkcarlos/goparts/storage@v0.1.0` | `storage/v0.1.0` |
| `llm/chat` | `github.com/bkcarlos/goparts/llm@v0.1.0` | `llm/v0.1.0` |
| 可选 Redis 缓存 | `github.com/bkcarlos/goparts/cache/redis@v0.1.0` | `cache/redis/v0.1.0` |

查看版本与升级：

```sh
go list -m -versions github.com/bkcarlos/goparts/logger
go get github.com/bkcarlos/goparts/logger@latest
go mod tidy
```

`@latest` 选择已发布版本，不表示主分支上的所有改动。生产项目可明确指定 `@v0.1.0`；
需要尚未打标签的提交时可使用 `@<commit>`，Go 会解析为伪版本。已发布标签不会被移动。
各模块独立升级，不要求一起改版本。发布者的标签操作见 [CONTRIBUTING.md](CONTRIBUTING.md)。

### 修改组件时如何本地联调

在业务项目的 `go.mod` 中保留正常的 `require` 版本，临时增加：

```go
replace github.com/bkcarlos/goparts/logger => /absolute/path/to/goparts/logger
replace github.com/bkcarlos/goparts/apperror => /absolute/path/to/goparts/apperror
```

替换目标是具体模块目录，而不是仓库根目录。开发完成移除本地绝对路径，再用发布版本验证。
另一种方式是在你自己的开发目录建立 workspace，把业务 module 和所需组件 module 都加入 `use`；
仓库自带 `go.work` 只管理这 23 个模块，不会自动覆盖仓库外的业务项目。

## 配置与默认值

客户端配置由调用方传入；各组件不会自动加载业务配置文件。`config` 模块提供可选的加载流程：**default 标签 → embed 默认文件 → 多个 JSON/YAML 文件 → env → 嵌套及顶层校验**。`.yaml` / `.yml` 自动使用 YAML，也可显式传入 `FormatYAML`；YAML 时长可写为 `5s`。读取环境变量的示例程序和组件本身是分开的。

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

固定上限也需与可配置默认值区分：当前配置文件上限为 1 MiB；飞书 Webhook 请求上限为 20 KiB、响应上限为 1 MiB。用户 API 和卡片 API 的响应上限现已可配置，默认分别为 8 MiB 和 2 MiB。

### YAML 文件和环境变量示例

先安装 `config` 和 `httpclient` 模块。以下完整函数展示如何把业务配置传给客户端，
可保存为业务包中的 Go 文件。`config` 不会自动替你创建其他组件：

```go
package example

import (
    "errors"
    "time"

    "github.com/bkcarlos/goparts/config"
    "github.com/bkcarlos/goparts/httpclient"
)

type AppConfig struct {
    Timeout time.Duration `yaml:"timeout" json:"timeout" default:"10s" env:"HTTP_TIMEOUT"`
    MaxResponseBytes int64 `yaml:"max_response_bytes" json:"max_response_bytes" default:"4194304" env:"HTTP_MAX_RESPONSE_BYTES"`
}

func (c *AppConfig) Validate() error {
    if c.Timeout <= 0 || c.MaxResponseBytes <= 0 {
        return errors.New("HTTP timeout and response size must be positive")
    }
    return nil
}

func NewHTTPFromFile(path string) (*httpclient.Client, error) {
    cfg, err := config.Load[AppConfig](config.Options{
        File: path,
        EnvPrefix: "APP_",
    })
    if err != nil { return nil, err }
    return httpclient.New(httpclient.Config{
        Timeout: cfg.Timeout,
        MaxResponseBytes: cfg.MaxResponseBytes,
    })
}
```

`config.yaml`：

```yaml
timeout: 5s
max_response_bytes: 16777216
```

调用 `NewHTTPFromFile("config.yaml")` 会使用 5 秒和 16 MiB；若启动前设置
`APP_HTTP_TIMEOUT=8s`，最终超时为 8 秒。显式指定不存在的文件会报错；无需文件时传空路径。
多个环境文件可以使用 `Options.Files`，后者覆盖前者，再由 `File` 和环境变量覆盖。
加载和校验失败时不返回部分配置，应在启动阶段处理。该模块没有后台监视或自动热更新。

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

`Capture` 的返回值表示**上报是否失败**，不替代原始业务错误。Reporter 同步调用注入的 Sink，不自动重试、去重或启动后台队列。默认记录包含 `code`、`message`、`fields` 和可选结构化 `attrs`；完整 `err.Error()` 仅在开启 `IncludeDetail` 后写入 `detail`。

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

LLM 模块可配置兼容协议的 BaseURL 和模型名；对接 DeepSeek、通义等兼容端点时，具体模型能力仍由服务商决定。可使用 `llm/chat.Accumulator` 聚合流，再通过 `workerpool.Stream` 串行调用卡片 `UpdateText`。同一卡片的更新需串行执行、共用递增 Sequence，最后调用 `Finish` 关闭流式模式。

接入步骤见 [用户登录与文档](feishu/user/README.md)、[卡片与流式更新](feishu/card/README.md)、[LLM 对话与流式回调](llm/README.md)。这些能力已通过本地模拟服务验证，尚未完成真实飞书应用和模型服务商的端到端联调。

## 对象存储与通用下载

上传通过 `storage.Client` 统一处理对象键、BasePath 和错误；具体签名和网络协议由
`storage.Backend` 实现。当前提供 `storage/aliyun`，S3、MinIO、COS 适配器尚未提供。
下载器只消费数据流，因此替换存储供应商时，下面的下载代码无需改变：

```go
package example

import (
    "context"

    "github.com/bkcarlos/goparts/download"
    "github.com/bkcarlos/goparts/storage"
)

func DownloadObject(ctx context.Context, objects *storage.Client,
    key, destination, expectedSHA256 string) (download.Result, error) {
    source := download.SourceFunc(func(ctx context.Context) (download.Stream, error) {
        reader, err := objects.Get(ctx, key, storage.GetOptions{})
        if err != nil { return download.Stream{}, err }
        return download.Stream{Body: reader, Size: reader.Length}, nil
    })
    client, err := download.New(download.Config{MaxBytes: 2 << 30})
    if err != nil { return download.Result{}, err }
    return client.Fetch(ctx, source, destination, download.Options{
        SHA256: expectedSHA256,
    })
}
```

`Fetch` 负责关闭读取流、校验长度、校验 SHA-256 并发布文件；目标父目录须存在，默认拒绝覆盖。
预期 SHA-256 为空时只计算摘要，不与可信值比对。不要把 OSS ETag 当作 SHA-256。
`SourceFunc` 每次调用都应重新打开数据流，不能复用已经读完或关闭的 Reader。

需要并行和断点续传时使用 `FetchRanges`：HTTPSource 已实现 `RangeSource`；自定义供应商
需要额外实现 `Probe` 和 `OpenRange`。上面的单流 `SourceFunc` 本身不具备范围读取能力。
`Auto` 模式在服务不支持范围或缺乏版本绑定时退回单流，`Parallel` 模式则报错。
续传状态位于 `<destination>.goparts-part/`；源版本变化时拒绝混合分片。
详情见 [对象存储](storage/README.md)、[下载与续传](download/README.md)。

## 并发与资源管理

组件不会统一接管应用进程。初始化、资源所有权和退出顺序由应用管理：

| 资源 | 使用约定 | 退出动作 |
| --- | --- | --- |
| HTTP / LLM Client | 创建后复用；请求携带 Context | 可调用 `CloseIdleConnections()` 释放空闲连接 |
| `storage.Get` 返回的 Reader | 保持 Context 有效直到读取完成 | 直接读取时必须 `Close()`；交给 download 后由下载器关闭 |
| `workerpool.Pool` / `Stream` | Submit 成功仅表示接纳，任务错误交给 observer | 先 `Close()` 停止接纳，再 `Wait(ctx)` 等待排空 |
| `chat.Accumulator` | Flush 回调串行调用，收到完整累计文本 | `Close()` 刷出尾部并停止计时器，检查返回错误 |
| `persistcache.PersistentCache` | 写入后台合并保存，`LastError()` 查看异步失败 | `Close()` 等待保存并执行最后一次落盘 |
| 日志文件 Writer | 由创建者持有；slog 不替调用方关闭 | 最后一条日志完成后 `Close()` |
| Redis 客户端 / SSH Client / SSH Pool | 显式创建和复用 | 由创建者 `Close()`；SSH 租用连接必须 release |
| 飞书长连接 | `Run(ctx)` 长时间运行 | 取消 Context 并等待 Run 返回 |

应用可用 `lifecycle` 管理这些步骤：停止接收请求 → 取消或排空任务 → 逆序释放依赖。
不要在等待工作池排空前关闭任务仍需要的连接或日志文件。已经取消的请求 Context 不适合直接
用作清理预算，可为退出创建独立、有限时长的 Context。

对于流式卡片，顺序应为：结束模型读取 → 关闭 Accumulator → 等待已接纳的卡片更新完成 →
发送 `Finish`。同一卡片用同一个队列 key，Sequence 在实际执行更新的任务内分配。
`Submit` 返回 nil 不代表卡片更新成功；observer 必须收集执行错误，失败时不要直接宣告生成完成。
参考 [工作池](workerpool/README.md)、[生命周期](lifecycle/README.md) 和
[流式组合测试](tests/stream_card_test.go)。

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
make stress       # 关键并发测试重复 20 轮，包含竞态检测
make fuzz         # Range 解析、LLM 工具参数各执行 10 秒模糊测试
make vuln         # 需先安装 govulncheck；逐模块漏洞检查
```

`-race` 需要当前平台支持竞态检测，并具备相应 CGO / C 编译工具链。`make integration` 需要启用本仓库的 `go.work`；若环境中设置了 `GOWORK=off`，运行前取消该覆盖或显式指定此文件。
`make stress` 同样使用 workspace；可执行 `make stress STRESS_COUNT=100` 或
`make fuzz FUZZTIME=1m` 延长检查。CI 另有 robustness 任务执行默认重复测试与模糊测试。

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
├── logger/       # 各独立模块有 go.mod、README、实现与测试
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
├── middleware/ ratelimit/ workerpool/ cache/ metrics/
├── persistcache/ safemap/ filetree/ utils/
├── ssh/ version/ artifact/
├── tests/        # workspace 跨模块集成测试
├── go.work
└── Makefile
```

每个模块分别管理版本。在当前多模块仓库中发布时，标签需包含模块目录前缀，例如 `logger/v0.1.0`、`feishu/v0.1.0`；Feishu 的 user/card/attachment/events 子包随 feishu 模块一起发布。发布流程、最低 Go 版本与标签规则见 [CONTRIBUTING.md](CONTRIBUTING.md)。`go.work` 和本地 `replace` 不会替代已发布模块的依赖声明。

## 远程 issues 与验收范围

完整对照见 [FEATURES.md](FEATURES.md)，版本记录见 [CHANGELOG.md](CHANGELOG.md)。
本轮覆盖 issues #1/#2/#3 中通用能力，以下范围按用户确认处理：

- 对象存储首先提供阿里云，其他供应商实现同一个 Backend / RangeSource 接口。
- 无法访问 BOS 协议与 toolkits 源码，跳过 BOS 专有协议及旧 API 的精确兼容；artifact 提供明确协议的通用客户端，logger/retry/apperror 提供通用便捷入口。
- 测试使用本地模拟 HTTP、WebSocket、SSH/SFTP、Redis 服务，不代表真实飞书、OSS、模型服务商或生产主机已联调。
- 分布式锁/去重可注入；缓存接口与进程内测试不等于任意多实例部署已验收。

## 常见问题

| 现象 | 原因和处理方式 |
| --- | --- |
| `go get github.com/bkcarlos/goparts` 找不到根模块 | 根目录没有 go.mod；安装 `.../logger`、`.../feishu` 等具体模块 |
| `unknown revision v0.1.x` | 确认模块前缀标签已发布；用 `go list -m -versions` 查看，不能仅依据主分支 README 推断版本 |
| 根目录 `go test ./...` 失败 | 根目录是 workspace；执行 `make check`，或进入具体模块后 `GOWORK=off go test ./...` |
| 只安装某模块，却提示 Go 版本不够 | `ssh`、`cache/redis` 要求 Go 1.26+；本仓库 go.work 也要求 Go 1.26+ |
| YAML/env 改了但客户端没有变化 | config 只在调用 Load 时读取，客户端接收初始化参数；重新加载后由业务决定重建实例 |
| 响应超过默认大小限制 | 调整对应 Config 的 MaxResponseBytes；LLM SSE 另由 MaxEventBytes 限制，存储对象流不等于 API JSON 响应 |
| LLM BaseURL 返回 404 | BaseURL 应为兼容 API 根路径，不要重复附加 `/chat/completions`；Responses 需服务端独立支持 |
| 流已显示部分内容，但最终返回错误 | 流式输出不是事务；Chat 需收到 `[DONE]`，Responses 需完成事件，提前断流应标记为未完成 |
| Webhook 能发通知，但不能读文档 | 文档需应用配置、用户授权和资源访问权限；Webhook token 不能代替用户 AccessToken |
| 飞书长连接收不到事件 | 检查开发者后台的事件订阅、应用权限和发布配置；连接成功不代表所有事件都已订阅 |
| 调用超时后是否可以直接重试 | HTTP/LLM/飞书不做应用层自动重试；写入可能已执行，先判断幂等性，retry 也需显式配置 RetryIf |
| Reporter.Capture 返回 nil，但业务仍失败 | 返回值只表示上报成功；业务必须继续返回或处理原始错误 |
| 工作池 Submit 成功但任务失败 | Submit 只确认接纳；检查 observer 中的 Event.Err，并在退出时等待 Wait |
| 下载提示目标已存在 | 默认禁止覆盖；确认业务允许后传 Overwrite，校验失败仍保留原文件 |
| 续传提示版本变化或状态锁定 | 不要混用旧分片；锁文件仅在确认没有存活下载进程后移除，必要时清理旧续传目录重下 |

`apperror` 只调用注入的 Sink，不自动发送飞书消息；通知出口由业务显式连接。
客户端中的可配置上限用于控制资源使用，不能代替服务商的接口限制。
本地模拟与 CI 测试的具体范围、覆盖率和未验证部分见 [VALIDATION.md](VALIDATION.md)。
