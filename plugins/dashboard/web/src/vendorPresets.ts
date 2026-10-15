// 文件名：vendorPresets.ts
// 主要功能：维护厂商常用 API 地址、协议与官方文档，供可编辑下拉选择使用。
// 作者：2guan

// 2026-10-13 核对；仅预填连接信息，模型及能力以厂商实际返回为准。
export const vendorPresets = [
  {
    "name": "OpenAI",
    "provider": "openai",
    "endpoint": "https://api.openai.com/v1",
    "docs": "https://developers.openai.com/api/reference/resources/models/methods/list",
    "note": ""
  },
  {
    "name": "Anthropic Claude",
    "provider": "anthropic",
    "endpoint": "https://api.anthropic.com/v1",
    "docs": "https://platform.claude.com/docs/en/api/overview",
    "note": ""
  },
  {
    "name": "Google Gemini",
    "provider": "gemini",
    "endpoint": "https://generativelanguage.googleapis.com/v1beta",
    "docs": "https://ai.google.dev/api/models",
    "note": ""
  },
  {
    "name": "DeepSeek",
    "provider": "openai",
    "endpoint": "https://api.deepseek.com",
    "docs": "https://api-docs.deepseek.com/",
    "note": ""
  },
  {
    "name": "小米 MiMo",
    "provider": "mimo",
    "endpoint": "https://api.xiaomimimo.com/v1",
    "docs": "https://platform.xiaomimimo.com/",
    "note": ""
  },
  {
    "name": "月之暗面 Kimi",
    "provider": "openai",
    "endpoint": "https://api.moonshot.cn/v1",
    "docs": "https://platform.kimi.com/docs/api/chat",
    "note": ""
  },
  {
    "name": "阿里云百炼（北京）",
    "provider": "openai",
    "endpoint": "https://dashscope.aliyuncs.com/compatible-mode/v1",
    "docs": "https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope",
    "note": "使用北京地域的 API Key；也可改为控制台提供的业务空间专属域名。"
  },
  {
    "name": "智谱 GLM",
    "provider": "openai",
    "endpoint": "https://open.bigmodel.cn/api/paas/v4",
    "docs": "https://docs.bigmodel.cn/cn/api/introduction",
    "note": "通用 API 地址，不是 Coding Plan 专用地址。"
  },
  {
    "name": "火山方舟（豆包）",
    "provider": "openai",
    "endpoint": "https://ark.cn-beijing.volces.com/api/v3",
    "docs": "https://docs.volcengine.com/docs/ark/base-url-and-authentication?lang=zh",
    "note": "通用 API 地址；部分模型需手动填写已开通的模型或接入点 ID。"
  },
  {
    "name": "xAI Grok",
    "provider": "openai",
    "endpoint": "https://api.x.ai/v1",
    "docs": "https://docs.x.ai/developers/quickstart",
    "note": ""
  },
  {
    "name": "硅基流动",
    "provider": "openai",
    "endpoint": "https://api.siliconflow.cn/v1",
    "docs": "https://docs.siliconflow.cn/docs/api/chat-completions-post",
    "note": ""
  }
];
