# persistcache：持久化更新时间

[返回总览](../README.md) · [业务值缓存](../cache/README.md)

Go 1.21+，保存 key 的最后更新时间，适合 CLI 同步、扫描、抓取等任务。
它不保存业务值；需要缓存内容和 TTL 时使用 cache。

```sh
go get github.com/bkcarlos/goparts/persistcache@v0.1.0
```

## 在工作成功后标记

```go
package example

import (
    "context"
    "errors"
    "time"

    "github.com/bkcarlos/goparts/persistcache"
)

func SyncIfNeeded(ctx context.Context, path, key string,
    syncData func(context.Context) error) (err error) {
    state, err := persistcache.New(path, persistcache.String)
    if err != nil { return err }
    defer func() { err = errors.Join(err, state.Close()) }()
    if !state.ShouldUpdate(key, time.Hour) { return nil }
    if err = syncData(ctx); err != nil { return err }
    return state.MarkUpdated(key)
}
```

示例适合单次 CLI 调用。同一进程处理多个任务时应复用一个实例；不要让多个实例同时管理同一文件。
只有实际操作成功后才 MarkUpdated，避免失败任务被错误地跳过。

## 接口与持久化

| 方法 | 说明 |
| --- | --- |
| `ShouldUpdate(key, expire)` | key 不存在或已经过期返回 true；expire<=0 总是 true |
| `MarkUpdated(key)` | 写入当前时间，通知后台合并保存；成功不代表磁盘已写入 |
| `GetLastUpdate` / `Has` | 查询时间和存在性 |
| `Delete` / `Clear` | 修改内存并通知后台保存 |
| `Keys` / `Len` | 当前 key 快照和数量，不承诺顺序 |
| `Save()` | 同步保存，可直接检查写入错误 |
| `LastError()` | 最近一次后台保存的错误；后续成功可清除旧错误 |
| `Close()` | 停止接受修改，等待后台结束，再同步保存；须检查返回值 |

String、Int、Int64 提供常见 key 编解码；`JSON[K]()` 支持可比较的结构体 key。
自定义 Serializer 的 Encode/Decode 必须可逆，编码不得碰撞。保存发现碰撞或编码失败时返回错误，
不会发布新的文件；读取坏 JSON、非法时间或无法解码的 key 时 New 失败，不自动丢弃原文件。

单文件上限 16 MiB，目录按需创建为 0700，临时文件为 0600；通过同目录原子替换保存。
该文件不提供加密或跨进程事务，ShouldUpdate+MarkUpdated 也不是并发任务锁。
关闭后 MarkUpdated/Delete/Clear 返回可用 `errors.Is(err, os.ErrClosed)` 判断的错误。

验证：本目录 `GOWORK=off go test -race ./...`；覆盖并发关闭、损坏文件、编码失败和旧文件保护。
