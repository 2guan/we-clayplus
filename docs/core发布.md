# core 发布与下载

core v0.3.3 起，私有 core 仓库的 GitHub Actions 完成六个平台构建和原生离线测试后，将二进制发布到阿里云 OSS `weclay`（北京）的 `core/<版本>/`。不再创建 GitHub Release；历史附件保留。

发布文件为六个平台二进制、LICENSE、SHA256SUMS 和 manifest.json。二进制沿用 `we-clay-core_<版本>_<系统>_<架构>` 命名，Windows 追加 `.exe`。例如 `we-clay-core_v0.3.3_darwin_arm64`。

## 自动下载

使用 `Core.OSS` 指定 Bucket、地域和前缀；默认使用运行授权 Key 换取短时 OSS 链接，原有 `OSS_ACCESS_KEY_ID`、`OSS_ACCESS_KEY_SECRET`、可选 `OSS_SESSION_TOKEN` 方式仍可使用。完整示例、只读 RAM 权限与 CNAME 配置见[私有 OSS 下载](OSS下载.md)。

- 固定版本，不自动追踪 latest；Actions 尚未完成上传的版本不能使用。
- Bucket 和对象保持私有。客户端签名读取，无需公开文件。
- 下载验证清单版本/协议/平台、文件大小与 SHA-256；未通过不启动 core。
- 有效缓存支持离线复用；版本、下载源和平台各自隔离。
- 旧调用未设置 `Core.OSS` 时保留 GitHub 下载方式，通过 `GitHubTokenEnv` 提供私有 Release 读取凭据。

## 发布保障

上传发生在所有构建与测试成功之后。只上传允许的 9 个文件，逐个读回校验；manifest.json 最后写入。已发布版本不覆盖，相同版本部分上传内容不一致时停止。

固定混淆工具链、Actions Secrets、RAM 发布权限及 CI 操作步骤位于私有 core 仓库的 `docs/二进制发布.md`。只有发布端持有写入权限；客户端凭据仅需 `oss:GetObject`，不得分发发布密钥。

GitHub Actions 构建日志和源码继续保存在私有 core 仓库。OSS 不包含源码、账号资料、设备状态、聊天内容或日志。离线测试通过不代表每个平台已完成真实微信登录验收。
