# cache/redis

可选独立模块，Go 1.26+。`New(redisClient,prefix,maxBytes)` 实现 cache.Store 的
Get/Set/Delete 结构接口，默认单值上限 16 MiB，零 TTL 不过期。连接和认证由调用方
配置 go-redis Client 并负责 Close；本模块不会自动扫描/连接 Redis。
可将此 Store 注入 cache.Loader，避免基础 cache 模块依赖 Redis。
Loader 的 singleflight 仅进程内；本适配器不把普通缓存操作伪装成跨进程事务锁。
