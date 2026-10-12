# core 二进制发布

下载源为 [we-clay/core-release](https://github.com/we-clay/core-release)。
将编译产物上传为 **GitHub Releases 附件**，不提交二进制到 Git 分支，不上传源码或账号数据。
we-clay 模块与 core Release 独立版本；`CoreConfig.Version` 指向 core 的 tag。

## 附件命名

上传未经压缩的二进制，名称匹配 Release tag 和 Go 的 `GOOS` / `GOARCH`。
从 we-clay v0.4.1 起识别 `we-clay-core_<tag>_<goos>_<goarch>`，Windows 加 `.exe`。
下面是已发布 core v0.3.0 的实际附件名：

| 平台 | 附件名 |
| --- | --- |
| macOS Apple Silicon | `we-clay-core_v0.3.0_darwin_arm64` |
| macOS Intel | `we-clay-core_v0.3.0_darwin_amd64` |
| Linux x86_64 | `we-clay-core_v0.3.0_linux_amd64` |
| Linux ARM64 | `we-clay-core_v0.3.0_linux_arm64` |
| Windows x86_64 | `we-clay-core_v0.3.0_windows_amd64.exe` |
| Windows ARM64 | `we-clay-core_v0.3.0_windows_arm64.exe` |

只需上传实际支持的平台；缺少本机平台附件时 Run 会报错，不尝试运行其他平台文件。
也兼容原先不带版本的 `we-clay-core-<goos>-<goarch>` 命名及已有缓存。新下载同时存在两种附件时优先使用带 tag 的命名；不尝试其他版本或平台。当前下载器不解压 `.zip` / `.tar.gz`。

## 编译与上传

在 **core 源码仓库**中为目标平台构建，示例为 Apple Silicon：

```sh
# CORE_VERSION 由维护者设为本次新的 Release tag
mkdir -p dist
GOOS=darwin GOARCH=arm64 go build -trimpath \
  -o "dist/we-clay-core_${CORE_VERSION:?请先设置新的 core tag}_darwin_arm64" ./cmd/we-clay-core
```

core 需要支持现有 `-stage`、`-data`、`-device-type`、`-listen`、`-grpc-listen`、`-api-token-env` 参数，
并提供 gRPC `CoreService.Invoke` / `Events`、`/api/login/view` 等现有接口。
仅交叉编译成功不代表已在相应系统验证运行。

1. 在 `core-release` 创建新的固定 tag Release。`v0.3.0` 已发布，不要覆盖其附件。
2. 上传对应名称的二进制。建议同时上传 `SHA256SUMS`，便于人工校验及兼容无摘要的附件。
3. 发布 Release，确认不是 Draft。附件上传状态应为 `uploaded`。
4. 把实际 tag 填入 `CoreConfig.Version`，或设置 README 使用的 `WE_CLAY_CORE_VERSION`。

macOS 可在只含待发布二进制的 `dist/` 中生成清单：

```sh
cd dist
shasum -a 256 we-clay-core* > SHA256SUMS
```

Linux 可使用 `sha256sum we-clay-core* > SHA256SUMS`。清单每行是十六进制 SHA-256、空格、附件文件名。
Run 优先使用 GitHub 附件元数据的 `digest`，缺少时才读取该清单。校验失败拒绝执行。
摘要用于验证下载内容与所选 Release 一致；发布权限本身仍是信任边界。

**版本发布后保持不可变。** 修改二进制应使用新 tag；Run 不接受 `latest`，有效缓存不会自动联网升级。
升级只改变版本，不自动删除或迁移账号数据；回退前应确认 core 的存储格式兼容。

## 权限与凭据

`core-release` 与 `we-clay` 为公开仓库，公开附件无需 GitHub 令牌；独立 `core` 源码仓库继续保持私有。
公开的二进制可被任何人下载，上传前应确认产物中没有构建时嵌入的账号信息或凭据。

如果以后将 Release 仓库设为私有，运行方需通过 `WE_CLAY_GITHUB_TOKEN` 提供对应仓库的 Contents 读取权限，
或用 `GitHubTokenEnv` 指向现有令牌环境变量。Go Modules 的 Git 认证与下载器的令牌相互独立。
令牌不放入 TOML、Go 常量、Release 文案或 Git；下载器也不会将它传给附件重定向服务器或 core 子进程。

官方 API 说明：[Release 附件下载](https://docs.github.com/en/rest/releases/assets#get-a-release-asset)。

## 已验证的组合

2026-10-12：we-clay v0.4.1 与 core-release v0.3.0，在 macOS ARM64 完成：

- 通过 `gateway.Run` 从真实 GitHub Release 下载对应附件，大小与 SHA-256 一致。
- 独立数据目录启动 core，使用 gRPC 读取登录状态，Mac 二维码生成成功。
- 取消 context 后 Run 返回，core 子进程退出，登录与 gRPC 端口释放。

本轮未扫码登录账号，未操作旧会话，也未验证其他系统的真实运行。
