# persistcache

`New(path, String/Int/Int64/JSON[K]())` 创建 `PersistentCache[K comparable]`。
缓存存储最后更新时间，提供 ShouldUpdate、MarkUpdated、GetLastUpdate、Has、
Delete、Clear、Keys、Len。ShouldUpdate 的 expire<=0 表示总需要更新。
写入异步合并为 JSON 文件，原子替换、0600 权限，默认文件上限 16 MiB。
`Save()` 显式同步持久化；`LastError()` 读取异步错误；退出前必须 `Close()`，
它等待保存工作完成并做最后一次同步写入。一个文件只交给一个缓存实例拥有，
不提供跨进程事务。序列化器必须可逆且不同 key 不可编码成同一字符串。
