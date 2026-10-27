// 文件名：types.ts
// 主要功能：定义控制台 API 类型，保持微信 ID 为字符串并显式描述页面状态。
// 作者：2guan
export type Contact = {
  enterprise?: boolean;
  profile_note?: string;
 wechat_pinned?: boolean;
 muted?: boolean;
  alias?: string;
  signature?: string;
  id: string;
  name: string;
  remark: string;
  avatar: string;
  kind: string;
  region: string;
  tags: string[];
  members: number;
  unread: number;
  pinned: boolean;
  mode_inherited?: boolean;
  mode: string;
  preview: string;
  time: string;
};
export type Message = {
	image_generated?: boolean;
	generation_prompt?: string;
	image_analysis?: {
		status: "pending" | "running" | "completed" | "failed";
		description?: string;
		error?: string;
		model?: string;
		task_id?: string;
		updated: string;
		cache_hit?: boolean;
	};
  music?: { app_id: string; title: string; artist?: string; url: string; audio_url: string; cover?: string; lyric?: string };
  rich?: {
    file_notice_id?: string;
    title?: string;
    description?: string;
    amount?: string;
    status?: string;
    author?: string;
    quote?: string;
    quote_id?: string;
    quote_type?: number;
    quote_url?: string;
    latitude?: number;
    longitude?: number;
    sticker?: string;
    image?: string;
  };
  articles?: {
    title: string;
    description?: string;
    url: string;
    image?: string;
  }[];
  publisher?: string;
  mentions?: string[];
  mention_all?: boolean;
  interaction?: { kind: string; actor_id?: string; actor_name?: string; target_id?: string; target_name?: string; all?: boolean; related_id?: string; verified: boolean };
  sender_id?: string;
  sender_avatar?: string;
  id: string;
  target: string;
  sender: string;
  content: string;
  kind: string;
  self: boolean;
  source: string;
  status: string;
  time: string;
  media?: string;
  duration?: number;
};
export type Moment = {
 like_users?: {id: string; name: string}[];
 original_images?: string[];
 thumbnail?: string;
  link?: string;
  title?: string;
  kind?: string;
  author_id?: string; // 稳定作者标识，用于按好友筛选；缺失时不通过昵称猜测。
  id: string;
  author: string;
  avatar: string;
  content: string;
  images: string[];
  liked: boolean;
  likes: number;
  time: string;
  comments: { author: string; content: string }[];
};
export type Vendor = {
  id: string;
  name: string;
  provider: string;
  endpoint: string;
  configured: boolean;
  key?: string;
};
export type BehaviorPolicy = {
  actors?: string[];
  allow_cross?: boolean;
  confirm?: boolean;
  operation: string;
  enabled: boolean;
  prompt: string;
  scope: string;
  list_mode: string;
  targets: string[];
};
export type Model = {
  vendor_id?: string;
  timeout?: number;
  temperature?: number;
  thinking?: string;
  context?: number;
  context_tokens?: number;
  max_output?: number;
  id: string;
  name: string;
  provider: string;
  model: string;
  endpoint: string;
  capabilities: string[];
  enabled: boolean;
  configured: boolean;
  key?: string;
};
export type Prompt = {
  id: string;
  name: string;
  description: string;
  content: string;
};
export type User = {
  id: string;
  name: string;
  nickname: string;
  role: string;
  enabled: boolean;
};
export type Task = {
	  reply_to?: string;
	  reply_retryable?: boolean;
	  retry_of?: string;
	  auto_retries?: number;
  insight?: { type: "conversation" | "group" | "moments"; prompt: string; target_name?: string;
 managed?: boolean; root_id?: string; parent_id?: string; turn_id?: string; question?: string;
 snapshot_id?: string; range?: {mode: string; count: number; start?: string; end?: string};
 captured?: string; cache_hit?: boolean; count?: number; image_count?: number; skipped_images?: number;
 progress?: string; requested_model?: string; retry_of?: string;
 };
  source?: string;
  delivery?: "sending" | "sent" | "unknown";
  id: string;
  kind: string;
  name: string;
  status: string;
  created: string;
  result: string;
  error?: string;
  input: string;
  model: string;
  target?: string;
};
export type Plugin = {
  id: string;
  name: string;
  description: string;
  version: string;
  enabled: boolean;
  builtin: boolean;
  status: string;
  abilities: string[];
  // 使用说明只来自插件目录，示例文本原样复制，不直接执行。
  usage?: {
    steps: string[];
    commands?: {
      name: string;
      description: string;
      syntax: string;
      examples?: string[];
    }[];
  };
};
export type Log = {
  id: string;
  time: string;
  level: string;
  component: string;
  message: string;
  target?: string;
  // 微信消息投影出的单行摘要仅供列表展示，完整原文仍保存在 message 中。
  summary?: string;
};
export type Field = {
  preview: boolean;
  key: string;
  label: string;
  type: string;
  default: string | number | boolean;
  help: string;
  options?: string[];
  min?: number;
  max?: number;
};
export type Group = {
  id: string;
  title: string;
  description: string;
  area: string;
  admin: boolean;
  effect: string;
  fields: Field[];
};
export type State = {
  uptime_seconds?: number | null; // 当前core进程运行秒数；无法读取或Mock时为null。
  config_revision: number;
  revision: number;
  contact_sync?: string;
  mock: boolean;
  scenario: string;
  account: {
    moment_cover?: string;
    photo?: string;
    name: string;
    id: string;
    avatar: string;
    owner: string;
    status: string;
  };
  contacts: Contact[];
  messages: Message[];
  moments: Moment[];
  vendors: Vendor[];
  pipeline: string[];
  pipelines?: Record<string, string[]>;
  policies: BehaviorPolicy[];
  models: Model[];
  prompts: Prompt[];
  tasks: Task[];
  plugins: Plugin[];
  logs: Log[];
  users: User[];
  settings: Record<string, string | number | boolean>;
  overrides: Record<string, Record<string, unknown>>;
  history: {
    id: string;
    time: string;
    actor: string;
    group: string;
    summary: string;
  }[];
  trend: {
    date: string;
    input: number;
    output: number;
    received: number;
    sent: number;
  }[];
};
export type Auth = {
  brandName: string;
  initialized: boolean;
  authenticated: boolean;
  contact_sync?: string;
  mock: boolean;
  user: User;
};
export type Action = Record<string, unknown> & { kind: string };
export type PageProps = {
  state: State;
  schema: Group[];
  user: User;
  act: (action: Action) => Promise<void>;
  notify: (message: string) => void;
  refresh: () => Promise<void>;
};
export const abilities: Record<string, string> = {
  chat: "对话",
  vision: "识图",
  audio: "识音频",
  video: "识视频",
  video_generation: "视频生成",
  tts: "语音合成",
  image: "图片生成",
  insight: "会话洞察",
};

