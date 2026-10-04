# ssh：SSH 命令与 SFTP 传输

[LLM 使用指南](AI_GUIDE.md)：按当前版本查找能力、源码入口和调用约定。

[返回总览](../README.md) · [版本更新](../version/README.md)

独立模块，**Go 1.26+**。使用 golang.org/x/crypto/ssh 与 pkg/sftp；业务通过本模块的 Client 操作。

```sh
go get github.com/bkcarlos/goparts/ssh@v0.1.0
```

## 使用私钥执行命令

以下函数接收调用方读取的私钥和已验证的 known_hosts 文件路径，不自动读取私钥文件。

```go
package example

import (
    "context"
    "errors"
    "time"

    partsssh "github.com/bkcarlos/goparts/ssh"
    gossh "golang.org/x/crypto/ssh"
)

func RemoteHealth(ctx context.Context, host, user, knownHosts string,
    privateKey []byte) (result partsssh.CommandResult, err error) {
    auth, err := partsssh.PrivateKey(privateKey, nil)
    if err != nil { return result, err }
    client, err := partsssh.NewSSHClient(ctx, partsssh.SSHConfig{
        Hosts: []string{host},
        User: user,
        Auth: []gossh.AuthMethod{auth},
        KnownHostsFile: knownHosts,
        Timeout: 15*time.Second,
        MaxOutputBytes: 1<<20,
    })
    if err != nil { return result, err }
    defer func() { err = errors.Join(err, client.Close()) }()
    return client.ExecuteCommand(ctx, "uname -s")
}
```

Hosts 可填写 `host:port`，省略端口默认 22；多个地址按顺序尝试，连接第一个成功地址。
这不是对多台机器广播命令。known_hosts 路径不会展开 `~`，由应用传入实际路径。

## 连接配置

| 参数 | 默认 / 要求 |
| --- | --- |
| Hosts / User | 至少一个地址和非空用户名 |
| Auth | 可组合 Password、PrivateKey、Agent；空值只适用于服务端允许的无认证模式 |
| KnownHostsFile / HostKeyCallback | 至少一个；显式 callback 优先，默认不跳过主机验证 |
| Timeout | 15 秒，每次连接/命令的预算；更早的 Context deadline 生效 |
| MaxOutputBytes | stdout 与 stderr 每路 4 MiB，超限返回错误 |
| MaxFileBytes | 单文件 10 GiB |

PrivateKey 第二个参数可传加密私钥的口令。`Agent(ctx, socket)` 返回认证方法和需要关闭的连接，
socket 为空时读取 SSH_AUTH_SOCK；在相关认证全部完成前不要关闭 agent 连接。

## 命令和文件

| 方法 | 结果 / 行为 |
| --- | --- |
| `ExecuteCommand` | 返回 Stdout、Stderr、ExitCode；非零退出同时返回错误，连接等错误时退出码可为 -1 |
| `ExecuteCommandWithSudo` | 命令整体作为 sh -c 参数引用，密码通过 stdin，不放命令参数；密码不能含换行 |
| `TransferFileWithProgress` | SFTP 上传到远端临时文件后 Rename |
| `DownloadFileWithProgress` | SFTP 下载到本地临时文件，不覆盖已有本地目标 |
| `DownloadDirectory` | 递归下载，拒绝符号链接和不安全路径；后续失败保留已完成文件 |
| `ResolveSymlink` | 显式解析远端路径 |
| `CheckDiskSpace` | 使用服务端 statvfs 扩展；不支持时返回错误 |

传输进度回调类型为 `func(written, total int64) error`，返回错误可中止；应快速返回。
SFTP 操作应传带 deadline 的 Context。远端 Rename 是否覆盖已有文件由服务端语义决定。
命令和路径由业务选择，不拼接未经校验的外部 shell 字符串；标准输出不自动记录到日志。

## 连接池与取消

`NewPool(cfg, size)` 限制同时租用数量；`Acquire(ctx)` 返回 client、`release(broken)` 和 error。
每次成功租用后必须归还。传输失败或连接不再可用时传 true，普通命令非零退出本身不代表连接损坏。
Pool.Close 拒绝新租用并关闭空闲连接；仍被租用的连接在归还时关闭。

取消运行中的命令会关闭 session；取消 SSH 握手或开通道可能关闭整条连接，此时不能直接放回池中复用。
SSHConfig 在初始化后视为只读，业务不应并发修改 Auth/Hosts。

## 验证

模块内 `GOWORK=off go test -race ./...` 使用本地 SSH/SFTP 协议服务器。
真实服务器的认证、known_hosts、sudo 策略和 statvfs 支持需在实际部署环境验证。
