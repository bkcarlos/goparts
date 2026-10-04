# 飞书用户登录与文档操作

[返回模块总览](../../README.md) · [Feishu 模块](../README.md) · [卡片操作](../card/README.md)

导入 `github.com/bkcarlos/goparts/feishu/user`，属于独立 `feishu` module 的子包，仅使用标准库。它与根包的群 Webhook 机器人分别初始化。

这里的“以用户身份操作”是：**应用通过用户授权取得 user_access_token，再在该用户已有权限范围内调用文档 API**。Webhook 地址不能登录用户，也不能换取文档权限。

## 借鉴范围

参考官方 [larksuite/cli 的设备授权实现](https://github.com/larksuite/cli/blob/7beffb086d7fa3c5b843d8affa7c089f49cfc65e/internal/auth/device_flow.go)和 [token 刷新实现](https://github.com/larksuite/cli/blob/7beffb086d7fa3c5b843d8affa7c089f49cfc65e/internal/auth/uat_client.go)，独立实现以下协议流程：

1. 使用 App ID / App Secret 请求 `/oauth/v1/device_authorization`。
2. 将授权链接和用户码交给真人，在飞书页面确认账号与权限。
3. 以 device_code 向 `/oauth/v3/token` 轮询；处理等待、降速、拒绝和过期。
4. 获取用户身份，保存用户 token；到期前用 refresh_token 刷新并保存轮换结果。
5. 文档请求仅携带用户 Bearer token。

没有引入、执行或复制整个 CLI，也没有新增 CLI/官方大 SDK 依赖。没有实现 CLI 的应用自动创建、命令注册、系统钥匙串、DPoP、授权代理或多进程凭证协调。上游使用 MIT 协议，来源与版本见 [NOTICE.md](NOTICE.md)。

## 应用与权限准备

- 准备自己飞书应用的 App ID 和 App Secret。应用须能使用设备授权流程；如平台返回 unauthorized_client 等错误，需要检查应用配置与平台支持，SDK 不绕过平台限制。
- 在应用后台开通并按平台要求生效相应的用户身份权限，登录时再由用户确认授权。
- 只读文档使用 `user.ScopeReadDocuments`（`docx:document:readonly`）；创建/编辑使用 `user.ScopeWriteDocuments`（`docx:document`）。模块自动追加 `offline_access`，请求可刷新授权。
- 用户本身必须能访问目标文档/文件夹；授权应用不会让用户获得其本来没有的文档权限。
- 配置的 Scopes 必须出现在服务端返回的授权范围中。若更换范围导致 ScopeError，重新发起登录。不同账号使用不同会话文件和 Client。

本版支持 Bearer 用户 token。若返回 DPoP token，会返回 `ErrUnsupportedToken`，不会擅自降级认证。

## 初始化与首次登录

| `Config` 字段 | 默认值 / 要求 | 说明 |
| --- | --- | --- |
| `AppID` / `AppSecret` | 必填 | 飞书应用凭据 |
| `Scopes` | 必须显式配置至少一个 | 模块自动追加 `offline_access` |
| `BaseURL` | `https://open.feishu.cn/open-apis` | API 根地址，包含 `/open-apis` |
| `AccountsURL` | `https://accounts.feishu.cn` | 设备授权和 Token 服务根地址 |
| `Timeout` | 15 秒 | 单个 HTTP 请求超时；`0` 使用默认值，负数无效 |
| `Store` | `MemoryStore` | 可替换为加密文件或自定义 `TokenStore` |
| `HTTPClient` | 新建客户端 | 复制配置，禁止重定向 |

响应读取上限固定为 8 MiB，当前未暴露为配置项。登录轮询的整体期限还受设备码有效期和调用方 Context 约束。

```go
import "github.com/bkcarlos/goparts/feishu/user"

client, err := user.New(user.Config{
    AppID: appID,
    AppSecret: appSecret,
    Scopes: []string{user.ScopeWriteDocuments},
    // Store 不设置时仅存内存；进程退出后需要重新登录。
})
if err != nil { return err }

auth, err := client.StartLogin(ctx)
if err != nil { return err }
fmt.Println(auth.VerificationURIComplete) // 给用户打开并确认
fmt.Println(auth.UserCode)

identity, err := client.CompleteLogin(ctx, auth)
if err != nil { return err }
fmt.Println(identity.Name, identity.OpenID)
```

无需本地回调 HTTP 服务。SDK 不自动打开浏览器，业务可以把链接展示在终端或自己的界面中。CompleteLogin 按服务器 interval 等待，slow_down 后每次额外等待 5 秒，直到用户确认、拒绝、设备码过期或调用方取消。

默认服务地址为 `https://accounts.feishu.cn` 和 `https://open.feishu.cn/open-apis`。可通过 AccountsURL/BaseURL 设置测试服务或对应 Lark 服务的完整根地址；端点会绑定到已保存会话，不能任意换域复用凭据。

## 跨进程重启保存会话

```go
store, err := user.NewEncryptedFileStore(sessionPath, encryptionKey)
if err != nil { return err }
client, err := user.New(user.Config{
    AppID: appID,
    AppSecret: appSecret,
    Scopes: []string{user.ScopeWriteDocuments},
    Store: store,
})
```

encryptionKey 必须是 32 字节随机密钥，存放在业务密钥管理器中，后续启动复用同一密钥。模块使用 AES-256-GCM 加密、0600 文件权限和临时文件原子替换；密钥不写进会话文件，也不回退为明文。自行实现 `TokenStore` 可接入数据库、Vault 或钥匙串。

每个会话复用一个 Client，可并发执行文档操作，刷新过程会串行化。文件存储支持**重启后恢复**，不提供多个进程同时刷新同一会话的事务锁。多实例部署需要在外层协调整个“读取 → 刷新 → 保存”事务，或使用独立的授权服务。

刷新成功但保存失败时，Client 在内存保留新 token；后续操作先重试保存，不再次使用旧 refresh token。此时不要销毁 Client，否则尚未持久化的新 token 会丢失。已经发出的刷新请求会在独立的有界 Context 中完成保存，最多受配置的请求超时约束，避免业务取消导致凭据轮换结果丢失。

`Logout(ctx)` 只清除本地会话；撤销飞书端授权需要在飞书相应授权管理入口进行。

## 文档操作

```go
doc, err := client.CreateDocument(ctx, "周报", folderToken)
if err != nil { return err }

added, err := client.AppendText(ctx, doc.ID, "本周完成了登录与文档封装。", user.WriteOptions{
    ClientToken: operationID, // 可选，由业务生成稳定的幂等操作 ID
})
if err != nil { return err }

content, err := client.ReadDocument(ctx, doc.ID)
if err != nil { return err }
fmt.Println(content)

_, err = client.UpdateText(ctx, doc.ID, added.Children[0].ID(), "更新后的内容", user.WriteOptions{})
```

| 方法 | 能力 |
| --- | --- |
| Me | 获取当前登录用户 |
| CreateDocument | 创建 docx 文档，文件夹为空时使用根目录 |
| GetDocument | 获取标题、文档 ID 和 revision |
| ReadDocument | 读取文档纯文本 |
| ListBlocks | 分页读取完整块 JSON，支持固定文档版本 |
| AppendBlocks / AppendText | 在文档或父块末尾追加内容，每次 1～50 块 |
| UpdateText | 替换指定文本类块的元素，包括原有行内样式 |
| CreateFileBlock / ReplaceFile | 创建文件块、关联已上传的文档素材 token |

传入 docx 的 document_id，不是整条 URL。Wiki 链接需要先在业务侧解析成对应文档 ID，本子包没有封装 Wiki、Sheets、Bitable 业务接口、Markdown 转换或文档删除。文件和图片素材上传见独立的 [attachment 子包](../attachment/README.md)，其中包含 docx 附件的完整组合示例。

`Block` 保留完整 JSON 结构，`Paragraph` 是简单段落构造器；复杂富文本通过 AppendBlocks 按官方协议传入。获取分页时建议先 GetDocument 固定 RevisionID，然后使用 PageToken 继续遍历，直到 HasMore 为 false；库每次只取一页。

写入默认使用最新版本（-1），也可通过 WriteOptions.RevisionID 指定版本。涉及多次写入时不保证整体事务性；创建文档成功后追加失败，已创建的文档仍然存在。

## 错误与执行语义

- `ErrLoginRequired`：没有会话或刷新凭据过期，需重新授权。
- `*OAuthError`：授权拒绝、应用配置或刷新错误；Description 保留服务端说明。
- `*ScopeError`：返回的授权范围不满足配置。
- `*APIError`：HTTP/业务状态失败；包含 Code、Message、RequestID。Error() 不直接输出服务端消息。
- 请求默认超时 15 秒；登录的整体截止时间来自设备码有效期和调用方 Context。
- 不跟随重定向，不自动重试文档读写，也不在用户权限不足时切换 tenant_access_token。网络超时后的写入状态可能未知，必要时由业务利用 ClientToken 和读取结果核对。
- 普通错误文本不打印凭据。Token 的默认格式化输出会脱敏；显式 JSON 序列化仍包含密钥，仅用于安全存储。

`APIError`、`OAuthError`、`ScopeError` 实现了统一错误接口，可直接交给 [apperror](../../apperror/README.md#已有模块适配) 上报，或先包装为自己的业务错误。上游原始 Message / Description 不自动进入上报字段。

## 示例命令

从 `feishu` 模块目录运行本地模拟流程，无需应用凭据，也不访问真实飞书：

```sh
GOWORK=off go run ./examples/userdocs_mock
```

真实接入使用 `examples/userdocs`。先自行配置 `FEISHU_APP_ID`、`FEISHU_APP_SECRET`、`FEISHU_TOKEN_KEY`（固定复用的 32 字节密钥，以 64 位十六进制表示）。示例默认申请 `docx:document`，可通过 `FEISHU_SCOPES` 改为只读范围。不要把密钥或会话文件提交进仓库。

```sh
GOWORK=off go run ./examples/userdocs login
GOWORK=off go run ./examples/userdocs whoami
GOWORK=off go run ./examples/userdocs read -doc DOCUMENT_ID
GOWORK=off go run ./examples/userdocs create -title '周报'
GOWORK=off go run ./examples/userdocs append -doc DOCUMENT_ID -text '新增段落'
GOWORK=off go run ./examples/userdocs update -doc DOCUMENT_ID -block BLOCK_ID -text '替换内容'
GOWORK=off go run ./examples/userdocs logout
```

写入命令会真实修改文档；示例只在显式执行对应命令时写入。可通过 `-session` 选择不同账号的加密会话文件。

## 验证与参考

```sh
GOWORK=off go test -race -cover ./...
GOWORK=off go vet ./...
```

已使用本地模拟服务器验证授权等待/降速/拒绝、token 轮换、并发刷新、保存失败恢复、加密存储和文档请求；尚未使用真实应用完成授权联调。

官方文档：[创建文档](https://open.feishu.cn/document/server-docs/docs/docs/docx-v1/document/create)、[读取纯文本](https://open.feishu.cn/document/server-docs/docs/docs/docx-v1/document/raw_content)、[获取所有块](https://open.feishu.cn/document/server-docs/docs/docs/docx-v1/document/list)、[追加块](https://open.feishu.cn/document/server-docs/docs/docs/docx-v1/document-block/create)、[更新块](https://open.feishu.cn/document/server-docs/docs/docs/docx-v1/document-block/patch)。

### 多账号与刷新协调

MultiMemoryStore 和 NewMultiEncryptedStore(dir,key) 以 open_id 隔离账户；每次读写
校验 token 的 OpenID。加密版复用现有 AES-256-GCM 存储，key 仍由调用方从秘密管理系统
提供，不把密码当密钥。NewManager 为每个账户缓存一个 Client。
`AsUser(ctx,id)` / `AsApplication(ctx)` 明确选择身份，Manager.AccessToken 可注入
Bitable/Wiki/Contact/attachment。未选择身份或用户令牌失败都不自动使用应用身份。
通过 Manager.User(id) 完成指定账户的登录；实际登录账户不匹配时拒绝落盘。

`Config.RefreshLocker` 可协调整个 Load→refresh→Save；多账号加密存储默认提供
基于独占锁文件的 FileLocker。不同进程必须共享相同目录和加密 key，禁止绕过锁
直接刷新。进程异常退出可能留下锁文件，确认无存活拥有者后由运维移除，程序不猜测
过期锁，避免双重刷新。分布式部署可注入具有同等事务语义的 Locker。
`OnPollTick(ctx, PollTick)` 在每轮等待前报告次数、间隔和到期时间，不包含 device code
或 token；返回错误可终止登录。MaxResponseBytes 可配置，默认 8 MiB。

CLI 也可用 NewPassphraseFileStore(path,passphrase,salt,iterations)，通过
PBKDF2-HMAC-SHA256 派生 AES-256 key（至少 12 字节口令、16 字节随机 salt、600000 次迭代）。
salt 必须首次随机生成后持久保存并复用；调用方负责 salt/迭代配置，组件不保存口令。
这不是对不可访问的 toolkits 存储文件格式的兼容承诺。
