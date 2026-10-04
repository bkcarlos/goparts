# feishu：LLM 使用指南

本文件说明当前源码快照中的能力。它不会因 go get 自动进入 LLM 上下文，需由使用方显式读取。

- 用途：Webhook、用户授权、文档、卡片、附件、长连接及业务 API。
- Module：`github.com/bkcarlos/goparts/feishu`；最低 Go 1.21。
- 详细接入：[README](README.md)。真实签名以当前选中版本的 Go 源码和 go doc 为准。

## 使用前核对

在调用方项目中执行（未安装时按项目要求安装指定版本，不自动升级）：

```sh
go list -m -json github.com/bkcarlos/goparts/feishu
go doc github.com/bkcarlos/goparts/feishu
go doc github.com/bkcarlos/goparts/feishu.New
go doc github.com/bkcarlos/goparts/feishu.Client.SendText
```

检查 Version、Dir、Replace；使用 Dir 指向的本地文件，不猜测缓存路径。若有 replace，优先核对实际替换源码。

## API 导航

`New`、`Client.SendText`。这里只列入口，不替代参数和返回值声明。

源码：[client.go](client.go)。

示例与行为验证：[examples/cards_mock/main.go](examples/cards_mock/main.go) · [examples/userdocs_mock/main.go](examples/userdocs_mock/main.go)。

## 必须保留的调用语义

- Webhook 地址不等于应用凭据或用户身份；写入接口默认单次尝试。
- user 客户端管理用户 token；多用户调用通过 Manager 与显式 Context 身份选择。
- 卡片更新需要正确 cardID/elementID 和顺序；事件去重只缓存成功结果。

## 使用方验证

生成代码后，在业务项目执行相关 `go test` / `go vet`。区分编译成功、本地模拟测试和真实服务联调。
源码中不存在的类型或方法不可凭名字补全；能力不足时明确说明，或通过已有接口注入适配器。

[仓库能力总索引](https://github.com/bkcarlos/goparts/blob/main/AI_GUIDE.md) 用于查找其他模块；
该链接指向主分支，不能替代已安装版本的接口依据。根目录总索引不随这个独立 module 一起下载。

## 子包导航

| 场景 | 包后缀 | 本地依据 |
| --- | --- | --- |
| 用户登录与 docx | user | [README](user/README.md) |
| 应用卡片与流式更新 | card | [README](card/README.md) |
| 聊天/云空间/素材上传 | attachment | [README](attachment/README.md) |
| WebSocket 长连接 | events | [README](events/README.md) |
| 表与记录/字段 | bitable | [源码](bitable/client.go) |
| Wiki 节点解析 | wiki | [源码](wiki/client.go) |
| 邮箱/手机号查询 ID | contact | [源码](contact/client.go) |
| 成功事件去重 | dedup | [源码](dedup/dedup.go) |
| JSON 1.0 卡片构建 | card/legacy | [源码](card/legacy/card.go) |

这些都是同一 module 的子包，不需要各自 go get 不同版本。先查目标包的 Config 与方法：
`go doc github.com/bkcarlos/goparts/feishu/user.Client`。
