# version：LLM 使用指南

本文件说明当前源码快照中的能力。它不会因 go get 自动进入 LLM 上下文，需由使用方显式读取。

- 用途：发布文件和清单、检查更新、下载替换和回滚。
- Module：`github.com/bkcarlos/goparts/version`；最低 Go 1.21。
- 详细接入：[README](README.md)。真实签名以当前选中版本的 Go 源码和 go doc 为准。

## 使用前核对

在调用方项目中执行（未安装时按项目要求安装指定版本，不自动升级）：

```sh
go list -m -json github.com/bkcarlos/goparts/version
go doc github.com/bkcarlos/goparts/version
go doc github.com/bkcarlos/goparts/version.StorageProvider
go doc github.com/bkcarlos/goparts/version.Publisher.UploadRelease
```

检查 Version、Dir、Replace；使用 Dir 指向的本地文件，不猜测缓存路径。若有 replace，优先核对实际替换源码。

## API 导航

`StorageProvider`、`Publisher.UploadRelease`、`Updater.Update`、`Rollback`。这里只列入口，不替代参数和返回值声明。

源码：[version.go](version.go)。

示例与行为验证：[version_test.go](version_test.go)。

## 必须保留的调用语义

- 这是应用二进制版本管理，不是发布 goparts Go module 标签。
- latest 是指针，版本 ID 按是否不同比较，不做语义版本大小判断。
- 发布前缀需单发布者/外部锁；更新保留 .bak，已有备份拒绝覆盖。

## 使用方验证

生成代码后，在业务项目执行相关 `go test` / `go vet`。区分编译成功、本地模拟测试和真实服务联调。
源码中不存在的类型或方法不可凭名字补全；能力不足时明确说明，或通过已有接口注入适配器。

[仓库能力总索引](https://github.com/bkcarlos/goparts/blob/main/AI_GUIDE.md) 用于查找其他模块；
该链接指向主分支，不能替代已安装版本的接口依据。根目录总索引不随这个独立 module 一起下载。
