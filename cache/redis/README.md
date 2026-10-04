# cache/redis：可选 Redis 适配器

[LLM 使用指南](AI_GUIDE.md)：按当前版本查找能力、源码入口和调用约定。

[基础缓存](../README.md) · [返回总览](../../README.md)

这是有独立 go.mod 的嵌套模块，要求 **Go 1.26+**。基础 cache 模块不强制引入 Redis。

```sh
go get github.com/bkcarlos/goparts/cache@v0.1.0
go get github.com/bkcarlos/goparts/cache/redis@v0.1.0
```

## 连接与加载

以下函数接收业务已经配置好的 Redis Client；连接、认证、TLS 与关闭由创建者管理。

```go
package example

import (
    "github.com/bkcarlos/goparts/cache"
    cacheredis "github.com/bkcarlos/goparts/cache/redis"
    goredis "github.com/redis/go-redis/v9"
)

func NewSharedCache(client *goredis.Client) (*cache.Loader, error) {
    store, err := cacheredis.New(client, "my-service:prod:", 4<<20)
    if err != nil { return nil, err }
    return &cache.Loader{Store: store}, nil
}
```

Client 可由 `goredis.NewClient` 创建，并在应用退出时 Close。适配器 New 不执行 Ping，
构造成功不表示连接可用；需要启动时验证连接的应用应显式调用 client.Ping(ctx)。
不要在仍有任务使用 Loader 时先关闭 Redis Client。

## 语义与边界

- `New(client, prefix, maxBytes)` 的 maxBytes=0 使用 16 MiB，负值无效；prefix 直接拼接到 key 前，不自动增加分隔符。
- Get 返回 `[]byte, hit, error`；Redis Nil 映射为 miss，其余错误保留。超限数据返回错误。
- Set 的 TTL=0 表示不过期，负 TTL 或值超过上限报错；Delete 对不存在的键成功。
- 通过结构接口实现 cache.Store，不要求适配器导入基础 cache 包；go-redis Cmdable 的其他实现也可注入。
- Loader 的并发加载合并仍只在进程内；这里没有分布式锁、事务或自动重试策略。

从本目录执行 `GOWORK=off go test -race ./...`。测试使用 miniredis，不需要真实 Redis 实例；
真实认证、TLS、集群拓扑和故障转移应在部署环境验证。
