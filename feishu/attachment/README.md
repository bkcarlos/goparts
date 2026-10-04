# 飞书附件

导入 `github.com/bkcarlos/goparts/feishu/attachment`，与 `user`、`card` 共用 Feishu module，仅使用标准库。通过 `TokenProvider` 组合应用机器人或已登录用户的身份，不重复实现登录和 token 刷新。

## 支持范围

| 场景 | Reader 入口 / 本地路径入口 | 返回值 | 平台单文件上限 |
| --- | --- | --- | --- |
| 聊天文件 | `UploadChat` / `UploadChatPath` | `ChatFile.FileKey` | 30 MiB |
| 云空间文件夹 | `UploadDrive` / `UploadDrivePath` | `StoredFile.FileToken` | 20 MiB |
| 文档、表格、多维表格素材 | `UploadMedia` / `UploadMediaPath` | `StoredFile.FileToken` | 20 MiB |

`SendFile` 使用聊天上传得到的 `file_key` 发送文件消息。云空间和素材的 `file_token` 不能替代 `file_key`；素材上传也不会自动显示在文档中，需要另行关联块或单元格。本包提供 docx 文件块的完整组合方式，Sheets / Bitable 的关联操作需由业务实现。

Webhook 地址不能用来上传文件。聊天文件上传也不同于 IM 图片上传，本包没有封装 `/im/v1/images`，不会生成卡片需要的 `image_key`。

## 应用机器人上传并发送

以下片段放在返回 `error` 的业务函数中，`ctx` 由调用方传入：

```go
import (
    "github.com/bkcarlos/goparts/feishu/attachment"
    "github.com/bkcarlos/goparts/feishu/card"
)

app, err := card.New(card.Config{AppID: appID, AppSecret: appSecret})
if err != nil { return err }
files, err := attachment.New(attachment.Config{
    TokenProvider: app.AccessToken,
    // Timeout: 2 * time.Minute,
    // MaxFileBytes: 10 * 1024 * 1024,
    // MaxResponseBytes: 2 * 1024 * 1024,
})
if err != nil { return err }

uploaded, err := files.UploadChatPath(ctx, "./report.pdf", attachment.UploadOptions{
    FileType: "pdf",
})
if err != nil { return err }
_, err = files.SendFile(ctx, attachment.Chat(chatID), uploaded.FileKey,
    attachment.SendOptions{UUID: sendID})
return err
```

`Chat(chatID)` 发到群聊；`User(openID)` 发给用户，也可自行构造 `Receiver` 指定 `union_id`、`user_id` 或 `email`。可选 UUID 最长 50 字符，用于消息去重；同一逻辑发送复用同一个值，不同消息使用不同值。

上传与发送分开调用，发送失败时可保留 `FileKey` 后续复用。`FileType` 默认为 `stream`，也支持 `pdf/doc/xls/ppt/opus/mp4`；可选 `DurationMS` 为音视频时长。该库不转码、不检测文件格式；`SendFile` 始终发送 `file` 消息，音视频专用消息需另行封装。

## 用户身份上传到云空间

先用 [user 登录流程](../user/README.md) 获取用户授权，在 `user.Config.Scopes` 中加入所需上传权限。已有会话缺少新权限时需要重新授权，修改配置不会自动扩权。

```go
// docs 是已完成用户登录的 *user.Client。
files, err := attachment.New(attachment.Config{TokenProvider: docs.AccessToken})
if err != nil { return err }
uploaded, err := files.UploadDrivePath(ctx, folderToken, "./report.pdf",
    attachment.UploadOptions{FileName: "季度报告.pdf"})
if err != nil { return err }
// uploaded.FileToken 可交给后续云空间文件操作。
```

文件夹 token 必填，当前身份必须具有目标文件夹的上传/编辑权限。这里只做文件上传，不负责创建文件夹、移动到 Wiki 或分享文件。

## 用户身份插入 docx 附件

飞书要求先创建文件块，再将素材上传到该块，最后绑定 token：

```go
// docs 已登录；files 使用 docs.AccessToken 初始化。
block, err := docs.CreateFileBlock(ctx, documentID, "",
    user.WriteOptions{ClientToken: createID})
if err != nil { return err }

uploaded, err := files.UploadMediaPath(ctx, attachment.MediaTarget{
    ParentType: "docx_file",
    ParentNode: block.BlockID,
    DocumentToken: documentID,
}, "./report.pdf", attachment.UploadOptions{})
if err != nil { return err }

_, err = docs.ReplaceFile(ctx, documentID, block.BlockID, uploaded.FileToken,
    user.WriteOptions{ClientToken: attachID})
return err
```

`CreateFileBlock` 的 `parentID` 为空时追加到文档根部。飞书实际返回外层 view 块，本方法解析其子节点，分别返回 `BlockID`（内层文件块）和 `ViewBlockID`。`ParentNode` 必须使用内层 `BlockID`，不能传文档 ID 或外层 view ID；`DocumentToken` 被编码为 `extra.drive_route_token`。