// 旧配置仍优先使用原默认模型；保存空数组后不再自动补入其他模型。
export function modelPipeline(state: State, capability: string): string[] {
  const saved =
    capability === "chat" ? state.pipeline : state.pipelines?.[capability];
  if (saved != null) return saved;
  const ids = state.models
    .filter((m) => m.enabled && m.capabilities.includes(capability))
    .map((m) => m.id);
  const preferred = String(state.settings[`routing.${capability}`] || "");
  return ids.includes(preferred)
    ? [preferred, ...ids.filter((id) => id !== preferred)]
    : ids;
}

export type MemorySource = { target: string; message_id: string; sender_id: string; hash: string };
export type MemoryItem = {
  id: string; scope: "person" | "group" | "bot"; subject: string; target: string; category: string; key: string; content: string;
  status: "pending" | "confirmed" | "conflicted" | "rejected" | "expired" | "superseded" | "deleted";
  sensitivity: "low" | "sensitive"; consent: boolean; allow_model?: boolean; origin: "manual" | "scan" | "explicit";
  created_at: string; confirmed_at: string; confirmed_by: string; expires_at?: string; version: number; sources: MemorySource[];
};
export type MemoryScan = { id: string; target: string; status: string; processed: number; total: number; cursor: number; upper: number; error?: string; warning?: string; created_at: string; updated_at: string };
export type MemoryTrace = { current_id: string; history_ids: string[]; memory_ids: string[]; memory_versions: number[]; [key: string]: unknown };
