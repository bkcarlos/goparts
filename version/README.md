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