三个步骤不构成事务。中途失败时，空块或已上传素材可能已经存在；请保留相关 ID，由业务决定继续关联或清理。`CreateFileBlock` 解析响应失败时也可能返回 `ViewBlockID`，不能据此认定创建未发生。创建与关联使用各自的 `ClientToken`，不要用同一个值代表不同操作。库不会自动重试或删除文档内容。

`MediaTarget.ParentType` 支持 `docx_file/docx_image/sheet_file/sheet_image/bitable_file/bitable_image`。docx 的 `ParentNode` 是对应文件/图片块 ID；Sheets / Bitable 是对应文档 token。`DocumentToken` 均需提供对应文档 token。图片素材上传后的块关联需自行使用官方协议实现。

## Reader、进度与配置

```go
uploaded, err := files.UploadChat(ctx, attachment.Source{
    Reader: reader,
    Size: sizeBytes,
}, attachment.UploadOptions{
    FileName: "data.zip",
    OnProgress: func(read, total int64) error {
        // 已从源读取的字节数；返回错误可中止上传。
        return nil
    },
})
```

- `Source.Size` 必须为真实字节数且大于零；不足或超出会返回 `ErrSizeMismatch`。每次调用消费一次 Reader，不会关闭调用方的 Reader；路径入口自行打开和关闭普通文件。
- 不将整个附件读入内存；multipart 使用已知 Content-Length 流式发送。路径入口默认使用文件 basename，Reader 入口必须指定文件名。文件名不允许路径分隔符、换行和空字符，最长 250 字符。
- `OnProgress` 同步执行，表示源读取进度，不代表服务端确认接收；到达 100% 后仍需检查上传返回值。回调应快速返回，勿阻塞。Context 无法强制中断任意阻塞的 Reader / 回调。
- `Timeout` 默认 2 分钟，包含获取 token、等待素材上传锁和 HTTP 操作；调用方 Context 更早的截止时间仍生效。自定义 HTTPClient 的 Timeout 保留，重定向始终禁用。
- `MaxFileBytes` 默认 30 MiB，`MaxResponseBytes` 默认 2 MiB；零使用默认值，负值无效。配置可收紧大小限制，但无法突破对应接口的 30 / 20 MiB 平台限制。当前不支持分片上传、断点续传和空文件。
- 素材上传在同一个 Client 内串行执行；复用 Client。没有自动实现平台 QPS / 日配额限流，多 Client 或多进程需要业务统一协调。
- 每次操作调用 `TokenProvider`，token 缓存和刷新由提供者管理；直接复用 `card.Client.AccessToken` 或 `user.Client.AccessToken`。用户权限失败时不会自动换成应用身份。

## 权限和错误

| 操作 | 常用权限 |
| --- | --- |
| 聊天文件上传 | `attachment.ScopeUploadChat`（`im:resource:upload`） |
| 云空间上传 | `attachment.ScopeUploadDrive`（`drive:file:upload`） |
| 文档素材上传 | `attachment.ScopeUploadMedia`（`docs:document.media:upload`） |
| 创建和关联 docx 文件块 | `user.ScopeWriteDocuments`（`docx:document`） |

上传权限不包含发消息权限。应用发送还需启用机器人能力和消息发送权限，并满足可用范围、群成员等要求，见 [卡片接入权限](../card/README.md)。用户身份发消息也需相应的用户消息发送权限。具体可用身份、权限替代项和资源访问限制，以官方接口说明为准。

`errors.Is` 可判断 `ErrFileTooLarge`、`ErrSizeMismatch`、Context 错误；`errors.As` 可获取 `*APIError` 的 HTTP 状态、业务 Code、Message 和 RequestID。`APIError` 实现统一编码 `feishu.attachment.api_error`，可直接交给 [apperror](../../apperror/README.md#已有模块适配) 上报；原始 Message 不进入默认错误文本和上报字段。

不自动重试上传和发送；网络失败时服务端是否保存可能未知，重试需提供新的 Reader，并自行评估重复附件。真实服务权限和端到端流程尚未联调。

## 本地示例与接口参考

从 `feishu` 目录运行，无需凭据，仅访问本地模拟服务：

```sh
GOWORK=off go run ./examples/attachments_mock
```

示例覆盖应用身份上传/发消息，以及用户身份云空间上传/文档附件关联。

官方接口：[聊天文件上传](https://open.feishu.cn/document/server-docs/im-v1/file/create)、[云空间上传](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/drive-v1/file/upload_all)、[素材上传](https://open.feishu.cn/document/server-docs/docs/drive-v1/media/upload_all)、[docx 附件流程](https://open.feishu.cn/document/ukTMukTMukTM/uUDN04SN0QjL1QDN/document-docx/docx-v1/faq)。
