# utils：格式化、归档与磁盘空间

[LLM 使用指南](AI_GUIDE.md)：按当前版本查找能力、源码入口和调用约定。

[返回总览](../README.md) · [文件树](../filetree/README.md) · [文件下载](../download/README.md)

Go 1.21+，独立标准库模块。函数按需调用，没有全局初始化。

```sh
go get github.com/bkcarlos/goparts/utils@v0.1.0
```

## 小工具

| 函数 | 输入示例 | 结果 / 语义 |
| --- | --- | --- |
| `FormatDuration` | `1500*time.Millisecond` | `"1.5s"`；舍入到毫秒 |
| `FormatBytes` | `1536` | `"1.50 KiB"`；使用二进制单位 |
| `GetVersionHash` | 构建元数据字符串 | SHA-256 前 8 字节的 16 位十六进制字符串，不是文件校验接口 |
| `GetEnvOrDefault` | 环境变量名称与 fallback | 未设置才回退，已经设置为空字符串不会回退 |

## 创建归档

```go
package example

import (
    "github.com/bkcarlos/goparts/utils"
    "github.com/bkcarlos/goparts/utils/diskspace"
)

func ArchiveBuild(sourceDir, archivePath, outputDir string) error {
    if err := diskspace.CheckDiskSpace(outputDir, 100<<20); err != nil {
        return err
    }
    return utils.CreateTarGz(sourceDir, archivePath)
}
```

调用例如 `ArchiveBuild("./build", "./dist/build.tar.gz", "./dist")`，输出父目录需已经存在。
100 MiB 只是示例预算，不是自动估算压缩结果；业务应按数据规模提供预算。

`CreateZip(source, destination)` 与 `CreateTarGz` 都使用相对归档路径，拒绝符号链接与特殊文件，
目标必须位于源目录之外且不能已经存在。先写临时文件再发布，失败清理临时文件；
不会覆盖目标。工具目前只创建归档，不提供解压 API。

## 磁盘空间

`diskspace.Available(path)` 返回 path 所在文件系统可用字节，
`CheckDiskSpace(path, required)` 不足时返回可用 `errors.Is(err, diskspace.ErrInsufficient)` 判断的错误。
path 必须存在；路径或系统调用错误直接返回。支持 Linux/macOS/BSD、Windows；其他平台返回不支持。

空间检查不会预留磁盘，检查通过后实际写入仍可能失败，应继续处理写入错误。

验证：模块内 `GOWORK=off go test -race ./...`，归档测试使用临时文件；磁盘空间测试只查询本地文件系统。
