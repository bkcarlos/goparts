# Changelog

## v0.1.0 — 2026-10-04

首批按模块独立发布，覆盖 23 个 Go modules。

- logger：结构化日志、context、嵌套脱敏、文件轮转、全局便捷入口和 trace。
- apperror：编码/说明、组合与继承、Registry/guidance、HTTP/退出码映射、结构化属性、Join 分支报告。
- config/httpclient/retry/lifecycle 原有基础上，补 YAML/embed/分层/指针校验、受控 HTTP 钩子及预览、Retry-After/随机源和分阶段停机。
- middleware、ratelimit、workerpool：标准 HTTP 中间件、令牌桶/熔断、有界队列及按 key 保序调度。
- cache、可选 cache/redis、persistcache：TTL/文件/Redis、singleflight 与更新时间持久化。
- metrics：最小指标接口、Nop 和 OTel 适配；组件通过回调对接。
- feishu：用户授权/加密多账号会话、显式身份、文档/附件、卡片 1.0/2.0、业务 API、WebSocket 与成功后去重。
- llm：OpenAI 兼容 Chat/Embeddings/Responses、SSE；会话裁剪、工具 Schema 校验、流式聚合和序号。
- storage/download：供应商无关接口、阿里云 OSS、目录/分片上传、Range 下载与续传、完整性校验和原子发布。
- ssh、version、artifact：SSH/SFTP/池、版本发布更新回滚、明确通用协议的制品目录。
- safemap、filetree、utils：并发容器、文件过滤、归档、格式化及跨平台磁盘空间。
- MIT、CI 模块矩阵、竞态/覆盖率/vet/漏洞检查和跨模块集成测试。

BOS 专有协议、toolkits 旧接口精确兼容按用户确认跳过；云服务真实联调单独验收。
ssh 和 cache/redis 需要 Go 1.26+，其余模块最低 Go 1.21。
