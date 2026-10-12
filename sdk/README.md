# 插件 SDK

SDK 是 Go 包集合，与 gateway 平行放在同一个仓库，共用根目录的 go.mod。
插件导入 `github.com/we-clay/we-clay/sdk/...`，由 gateway 注入所需能力。
SDK 本身无需启动。当前 SDK 调用 gateway，不能直接把连接地址改成 core。

- [插件开发](../docs/插件开发.md)
- [协议生成与验证](../docs/开发指南.md)
- [完整插件示例](../examples/plugin/)

`proto/` 源文件位于仓库根目录的 `../proto`；此目录保留生成代码和调用封装。
消息大文件使用能力探测和分块传输。旧版 gateway 不支持的文件或大消息会在发送前失败。
协议服务名和字段编号保持原有值；插件握手标识改为 we-clay，旧插件需要更新 SDK 并重新编译。

`sdk/corewire` 是 gateway/core 的进程间协议包，由 core 实现；普通插件仍导入各业务 SDK 包，
由 gateway 注入能力。它不包含 core 源码，不需要新增仓库或 go.mod。
