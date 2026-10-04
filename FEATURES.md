# 完整需求验收清单

来源：远程 issues [#1](https://github.com/bkcarlos/goparts/issues/1)、[#2](https://github.com/bkcarlos/goparts/issues/2)、[#3](https://github.com/bkcarlos/goparts/issues/3)。完成表示实现、文档和本地测试已通过；真实服务联调另记。供应商范围按用户确认先支持阿里云。

| 能力 | 状态 |
| --- | --- |
| 飞书附件、长连接、Retry-After、通用存储及阿里云、单流下载 | 已提交，本地测试通过 |
| 错误映射、Registry/guidance、结构化属性、Join 全分支 | 已实现，测试通过 |
| 日志脱敏、轮转、全局入口、HTTP trace | 已实现，测试通过 |
| 配置 embed/多层合并/注册表、指针和嵌套校验、通用规则 | 已实现，测试通过 |
| HTTP 钩子、受控错误 Body 预览 | 已实现，测试通过 |
| 生命周期分阶段退出及任务预算、retry 便捷入口/随机源 | 已实现，测试通过 |
| HTTP middleware | 已实现，测试通过 |
| 限流、熔断、有界任务池、按 key 串行 | 已实现，测试通过 |
| TTL/文件/Redis 缓存、singleflight、持久化更新缓存 | 已实现，测试通过 |
| metrics 接口与 OTel 适配、组件观测钩子 | 已实现，测试通过 |
| Bitable/Wiki/Contact 业务 API、旧版卡片 | 已实现，测试通过 |
| 多用户会话、身份选择、登录轮询、TokenCache 与事件去重 | 已实现，测试通过 |
| LLM Responses 普通/流式 | 已实现，测试通过 |
| LLM 会话、工具注册/校验/调度、聚合节流、Sequence | 已实现，测试通过 |
| HTTP Range 分片/续传、存储目录操作、MD5 | 已实现，测试通过 |
| SSH/SFTP、多主机、池、远程文件/空间/符号链接 | 已实现，测试通过 |
| 版本发布/索引/检查/下载/校验/替换、存储抽象 | 已实现，测试通过 |
| 制品库通用客户端和 BOS 兼容 | 通用客户端已实现、测试通过；BOS 专有协议按用户确认跳过 |
| SafeMap/OrderedMap、文件树过滤、格式化/压缩/磁盘空间 | 已实现，测试通过 |
| logger/retry/errx 下游旧 API 精确兼容 | 精确兼容按用户确认跳过；通用入口已实现 |
| CI、CHANGELOG、CONTRIBUTING、模块版本标签 | 已实现；23 模块 CI 与集成检查通过，v0.1.0 按模块发布 |
| LICENSE | MIT，已添加 |

## 验证与排除项

- 实现提交 `2794348` 的 [GitHub Actions](https://github.com/bkcarlos/goparts/actions/runs/37215012725) 全部通过：23 模块 race/cover、vet、govulncheck、Windows 编译及独立集成任务。
- 本地 macOS/arm64 完整 `make check` 通过，最后的小修复另做针对性回归；发布记录见 [VALIDATION.md](VALIDATION.md)。
- 漏洞扫描结论为未发现可达漏洞；依赖模块可能含未导入包的公告，不等于整份依赖树没有任何公告。
- 明确跳过：BOS 专有协议、无法获得源码的 toolkits/common 精确接口兼容。通用 artifact 和便捷 API 已实现。
- 对象存储首批仅阿里云，保留 Backend/RangeSource；真实飞书/LLM/OSS/生产 SSH 联调未执行。
- 跨进程文件刷新锁、注入式分布式缓存/去重与本地模拟测试已有覆盖；具体 Redis 集群、高可用和生产锁实现仍需按部署方式验收。
