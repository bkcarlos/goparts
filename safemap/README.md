# safemap：并发 Map 与有序 Map

[返回总览](../README.md) · [带 TTL 的缓存](../cache/README.md)

Go 1.21+，提供 `SafeMap[K,V]` 与 `OrderedSafeMap[K,V]`；零值可用，key 必须 comparable。
用于需要频繁快照或遍历的进程内数据，不提供 TTL、持久化或事务。

```sh
go get github.com/bkcarlos/goparts/safemap@v0.1.0
```

## 常见用法

```go
package example

import "github.com/bkcarlos/goparts/safemap"

func OrderedNames() []string {
    names := safemap.NewOrdered[string, string]()
    names.Set("a", "Alice")
    names.Set("b", "Bob")
    names.Set("a", "Alicia") // 更新不改变 a 的顺序。
    return names.Values()     // ["Alicia", "Bob"]
}

func RemoveDisabled(states *safemap.SafeMap[string, bool]) {
    states.Range(func(key string, enabled bool) bool {
        if !enabled { states.Delete(key) }
        return true
    })
}
```

| 操作 | 约定 |
| --- | --- |
| `Set` / `Get` | 设置、读取；Get 的 bool 区分缺失与零值 |
| `GetOrDefault` | 只有 key 不存在才返回 fallback |
| `Delete` / `Clear` | 不存在可删除；Clear 后可继续使用 |
| `Keys` / `Values` / `Len` | 各自取当前快照；普通 Map 不承诺顺序 |
| `Snapshot` | 普通 Map 返回新 map，有序 Map 返回 `[]Pair[K,V]` |
| `Range` | 对快照遍历，回调返回 false 提前结束 |

OrderedSafeMap 删除后重新插入的 key 移到末尾。Range 不持锁执行回调，因此回调可修改原 Map；
遍历视图仍是开始时取得的快照，新加元素不保证被本次 Range 看到。

快照只复制容器，value 内部的 map、slice、pointer 仍共享底层数据，需要业务自己同步或复制。
多次方法调用不是原子操作：不要用 Has+Set 去重，或 Get+Set 做并发计数。
并发修改时 Keys 和 Values 两次调用可能来自不同快照；需要对应关系请用一次 Snapshot。

验证：模块内 `GOWORK=off go test -race ./...`；覆盖并发写入、顺序、快照隔离和回调中修改。
