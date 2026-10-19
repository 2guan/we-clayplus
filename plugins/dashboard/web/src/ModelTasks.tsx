// 文件名：ModelTasks.tsx
// 主要功能：复用 AI 任务状态、媒体预览与可取消任务列表。
// 作者：2guan
import { lazy, Suspense, useState, useEffect, type ReactNode } from "react";
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
 ArrowClockwise,
 DownloadSimple,
} from "@phosphor-icons/react";
import { type Contact, type Task, type PageProps } from "./types";
import { Badge, Modal, Empty } from "./ui";
import { dateTime, request, download } from "./api";
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
      if (!confirm("确定删除这份分析报告及其重试、追问记录？删除后无法恢复，不影响原始聊天记录。")) return;
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
export function TaskList({ state, act, notify, refresh, onReportDeleted, reportsOnly = false }: PageProps & { onReportDeleted?: () => void; reportsOnly?: boolean }) {
  const [detail, setDetail] = useState<Task | null>(null);
  const currentDetail = state.tasks.find((t) => t.id === detail?.id);
 const visibleTasks=reportsOnly?reportRoots(state.tasks):state.tasks;
  return (
    <section className="panel task-panel">
      <div className="section-heading">
        <div>
          <h3>{reportsOnly ? "历史报告" : "任务记录"}</h3>
          <p>{reportsOnly ? "查看分析记录与生成依据。" : "每一次调用都有清晰的结果。"}</p>
        </div>
        <Badge>{visibleTasks.length} 项</Badge>
      </div>
      {!visibleTasks.length ? (
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
              {visibleTasks
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
                      <TaskBadge task={reportsOnly ? reportLatest(state.tasks,t) : t} />
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
            {currentDetail.kind === "insight" ? <ReportDetail task={currentDetail} state={state} act={act} notify={notify} refresh={refresh} onOpen={id=>setDetail({...currentDetail,id})} /> : <TaskResult task={currentDetail} />}
          </>
        )}
      </Modal>
    </section>
  );
}

const reportBusy = (task: Task) => ["running", "queued"].includes(task.status);
export const reportLatest=(tasks:Task[],task:Task)=>tasks.filter(t=>(t.id===(task.insight?.root_id||task.id)||t.insight?.root_id===(task.insight?.root_id||task.id))&&!t.insight?.question).at(-1)||task;
export const reportRoots = (tasks: Task[]) => tasks.filter(t => t.kind === "insight" && !t.insight?.retry_of && !t.insight?.parent_id);
function ReportStream({ task, refresh }: {task: Task; refresh: PageProps["refresh"]}) {
 const [body,setBody]=useState("");
 useEffect(()=>{
  setBody("");if(!reportBusy(task))return;
  const stream=new EventSource(`/api/task-stream?id=${encodeURIComponent(task.id)}`);
  const read=(event: MessageEvent<string>,append: boolean)=>{try{const text=JSON.parse(event.data).text||"";setBody(old=>append?old+text:text)}catch{}};
  stream.addEventListener("delta", e=>read(e as MessageEvent<string>,true));
  stream.addEventListener("snapshot", e=>read(e as MessageEvent<string>,false));
  stream.addEventListener("done",()=>{stream.close();void refresh()});
  return ()=>stream.close();
 },[task.id,task.status,refresh]);
 return <>{reportBusy(task) && <p className="muted" role="status">{task.insight?.progress || "正在生成…"}</p>}<TaskResult task={body && reportBusy(task) ? {...task,result:body} : task} /></>;
}
export function ReportDetail({task,state,act,notify,refresh,onOpen}: Pick<PageProps,"state"|"act"|"notify"|"refresh"> & {task: Task;onOpen?: (id:string)=>void}) {
 const root=task.insight?.root_id||task.id;
 const family=state.tasks.filter(t=>t.id===root||t.insight?.root_id===root);
 const attempts=family.filter(t=>!t.insight?.question);
 const base=attempts.at(-1)||task;
 const turns=new Map<string,Task>();
 for(const t of family)if(t.insight?.question)turns.set(t.insight.turn_id||t.id,t);
 const supplements=[...turns.values()];
 const evidenceBase=attempts.filter(t=>t.result).at(-1)||base;
 const parent=[evidenceBase,...supplements].filter(t=>t.result).at(-1);
 const busy=family.some(reportBusy);
 const [question,setQuestion]=useState("");const [posting,setPosting]=useState(false);const [error,setError]=useState("");
 const [rebuild,setRebuild]=useState(false);const [count,setCount]=useState("50");
 useEffect(()=>{setQuestion("");setError("");setRebuild(false)},[root]);
 const run=async(id: string,operation: string)=>{
  setPosting(true);setError("");
  try {const created=await request<{id:string}>("/api/reports/action",{id,operation,question,range:operation==="rebuild"?{mode:"count",count:Number(count)}:undefined});if(operation==="followup")setQuestion("");await refresh();if(operation==="rebuild")onOpen?.(created.id);notify(operation==="followup"?"追问已提交":"报告任务已创建")}
  catch(e){setError((e as Error).message)}finally{setPosting(false)}
 };
 const retry=(t: Task)=><button type="button" className="button" disabled={busy||posting} onClick={()=>void run(t.id,"retry")}><ArrowClockwise size={15} />{posting?"正在提交…":"重试"}</button>;
 const exportReport=()=>{
  const text=[`# 分析报告`, `资料采集时间（北京时间）：${base.insight?.captured?new Date(base.insight.captured).toLocaleString("zh-CN",{timeZone:"Asia/Shanghai"}):"未记录"}`,evidenceBase.error?`> 报告可能不完整：${evidenceBase.error}`:"",evidenceBase.result,
   ...supplements.flatMap((t,i)=>{
     const saved=family.filter(old=>old.insight?.turn_id===t.insight?.turn_id&&old.result).at(-1)||t;
     return [`## 补充 ${i+1}`,`追问：${t.insight?.question}`,t.error?`> 本轮未完成：${t.error}`:"",saved.result];
   })].filter(Boolean).join("\n\n");
  download("分析报告与追问.md",text,"text/markdown;charset=utf-8");
 };
 return <div className="report-detail">
  <ReportMetadata task={base} contacts={state.contacts} notify={notify} actions={<DeleteReportButton task={base} act={act} notify={notify}/>} />
  {base.insight?.captured && <p className="report-source-note">{base.insight.cache_hit?"使用缓存资料":"资料快照"} · 北京时间 {new Date(base.insight.captured).toLocaleString("zh-CN",{timeZone:"Asia/Shanghai"})} · {base.insight.count} 条记录 · {base.insight.image_count||0} 张缩略图{base.insight.skipped_images?` · 跳过 ${base.insight.skipped_images} 张不可读图片`:""}</p>}
  <div className="report-action-row">
   {["failed","interrupted","cancelled"].includes(base.status) && (base.insight?.managed?retry(base):<button className="button" onClick={()=>setRebuild(true)} title="旧报告需要补选分析范围">重试</button>)}
   {evidenceBase.result && <button type="button" className="button" onClick={exportReport}><DownloadSimple size={15}/>导出报告与追问</button>}
   {!base.insight?.snapshot_id && !busy && <button className="text-link" onClick={()=>setRebuild(v=>!v)}>重建资料</button>}
   {family.filter(reportBusy).map(t=><button key={t.id} className="button" onClick={()=>act({kind:"cancel-task",id:t.id}).catch(e=>setError(e.message))}>取消任务</button>)}
  </div>
  {rebuild && <div className="report-rebuild"><p>旧报告没有完整资料快照。重新选择范围后会创建一份新报告，资料可能与原分析不同。</p><label>重新读取条数<input type="number" min="1" value={count} onChange={e=>setCount(e.target.value)}/></label><button className="button" disabled={posting||busy||!Number.isSafeInteger(Number(count))||Number(count)<1} onClick={()=>void run(base.id,"rebuild")}>重建资料并分析</button></div>}
  {error && <p className="field-error" role="alert">{error}</p>}
  <ReportStream task={base} refresh={refresh}/>
  {attempts.length>1 && <details className="report-attempts"><summary>查看之前的 {attempts.length-1} 次尝试</summary>{attempts.slice(0,-1).map(t=><section key={t.id}><p>{reportDate(t.created)} · <TaskBadge task={t}/></p><TaskResult task={t}/></section>)}</details>}
  {supplements.map((t,i)=><section className="report-supplement" key={t.insight?.turn_id||t.id}><h3>补充 {i+1}</h3><p className="report-question">追问：{t.insight?.question}</p><ReportStream task={t} refresh={refresh}/>{["failed","interrupted","cancelled"].includes(t.status)&&retry(t)}
   {family.some(old=>old.id!==t.id&&old.insight?.turn_id===t.insight?.turn_id) && <details className="report-attempts"><summary>查看本轮之前的尝试</summary>{family.filter(old=>old.id!==t.id&&old.insight?.turn_id===t.insight?.turn_id).map(old=><section key={old.id}><p>{reportDate(old.created)} · <TaskBadge task={old}/></p><TaskResult task={old}/></section>)}</details>}
  </section>)}
  <form className="report-followup" onSubmit={e=>{e.preventDefault();if(parent)void run(parent.id,"followup")}}>
   <h3>追问与补充</h3><p className="muted">追加补充章节，保留原文。沿用原资料快照，不重新读取微信。</p>
   {base.error && base.result && <p>原报告尚未完整生成，追问会基于现有正文和资料回答。</p>}
   <label>追问内容<textarea rows={3} value={question} onChange={e=>setQuestion(e.target.value)} placeholder="例如：展开第三点，并列出对应依据…" onKeyDown={e=>{if((e.metaKey||e.ctrlKey)&&e.key==="Enter"){e.preventDefault();if(parent&&base.insight?.snapshot_id&&question.trim()&&!busy&&!posting)void run(parent.id,"followup")}}}/></label>
   <button className="button primary" disabled={!parent||!base.insight?.snapshot_id||!question.trim()||busy||posting}>{posting?"正在提交…":"生成补充"}</button>
   {!parent && <p className="muted">报告尚无正文，请先重试生成。</p>}
  </form>
 </div>;
}
