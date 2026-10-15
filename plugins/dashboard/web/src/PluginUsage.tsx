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
        {/* 命令语法包含占位参数，单独展示；仅明确声明的示例提供复制操作。 */}
        {commands.map((command, index) => (
          <section className="plugin-command" key={`${command.name}-${index}`}>
            <h3>{command.name}</h3>
            {command.description && <p>{command.description}</p>}
            {command.syntax && (
              <pre className="plugin-command-syntax">{command.syntax}</pre>
            )}
            {!!command.examples?.length && (
              <div className="plugin-trigger-list" aria-label="触发词示例">
                <span className="plugin-trigger-caption">
                  触发词示例 · 点击复制
                </span>
                {command.examples.map((example, i) => (
                  <button
                    type="button"
                    className="plugin-trigger"
                    key={i}
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
            )}
          </section>
        ))}
        {!!commands.length && !commands.some((c) => c.examples?.length) && (
          <p className="hint">
            插件未提供触发词示例，请按上述命令用法填写参数。
          </p>
        )}
        <div className="plugin-copy-feedback" role="status" aria-live="polite">
          {error || (copied !== null ? "触发词已复制" : "")}
        </div>
      </div>
    </Modal>
  );
}
