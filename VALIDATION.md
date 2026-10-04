# v0.1.0 验证记录

日期：2026-10-05（Asia/Shanghai）。仓库按模块独立发布，共 23 个模块；清单见 Makefile。

## 测试增强（v0.1.0 后续提交）

2026-10-05 新增 16 个测试文件、45 个测试函数（含表驱动子用例）、2 个 fuzz 目标。
本次没有修改生产实现、依赖版本或已发布标签。

- 工作池：关闭后拒绝提交、排队任务取消、排空等待、解除阻塞提交者、观察事件中的任务错误。
- 缓存：取消不修改数据、坏文件/大小边界、加载与写入失败后重试、panic 恢复、返回值副本隔离。
- 持久化与日志：序列化冲突/失败保留旧文件、并发关闭落盘、轮转失败和并发写入不丢失完整日志记录。
- 熔断与重试：旧请求完成不重置新状态、探测失败重新打开、失败分类、并发令牌上限、重试条件与观察事件。
- 飞书：多维表格 CRUD 请求与响应、分页与用户身份、Wiki 链接解析、通讯录边界、卡片 1.0 JSON、事件去重容量/取消/失败重试。
- LLM：非法工具参数不执行处理器、会话失败不修改历史、token 预算、工具调用顺序、流式聚合失败/大小限制、并发序号唯一性。
- 下载与 HTTP：损坏分片重新下载、非法 Range 响应、分片关闭失败保护目标文件、鉴权/CORS/请求 ID 信任边界、日志不包含查询参数。

本地验证：`make check`（23 模块 race/cover/vet 与集成）、`make stress`（20 轮）、
`make fuzz`（每个目标 10 秒，2 个 worker）均通过。此次 fuzz 分别执行约 30 万次 Range 输入、
15.5 万次工具参数输入；这是限时采样结果，不是完整输入空间验证。

覆盖率为同一平台上的包内语句覆盖率，普通 `go test -cover` 不统计其他包测试对该包的覆盖：

| 包 | 增强前 | 增强后 |
| --- | ---: | ---: |
| cache | 65.5% | 81.3% |
| persistcache | 62.6% | 83.7% |
| safemap | 48.4% | 100.0% |
| utils/diskspace | 0.0% | 92.3% |
| middleware | 77.0% | 91.8% |
| ratelimit | 78.8% | 85.0% |
| workerpool | 84.7% | 90.8% |
| logger | 61.5% | 66.0% |
| retry | 65.8% | 97.4% |
| llm/chat | 70.5% | 95.8% |
| feishu/bitable | 0.0% | 98.3% |
| feishu/contact | 0.0% | 92.9% |
| feishu/wiki | 0.0% | 96.0% |
| feishu/card/legacy | 0.0% | 100.0% |
| feishu/dedup | 78.9% | 92.1% |
| download | 76.3% | 79.2% |

飞书业务子包增强前已有部分跨包协议检查；表中的 0% 不等于此前没有任何间接测试。
SSH 等未列出的包覆盖率未改变，仍需后续补充；覆盖率 100% 也不代表所有组合或真实服务行为已验证。

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
