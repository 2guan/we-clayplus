# gateway

导入 `github.com/we-clay/we-clay/gateway`，在自己的程序中调用 `Run(ctx, cfg, options...)`。
gateway 提供 Go 库，应用自行编写 `main`；无需独立 gateway 程序。

| 入口 | 用途 |
| --- | --- |
| `Run` | 阻塞运行；取消 context 后关闭连接并回收托管 core |
| `WithHandlers(Handlers{...})` | 注册 `OnReady`、`OnMessage`、`OnEvent` |
| `WithoutPlugins()` | 纯机器人模式，不加载插件、不处理内置管理命令 |
| `Client` | 回调提供的业务客户端，可保存后主动调用 |
| `Client.SendText` / `Client.SendCard` / `Client.SendVideo` | 发文字、联系人名片或自动准备封面与时长的本机 MP4 |
| `Client.Message` / `Client.Moments` 等 | 13 个 SDK 能力域，包括媒体、联系人、群聊、朋友圈 |
| `CoreConfig` | 固定版本下载、SHA-256 校验、缓存和子进程管理 |
| `LoadConfig` | 可选的 TOML 配置读取 |
| `ErrAlreadyRunning` | 同一进程已有活动 gateway |
| `ErrClientClosed` | 客户端所属 Run 已停止，不能用它操作下一次登录的账号 |
| `ErrNotImplemented` | 方法仅保留占位，不发出请求 |
| `ErrAbilityPanic` | 底层能力发生异常，结果可能未知，需核对后处理 |

原 `Run(ctx, cfg)` 调用继续有效。单进程仅支持一个活动 gateway。
业务方法沿用 SDK 签名，使用 Run 的 context，网络请求默认两分钟超时。
接口覆盖不代表所有底层方法均已实现。详情、生命周期和错误边界见[直接接入](../docs/直接接入.md)。

[快速开始](../README.md#快速开始) · [运行指南](../docs/运行指南.md) · [插件开发](../docs/插件开发.md)
