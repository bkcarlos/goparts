# Config 独立模块

[返回模块总览](../README.md) · [HTTP 配置](../httpclient/README.md#配置参数) · [统一错误与上报](../apperror/README.md)

模块名 `github.com/bkcarlos/goparts/config`，Go 1.21+，仅依赖标准库。用于结构体配置加载与启动校验。

## 使用

```go
type AppConfig struct {
    Name    string        `json:"name" default:"demo" env:"NAME" required:"true"`
    Port    int           `json:"port" default:"8080" env:"PORT"`
    Timeout time.Duration `json:"timeout" default:"5s" env:"TIMEOUT"`
}

cfg, err := config.Load[AppConfig](config.Options{
    File: "config.json",
    EnvPrefix: "APP_",
})
if err != nil { return err }
```

按 **default 标签 → JSON 文件 → env 标签** 覆盖，然后执行 required 和自定义校验。加载失败返回零值和错误，不返回部分配置。

完整示例：[examples/basic/main.go](examples/basic/main.go)。它还实现了 `Validate() error` 来校验端口范围和超时。

## 配置规则

| 项目 | 行为 |
| --- | --- |
| `Options.File` | 可选 JSON 文件；指定后文件必须存在；最大 1 MiB |
| `Options.EnvPrefix` | 拼接在 env 标签前，例如 `APP_` + `PORT` |
| `Options.LookupEnv` | 可注入配置来源，默认 `os.LookupEnv` |
| `default:"..."` | 字符串默认值，支持空字符串 |
| `env:"..."` | 显式映射环境变量；未标记或 `env:"-"` 的字段不从环境变量加载 |
| `required:"true"` | 拒绝字段零值，包括空字符串、0、false、nil；不适合“false 或 0 也合法”的必填判断 |
| `Validate() error` | 在 `*T` 上实现，校验跨字段规则，错误链保留 |

默认值和环境变量支持 string、bool、整数、浮点数、`time.Duration`、逗号分隔的字符串切片，以及实现 `encoding.TextUnmarshaler` 的值类型。字符串切片会去除每项首尾空格，空字符串得到空切片。

支持嵌套的值结构体，环境变量名始终由标签显式指定；不会自动把父字段名拼入变量名。`T` 必须是结构体，指针字段上的 default/env 标签不支持，也不遍历嵌套指针。未导出字段跳过。

JSON 按标准 `encoding/json` 规则解析，拒绝未知字段、多个 JSON 对象和尾部垃圾。`time.Duration` 在 JSON 中使用整数纳秒，例如 5 秒为 `5000000000`；在 default/env 中使用 `5s`。若需要 JSON 字符串时长，可定义实现相应 JSON/Text 解码接口的类型。

环境变量存在但为空时仍会覆盖文件值。required 按 Go 零值判断，不检查切片长度；复杂必填规则放在 `Validate` 中。default/env 转换错误仅报告字段名和类型，不回显值；自定义 Validate 的错误文本由业务控制。

当前不包含 YAML、热更新或远程配置中心。

## 把配置传给其他模块

加载结果是业务自己的结构体，可按需拆给其他组件；`config` 不依赖这些组件：

```go
type HTTPSettings struct {
    Timeout time.Duration `default:"10s" env:"HTTP_TIMEOUT"`
    MaxResponseBytes int64 `default:"4194304" env:"HTTP_MAX_RESPONSE_BYTES"`
}

settings, err := config.Load[HTTPSettings](config.Options{EnvPrefix: "APP_"})
if err != nil { return err }
client, err := httpclient.New(httpclient.Config{
    Timeout: settings.Timeout,
    MaxResponseBytes: settings.MaxResponseBytes,
})
```

此片段需要业务同时引入 `config` 和 `httpclient`。上限单位为字节，例如 `APP_HTTP_MAX_RESPONSE_BYTES=16777216` 表示 16 MiB；未配置文件和环境变量时使用结构体 default 值。客户端仍执行自己的参数校验。

## 独立运行

在本模块目录执行：

```sh
GOWORK=off go run ./examples/basic
DEMO_PORT=9090 DEMO_TIMEOUT=2s GOWORK=off go run ./examples/basic
GOWORK=off go test -race -cover ./...
GOWORK=off go vet ./...
```

使用文件时运行 `go run ./examples/basic -config /path/to/config.json`。安装与本地联调方式见[接入指南](../README.md#接入业务项目)。
