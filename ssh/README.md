# ssh

独立模块，**Go 1.26+**，使用当前 `golang.org/x/crypto/ssh` 和 `pkg/sftp`。

NewSSHClient(ctx, SSHConfig) 按 Hosts 顺序连接第一个成功主机，默认端口 22。
认证可组合 Password、PrivateKey（支持加密私钥）、Agent；Auth 留空仅适用于服务端
明确允许的无认证模式。必须提供 KnownHostsFile 或 HostKeyCallback，不默认跳过主机验证。

ExecuteCommand 返回独立 stdout/stderr 和退出码，默认每路上限 4 MiB；命令由业务决定，
不要拼接不可信 shell 字符串。ExecuteCommandWithSudo 对命令整体引用，密码经 stdin 传入，
不写入命令参数。默认连接/命令超时 15 秒，可配置，输出不隐式写日志。
取消已运行命令关闭 session；取消握手/开通道可能关闭整个连接，此时池应标记 broken。

TransferFileWithProgress / DownloadFileWithProgress 使用 SFTP，默认单文件限 10 GiB；
上传用远端临时文件再 Rename，是否替换已有远端文件取决于服务器 Rename 语义。
本地下载不覆盖已有目标。DownloadDirectory 递归下载，拒绝符号链接和不安全路径；
ResolveSymlink 显式解析远端路径，CheckDiskSpace 使用 statvfs 扩展（不支持会返回错误）。
SFTP 操作应传带 deadline 的 context。目录中已完成文件在后续失败时保留。

NewPool(cfg,size).Acquire(ctx) 返回 client 和 release(broken)，必须调用 release。
池限制租用数量并复用连接；Close 拒绝新租用、关闭空闲连接，租用中的连接归还时关闭。
