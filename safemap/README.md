# safemap

并发安全泛型 `SafeMap[K,V]` / `OrderedSafeMap[K,V]`，零值可用。
共同提供 Set/Get/GetOrDefault/Has/Delete/Clear/Keys/Values/Len/Snapshot/Range。
OrderedSafeMap 保留插入顺序，更新不移动位置，删除再插入放到末尾。
Snapshot 和 Range 不持锁调用用户逻辑；容器被复制，但 value 内的指针仍由业务负责同步。
多个方法组合不是原子事务，不要用 Has+Set 实现并发去重。
