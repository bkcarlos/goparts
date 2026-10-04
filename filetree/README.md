# filetree：遍历文件树与组合过滤

[LLM 使用指南](AI_GUIDE.md)：按当前版本查找能力、源码入口和调用约定。

[返回总览](../README.md) · [归档工具](../utils/README.md) · [目录上传](../storage/README.md#目录上传与兼容校验)

Go 1.21+，基于标准库 WalkDir。适合扫描待归档、上传或同步的本地文件。

```sh
go get github.com/bkcarlos/goparts/filetree@v0.1.0
```

## 筛选文件

```go
package example

import "github.com/bkcarlos/goparts/filetree"

func ConfigFiles(root string) ([]string, error) {
    return filetree.GetFileTreeFilesOnly(root,
        filetree.ExcludeHidden,
        filetree.FilterByExtension(".yaml", ".yml", ".json"),
    )
}
```

`GetFileTreeFilesOnly` 返回文件路径切片；`GetFileTree` 返回 `*Node`，
包含 Name、Path、Directory、Size、Children。Path 随 root 使用相对或绝对路径，不自动转换为绝对路径。
文件列表是本次扫描的快照，后续读文件仍需处理被删除或变化的情况。

## 过滤规则

| 过滤器 | 规则 |
| --- | --- |
| `FilterByExtension(exts...)` | 扩展名忽略大小写，可带或不带前导点 |
| `FilterByPattern(pattern)` | basename 的 filepath.Match glob；模式无效时返回错误 |
| `FilterByPrefix` / `FilterBySuffix` | 按 basename 前缀/后缀匹配，区分大小写 |
| `ExcludeHidden` | 路径任意一段以点开头时排除，隐藏目录整棵子树跳过 |
| `Combine(filters...)` | 所有非 nil 条件都通过才保留，即 AND |

文件名、扩展名等内置过滤器放行目录以继续遍历；自定义 Filter 接收斜杠分隔的相对路径和 DirEntry，
拒绝目录就剪掉其子树。根节点不经过过滤器。

遍历按 WalkDir 字典序进行，不跟随符号链接；链接自身仍可能进入结果，不能把这个列表等同于
“全部都是普通文件”。归档或上传前仍应执行相应文件类型检查。扫描失败返回错误；树形式的返回值
可能只包含部分结果，应先检查错误，文件列表形式失败则返回 nil。

验证：模块内 `GOWORK=off go test -race ./...`，使用临时目录检查过滤、剪枝和符号链接。
