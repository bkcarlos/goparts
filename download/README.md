# 通用流式下载

独立 module：`github.com/bkcarlos/goparts/download`，Go 1.21+，只使用标准库。数据源实现 `Source.Open(ctx)`，下载器统一处理流式落盘、大小限制、SHA-256、进度和发布目标文件；HTTP 或对象存储都可接入。

## HTTP 下载

```go
source, err := download.NewHTTPSource(url, download.HTTPConfig{
    // Headers: http.Header{"Authorization": {"Bearer " + token}},
})
if err != nil { return err }
client, err := download.New(download.Config{
    // Timeout: 30 * time.Minute,
    // MaxBytes: 10 * 1024 * 1024 * 1024,
})
if err != nil { return err }
result, err := client.Fetch(ctx, source, "./app.zip", download.Options{
    SHA256: expectedSHA256, // 为空时只计算，不与预期值比较
    OnProgress: func(written, total int64) error { return nil },
})
```

成功结果包含 Path、Bytes、SHA256。HTTP 默认只接受 200 完整响应，不跟随重定向，拒绝 Range / If-Range 输入和意外内容编码，避免将部分文件当作完整下载。Header 初始化时复制；错误文本和统一错误字段不含带凭据 URL 或响应正文。

## 对象存储下载

业务组合 `storage.Client`，download 本身不导入 storage 或任意云 SDK：

```go
// store 可以是阿里云适配器，也可以是后续实现的其他供应商。
source := download.SourceFunc(func(ctx context.Context) (download.Stream, error) {
    reader, err := store.Get(ctx, "releases/app.zip", storage.GetOptions{})
    if err != nil { return download.Stream{}, err }
    return download.Stream{Body: reader, Size: reader.Length}, nil
})
result, err := client.Fetch(ctx, source, "./app.zip", download.Options{
    SHA256: expectedSHA256,
})
```

适配任意数据源只需返回 `io.ReadCloser` 和字节数（未知为 -1）；下载器负责 Close。Source 必须遵守 Context，不能忽略取消后永久阻塞。多次下载时每次 Open 都应创建新流。

## 执行语义和配置

- 默认总超时 30 分钟，最大文件 10 GiB，缓冲区 64 KiB；通过 Timeout、MaxBytes、BufferSize 配置，零使用默认值、负值无效，缓冲区最大 16 MiB。整个下载流受同一 Context 管理。
- 只在目标同目录创建权限 0600 的随机临时文件，读取完成、长度与校验通过、流关闭和文件同步成功后才发布；失败清理临时文件，已有目标文件保持不变。
- 默认禁止覆盖，通过硬链接原子发布，能够检测检查后出现的竞争写入；文件系统不支持硬链接时直接返回错误，不退化成有覆盖风险的操作。`Overwrite:true` 使用同目录 rename 替换，遵循运行平台的文件系统语义。
- 目标父目录必须存在，且由可信调用方管理。不会创建任意目录或使用远端文件名推导本地路径；不接受已有非普通文件作为覆盖目标。
- Progress 表示已写临时文件的字节数，total=-1 表示未知；100% 不代表最终校验/发布已经完成。回调错误中止下载，应快速返回。取消无法强行打断不遵守 Context 的自定义 Reader/回调。
- `ErrTooLarge`、`ErrSizeMismatch`、`ErrChecksum`、`ErrExists` 可用 errors.Is 判断。HTTP 错误可 errors.As 为 `*HTTPError`，统一上报编码 `download.http_failed`。
- `Fetch` 为单流完整下载，失败重试时重新打开流；`FetchRanges` 提供范围调度和断点续传，见下文。两者都不自动重试失败请求。SHA-256 用于内容校验；云存储 ETag 不能直接充当 SHA-256/MD5。

## 测试

```sh
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

本地测试覆盖 HTTP/任意 Source、校验、大小、取消、进度错误、竞争写入和失败文件清理；不访问真实云服务。

### Range、自动选择和断点续传

`FetchRanges(ctx, source, destination, RangeOptions{Mode:Auto, Workers:4,
ChunkBytes:8<<20, Resume:true})` 使用通用 RangeSource，不绑定 OSS。
HTTPSource 提供 Probe / RangeSupport / OpenRange；GET bytes=0-0 验证实际范围响应。
Auto 在不支持范围或缺少版本绑定时回退普通 Fetch；Parallel 则返回错误。
范围操作需要强 ETag 或预期 SHA-256，If-Match 防止跨版本拼接。

续传状态写在 `<destination>.goparts-part/`，包含源指纹、ETag、分片大小和完整分片；
每片有独立校验和，失败保留完整片，恢复时校验后复用；源/版本/参数变化拒绝继续。
全部完成后再按完整 SHA-256（如提供）校验并原子发布，成功删除状态目录。
同一状态目录一次仅一个下载器；崩溃遗留 lock 需确认无存活下载后人工移除。
目录必须由调用方可信持有。OnProgress 当前报告最终组装的写入进度，回调失败不发布。
中断发生在分片内部时该片从头下载；不保证保存单片内的字节偏移。


完整的 HTTP 续传函数：

```go
package example

import (
    "context"

    "github.com/bkcarlos/goparts/download"
)

func ResumeHTTP(ctx context.Context, url, destination, expectedSHA256 string) (download.Result, error) {
    source, err := download.NewHTTPSource(url, download.HTTPConfig{})
    if err != nil { return download.Result{}, err }
    client, err := download.New(download.Config{MaxBytes: 2<<30})
    if err != nil { return download.Result{}, err }
    return client.FetchRanges(ctx, source, destination, download.RangeOptions{
        Options: download.Options{SHA256: expectedSHA256},
        Mode: download.Auto,
        Workers: 4,
        ChunkBytes: 8<<20,
        Resume: true,
    })
}
```

| RangeOptions | 默认 / 语义 |
| --- | --- |
| Mode | 空值等同 Auto；Single 直接走 Fetch；Parallel 要求稳定范围数据源 |
| Workers | 0 默认 4，允许 1..64 |
| ChunkBytes | 0 默认 8 MiB；单次最多 100000 片 |
| Resume | false 使用临时分片目录并在结束时清理；true 保留失败后的完成片 |
| Options | 复用 SHA256、Overwrite、OnProgress |

重试续传需保持同一来源、身份、分片大小和预期摘要。提供自定义 RangeSource 时，Probe 必须返回稳定 Identity，
OpenRange 必须返回所要求版本的精确字节范围；不能把完整对象流直接冒充范围流。
在 Auto 回退单流时 Resume 不会使单流下载保存断点。若业务必须使用断点能力，应选择 Parallel 并处理不支持错误。
