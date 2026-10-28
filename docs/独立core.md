# 独立 core

## 保留 core，重启应用

下面的 `cfg` 在每次启动和停止时使用相同配置。`WE_CLAY_DATA_DIR` 必须是现有账号目录的固定绝对路径，不能每次创建临时目录。

```go
cfg := gateway.Config{
    CoreAddress: "127.0.0.1:8088",
    Core: &gateway.CoreConfig{
        Version: "v0.3.1", // 固定已发布版本，不自动升级
        DataDir: os.Getenv("WE_CLAY_DATA_DIR"),
        LoginAddress: "127.0.0.1:8086",
        KeepAlive: true,
    },
}
err := gateway.Run(ctx, cfg, gateway.WithoutPlugins(), gateway.WithHandlers(handlers))
```

首次调用会下载、校验、启动 core，设备类型默认 `mac`；配套支持 `win` 的 core 发布版时，可用 `Core.DeviceType: "win"` 和独立新账号目录选择 Windows 设备信息。取消 `ctx` 或关闭应用会释放 gateway 的插件、缓存和 gRPC 连接，core 继续运行并保留微信连接。下次应用启动，按账号目录中的记录核对版本、地址、设备类型、进程启动标识和令牌后复用；只有微信状态就绪后才触发新的 `OnReady`。业务层应使用新 Client，旧 Client 已关闭。

- 支持 macOS、Linux、Windows；不是开机自启服务，也不会在 core 崩溃后无限重启。
- 一个 core 仍只允许一个活动事件订阅者，不要同时启动两套 robot。
- core 有有界的持久化事件重放队列；业务层要落库去重，不能把它当成无限消息归档，也不要对重放历史自动回复。
- 旧进程记录缺少设备类型时按 `mac` 兼容；类型不匹配时拒绝复用和停止。维护时保持原类型，新 Windows 设备须另用新目录，不能把 Mac 档案改为 `win`。Windows 模式尚未完成真实登录验收。
- 已有版本/地址不同会报错，不会自动停止或升级在线 core。修改配置前先用原配置调用 `StopCore`。
- 账号目录也必须保持为启动 core 时的绝对路径。需要迁移目录时，先用原配置显式 `StopCore`，移动目录后再用新路径启动；不要在 core 在线时移动目录，也不要把 `gateway-core.json` 复制到另一套活动目录。新版本会核对记录中的启动路径；旧版记录会先核对运行进程的 `-data` 参数，再补写路径。
- 没有合法进程记录时，不接管占用端口的程序；首次从旧托管模式切换，要先退出旧应用一次。
- 启动后登录超时或取消时仍保留 core，可再次连接或显式停止。

## 显式停止

```go
stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()
if err := gateway.StopCore(stopCtx, cfg); err != nil {
    log.Print(err)
}
```

不要在普通程序退出时 `defer StopCore`，否则会失去独立运行效果。停止发送操作系统退出信号，保留账号/设备目录，不调用微信退出登录。身份无法确认时不会结束任何进程，需要检查本机日志。

`gateway-core.json` 位于账号目录，仅本机用户可读，包含 API 令牌。如果没有配置 `WE_CLAY_CORE_TOKEN`，gateway 会自动生成并保存令牌。浏览器登录页的受保护操作（例如提交验证码、查看收件箱）仍需要在访问令牌框填写该本机令牌；不要把记录或令牌粘贴到日志、仓库或聊天。普通扫码状态由 core 页面提供。

媒体调用见[统一媒体下载](媒体下载.md)。
