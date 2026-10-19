// 文件名：memoryRoute.ts
// 主要功能：统一模型中心记忆页签与旧记忆链接的路由。
// 作者：2guan
export const memoryTabs: [string, string][] = [
  ["memory-items", "记忆与审核"],
  ["memory-settings", "记忆设置"],
  ["memory-trace", "本次回复依据"],
];

export function currentPage() {
  const page = location.hash.slice(1).split("?")[0] || "overview";
  if (page !== "memory") return page;
  const params = new URLSearchParams(location.hash.split("?")[1]);
  const oldTab = params.get("tab") || "items";
  params.set("tab", memoryTabs.some(([id]) => id === `memory-${oldTab}`) ? `memory-${oldTab}` : "memory-items");
  history.replaceState(null, "", `#models?${params}`);
  return "models";
}
