# artifact

通用制品目录客户端，**不声明兼容 BOS 专有协议**。缺少 BOS/toolkits 源码部分按用户确认跳过。
PackageManager 可注入任何 Backend；内置 HTTPBackend 使用下列明确约定的 JSON 协议：

- `GET /packages/{id}` → AppPackage：id/name/version/commit_id/url/internal_url/sha256/size。
- `GET /packages?commit_id=...&cursor=...&limit=...` → `{items:[], next_cursor:"..."}`。

已有平台协议不同时实现 Backend，保留上层 GetPackageInfo / QueryWithCommitID /
ListPackages / DownloadURL / DownloadPackage。默认目录响应限 4 MiB、超时 15 秒。
通过显式 InternalAvailable(ctx,url) 探测器决定内部 URL 可用性，失败使用公共 URL；
不猜测域名转换规则，不主动扫描内网。探测器必须限制探测对象和超时。
下载不会携带目录认证头、不跟随重定向，必须校验 SHA-256 和长度，默认最大 10 GiB、
30 分钟，临时写入后无覆盖发布。下载 URL 应由可信目录提供。
