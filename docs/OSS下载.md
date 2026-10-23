# 从私有 OSS 自动下载 core

we-clay v0.6.0 增加 OSS 下载源；新的 core 版本由 Actions 发布到北京地域私有 Bucket `weclay` 的 `core/<版本>/`。配置 OSS 后，默认使用运行授权 Key 向授权服务申请短时签名链接，不必向使用者分发阿里云 AK/SK。保留 GitHub 下载兼容：不设置 `Core.OSS` 时继续原 GitHub Release 行为，已有缓存不会被覆盖或混用。

## 使用

```go
cfg := gateway.Config{
    CoreAddress: "127.0.0.1:8088",
    Core: &gateway.CoreConfig{
        Version: "v0.4.6", // 必须已经发布；不会自动追踪 latest
        DataDir: os.Getenv("WE_CLAY_DATA_DIR"), // 原账号固定绝对路径
        KeepAlive: true,
        OSS: &gateway.OSSConfig{
            Bucket: "weclay",
            Region: "cn-beijing",
            Prefix: "core",
        },
    },
}
```

默认通过运行该程序的环境提供 `WE_CLAY_AUTH_KEY`。gateway 将 Key 仅发送给 `https://nww.hihfy.com/we-clay-auth/v1/download`，取得当前系统、固定 core 版本对应的短时 OSS 清单和附件链接。链接只用于本次 HTTPS 下载，不写进配置、日志、缓存回执或 core 子进程。core 启动后仍自行验证同一个授权 Key。

已有 `OSS_ACCESS_KEY_ID`、`OSS_ACCESS_KEY_SECRET` 成对配置时，继续使用原有 OSS SDK V4 签名方式；使用 STS 时另设 `OSS_SESSION_TOKEN`。请使用专用只读 RAM 凭据，发布用的写入凭据只放 Actions Secrets，不分发给机器人开发者。只填 AK 或只填 SK 会直接报错，不会静默切换下载方式。

只读 RAM 策略示例：

```json
{
  "Version": "1",
  "Statement": [{
    "Effect": "Allow",
    "Action": ["oss:GetObject"],
    "Resource": ["acs:oss:*:*:weclay/core/*"]
  }]
}
```

私有 Bucket 保持私有，浏览器匿名访问返回 403 是正常现象。Key 模式使用授权服务签发的短时链接，旧 AK/SK 模式通过官方 OSS SDK V4 签名读取；两种方式都不需要公开 ACL，也不会把 OSS 密钥上传给微信。官方 SDK 使用方式见[阿里云文档](https://www.alibabacloud.com/help/zh/oss/developer-reference/manual-for-go-sdk-v2/)。

## 可选 TOML

```toml
[core]
version = "v0.4.6"
data_dir = "/absolute/path/to/existing/core-data"
keep_alive = true
# download_auth_base_url = "https://nww.hihfy.com/we-clay-auth" # 默认值

[core.oss]
bucket = "weclay"
region = "cn-beijing"
prefix = "core"
# endpoint = "https://oss-cn-beijing.aliyuncs.com" # 默认地域 endpoint
# endpoint = "https://weclay.cn-beijing.taihangrda.cn" # 已绑定 HTTPS 的自定义 Bucket 域名
# cname = true # 使用自定义域名时必须设置；默认 false
```

默认使用外网 HTTPS 地域 endpoint；若 Bucket 要求 CNAME，填写已绑定证书的自定义 Bucket 域名并设 `CName: true`。不要填内网地址给外网机器，也不要跳过 TLS 校验。

## 下载与升级

1. 有效缓存优先复用。首次下载时，Key 模式先向授权服务申请两个短时链接；旧 AK/SK 模式直接签名访问 OSS。两者均按固定版本读取 `core/<版本>/manifest.json`；该文件在全部文件上传且校验成功后最后写入。
2. 校验清单版本、协议版本、当前系统/架构、文件大小和 SHA-256。
3. 下载至临时文件，验证通过后才赋予执行权限并原子放入缓存；失败不启动 core、不回退其他来源。
4. 缓存按 OSS 位置、版本、平台隔离。有效缓存可离线复用，每次运行重新计算 SHA-256。
5. 更换下载源或升级独立 core，先用原配置 StopCore，再用新配置启动。账号数据目录不变。

上传者及 Bucket 权限是下载信任边界；SHA-256 用于发现损坏或内容不一致，不是独立的作者签名。
