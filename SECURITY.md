# 敏感信息管理

上游 `we-clay/we-clay` 私有保存 gateway 与 SDK；用户授权的 `2guan/we-clayplus` 为公开仓库，接收本项目可公开的开发改动。独立 `we-clay/core` 源码仓库保持 **Private**。不得提交账号密码、API 令牌、登录二维码、认证票据、
设备标识、会话文件、私钥、聊天记录、真实媒体、日志和已填写的运行配置。

令牌从环境变量读取。本地运行目录为 `runtime/`，编译输出在 `bin/`；二者均已忽略。
`configs/` 只保存无凭据的示例。`.gitignore` 不替代提交前检查。

## 提交与推送

1. 检查待提交文件，确认只包含代码、无凭据示例和离线测试。
2. 执行 `git add` 后运行 `make check-secrets`，需安装 Gitleaks v8.30.1 或兼容版本。
3. 使用 GitHub API 核对两个目标仓库与可见性：`2guan/we-clayplus/main` 为公开交付目标，`we-clay/we-clay/feature/dashboard-ai-plugin` 为私有上游交付目标。不得将 core 源码或运行数据放入任一目标。
4. 按 `AGENTS.md` 的双仓库交付约定同步本次变更，并分别核对远端提交；公开仓库的授权不包含无关私有代码或敏感信息。

检查脚本扫描 Git 索引中的实际文件内容，拒绝运行目录和未处理的扫描结果，
不输出候选密钥值。当前 `security/reviewed-findings.json` 为空，不接受未审核例外。
本次迁移仅导入已检查的 gateway、SDK 源码，未导入旧 Git 历史、core 源码或运行数据。

## 运行边界

gateway/core gRPC 使用 Authorization 元数据携带 Bearer 令牌，调用和事件订阅均验证。
只允许回环 IP 地址，跨机器使用受信任的加密隧道；gateway 无 HTTP 回调监听。外部插件以独立进程执行，具有当前系统用户的权限，只加载可信插件。Dashboard 与 AI 为进程内基础插件。

## 二进制下载

自动模式从指定 Release 下载当前平台附件，校验大小与 SHA-256 后才执行；
GitHub 下载令牌只从环境读取，不传给附件重定向地址或 core。缓存和账号状态都在本地 `runtime/`。
发布仓库及其维护者属于信任边界，固定版本并保持已发布附件不可变。
默认在调用方取消 context 时回收本次启动的 core。`KeepAlive: true` 时，core 脱离终端独立运行，取消只关闭 gateway。
独立模式在固定账号目录保存权限为 0600 的 `gateway-core.json`，含本机 API 随机令牌、PID、启动标识与版本；该文件不得上传、复制到其他设备或写入日志。
复用/停止均核对进程身份与 gRPC 鉴权；配置不符或未知监听者会报错。不调用微信退出登录接口。

## Web 控制台

控制台默认只监听 `127.0.0.1:8208`，与 core 的 gRPC 令牌和微信登录独立。
首次访问直接创建管理员账号，密码至少 6 位；初始化完成后不可重复创建，会话和角色权限由服务端检查。
Mock 免登录演示只允许回环监听及本机 Host，使用独立 `mock.db`，不访问真实模型或微信。
运行数据、`credential.key`、上传媒体与浏览器测试产物均不得提交。更多边界见 [控制台与 AI](docs/控制台与AI.md)。

## 私有 OSS

OSS 下载凭据仅从环境读取；不传给 core，不写入配置与缓存回执。用户侧 RAM 权限仅需 weclay/core/* 的 GetObject。Actions 写入凭据不得分发给客户端。下载通过 HTTPS，拒绝重定向以防签名/STS 票据外泄，记录错误时不输出原始 OSS 响应或签名 URL。

## core 运行授权

运行授权 Key 只从所选环境变量读取，以 `WE_CLAY_AUTH_KEY` 传入 core，不进入 argv、TOML 或进程记录。独立进程记录仅保存单向摘要用于提示 Key 变更；该摘要不能替代服务端授权。授权校验和续期均由 core 完成。它与 OSS/GitHub 下载凭据、本机 API Token 分离。见 docs/运行授权.md。
