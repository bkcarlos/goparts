# version

供应商无关的 Publisher / Updater。StorageProvider 仅需原子 Put 和返回 owned
reader 的 Open；不存在映射为 version.ErrNotFound。StorageAdapter 通过函数注入，
可接 storage.Client（阿里云或其他 Backend），不强制依赖对象存储 SDK。

```go
adapter := version.StorageAdapter{
    PutFunc: func(ctx context.Context, key string, r io.Reader, size int64) error {
        _, err := objects.Put(ctx, key, r, size, storage.PutOptions{})
        return err
    },
    OpenFunc: func(ctx context.Context, key string) (io.ReadCloser, error) {
        r, err := objects.Get(ctx, key, storage.GetOptions{})
        if errors.Is(err, storage.ErrNotFound) { return nil, version.ErrNotFound }
        if err != nil { return nil, err }
        return r, nil
    },
}
publisher := &version.Publisher{Store:adapter, Workers:4}
release, err := publisher.UploadRelease(ctx, "v0.1.0", buildTime, commitID,
    []version.BinFileInfo{{Path:"./bin/app", Platform:"linux", Arch:"amd64"}})
```

依次上传二进制（记录 SHA-256/MD5）、不可变 release.json、index.json，最后更新 latest.json。
相同 release 版本拒绝覆盖。一个前缀必须由一个发布者或外部分布式锁保护，跨机器并发发布
不由普通对象存储 Put 自动提供事务。失败可能留下未引用文件，调用方可检查后清理。

Updater 提供 GetLatestVersion / CheckForUpdate / Release / DownloadPlatform / DownloadFile /
Update；平台和架构默认 runtime.GOOS/GOARCH。版本是 opaque ID，是否更新按 latest 指针与
当前 ID 是否不同判断，不做语义版本大小推断。多个同平台文件时显式 DownloadFile 选文件。
下载默认上限 2 GiB，必须有 SHA-256（或兼容旧清单的 MD5），严格检查长度，临时写入后发布。
Update 保留 `<target>.bak`、沿用权限，再原子替换；Rollback 恢复备份。已有备份拒绝覆盖。
Windows 运行中的可执行文件可能被锁定，应由外部 launcher 停止应用后替换。
清单必须来自可信存储；checksum 校验传输损坏，不等于软件签名验证。

## 安装与最小更新流程

Go 1.21+，`version` 管理的是应用二进制发布，不负责发布本仓库的 Go module 标签。

```sh
go get github.com/bkcarlos/goparts/version@v0.1.0
```

下面接收已初始化的 StorageProvider，下载指定平台的二进制到一个新文件；下载成功后再由部署流程切换。

```go
package example

import (
    "context"

    "github.com/bkcarlos/goparts/version"
)

func DownloadUpdate(ctx context.Context, store version.StorageProvider,
    current, destination string) (version.UpdateInfo, error) {
    updater := &version.Updater{Store: store, MaxBytes: 2<<30}
    update, err := updater.CheckForUpdate(ctx, current)
    if err != nil || !update.Available { return update, err }
    _, err = updater.DownloadPlatform(ctx, update.Release.Version, "", "", destination)
    return update, err
}
```

空 platform/arch 使用 runtime.GOOS/GOARCH；目标父目录需存在，普通下载不覆盖已有文件。
多个文件属于同一个平台时，DownloadPlatform 无法替业务选文件，应从 Release.Files 中选择后调用 DownloadFile。
如果要直接替换本地目标，使用 `updater.Update(ctx, current, target)`，它创建 `.bak`；回滚调用 `version.Rollback(target)`。

## 存储布局和失败处理

| 对象键 | 内容 | 更新方式 |
| --- | --- | --- |
| `releases/<version>/<platform>/<arch>/<name>` | 二进制数据 | 发布过程中写入 |
| `releases/<version>/release.json` | 版本、构建时间、commit、文件列表与校验和 | 同版本已有清单时拒绝重复发布 |
| `index.json` | 历史版本列表及最新版本 | 发布后更新 |
| `latest.json` | 当前最新版本 ID | 所有发布步骤成功后最后更新 |

这些是相对 StorageProvider 的键；接 storage.Client 时可用 BasePath 为不同应用隔离。
StorageProvider.Put 必须对单个 key 原子替换，Open 将不存在映射成 version.ErrNotFound，并返回由模块关闭的流。
协议不具备跨对象事务，单个前缀需外部串行化发布；失败可能留下未引用数据，不能随意删除其他发布者的对象。

`CheckForUpdate` 按 ID 是否不同判断，不做大小比较；把 latest 切到旧 ID 也会提示可更新。
下载必须验证清单中的 SHA-256（兼容旧清单的 MD5），`errors.Is(err, version.ErrChecksum)` 可识别不匹配。
清单是信任入口：摘要能检测传输损坏，不能替代发布者签名或权限控制。

## 验证

本目录 `GOWORK=off go test -race ./...`；仓库 `make integration` 覆盖 storage 适配、
发布清单、文件校验、更新和回滚。真实云存储的并发发布与运行中二进制替换须按部署环境验证。
