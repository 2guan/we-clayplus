# 完整插件示例

示例与仓库共用根 go.mod。请在仓库根目录执行 `make example`，
生成 `bin/we-clay-plugin-example`，由 gateway 启动。

- `main.go`：元数据、能力字段、配置及入口。
- `lifecycle.go`：加载与卸载。
- `event.go`：消息和会话事件。
- `command.go`：命令处理。
- `capability.go`：供其他插件调用的能力。
- `session.go`：会话状态。

示例含真实发送消息的分支。按 [运行指南](../../docs/运行指南.md) 设置插件白名单及开关后再启用。
默认提供的本地配置模板禁用该插件。构建和离线测试不会启动它或发送消息。
