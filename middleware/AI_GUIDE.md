# middleware：LLM 使用指南

本文件说明当前源码快照中的能力。它不会因 go get 自动进入 LLM 上下文，需由使用方显式读取。

- 用途：请求 ID、访问日志、恢复、鉴权、CORS、超时和 Body 限制。
- Module：`github.com/bkcarlos/goparts/middleware`；最低 Go 1.21。
- 详细接入：[README](README.md)。真实签名以当前选中版本的 Go 源码和 go doc 为准。

## 使用前核对

在调用方项目中执行（未安装时按项目要求安装指定版本，不自动升级）：

```sh
go list -m -json github.com/bkcarlos/goparts/middleware
go doc github.com/bkcarlos/goparts/middleware
go doc github.com/bkcarlos/goparts/middleware.Chain
go doc github.com/bkcarlos/goparts/middleware.RequestID
```

检查 Version、Dir、Replace；使用 Dir 指向的本地文件，不猜测缓存路径。若有 replace，优先核对实际替换源码。

## API 导航

`Chain`、`RequestID`、`BodyLimit`、`AuthBearer`。这里只列入口，不替代参数和返回值声明。

源码：[middleware.go](middleware.go)。

示例与行为验证：[behavior_test.go](behavior_test.go)。

## 必须保留的调用语义

- Chain 第一个中间件最外层；若预检不需认证，将 CORS 放在认证外层。
- 未知 Content-Length 时仍需在读取 Body 时处理 MaxBytesError。
- Timeout 缓冲响应，不用于 SSE/WebSocket；限流请显式组合 ratelimit。

## 使用方验证

生成代码后，在业务项目执行相关 `go test` / `go vet`。区分编译成功、本地模拟测试和真实服务联调。
源码中不存在的类型或方法不可凭名字补全；能力不足时明确说明，或通过已有接口注入适配器。

[仓库能力总索引](https://github.com/bkcarlos/goparts/blob/main/AI_GUIDE.md) 用于查找其他模块；
该链接指向主分支，不能替代已安装版本的接口依据。根目录总索引不随这个独立 module 一起下载。
