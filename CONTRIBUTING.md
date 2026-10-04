# 开发与发布

每个模块独立维护 go.mod，生产代码不导入其他兄弟模块；通过小接口/函数注入组合。
同模块子包可共享实现。跨模块组合测试放 tests/，不要为测试给生产模块增加本地 replace。
cache/redis 是可选嵌套模块，须单独测试和发布。ssh 与 cache/redis 最低 Go 1.26，其余
模块保持 Go 1.21；完整 workspace 使用 Go 1.26+。推荐当前受支持 Go 的最新补丁。

修改一组功能后：

```sh
make check
# 并发状态、取消与关闭测试重复 20 轮，可用 STRESS_COUNT 调整：
make stress
# 两个解析入口分别执行 10 秒模糊测试，可用 FUZZTIME=1m 延长：
make fuzz
# 安装工具后额外检查可调用的漏洞：
go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
make vuln
```

测试应覆盖成功、取消、服务失败、输入边界和关键并发行为，不使用真实凭据，不自动发送通知。
并发测试优先用通道同步和有界等待；错误路径应验证返回错误、未执行的副作用、原数据保留和资源清理。
避免只检查“没有报错”，或通过固定 Sleep 猜测任务执行顺序。新增的 fuzz 种子随普通测试运行，
CI robustness 任务另执行 `make stress` 和 `make fuzz`；fuzz 失败输入应保留为回归语料。
加新模块时同时更新 Makefile、go.work、CI matrix、README 和 FEATURES，
并补充根目录 AI_GUIDE.md 索引、模块内 AI_GUIDE.md 和模块 README 的指南入口。
AI 指南不替代实际 API；修改接口、错误语义或资源所有权时同步调用约定，核对所引用符号与文件。
公共结构体用可选新增字段维持兼容；破坏性改变需主版本升级及迁移说明。

多模块发布顺序：实现与文档 → race/cover/vet/集成/漏洞检查 → 提交推送 → 每模块标记版本。
标签为 `<module>/vX.Y.Z`，例如 `logger/v0.1.0`、`cache/redis/v0.1.0`；Go 安装参数中的
版本仅写 `@v0.1.0`。子包如 feishu/card、metrics/otel 随所属 module 发布，不单独打标签。
已推送标签不可移动，修复创建新补丁版本。发布时检查工作区干净，并记录 CHANGELOG。

新 SDK/协议对接必须注明验证范围。只通过本地协议测试时，不可声称真实云服务联调完成。
