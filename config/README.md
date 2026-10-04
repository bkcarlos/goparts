# Config 独立模块

[返回模块总览](../README.md) · [HTTP 配置](../httpclient/README.md#配置参数) · [统一错误与上报](../apperror/README.md)

模块名 `github.com/bkcarlos/goparts/config`，Go 1.21+，支持 JSON / YAML 结构体配置加载与启动校验。YAML 使用 [go.yaml.in/yaml/v3](https://pkg.go.dev/go.yaml.in/yaml/v3)；依赖仅属于本模块，其他 goparts 模块无需引入。

## 使用

```sh
go get github.com/bkcarlos/goparts/config@v0.1.0
```

```go
type AppConfig struct {
    Name    string        `json:"name" yaml:"name" default:"demo" env:"NAME" required:"true"`
    Port    int           `json:"port" yaml:"port" default:"8080" env:"PORT"`
    Timeout time.Duration `json:"timeout" yaml:"timeout" default:"5s" env:"TIMEOUT"`
}

cfg, err := config.Load[AppConfig](config.Options{
    File: "config.yaml", // 原有 config.json 用法仍然支持
    EnvPrefix: "APP_",
})
if err != nil { return err }
```

按 **default 标签 → DefaultsFile → Files（按顺序）→ File → env 标签** 覆盖，然后执行 required、字段规则和嵌套/顶层自定义校验。加载失败返回零值和错误，不返回部分配置。

完整示例：[examples/basic/main.go](examples/basic/main.go)。它还实现了 `Validate() error` 来校验端口范围和超时。

对应的 YAML 文件可包含注释，并使用可读时长：

```yaml
# 环境变量 APP_PORT 可覆盖此处的端口
name: order-service
port: 8080
timeout: 5s
```

### 文件格式选择

`.yaml` / `.yml` 后缀自动使用 YAML，大小写不敏感；其他后缀及无后缀文件继续按 JSON 处理，兼容原有用法。不会猜测内容并在两种格式间回退。

文件没有标准后缀时，可以显式指定格式：

```go
cfg, err := config.Load[AppConfig](config.Options{
    File: "application.conf",
    Format: config.FormatYAML,
    EnvPrefix: "APP_",
})
```

`FormatAuto`（零值）按后缀选择；`FormatJSON` / `FormatYAML` 优先于后缀。不支持的 Format 返回错误。

## 配置规则

| 项目 | 行为 |
| --- | --- |
| `Options.Defaults` / `DefaultsFile` | 可选 fs.FS（含 embed.FS）与默认文件路径，先于外部文件读取 |
| `Options.Files` | 按给定顺序覆盖多个文件，之后再读取 File；显式指定的文件都必须存在 |
| `Options.File` | 可选 JSON / YAML 文件；指定后文件必须存在；最大 1 MiB |
| `Options.Format` | 默认 `FormatAuto`；可显式指定 `FormatJSON` / `FormatYAML` |
| `Options.EnvPrefix` | 拼接在 env 标签前，例如 `APP_` + `PORT` |
| `Options.LookupEnv` | 可注入配置来源，默认 `os.LookupEnv` |
| `default:"..."` | 字符串默认值，支持空字符串 |
| `env:"..."` | 显式映射环境变量；未标记或 `env:"-"` 的字段不从环境变量加载 |
| `required:"true"` | 拒绝字段零值，包括空字符串、0、false、nil；不适合“false 或 0 也合法”的必填判断 |
| `Validate() error` | 在 `*T` 上实现，校验跨字段规则，错误链保留 |

默认值和环境变量支持 string、bool、整数、浮点数、`time.Duration`、逗号分隔的字符串切片，以及实现 `encoding.TextUnmarshaler` 的值类型。字符串切片会去除每项首尾空格，空字符串得到空切片；其他类型切片使用 JSON 数组形式，例如整型切片的环境值 `[1,2,3]`。

支持嵌套值结构体和可选指针字段，default/env 可以按需初始化指针；没有数据的可选指针保持 nil。环境变量名始终由标签显式指定，不会自动拼接父字段名。`T` 本身必须是结构体，未导出字段跳过。

JSON 按标准 `encoding/json` 规则解析，拒绝未知字段、多个 JSON 对象和尾部垃圾。`time.Duration` 在 JSON 中使用整数纳秒，例如 5 秒为 `5000000000`；在 default/env 中使用 `5s`。若需要 JSON 字符串时长，可定义实现相应 JSON/Text 解码接口的类型。

YAML 使用 `yaml:"..."` 标签，JSON 使用 `json:"..."` 标签；支持两种格式时建议同时声明。未设置 yaml 标签时，YAML 库使用字段名的小写形式，例如 `AppName` 对应 `appname`，不会自动复用 `json:"app_name"`。嵌套字段和字符串列表按 YAML 映射、序列加载。

YAML 文件必须是**单份文档、顶层映射**，拒绝未知结构体字段、重复键、语法错误、多文档、空文件及顶层 null / 列表 / 标量；空映射 `{}` 可以只保留默认值。`time.Duration` 在 YAML 中使用 `5s`、`250ms` 等字符串，不接受整数纳秒。正常锚点和别名由 YAML 库处理，递归或过量展开会报错。自定义 YAML 解码器内部的字段校验由其自身负责。

YAML 文件中未出现的字段保留默认值，文件中的零值如 `false` 和 `0` 仍会覆盖默认值。文件解码失败只返回通用错误说明，不回显原始配置值。

环境变量存在但为空时仍会覆盖文件值。required 按 Go 零值判断，不检查切片长度；复杂必填规则放在 `Validate` 中。default/env 转换错误仅报告字段名和类型，不回显值；自定义 Validate 的错误文本由业务控制。

当前不包含热更新或远程配置中心。

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
GOWORK=off go run ./examples/basic -config ./examples/basic/config.yaml
DEMO_PORT=8088 GOWORK=off go run ./examples/basic -config ./examples/basic/config.yaml
GOWORK=off go test -race -cover ./...
GOWORK=off go vet ./...
```

YAML 示例文件见 [config.yaml](examples/basic/config.yaml)。原有 JSON 文件仍可通过 `go run ./examples/basic -config /path/to/config.json` 加载。安装与本地联调方式见[接入指南](../README.md#接入业务项目)。

### 分层与校验

加载顺序：default 标签 → `Defaults` (`fs.FS`，例如 embed.FS) 的
`DefaultsFile` → `Files`（按顺序）→ `File` → 环境变量 → 校验。
多层覆盖同名标量，保留未覆盖的结构体字段和 map 项；slice 整体替换。
每层仍严格拒绝未知字段。`LoadRegistry[T](fs, "projects/*.yaml", opts)`
按匹配文件分别加载，以完整 FS 路径为 key 返回注册表。
支持可选 `*T`：没有数据的字段保持 nil；default/env 可初始化指针，
递归类型不会无限分配。已存在的嵌套结构体会执行自己的 Validate。
字符串支持 `url:"true"`（HTTP/S，无 userinfo）、`host:"true"`（主机名/IP）、
`oneof:"dev,prod"`、`regex:"^[a-z]+$"`。业务跨字段规则继续使用 Validate。
