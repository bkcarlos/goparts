# cache

`Store` 提供 Get/Set/Delete，值为字节，TTL 为零表示不过期，负值报错。
`NewMemory(maxEntries)` 有容量上限、过期检查和最旧写入淘汰；可传观测回调记录命中率。
`NewFile(dir, maxBytes)` 使用 key 的 SHA-256 文件名、0600 临时文件和原子替换，
适合 CLI。文件缓存不自动清理过期文件，Get 将过期项视为 miss，Delete 可清理。
文件实现单条默认限 16 MiB，不加密，不适合直接存秘密；加密令牌使用 feishu/user。

`Loader{Store: store}.LoadOrFetch(ctx,key,ttl,fetch)` 对同一 Loader 的并发 miss
合并请求；返回独立值快照。发起方 context 控制 fetch，等待者可独立取消。
失败不缓存，panic 转为错误，不遗留等待者。不同进程的协调需分布式存储和锁适配。
Redis 可通过 Store 接口注入；当前无强制 Redis 客户端依赖。

需要 Redis 时单独引入 [cache/redis](redis/README.md) 可选模块（Go 1.26+），
通过 go-redis Client 注入，基础缓存仍保持 Go 1.21 且不依赖 Redis。
