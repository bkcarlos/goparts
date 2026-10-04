# llm：LLM 使用指南

本文件说明当前源码快照中的能力。它不会因 go get 自动进入 LLM 上下文，需由使用方显式读取。

- 用途：Chat、SSE、Embeddings、Responses；chat 子包提供应用层辅助。
- Module：`github.com/bkcarlos/goparts/llm`；最低 Go 1.21。
- 详细接入：[README](README.md)。真实签名以当前选中版本的 Go 源码和 go doc 为准。

## 使用前核对

在调用方项目中执行（未安装时按项目要求安装指定版本，不自动升级）：

```sh
go list -m -json github.com/bkcarlos/goparts/llm
go doc github.com/bkcarlos/goparts/llm
go doc github.com/bkcarlos/goparts/llm.New
go doc github.com/bkcarlos/goparts/llm.Client.Chat
```

检查 Version、Dir、Replace；使用 Dir 指向的本地文件，不猜测缓存路径。若有 replace，优先核对实际替换源码。

## API 导航

`New`、`Client.Chat`、`Client.ChatStream`、`Client.Embeddings`、`Client.Responses`。这里只列入口，不替代参数和返回值声明。

源码：[client.go](client.go) · [responses.go](responses.go)。

示例与行为验证：[examples/stream/main.go](examples/stream/main.go) · [chat/behavior_test.go](chat/behavior_test.go)。

## 必须保留的调用语义

- BaseURL 使用服务商的兼容 API 根路径；Chat 兼容不意味着支持 Responses。
- Chat 流必须收到 [DONE]；Responses 流必须完成。提前断流可能已产生部分文本。
- 根包只传递工具数据；chat.ToolRegistry 只执行显式注册且通过参数校验的 handler。

## 使用方验证

生成代码后，在业务项目执行相关 `go test` / `go vet`。区分编译成功、本地模拟测试和真实服务联调。
源码中不存在的类型或方法不可凭名字补全；能力不足时明确说明，或通过已有接口注入适配器。

[仓库能力总索引](https://github.com/bkcarlos/goparts/blob/main/AI_GUIDE.md) 用于查找其他模块；
该链接指向主分支，不能替代已安装版本的接口依据。根目录总索引不随这个独立 module 一起下载。

## 应用层辅助

[chat/README.md](chat/README.md) 介绍 Session、ToolRegistry、Accumulator 和 SequenceAllocator。
`go doc github.com/bkcarlos/goparts/llm/chat.ToolRegistry.Dispatch` 可核对工具分发签名。
Session 的方法并发安全不意味着整个模型对话回合原子化；同一会话仍需业务按回合串行调度。
