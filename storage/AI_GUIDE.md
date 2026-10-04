# storage：LLM 使用指南

本文件说明当前源码快照中的能力。它不会因 go get 自动进入 LLM 上下文，需由使用方显式读取。

- 用途：供应商无关接口、上传下载、分页、签名 URL 和目录上传。
- Module：`github.com/bkcarlos/goparts/storage`；最低 Go 1.21。
- 详细接入：[README](README.md)。真实签名以当前选中版本的 Go 源码和 go doc 为准。

## 使用前核对

在调用方项目中执行（未安装时按项目要求安装指定版本，不自动升级）：

```sh
go list -m -json github.com/bkcarlos/goparts/storage
go doc github.com/bkcarlos/goparts/storage
go doc github.com/bkcarlos/goparts/storage.New
go doc github.com/bkcarlos/goparts/storage.Backend
```

检查 Version、Dir、Replace；使用 Dir 指向的本地文件，不猜测缓存路径。若有 replace，优先核对实际替换源码。

## API 导航

`New`、`Backend`、`Client.Get`、`Client.UploadDirectory`。这里只列入口，不替代参数和返回值声明。

源码：[storage.go](storage.go) · [directory.go](directory.go)。

示例与行为验证：[examples/basic/main.go](examples/basic/main.go) · [directory_test.go](directory_test.go)。

## 必须保留的调用语义

- 当前云适配器为 aliyun；不编造 S3/MinIO/COS 包。业务对象键相对 Client.BasePath。
- Get 返回流需关闭并保持 Context 有效；Put 消费但不关闭调用方 Reader。
- 上传通常覆盖同名对象；目录失败不回滚已上传对象。ETag 不等同 SHA-256/MD5。

## 使用方验证

生成代码后，在业务项目执行相关 `go test` / `go vet`。区分编译成功、本地模拟测试和真实服务联调。
源码中不存在的类型或方法不可凭名字补全；能力不足时明确说明，或通过已有接口注入适配器。

[仓库能力总索引](https://github.com/bkcarlos/goparts/blob/main/AI_GUIDE.md) 用于查找其他模块；
该链接指向主分支，不能替代已安装版本的接口依据。根目录总索引不随这个独立 module 一起下载。
