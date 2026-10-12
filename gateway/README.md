# gateway

导入 `github.com/we-clay/we-clay/gateway`，在自己的程序中调用 `Run(ctx, Config)`。
该目录提供 Go 库，不包含 `main` 或 CLI，不需要额外的 gateway 程序。

- `Run`：启动并阻塞运行；取消 context 后清理插件和连接，返回调用方。
- `CoreConfig`：可选固定版本下载、SHA-256 校验、缓存和 core 子进程管理。
- `Config`：直接在 Go 中配置 core 地址、数据与插件目录。
- `LoadConfig`：可选的 TOML 配置读取。
- `ErrAlreadyRunning`：同一进程已有运行中的 gateway。

内部实现位于 `internal/`，插件只导入 `sdk/`。单进程当前仅支持一个活动 gateway。
运行示例见[根目录 README](../README.md)，配置及停止规则见[运行指南](../docs/运行指南.md)。
