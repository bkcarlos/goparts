# utils

小型独立工具：FormatDuration（毫秒舍入）、FormatBytes（二进制单位）、
GetVersionHash（元数据字符串 SHA-256 前 8 字节）、GetEnvOrDefault（已设置空值不回退）。

CreateZip/CreateTarGz(source,destination) 生成相对路径归档，拒绝符号链接和特殊文件，
目标须在源目录之外，不覆盖已有目标。先写临时文件，成功后发布；失败清理临时文件。
`utils/diskspace.Available` 返回可用字节；CheckDiskSpace 检查是否足够，
不足返回可用 errors.Is 判断的 ErrInsufficient。支持 Linux/macOS/BSD 和 Windows，
其他平台显式返回不支持。空间检查不是预留，仍应处理实际写入时空间不足。
