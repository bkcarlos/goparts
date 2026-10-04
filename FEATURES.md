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
| TTL/文件缓存、singleflight、持久化更新缓存 | 已实现，测试通过 |
| metrics 接口与 OTel 适配、组件观测钩子 | 已实现，测试通过 |
| Bitable/Wiki/Contact 业务 API、旧版卡片 | 待实现 |
| 多用户会话、身份选择、登录轮询、TokenCache 与事件去重 | 待实现 |
| LLM Responses 普通/流式 | 已实现，测试通过 |
| LLM 会话、工具注册/校验/调度、聚合节流、Sequence | 已实现，测试通过 |
| HTTP Range 分片/续传、存储目录操作、MD5 | 待实现 |
| SSH/SFTP、多主机、池、远程文件/空间/符号链接 | 待实现 |
| 版本发布/索引/检查/下载/校验/替换、存储抽象 | 待实现 |
| 制品库通用客户端和 BOS 兼容 | 通用客户端待实现；BOS 专有协议按用户确认跳过 |
| SafeMap/OrderedMap、文件树过滤、格式化/压缩/磁盘空间 | 已实现，测试通过 |
| logger/retry/errx 下游旧 API 精确兼容 | 精确兼容按用户确认跳过；通用入口待实现 |
| CI、CHANGELOG、CONTRIBUTING、模块版本标签 | 待实现 |
| LICENSE | MIT，已添加 |
