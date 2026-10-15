// 文件名：ModelTasks.tsx
// 主要功能：复用 AI 任务状态、媒体预览与可取消任务列表。
// 作者：2guan
import { lazy, Suspense, useState, type ReactNode } from "react";
import {
  CheckCircle,
  SpeakerHigh,
  WarningCircle,
  X,
  ArrowUpRight,
  Trash,
  TextAlignLeft,
  CaretDown,
  Copy,
} from "@phosphor-icons/react";
import { type Contact, type Task, type PageProps } from "./types";
import { Badge, Modal, Empty } from "./ui";
import { dateTime } from "./api";
const MarkdownReport = lazy(() => import("./MarkdownReport"));
const reportTypes = { conversation: "会话总结", group: "群聊分析", moments: "朋友圈内容" };
const reportDate = (value: string) => value ? new Date(value).toLocaleString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }) : "—";
const reportType = (task: Task) => task.insight ? reportTypes[task.insight.type] || "未记录类型" : "未记录类型";
const reportTarget = (task: Task, contacts: Contact[]) => {
  const contact = contacts.find(c => c.id === task.target);
  return task.insight?.target_name || contact?.remark || contact?.name || task.target || "未记录对象";
};
const reportModel = (task: Task) => task.model || (["queued", "running"].includes(task.status) ? "等待分配" : "未记录模型");
export const reportLabel = (task: Task, contacts: Contact[]) => `${reportDate(task.created)} · ${reportType(task)} · ${reportTarget(task, contacts)}`;

function ReportPrompt({ task, notify, inline = false }: Pick<PageProps, "notify"> & { task: Task; inline?: boolean }) {
  const [open, setOpen] = useState(false);
  const prompt = task.insight?.prompt || task.input;
  if (!prompt) return <span className="report-muted">未记录提示词</span>;
  const copyButton = <button type="button" className="icon-button" aria-label="复制完整提示词" title="复制完整提示词" onClick={async () => {
    try { await navigator.clipboard.writeText(prompt); notify("已复制完整提示词"); }
    catch { notify("复制失败，请检查浏览器剪贴板权限"); }
  }}><Copy size={16} /></button>;
  if (inline) return <div className="report-prompt-preview">
    <details className="report-prompt-inline">
      <summary className="report-prompt-link" aria-label="查看任务提示词"><TextAlignLeft size={15} aria-hidden="true" /><span>{prompt}</span><CaretDown size={14} className="report-prompt-chevron" aria-hidden="true" /></summary>
      <pre className="report-prompt-full">{prompt}</pre>
    </details>
    {copyButton}
  </div>;
  return <>
    <div className="report-prompt-preview">
      <button type="button" className="report-prompt-link" aria-label="查看任务提示词" onClick={() => setOpen(true)}>
        <TextAlignLeft size={15} aria-hidden="true" /><span>{prompt}</span>
      </button>
      {copyButton}
    </div>
    <Modal open={open} onClose={() => setOpen(false)} title="任务提示词" description={`${reportType(task)} · ${reportDate(task.created)}`} wide>
      <div className="report-prompt-actions">{copyButton}</div>
      <pre className="report-prompt-full">{prompt}</pre>
    </Modal>
  </>;
}

