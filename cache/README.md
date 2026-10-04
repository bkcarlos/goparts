# cache：TTL 缓存与并发加载合并

[返回总览](../README.md) · [Redis 适配器](redis/README.md) · [更新时间缓存](../persistcache/README.md)

独立模块，Go 1.21+。适合缓存 API 结果、配置快照等字节数据；对象的 JSON 编解码由业务决定。

```sh
go get github.com/bkcarlos/goparts/cache@v0.1.0
```

## 内存缓存与 Loader

```go
package example

import (
    "context"
    "time"

    "github.com/bkcarlos/goparts/cache"
)

func LoadProfile(ctx context.Context, loader *cache.Loader, userID string,
    fetch func(context.Context) ([]byte, error)) ([]byte, error) {
    return loader.LoadOrFetch(ctx, "profile:"+userID, 5*time.Minute, fetch)
}

func NewProfileCache() (*cache.Loader, error) {
    store, err := cache.NewMemory(1024)
    if err != nil { return nil, err }
    return &cache.Loader{Store: store}, nil
}
```

在应用启动时创建 Loader 并复用。每次调用都创建新 Loader 会失去并发 miss 合并效果。
如果 key 中缺少租户、身份或查询条件，会错误复用其他请求的数据，这些维度应由业务编码进 key。

| 操作 | 返回和约定 |
| --- | --- |
| `Get(ctx, key)` | `value, hit, err`；miss 是 `hit=false, err=nil`，不能只用 value 是否为空判断 |
| `Set(ctx, key, value, ttl)` | TTL=0 不过期；负 TTL 报错；内存实现复制输入切片 |
| `Delete(ctx, key)` | key 不存在也成功 |
| `LoadOrFetch` | 命中直接返回；同 Loader、同 key 的并发 miss 合并 fetch；返回独立切片 |

发起方 Context 控制 fetch，等待者可以独立取消，不会因单个等待者取消而中止发起方。
fetch 错误不缓存，panic 转成错误；Store.Get/Set 错误继续返回，不静默伪装成成功。
fetch 不能递归请求同一个 Loader 的同一个 key。

## 实现与配置

| 实现 | 默认值 | 适用范围 |
| --- | --- | --- |
| `NewMemory(maxEntries, observer...)` | maxEntries=0 使用 1024；负值无效 | 进程内缓存，达到容量时淘汰最旧写入项，不是 LRU |
| `NewFile(dir, maxBytes)` | maxBytes=0 使用 16 MiB；最大 1 GiB | 本地 CLI 缓存；key 用 SHA-256 转成文件名 |
| `cache/redis.New(...)` | 独立模块，默认单值 16 MiB | 共享 Redis 数据，连接生命周期由业务管理 |

内存 observer 观测 Get 的命中、耗时和错误，需支持并发。文件实现以 0600 临时文件原子替换，
读取损坏或超限条目返回错误；过期文件只被视为 miss，不会自动删除，调用方可 Delete 或定期清理。
文件缓存不加密，用户令牌应使用飞书用户模块的加密存储。

Store 是 Get/Set/Delete 的小接口，可以替换供应商。Loader 的合并只在本实例内生效，
即使底层是 Redis，也不自动提供跨进程锁或分布式 singleflight。

## 验证

模块内 `GOWORK=off go test -race ./...`；测试覆盖副本隔离、TTL、取消不修改值、
坏文件、大小限制、并发合并和失败后的下一次加载。
