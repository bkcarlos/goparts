# filetree

`GetFileTree(root, filters...)` 返回树，`GetFileTreeFilesOnly` 返回文件路径快照。
过滤器可组合：FilterByExtension、FilterByPattern（basename glob）、
FilterByPrefix、FilterBySuffix、ExcludeHidden、Combine。
扩展名/文件名过滤保留目录遍历；自定义 Filter 拒绝目录则剪掉其子树。
遍历顺序遵循 WalkDir 的字典序，不跟随符号链接，避免链接环；文件列表会包含链接本身。