export function ReportMetadata({ task, contacts, actions, notify }: Pick<PageProps, "notify"> & { task: Task; contacts: Contact[]; actions?: ReactNode }) {
  return <dl className="report-metadata" aria-label="报告信息">
    <div><dt>任务类型</dt><dd>{reportType(task)}</dd></div>
    <div><dt>分析对象</dt><dd>{reportTarget(task, contacts)}</dd></div>
    <div><dt>使用模型</dt><dd>{reportModel(task)}</dd></div>
    <div><dt>创建时间</dt><dd>{reportDate(task.created)}</dd></div>
    <div><dt>状态</dt><dd><TaskBadge task={task} /></dd></div>
    {actions && <div><dt>操作</dt><dd>{actions}</dd></div>}
    <div className="report-metadata-prompt"><dt>任务提示词</dt><dd><ReportPrompt key={task.id} task={task} notify={notify} inline /></dd></div>
  </dl>;
}
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
  if (task.error && !(task.kind === "insight" && task.result))
    return (
      <div className="task-error">
        <WarningCircle size={28} />
        <h3>这次任务没有完成</h3>
        <p>{task.error}</p>
      </div>
    );
  if ((task.status === "running" || task.status === "queued") && !(task.kind === "insight" && task.result))
    return (
      <div className="empty">
        <span className="loader" />
        <h3>正在处理</h3>
        <p>可以切换页面，任务会在后台继续。</p>
      </div>
    );
  if (!task.result) return <Empty title="任务已停止" />;
  if (task.error && task.kind === "insight")
    return (
      <>
        <div className="task-error" role="alert">
          <WarningCircle size={24} />
          <h3>报告可能不完整</h3>
          <p>{task.error}</p>
        </div>
        <div className="task-result">
          <Suspense fallback={<div className="prose">{task.result}</div>}>
            <MarkdownReport content={task.result} />
          </Suspense>
        </div>
      </>
    );
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
export function DeleteReportButton({ task, act, notify, onDeleted }: Pick<PageProps, "act" | "notify"> & { task: Task; onDeleted?: () => void }) {
  const [busy, setBusy] = useState(false);
  if (task.kind !== "insight" || ["queued", "running"].includes(task.status)) return null;
  return <button
    className="icon-button"
    title="删除报告"
    aria-label="删除报告"
    disabled={busy}
    onClick={async () => {
      if (!confirm("确定删除这份分析报告？删除后无法恢复，不影响原始聊天记录。")) return;
      setBusy(true);
      try {
        await act({ kind: "delete-report", id: task.id });
        onDeleted?.();
        notify("分析报告已删除");
      } catch (e) { notify((e as Error).message); }
      finally { setBusy(false); }
    }}
  >{busy ? <span className="loader" /> : <Trash size={18} />}</button>;
}

// 任务详情使用最新状态回读；取消由后端传播 context，界面只展示确认结果。
export function TaskList({ state, act, notify, onReportDeleted, reportsOnly = false }: PageProps & { onReportDeleted?: () => void; reportsOnly?: boolean }) {
  const [detail, setDetail] = useState<Task | null>(null);
  const currentDetail = state.tasks.find((t) => t.id === detail?.id);
  return (
    <section className="panel task-panel">
      <div className="section-heading">
        <div>
          <h3>{reportsOnly ? "历史报告" : "任务记录"}</h3>
          <p>{reportsOnly ? "查看分析记录与生成依据。" : "每一次调用都有清晰的结果。"}</p>
        </div>
        <Badge>{state.tasks.length} 项</Badge>
      </div>
      {!state.tasks.length ? (
        <Empty title="暂无任务" text="实验室测试与洞察报告会记录在这里。" />
      ) : (
        <div className="table-scroll">
          <table className={reportsOnly ? "report-table" : undefined}>
            <thead>
              <tr>
                <th>{reportsOnly ? "任务类型" : "任务"}</th>
                {reportsOnly && <><th>任务提示词</th><th>分析对象</th></>}
                <th>使用模型</th>
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
                        {reportsOnly ? reportType(t) : t.name}
                      </button>
                      {!reportsOnly && <small>{t.input.slice(0, 45)}</small>}
                    </td>
                    {reportsOnly && <><td className="report-prompt-cell"><ReportPrompt task={t} notify={notify} /></td><td className="report-target-cell">{reportTarget(t, state.contacts)}</td></>}
                    <td>{reportModel(t)}</td>
                    <td>{reportsOnly ? reportDate(t.created) : dateTime(t.created)}</td>
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
                      <DeleteReportButton task={t} act={act} notify={notify} onDeleted={onReportDeleted} />
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      )}
      <Modal
        open={!!currentDetail}
        onClose={() => setDetail(null)}
        title={detail?.name || "任务详情"}
        description={detail?.model || "执行详情"}
        wide
      >
        {currentDetail && (
          <>
            {currentDetail.kind === "insight" && <ReportMetadata task={currentDetail} contacts={state.contacts} notify={notify} actions={<DeleteReportButton task={currentDetail} act={act} notify={notify} onDeleted={() => { setDetail(null); onReportDeleted?.(); }} />} />}
            <TaskResult task={currentDetail} />
          </>
        )}
      </Modal>
    </section>
  );
}
