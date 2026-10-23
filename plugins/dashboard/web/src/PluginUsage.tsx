// 文件名：PluginUsage.tsx
// 主要功能：展示插件介绍、使用步骤与命令示例，支持复制触发词及失败反馈。
// 作者：2guan
import { useState } from "react";
import { Copy, Check } from "@phosphor-icons/react";
import type { Plugin } from "./types";
import { Modal } from "./ui";

export function PluginUsage({
  plugin,
  onClose,
}: {
  plugin: Plugin;
  onClose: () => void;
}) {
  const [copied, setCopied] = useState<string | null>(null);
  const [error, setError] = useState("");
  const commands = plugin.usage?.commands || [];

  // 浏览器拒绝剪贴板时保留可选中的原文，绝不把失败显示成复制成功。
  async function copy(text: string) {
    setError("");
    setCopied(null);
    try {
      await navigator.clipboard.writeText(text);
      setCopied(text);
    } catch {
      setError("无法访问剪贴板，请选中示例文字手动复制。");
    }
  }

  return (
    <Modal
      open
      onClose={onClose}
      title={`${plugin.name} · 使用说明`}
      description="了解插件用法，点击示例即可复制触发词。"
    >
      <div className="plugin-usage">
        <section>
          <h3>插件介绍</h3>
          <p>{plugin.description || "插件暂未提供介绍。"}</p>
        </section>
        <section className="plugin-examples">
          <h3>触发词示例</h3>
          <p className="plugin-trigger-caption">
            点击标签复制；需要参数、真实 @ 或引用时，请按下方提示补全。
          </p>
          {commands.map((command, index) => (
            <div className="plugin-command" key={`${command.name}-${index}`}>
              <h4>{command.name}</h4>
              {command.description && <p>{command.description}</p>}
              {!!command.examples?.length ? (
                <div
                  className="plugin-trigger-list"
                  aria-label={`${command.name}示例`}
                >
                  {[...new Set(command.examples)].map((example) => (
                    <button
                      type="button"
                      className="plugin-trigger"
                      key={example}
                      onClick={() => void copy(example)}
                      aria-label={`复制触发词：${example}`}
                    >
                      <code>{example}</code>
                      {copied === example ? (
                        <Check size={16} aria-hidden="true" />
                      ) : (
                        <Copy size={16} aria-hidden="true" />
                      )}
                    </button>
                  ))}
                </div>
              ) : command.syntax ? (
                <code className="plugin-command-syntax">{command.syntax}</code>
              ) : (
                <p className="hint">
                  此项通过页面或系统事件触发，无需聊天口令。
                </p>
              )}
            </div>
          ))}
          {!commands.length && (
            <p className="hint">
              此插件通过页面或系统事件触发，无需聊天触发词；具体入口见使用说明。
            </p>
          )}
          <div
            className="plugin-copy-feedback"
            role="status"
            aria-live="polite"
          >
            {error || (copied !== null ? "触发词已复制" : "")}
          </div>
        </section>
        <section>
          <h3>使用说明</h3>
          <ol>
            {(plugin.usage?.steps?.length
              ? plugin.usage.steps
              : ["插件暂未提供详细使用说明，请查阅插件文档。"]
            ).map((step, i) => (
              <li key={i}>{step}</li>
            ))}
          </ol>
        </section>
      </div>
    </Modal>
  );
}
