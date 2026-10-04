# goparts：面向 LLM 的能力索引与接入指南

用于让 AI 编程助手发现已有封装、定位当前版本的 API，并生成可编译的业务代码。
这是显式提供给模型的项目文档，不是 go get 插件，也不是所有 AI 工具都会自动加载的规则文件。

## 先确定任务与版本

1. 用下方能力索引选最小的一组模块，不默认安装整个仓库。
2. 在使用方项目查明已选择的 module 版本、源码目录和 replace。
3. 先读对应模块的 AI_GUIDE.md，再读相关 README、公开 API 与一个匹配场景的示例。
4. 生成代码前核对构造参数、返回值、身份、错误和资源所有权。
5. 在使用方项目编译和测试；只在实际执行过验证时报告通过。

仓库包含 **23 个独立 module**，根目录没有 go.mod。普通模块最低 Go 1.21，ssh 和 cache/redis
最低 Go 1.26；整个仓库的 go.work 要求 Go 1.26。每个模块独立初始化，没有 `goparts.New()` 统一入口。

## 在使用方项目读取安装版本

以 logger 为例，以下命令用于查阅，不会升级依赖：

```sh
go list -m -json github.com/bkcarlos/goparts/logger
go list -m -f '{{.Dir}}' github.com/bkcarlos/goparts/logger
go doc github.com/bkcarlos/goparts/logger
go doc github.com/bkcarlos/goparts/logger.New
```

查看 JSON 中的 Path、Version、Dir、Replace。Dir 是当前实际模块源码位置，可能来自 workspace、
本地 replace 或 module cache；不要手工拼接缓存路径。若尚未下载、命令失败或 Dir 为空，先按项目
允许的方式解析/下载已选版本；不要为了取得文档自动把依赖更新成 latest。

在 Dir 中读取 `AI_GUIDE.md`、`README.md`、go.mod 和相关 .go 文件。旧版本没有 AI_GUIDE.md 时，
退回其本地 README、GoDoc、源码和测试，不以主分支指南替代旧版签名。
本次指南在 v0.1.0 之后加入，已发布的 v0.1.0 标签不会因此变化。

