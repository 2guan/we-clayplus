import type { Plugin } from "./types";

export const pluginCategories = [
  { id: "all", label: "全部", plugins: [] },
  {
    id: "foundation",
    label: "基础与 AI",
    plugins: ["dashboard", "ai", "prompt_guard", "context_cleaner"],
  },
  {
    id: "games",
    label: "游戏与聚会",
    plugins: [
      "turtlesoup",
      "undercover",
      "werewolf",
      "avalon",
      "murder",
      "chemistry",
    ],
  },
  {
    id: "stories",
    label: "剧情与冒险",
    plugins: ["romance", "radio", "dungeon", "trpg"],
  },
  {
    id: "daily",
    label: "日常与内容",
    plugins: [
      "statistics",
      "wordcloud",
      "digest",
      "news",
      "video_parser",
      "music",
      "meme",
      "what_to_eat",
      "moyu_daily",
      "fortune",
      "personality",
      "group_digest",
      "group_memes",
    ],
  },
  {
    id: "automation",
    label: "自动化",
    plugins: ["cron", "auto_accept", "welcome"],
  },
  { id: "extensions", label: "扩展插件", plugins: [] },
];

export function pluginCategory(plugin: Plugin): string {
  return (
    pluginCategories.find((c) => c.plugins.includes(plugin.id))?.id ||
    "extensions"
  );
}

export function readPluginCategory(): string {
  const value = new URLSearchParams(location.hash.split("?")[1]).get(
    "category",
  );
  return pluginCategories.some((c) => c.id === value) ? value! : "all";
}
