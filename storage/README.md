# 通用对象存储

独立 module：`github.com/bkcarlos/goparts/storage`，Go 1.21+。业务依赖 `storage.Client` 或 `storage.Backend`，供应商通过适配器接入。第一批实现 **阿里云 OSS**；未来 S3、MinIO、COS 等实现同一接口即可替换，当前尚未提供这些适配器。

根包仅使用标准库；`storage/aliyun` 使用阿里云官方 Go SDK v2 v1.6.0 处理签名、协议和分片上传，业务 API 不暴露 SDK 类型。

## 初始化和使用

```go
import (
    "github.com/bkcarlos/goparts/storage"
    "github.com/bkcarlos/goparts/storage/aliyun"
)

backend, err := aliyun.New(aliyun.Config{
    Region: "cn-hangzhou",
    Bucket: bucket,
    Credentials: aliyun.Credentials{
        AccessKeyID: accessKeyID,
        AccessKeySecret: accessKeySecret,
        // SecurityToken: stsToken,
    },
})
if err != nil { return err }
store, err := storage.New(backend, storage.Config{BasePath: "my-app/releases"})
if err != nil { return err }

object, err := store.PutFile(ctx, "v1/app.zip", "./app.zip", storage.PutOptions{
    ContentType: "application/zip",
    Metadata: map[string]string{"version": "v1"},
    OnProgress: func(read, total int64) error { return nil },
})
if err != nil { return err }

reader, err := store.Get(ctx, object.Key, storage.GetOptions{})
if err != nil { return err }
defer reader.Close()
// reader 是流；reader.Object 包含大小、ETag、修改时间等信息。
```

对象实际写入 `my-app/releases/v1/app.zip`，业务始终使用 `v1/app.zip`。BasePath 不代替云端权限控制。对象键允许 UTF-8 和目录标记尾斜线，拒绝空键、绝对路径、`.` / `..` 段、重复斜线、反斜线、NUL 和换行，不静默清洗路径。

| 方法 | 行为 |
| --- | --- |
| `Put` / `PutFile` | Reader + 确切字节数 / 普通本地文件；默认覆盖同名对象，不是原子 compare-and-swap |
| `Get` | 流式读取，调用方必须 Close；支持 Offset、Length 和 IfMatch |
| `ReadAll` | 有界读入内存，默认最多 16 MiB，可配置 `storage.Config.MaxReadBytes` |
| `Stat` / `Exists` | 获取元信息 / 判断存在；只有对象不存在返回 false，权限或网络错误继续返回 |
| `Delete` | 删除指定键；具体版本保留遵循 Bucket 的版本策略 |
| `List` | 一次一页；Prefix、Cursor、Limit；默认 100，上限 1000，原样使用 NextCursor 继续读取 |
| `PresignGet` | 临时 GET 下载链接及到期时间；链接携带授权，不要记录到公开日志 |

`GetOptions.Length=0` 表示读取到末尾；ETag 是不透明版本标识，分片上传的 ETag 不等于文件 MD5。范围读取如果供应商忽略 Range，不会把完整对象误当成部分数据；可使用 `IfMatch` 防止读取已变更的对象。

`Put` 消费一次 Reader，不关闭调用方 Reader；`PutFile` 自行打开并关闭文件。进度表示源读取量，不是服务端保存确认；返回进度错误会中止。长度不匹配返回 `ErrSizeMismatch`，但服务端可能已经接收数据；任何失败都不能推断对象一定不存在。Reader、进度回调、凭据提供器需要遵守 Context 并及时返回，Go 无法强制打断任意阻塞代码。

## 阿里云适配器配置

| 参数 | 默认值 / 说明 |
| --- | --- |
| Bucket / Region | 必填；Endpoint 为空时由官方 SDK 按 Region 生成 |
| Credentials | 显式 AK/SK，可带 STS token；不自动读取业务环境变量 |
| CredentialsProvider | 可选 `func(context.Context) (aliyun.Credentials, error)`；优先于静态凭据，可自行刷新 STS，必须并发安全 |
| Endpoint | 可选 HTTP(S) 源站，不包含 Bucket、路径、查询参数或凭据 |
| UsePathStyle | 默认 false；测试服务或兼容网关可开启 |
| RequestTimeout | 每个 HTTP 请求 2 分钟，包含 Get 流的读取；注入 HTTPClient 的更短超时保留 |
| MaxResponseBytes | 元数据和错误响应最多 4 MiB，不限制 Get 的对象正文；下载总大小由业务或 download 限制 |
| MultipartThreshold | 100 MiB，达到此大小使用分片上传 |
| PartSize / Parallelism | 8 MiB / 3；范围 100 KiB～1 GiB / 1～64 |
| CleanupTimeout | 分片失败后尝试清理，默认 10 秒；使用独立于已取消上传 Context 的清理预算 |

零值使用默认值，负值无效。为满足最多 10000 个分片，分片大小可能自动增大；超出当前 1 GiB × 10000 的适配器容量则拒绝。Reader 上传由 SDK 缓冲分片，内存随 PartSize 和并发数增长，不缓存整份文件。上传中断后尝试 Abort，清理失败会与原错误一起返回；远端不完整分片仍需 Bucket 生命周期规则兜底。当前不保留断点上传检查点，也不封装目录批量上传。

SDK 应用层重试设为一次，重定向禁用；业务按幂等性决定重试并重新打开输入流。Get 返回后必须保持 Context 有效直到读取结束。临时签名 URL 的实际有效期同时受 STS 凭据有效期限制。

## 供应商替换

新适配器实现以下接口：

```go
type Backend interface {
    Put(context.Context, string, io.Reader, int64, PutOptions) (Object, error)
    Get(context.Context, string, GetOptions) (*Reader, error)
    Stat(context.Context, string) (Object, error)
    Delete(context.Context, string) error
    List(context.Context, ListOptions) (Page, error)
    PresignGet(context.Context, string, time.Duration) (SignedURL, error)
}
```

适配器接收 Bucket 相对键，Client 统一处理 BasePath；不支持的能力返回 `ErrUnsupported`。适配器必须消费完 Put 输入后再返回、保持 Get 流和元信息一致、遵守 Context 并保证并发安全。测试中的内存 Backend 与真实阿里云适配器使用相同业务入口。

通用错误包括 `ErrNotFound`、`ErrPermission`、`ErrPrecondition`、`ErrUnsupported`；`*storage.Error` 保留供应商 code、操作、状态、RequestID，并通过 `errors.Is/As` 保留底层原因。统一上报编码是 `storage.operation_failed`，默认不输出原始 SDK URL、凭据或服务端正文。

流式落盘可组合独立 [download 模块](../download/README.md)，无需让 download 依赖阿里云 SDK。

## 测试

```sh
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

本地模拟服务验证签名请求、普通/分片上传、分片失败清理、范围、分页、下载和统一错误；尚未在真实 OSS Bucket 联调。[官方 SDK](https://github.com/aliyun/alibabacloud-oss-go-sdk-v2/tree/v1.6.0)。

### 目录上传与兼容校验

`UploadDirectory(ctx,prefix,root,DirectoryOptions{Workers:4})` 并发上传目录中的
普通文件，以相对路径组成 key，再应用 Client.BasePath。Filter 可过滤文件或剪枝目录，
拒绝符号链接/特殊文件。返回成功上传列表和组合错误；失败不回滚已上传对象。
OnProgress 在不同文件工作协程中调用，业务回调须自行区分和同步。
`ComputeFileMD5(path)` 用于旧制品校验；安全完整性优先使用 SHA-256，不把 OSS ETag 当 MD5。
