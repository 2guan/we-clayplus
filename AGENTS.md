# 维护约定

- 提交作者和提交者统一为 `youfu-fun <24789121+youfu-fun@users.noreply.github.com>`。每次新建 clone/worktree 后先设置仓库级 `user.name` / `user.email`；提交前用 `git var GIT_AUTHOR_IDENT` 和 `git var GIT_COMMITTER_IDENT` 核对，推送前检查将推送的提交身份。不得依赖机器全局 Git 身份。

- 修改前阅读根目录 README.md 和 SECURITY.md。
- gateway 提供可导入的 Run 入口，不增加上层 main/CLI；内部实现位于 gateway/internal。
- gateway、sdk 保持平行目录；整个仓库只有根目录一个 go.mod，不添加本地 replace。
- README、运行指南、开发说明及新增维护文档使用中文；许可证和生成代码保留原格式。
- 本仓库允许公开并推送；独立 core 源码仓库保持私有。每次推送前查询目标仓库可见性，并运行暂存内容敏感信息检查。
- 不提交账号凭据、设备/会话状态、真实消息、日志、媒体、私钥或测试账号标识。
- core 保持独立；上层编译不依赖 core 源码。SDK 不导入 gateway 的实现包。
- 保留已有协议标识和行为；离线测试通过不代表所有真实微信功能已验证。
- 微信测试使用 Mac 模式。未获用户明确授权，不执行真实账号的写操作。

- 修改跨层业务接口时，对照 core 的实际参数校验与已有实测记录；发送→撤回这类串联流程必须验证原始回执字段完整传递，不能只用固定成功响应的模拟服务作为验收依据。
