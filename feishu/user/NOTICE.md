# 协议参考来源

本子包参考官方 [larksuite/cli](https://github.com/larksuite/cli) 的用户设备授权和 token 刷新流程，按本仓库的独立组件接口重新实现。

参考版本：`7beffb086d7fa3c5b843d8affa7c089f49cfc65e`。

- `internal/auth/device_flow.go`：设备授权请求、轮询、offline_access、降速与拒绝处理。
- `internal/auth/uat_client.go`：用户 token 刷新、过期时间和轮换后的存储。
- `internal/auth/paths.go`、`internal/core/types.go`：飞书账号域名与 OAuth 路径。

上游版权：Copyright (c) 2026 Lark Technologies Pte. Ltd.，采用 [MIT License](https://github.com/larksuite/cli/blob/7beffb086d7fa3c5b843d8affa7c089f49cfc65e/LICENSE)。本模块未引入该 CLI 为依赖，未打包其源码、命令系统或应用注册功能。
