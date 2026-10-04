# artifact：制品目录与校验下载

[LLM 使用指南](AI_GUIDE.md)：按当前版本查找能力、源码入口和调用约定。

[返回总览](../README.md) · [自行发布版本](../version/README.md) · [通用下载](../download/README.md)

Go 1.21+，适合从已有制品平台查询包并下载。PackageManager 可注入任意 Backend；
内置 HTTPBackend 使用本文约定的 JSON 协议，不声明兼容 BOS 专有协议或不可访问的 toolkits 旧接口。

```sh
go get github.com/bkcarlos/goparts/artifact@v0.1.0
```

## 创建客户端并下载

```go
package example

import (
    "context"
    "net/http"
    "time"

    "github.com/bkcarlos/goparts/artifact"
)

func DownloadPackage(ctx context.Context, catalogURL, token, packageID,
    destination string) error {
    backend, err := artifact.NewHTTPBackend(artifact.Config{
        BaseURL: catalogURL,
        Headers: http.Header{"Authorization": {"Bearer " + token}},
        Timeout: 15*time.Second,
    })
    if err != nil { return err }
    manager := &artifact.PackageManager{Backend: backend, MaxBytes: 2<<30}
    pkg, err := manager.GetPackageInfo(ctx, packageID)
    if err != nil { return err }
    return manager.DownloadPackage(ctx, pkg, destination)
}
```

调用会先查目录，再访问目录返回的下载 URL。目录认证 Header 不会复制到下载请求，
下载地址应本身可访问（例如短期签名 URL）。目标父目录需已存在，已有目标不会被覆盖。

## 内置目录协议

BaseURL 不带查询参数和 fragment；可包含平台的 API 前缀，客户端追加以下路径：

| 请求 | 响应 |
| --- | --- |
| `GET /packages/{id}` | 单个 AppPackage，响应 id 必须匹配请求 |
| `GET /packages?commit_id=...&cursor=...&limit=...` | `{ "items": [...], "next_cursor": "..." }` |

单包直接返回 JSON 对象，不使用额外 data/code 包装。例如内容为 `hello` 的包：

```json
{
  "id": "pkg-001",
  "name": "hello.txt",
  "version": "v1.0.0",
  "commit_id": "build-001",
  "url": "https://downloads.example.com/hello.txt",
  "sha256": "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
  "size": 5
}
```

可选 `internal_url` 表示内网下载地址。HTTPBackend 本身不会猜测现有平台字段，
若服务端协议不同，实现只有 GetPackageInfo 和 ListPackages 两个方法的 Backend 即可保留上层调用。

## 分页和地址选择

- `ListPackages(ctx, Query{CommitID, Cursor, Limit})` 一次取一页，Limit 为 0..1000；0 不发送 limit 参数。
- `QueryWithCommitID(ctx, commit, cursor, limit)` 要求非空 commit；用响应 NextCursor 继续，不自动读取所有页。
- `DownloadURL(ctx, pkg)` 默认选公共 URL。只有 InternalURL 有效且显式配置的 InternalAvailable 回调返回 true 才选内部 URL。
- 内部地址探测失败可选公共地址；一旦选择内部地址后的实际下载失败，不会隐式再次下载公共地址。

InternalAvailable 是调用方的函数，应限制其超时和可探测目标。本模块不扫描内网、不重写域名。

## 默认值、错误和下载保证

| 配置位置 | 默认值 |
| --- | --- |
| HTTPBackend.Timeout | 15 秒 |
| HTTPBackend.MaxResponseBytes | 4 MiB，最大允许 64 MiB |
| PackageManager.Timeout | 下载总预算 30 分钟 |
| PackageManager.MaxBytes | 单包 10 GiB |

零值使用默认，负值无效。目录非 2xx 和下载非 200 返回可用 errors.As 检查的 `*artifact.StatusError`。
SHA-256 必须是有效的 64 位十六进制摘要，Size 必须非负；实际下载同时验证长度和摘要，拒绝意外压缩编码与重定向。
临时文件在目标同目录，只有验证和同步完成后才无覆盖发布；失败清理临时文件。
目录可用不代表下载端点可用，两个阶段的超时和认证相互独立。

验证：模块内 `GOWORK=off go test -race ./...` 使用本地目录与文件服务，不代表已兼容第三方制品平台。
