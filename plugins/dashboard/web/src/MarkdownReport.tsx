// 文件名：MarkdownReport.tsx
// 主要功能：安全渲染洞察报告的 Markdown、表格及 LaTeX 数学公式。
// 作者：2guan
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import remarkMath from 'remark-math';
import rehypeKatex from 'rehype-katex';
import 'katex/dist/katex.min.css';
import './markdown-report.css';

export default function MarkdownReport({content}: {content: string}) {
 // 不执行模型生成的 HTML，公式禁用信任扩展；代码块保持原文，不参与公式解析。
 return <article className="markdown-report"><Markdown
  skipHtml
  remarkPlugins={[remarkGfm, remarkMath]}
  rehypePlugins={[[rehypeKatex,{trust:false,strict:'ignore',throwOnError:false,maxExpand:1000}]]}
  components={{
   a: ({href,children}) => <a href={href} target="_blank" rel="noopener noreferrer">{children}</a>,
   table: ({children}) => <div className="report-table-scroll" tabIndex={0} aria-label="报告表格"><table>{children}</table></div>,
   // 报告里的外部图片不自动请求，避免模型输出地址造成隐式网络访问。
   img: ({alt}) => <span className="muted">[图片：{alt || '未提供说明'}]</span>,
  }}
 >{content}</Markdown></article>;
}
