// 文件名：ModelTasks.tsx
// 主要功能：复用 AI 任务状态、媒体预览与可取消任务列表。
// 作者：2guan
import { lazy, Suspense, useState } from "react";
import {
  CheckCircle,
  SpeakerHigh,
  WarningCircle,
  X,
  ArrowUpRight,
} from "@phosphor-icons/react";
import { type Task, type PageProps } from "./types";
import { Badge, Modal, Empty } from "./ui";
import { dateTime } from "./api";
const MarkdownReport = lazy(() => import("./MarkdownReport"));
export function TaskBadge({ task }: { task: Task }) {
  const label: Record<string, string> = {
    queued: "排队中",
    running: "处理中",
    completed: "已完成",
    failed: "失败",
    cancelled: "已取消",
    interrupted: "已中断",
  };
  return (
    <Badge
      tone={
        task.status === "completed"
          ? "green"
          : ["failed", "interrupted"].includes(task.status)
            ? "red"
            : ""
      }
    >
      {task.status === "completed" && <CheckCircle size={12} />}
      {label[task.status] || task.status}
    </Badge>
  );
}
// 根据任务类型展示文本、图片和音频，不将模型文本当作 HTML 执行。
export function TaskResult({ task }: { task: Task }) {
  if (task.error)
    return (
      <div className="task-error">
        <WarningCircle size={28} />
        <h3>这次任务没有完成</h3>
        <p>{task.error}</p>
      </div>
    );
  if (task.status === "running" || task.status === "queued")
    return (
      <div className="empty">
        <span className="loader" />
        <h3>正在处理</h3>
        <p>可以切换页面，任务会在后台继续。</p>
      </div>
    );
  if (!task.result) return <Empty title="任务已停止" />;
  return (
    <div className="task-result">
      {task.kind === "image" ? (
        <img src={task.result} alt="生成结果" />
      ) : task.kind === "video_generation" ? (
        <>
          <video
            controls
            playsInline
            preload="metadata"
            src={task.result}
            aria-label="生成视频"
          />
          <a href={task.result} download className="text-link">
            下载视频
          </a>
        </>
      ) : task.kind === "tts" ? (
        <>
          <SpeakerHigh size={44} />
          <audio controls src={task.result} />
          <a href={task.result} download className="text-link">
            下载音频
          </a>
        </>
      ) : task.kind === "insight" ? (
        <Suspense fallback={<div className="prose">{task.result}</div>}><MarkdownReport content={task.result} /></Suspense>
      ) : (
        <div className="prose">{task.result}</div>
      )}
    </div>
  );
}
// 任务详情使用最新状态回读；取消由后端传播 context，界面只展示确认结果。
export function TaskList({ state, act, notify }: PageProps) {
  const [detail, setDetail] = useState<Task | null>(null);
  return (
    <section className="panel task-panel">
      <div className="section-heading">
        <div>
          <h3>任务记录</h3>
          <p>每一次调用都有清晰的结果。</p>
        </div>
        <Badge>{state.tasks.length} 项</Badge>
      </div>
      {!state.tasks.length ? (
        <Empty title="暂无任务" text="实验室测试与洞察报告会记录在这里。" />
      ) : (
        <div className="table-scroll">
          <table>
            <thead>
              <tr>
                <th>任务</th>
                <th>模型</th>
                <th>创建时间</th>
                <th>状态</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {state.tasks
                .slice(-20)
                .reverse()
                .map((t) => (
                  <tr key={t.id}>
                    <td>
                      <button
                        className="table-link"
                        onClick={() => setDetail(t)}
                      >
                        {t.name}
                      </button>
                      <small>{t.input.slice(0, 45)}</small>
                    </td>
                    <td>{t.model || "—"}</td>
                    <td>{dateTime(t.created)}</td>
                    <td>
                      <TaskBadge task={t} />
                    </td>
                    <td>
                      {["running", "queued"].includes(t.status) ? (
                        <button
                          className="icon-button"
                          aria-label="取消任务"
                          onClick={() =>
                            act({ kind: "cancel-task", id: t.id }).catch((e) =>
                              notify(e.message),
                            )
                          }
                        >
                          <X size={17} />
                        </button>
                      ) : (
                        <button
                          className="icon-button"
                          aria-label="查看任务"
                          onClick={() => setDetail(t)}
                        >
                          <ArrowUpRight size={18} />
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      )}
      <Modal
        open={!!detail}
        onClose={() => setDetail(null)}
        title={detail?.name || "任务详情"}
        description={detail?.model || "执行详情"}
        wide
      >
        {detail && (
          <TaskResult
            task={state.tasks.find((t) => t.id === detail.id) || detail}
          />
        )}
      </Modal>
    </section>
  );
}
