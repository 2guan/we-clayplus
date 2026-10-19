// 会话行铺满列表宽度，滚动滑块覆盖在内容上，不为轨道预留底色区域。
import { useId, useLayoutEffect, useRef, useState, type ReactNode } from "react";

export function ConversationScroller({ children }: { children: ReactNode }) {
  const id = useId();
  const list = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const drag = useRef<{ y: number; top: number } | null>(null);
  const [active, setActive] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [focused, setFocused] = useState(false);
  const [metrics, setMetrics] = useState({ top: 0, range: 0, height: 0, track: 0 });
  const measure = () => {
    const el = list.current;
    if (!el) return;
    setMetrics({ top: el.scrollTop, range: el.scrollHeight - el.clientHeight,
      height: Math.min(el.clientHeight, Math.max(28, el.clientHeight ** 2 / Math.max(1, el.scrollHeight))),
      track: el.clientHeight });
  };
  useLayoutEffect(() => {
    measure();
    const observer = new ResizeObserver(measure);
    if (list.current) observer.observe(list.current);
    if (content.current) observer.observe(content.current);
    return () => { observer.disconnect(); clearTimeout(timer.current); };
  }, []);
  useLayoutEffect(measure, [children]);
  const travel = metrics.track - metrics.height;
  return (
    <div className="conversation-scroll-area">
      <div id={id} ref={list} className="conversations" onScroll={() => {
        measure();
        setActive(true);
        clearTimeout(timer.current);
        timer.current = setTimeout(() => setActive(false), 700);
      }}>
        <div ref={content}>{children}</div>
      </div>
      {metrics.range > 0 && (
        <div
          className={`conversation-scroll-thumb${active || dragging || focused ? " visible" : ""}`}
          style={{ height: metrics.height, top: metrics.top / metrics.range * travel }}
          role="scrollbar" tabIndex={0} aria-label="滚动会话列表"
          aria-controls={id} aria-orientation="vertical"
          aria-valuemin={0} aria-valuemax={Math.round(metrics.range)} aria-valuenow={Math.round(metrics.top)}
          onFocus={() => setFocused(true)} onBlur={() => setFocused(false)}
          onPointerDown={(e) => {
            if (e.button !== 0) return;
            e.preventDefault();
            drag.current = { y: e.clientY, top: list.current?.scrollTop || 0 };
            setDragging(true);
            e.currentTarget.setPointerCapture(e.pointerId);
          }}
          onPointerMove={(e) => {
            if (drag.current && list.current && travel > 0)
              list.current.scrollTop = drag.current.top + (e.clientY - drag.current.y) * metrics.range / travel;
          }}
          onPointerUp={(e) => { if (e.currentTarget.hasPointerCapture(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId); }}
          onLostPointerCapture={() => { drag.current = null; setDragging(false); }}
          onKeyDown={(e) => {
            const el = list.current;
            if (!el) return;
            const positions: Record<string, number> = {
              ArrowUp: el.scrollTop - 40, ArrowDown: el.scrollTop + 40,
              PageUp: el.scrollTop - el.clientHeight, PageDown: el.scrollTop + el.clientHeight,
              Home: 0, End: metrics.range,
            };
            if (e.key in positions) { e.preventDefault(); el.scrollTop = positions[e.key]; }
          }}
        />
      )}
    </div>
  );
}