模块 ZIP 以具体 module 目录为边界，根仓库文档不会自动包含进每个子模块，嵌套 module 也独立分发。
因此每个 module 内都有短指南；**只安装 logger 不会获得其他 22 个模块的源码或根目录总索引**。
规则参考 [Go module ZIP 文件](https://go.dev/ref/mod#zip-files)。

版本标签使用 `<module>/vX.Y.Z`，安装参数是 `github.com/bkcarlos/goparts/<module>@vX.Y.Z`。
可通过 `go list -m -versions <module-path>` 检查可用版本，不编造尚未发布的版本号。
子包共享父模块版本，例如 feishu/card 随 feishu 发布；cache/redis 则有独立 go.mod 和标签。

## 按能力查找 module

下表 module 名统一加前缀 `github.com/bkcarlos/goparts/`。链接指向模块内部指南，后续只读取与任务相关的文档。

| 需求 | Module | 能力入口 |
| --- | --- | --- |
| 结构化日志 | [logger](logger/AI_GUIDE.md) | `New`、`Config`、`NewRotatingWriter` |
| 配置加载 | [config](config/AI_GUIDE.md) | `Load`、`Options`、`LoadRegistry` |
| HTTP 与 JSON | [httpclient](httpclient/AI_GUIDE.md) | `New`、`Client.Do`、`Client.DoJSON` |
| 可控重试 | [retry](retry/AI_GUIDE.md) | `New`、`Retrier.Do`、`Config` |
| 启动与退出管理 | [lifecycle](lifecycle/AI_GUIDE.md) | `New`、`Manager.Add`、`Manager.OnQuiesce` |
| 编码错误与上报 | [apperror](apperror/AI_GUIDE.md) | `New`、`Base.Wrap`、`NewReporter` |
| 飞书通知与应用操作 | [feishu](feishu/AI_GUIDE.md) | `New`、`Client.SendText` |
| 兼容模型接口 | [llm](llm/AI_GUIDE.md) | `New`、`Client.Chat`、`Client.ChatStream` |
| 通用对象存储 | [storage](storage/AI_GUIDE.md) | `New`、`Backend`、`Client.Get` |
| 供应商无关下载 | [download](download/AI_GUIDE.md) | `New`、`Client.Fetch`、`Client.FetchRanges` |
| HTTP 中间件 | [middleware](middleware/AI_GUIDE.md) | `Chain`、`RequestID`、`BodyLimit` |
| 限流与熔断 | [ratelimit](ratelimit/AI_GUIDE.md) | `New`、`Bucket.Wait`、`NewCircuitBreaker` |
| 任务池与按 key 保序 | [workerpool](workerpool/AI_GUIDE.md) | `New`、`Pool.Submit`、`NewStream` |
| 业务值缓存 | [cache](cache/AI_GUIDE.md) | `NewMemory`、`NewFile`、`Store` |
| Redis 缓存适配 | [cache/redis](cache/redis/AI_GUIDE.md) | `New`、`Store.Get`、`Store.Set` |
| 更新时间持久化 | [persistcache](persistcache/AI_GUIDE.md) | `New`、`Serializer`、`PersistentCache.ShouldUpdate` |
| 指标接口与 OTel | [metrics](metrics/AI_GUIDE.md) | `Counter`、`Histogram`、`Gauge` |
| 并发 Map | [safemap](safemap/AI_GUIDE.md) | `New`、`NewOrdered`、`SafeMap.Snapshot` |
| 文件树与过滤 | [filetree](filetree/AI_GUIDE.md) | `GetFileTree`、`GetFileTreeFilesOnly`、`FilterByPattern` |
| 归档与小工具 | [utils](utils/AI_GUIDE.md) | `FormatBytes`、`GetEnvOrDefault`、`CreateZip` |
| SSH/SFTP | [ssh](ssh/AI_GUIDE.md) | `NewSSHClient`、`SSHConfig`、`Client.ExecuteCommand` |
| 应用版本分发 | [version](version/AI_GUIDE.md) | `StorageProvider`、`Publisher.UploadRelease`、`Updater.Update` |
| 已有制品目录 | [artifact](artifact/AI_GUIDE.md) | `NewHTTPBackend`、`PackageManager`、`Backend` |

## 同模块子包的选择

| 需求 | 导入包后缀 | 安装 module | 接入文档 / 源码 |
| --- | --- | --- | --- |
| 群 Webhook 通知 | feishu | feishu | [Webhook](feishu/README.md) |
| 用户登录、刷新、docx | feishu/user | feishu | [用户身份](feishu/user/README.md) |
| 应用机器人卡片及更新 | feishu/card | feishu | [卡片](feishu/card/README.md) |
| JSON 1.0 卡片构建 | feishu/card/legacy | feishu | [构建器](feishu/card/legacy/card.go) |
| 聊天、云空间、文档素材上传 | feishu/attachment | feishu | [附件](feishu/attachment/README.md) |
| WebSocket 长连接与事件应答 | feishu/events | feishu | [事件](feishu/events/README.md) |
| 表、记录、字段 | feishu/bitable | feishu | [业务 API](feishu/README.md#业务-api-与身份)、[源码](feishu/bitable/client.go) |
| Wiki token 解析 | feishu/wiki | feishu | [源码](feishu/wiki/client.go) |
| 邮箱/手机查询用户 ID | feishu/contact | feishu | [源码](feishu/contact/client.go) |
| 成功事件去重与响应重放 | feishu/dedup | feishu | [接口和内存实现](feishu/dedup/dedup.go) |
| 会话裁剪、工具校验、流式聚合 | llm/chat | llm | [应用层指南](llm/chat/README.md) |
| 阿里云 OSS 适配 | storage/aliyun | storage | [配置与契约](storage/README.md) |
| OTel 指标适配 | metrics/otel | metrics | [Meter 注入](metrics/README.md) |
| 本地磁盘空间 | utils/diskspace | utils | [可用空间](utils/README.md) |

例如需要查用户登录签名，使用 `go doc github.com/bkcarlos/goparts/feishu/user.Client.StartLogin`；
不要根据其他 SDK 的相似方法猜测本库的调用参数。

## 常见组合路线

### HTTP 服务

config.Load 读取并校验业务配置 → logger.New → middleware.Chain → 业务 handler。
业务错误使用 apperror 分类，在应用边界通过注入的 Sink 上报；生命周期由 lifecycle 管理。

HTTP 出站调用使用 httpclient；有明确可重试错误和幂等性依据时再组合 retry。
ratelimit 不会自动接管请求，需要在调用前 Wait/Allow 或用 CircuitBreaker.Do 包裹。
参考 [HTTP 重试组合](tests/http_retry_test.go)、[统一错误上报组合](tests/error_reporting_test.go)。
这些跨模块测试只在完整仓库内提供，不随单个 module 下载。

### 飞书 + LLM 流式卡片

1. feishu/events 接收订阅事件；HTTP 回调则使用 card 提供的解码校验能力。
2. 显式选择应用或用户身份，不拿 Webhook URL 当 AccessToken。
3. llm.Client.ChatStream 接收增量；需要全文聚合时交给 chat.Accumulator。
4. 同一卡片用 workerpool.Stream 的同一个 key 串行更新；实际执行任务时分配 Sequence。
5. 结束模型读取后 Close Accumulator，再排空更新队列，确认任务无错误后调用 card.Client.Finish。

Submit 成功不等于卡片发送成功，observer 中的错误必须被应用收集。
卡片全文更新、增量模型文本、cardID、messageID、elementID 不能混用。
参考 [流式会话函数](llm/chat/README.md)、[卡片接口](feishu/card/README.md)、[保序组合测试](tests/stream_card_test.go)。

### 对象存储下载与发布

storage/aliyun 创建 Backend → storage.New → 用 SourceFunc 把 Get 流交给 download.Fetch。
更换供应商时替换 Backend；下载器无需导入云 SDK。

需要断点续传时实现 RangeSource，或直接用已实现范围读取的 HTTPSource；普通 SourceFunc 不能直接当 RangeSource。
发布版本可用 version.StorageAdapter 连接同一 storage.Client；已有制品平台则实现 artifact.Backend。
参考 [通用下载](download/README.md)、[存储下载组合](tests/storage_download_test.go)、[版本存储组合](tests/version_storage_test.go)。

## 生成代码时保留这些约定

| 容易混淆的点 | 正确行为 |
| --- | --- |
| README 的 main 版本与已安装版本 | 以实际 module 的源码为接口依据；新文档可能描述尚未安装的能力 |
| 返回 error | 先判断再解引用响应，不忽略构造、写入、关闭或排空错误 |
| Reporter.Capture | 返回的是上报结果，仍需处理/返回原始业务错误 |
| 配置零值 | 按各构造器文档解释；“零用默认”不意味着任意零值配置都合法 |
| 超时与重试 | Context 取消并不证明服务端未执行；写入/模型请求不盲目重试 |
| 读取流 | 谁接收所有权谁关闭；storage.Get 直接用时由业务 Close，交给 download 后由下载器 Close |
| Context 生命周期 | 流读取结束前保持有效；清理与排空使用独立有限预算 |
| callback / observer | 按模块约定及时返回并处理并发；不得重入有明确禁止说明的对象 |
| 缓存 | TTL=0 通常表示不过期；Get 的 hit 与 error 分别判断 |
| 任务池 | Submit 只接纳；Close 后 Wait；等待超时不强行停止 goroutine |
| LLM 工具执行 | 只分发到显式注册的业务 handler；Schema 校验不替代权限判断 |
| 多副本 | 进程内 Map、去重、序号和 Loader 不自动变成分布式协调 |
| 凭据与副作用 | 从调用方配置取得，不把密钥写入示例或日志；不因查文档触发真实发送/写入 |

## 不存在或需要外部适配的能力

- 不存在根模块 `goparts.New()`，也没有自动初始化全部客户端的全局 SDK。
- 当前只有阿里云 OSS 适配器；其他对象存储需实现 Backend，不能假设已有对应包。
- config 没有热更新；chat.Session 没有内置数据库持久化。
- Chat 兼容端点不一定支持 Responses；没有 Anthropic 原生协议客户端。
- artifact 使用明确的通用目录协议，没有宣称 BOS 专有协议或 toolkits 精确兼容。
- HTTP/LLM/飞书没有默认的应用层自动重试；需要显式策略。
- 本指南不会把 Go 函数自动暴露成运行时 LLM tools 或 MCP 服务。若希望模型实际调用能力，
  应由业务定义工具 Schema、注册受控 handler 并处理身份和权限；可参考 llm/chat.ToolRegistry。

## 给使用方 AI 工具的上下文模板

将下面文字放入该工具支持的项目规则，或直接与任务一起提供。规则入口由使用方选择；
不需要修改 module cache，不要求任何特定 AI 产品，也不保证仅创建同名文件就自动生效。

```text
本项目按需使用 github.com/bkcarlos/goparts 的独立模块。
接入前先查询实际选择的 module 版本、Dir 和 Replace，读取该目录中的 AI_GUIDE.md、README.md。
旧版本没有指南时，直接查其 GoDoc、源码和测试；主分支文档仅用于发现能力。
先复用已有封装；核对函数签名、错误、Context、并发和资源关闭约定，不编造方法。
不要为了遵循指南自动升级依赖或引入所有模块。
生成代码后运行相关编译/测试；说明真正执行的验证与仍需真实服务联调的部分。
当前需求：<填写业务场景、已有模块版本和限制>。
```

## 文档维护与验证

新增/删除 module 时，同时更新这里的 23 模块索引、对应 AI_GUIDE.md、README 入口及 go.work/Makefile。
API 改动时同步指南中的路由与调用约定，完整参数保留在源码及模块 README，避免复制大量易过期签名。
示例应优先引用仓库内可编译源码或已验证的 README 完整函数。

使用方在自己的项目运行相关 `go test`、`go vet`；维护整个仓库时执行
`make check`，并按改动需要执行 `make stress` / `make fuzz`。
本地 mock 测试不证明真实飞书权限、模型服务商、OSS 或 SSH 环境已联调，见 [验证记录](VALIDATION.md)。
