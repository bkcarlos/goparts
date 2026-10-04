# safemap：LLM 使用指南

本文件说明当前源码快照中的能力。它不会因 go get 自动进入 LLM 上下文，需由使用方显式读取。

- 用途：泛型普通 Map、有序 Map 和快照遍历。
- Module：`github.com/bkcarlos/goparts/safemap`；最低 Go 1.21。
- 详细接入：[README](README.md)。真实签名以当前选中版本的 Go 源码和 go doc 为准。

## 使用前核对

在调用方项目中执行（未安装时按项目要求安装指定版本，不自动升级）：

```sh
go list -m -json github.com/bkcarlos/goparts/safemap
go doc github.com/bkcarlos/goparts/safemap
go doc github.com/bkcarlos/goparts/safemap.New
go doc github.com/bkcarlos/goparts/safemap.NewOrdered
```

检查 Version、Dir、Replace；使用 Dir 指向的本地文件，不猜测缓存路径。若有 replace，优先核对实际替换源码。

## API 导航

`New`、`NewOrdered`、`SafeMap.Snapshot`、`OrderedSafeMap.Range`。这里只列入口，不替代参数和返回值声明。

源码：[map.go](map.go)。

示例与行为验证：[behavior_test.go](behavior_test.go)。

## 必须保留的调用语义

- 零值可用；有序 Map 更新不移动位置，删除重插放到末尾。
- 快照只复制容器，不深拷贝 value 内部数据。
- 多个方法调用不是原子事务；不要用 Has+Set 做并发去重。

## 使用方验证

生成代码后，在业务项目执行相关 `go test` / `go vet`。区分编译成功、本地模拟测试和真实服务联调。
源码中不存在的类型或方法不可凭名字补全；能力不足时明确说明，或通过已有接口注入适配器。

[仓库能力总索引](https://github.com/bkcarlos/goparts/blob/main/AI_GUIDE.md) 用于查找其他模块；
该链接指向主分支，不能替代已安装版本的接口依据。根目录总索引不随这个独立 module 一起下载。
