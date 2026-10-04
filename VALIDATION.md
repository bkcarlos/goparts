# v0.1.0 验证记录

日期：2026-10-05（Asia/Shanghai）。仓库按模块独立发布，共 23 个模块；清单见 Makefile。

## 已验证

- 本地 macOS/arm64，Go 1.27.1：完整 `make check` 通过，包括各模块 `go test -race -cover`、`go vet` 和 workspace 集成测试。
- 随后的 HTTP Flush 透传、范围 Body 关闭错误处理、用户刷新锁/轮询与回调去重测试，针对相关模块的 race/vet 回归通过。
- [实现提交 2794348 的远程 CI](https://github.com/bkcarlos/goparts/actions/runs/37215012725)：23 个模块任务和 1 个集成任务全部成功。模块任务包括 race/cover、vet、govulncheck、Windows/amd64 编译。
- SSH/SFTP 使用真实本地协议服务器；Redis 使用 miniredis；HTTP、飞书和 LLM 使用本地模拟服务。
- 集成覆盖 HTTP+retry+apperror+logger、storage+download、storage+version 和流式聚合+按 key 序号更新。

本地漏洞数据库访问曾遇到 TLS 超时，因此全量漏洞验收采用远程 CI。检查器须使用当前 Go
编译；用旧 Go 构建的检查器可能无法解析新标准库语法。CI 使用 stable 工具链安装检查器。
govulncheck 未发现可达漏洞；例如 x/crypto 模块含 openpgp 的安全公告，但项目未导入
或调用该包。该结论不是对所有依赖包或未来漏洞的永久保证。

## 发布与未验证边界

版本标签形式为 `<module>/v0.1.0`，包含可选 `cache/redis/v0.1.0`。
生产调用方使用 `go get github.com/bkcarlos/goparts/<module>@v0.1.0` 固定版本。
ssh/cache/redis 要求 Go 1.26+，其余模块声明 Go 1.21；完整 workspace 要求 Go 1.26+。
标签推送不触发重复的全矩阵任务，分支提交和 PR 仍执行 CI。

未进行真实飞书授权、真实 OSS 上传、付费模型请求或生产主机连接；这些需使用实际应用权限
和部署配置联调。未接入 BOS 专有协议，未声明兼容不可访问的 toolkits 旧接口格式；用户已明确跳过。
