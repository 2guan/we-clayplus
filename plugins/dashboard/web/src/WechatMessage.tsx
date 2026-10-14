// 文件名：WechatMessage.tsx
// 主要功能：展示微信支付通知、位置、引用、表情及可重试加载的原生媒体。
// 作者：2guan
import { useRef, useState } from "react";
import type { Message } from "./types";

// 微信文字表情保留中文可访问名称，用 Unicode 对应表情显示；未识别代码原样保留。
const faces: Record<string, string> = {
  微笑: "🙂",
  撇嘴: "😒",
  色: "😍",
  发呆: "😳",
  得意: "😎",
  流泪: "😢",
  害羞: "☺️",
  闭嘴: "🤐",
  睡: "😴",
  大哭: "😭",
  尴尬: "😅",
  发怒: "😡",
  调皮: "😛",
  呲牙: "😁",
  惊讶: "😮",
  难过: "🙁",
  酷: "😎",
  抓狂: "😫",
  吐: "🤮",
  偷笑: "🤭",
  可爱: "😊",
  白眼: "🙄",
  傲慢: "😏",
  困: "🥱",
  惊恐: "😱",
  流汗: "😓",
  憨笑: "😄",
  悠闲: "😌",
  奋斗: "💪",
  咒骂: "🤬",
  疑问: "❓",
  嘘: "🤫",
  晕: "😵",
  衰: "😞",
  骷髅: "💀",
  敲打: "🔨",
  再见: "👋",
  擦汗: "😅",
  抠鼻: "👃",
  鼓掌: "👏",
  坏笑: "😏",
  左哼哼: "😤",
  右哼哼: "😤",
  哈欠: "🥱",
  鄙视: "👎",
  委屈: "🥺",
  快哭了: "🥹",
  阴险: "😈",
  亲亲: "😘",
  可怜: "🥺",
  笑脸: "😄",
  生病: "🤒",
  破涕为笑: "😂",
  吐舌: "😛",
  脸红: "😳",
  恐惧: "😨",
  失望: "😞",
  无语: "😑",
  嘿哈: "🙌",
  捂脸: "🤦",
  奸笑: "😏",
  机智: "😎",
  皱眉: "😟",
  耶: "✌️",
  吃瓜: "🍉",
  加油: "💪",
  汗: "😓",
  天啊: "😱",
  Emm: "🤔",
  社会社会: "🙏",
  旺柴: "🐶",
  好的: "👌",
  打脸: "🤦",
  哇: "😮",
  翻白眼: "🙄",
  666: "👍",
  让我看看: "🧐",
  叹气: "😮‍💨",
  苦涩: "🥲",
  裂开: "🫠",
  玫瑰: "🌹",
  凋谢: "🥀",
  嘴唇: "💋",
  爱心: "❤️",
  心碎: "💔",
  拥抱: "🫂",
  强: "👍",
  弱: "👎",
  握手: "🤝",
  胜利: "✌️",
  抱拳: "🙏",
  勾引: "☝️",
  拳头: "✊",
  OK: "👌",
  合十: "🙏",
  庆祝: "🎉",
  礼物: "🎁",
  红包: "🧧",
  发: "🀅",
  福: "🧧",
  咖啡: "☕",
  蛋糕: "🎂",
};
export function WechatText({ text }: { text: string }) {
  return (
    <>
      {text.split(/(\[[^\]\n]{1,12}\])/g).map((part, i) => {
        const name = part.slice(1, -1);
        return part.startsWith("[") && faces[name] ? (
          <span
            key={i}
            role="img"
            aria-label={name}
            title={part}
            className="wechat-inline-face"
          >
            {faces[name]}
          </span>
        ) : (
          part
        );
      })}
    </>
  );
}

export function WechatMessage({
  message: m,
  preview,
  openQuote,
  openPayment,
}: {
  message: Message;
  preview: (src: string) => void;
  openQuote?: (message: Message) => void;
  openPayment?: (message: Message) => void;
}) {
  const [failed, setFailed] = useState(false);
  const [playing, setPlaying] = useState(false);
  const audio = useRef<HTMLAudioElement>(null);
  const [attempt, setAttempt] = useState(0);
  const r = m.rich;
  const src =
    m.media ||
    `/api/message-media?target=${encodeURIComponent(m.target)}&id=${encodeURIComponent(m.id)}`;
  // 点击收到的支付卡片进入人工确认；展示原通知，不根据金额猜测到账状态。
  if (m.kind === "transfer" || m.kind === "redpacket")
    return (
      <div
        className={`wechat-payment ${m.kind}`}
        role={!m.self && openPayment ? "button" : undefined}
        tabIndex={!m.self && openPayment ? 0 : undefined}
        aria-label={
          !m.self && openPayment
            ? m.kind === "redpacket"
              ? "查看并领取红包"
              : "查看并确认收款"
            : undefined
        }
        onClick={() => {
          if (!m.self) openPayment?.(m);
        }}
        onKeyDown={(e) => {
          if (!m.self && openPayment && (e.key === "Enter" || e.key === " ")) {
            e.preventDefault();
            openPayment(m);
          }
        }}
      >
        <span className="wechat-payment-icon" aria-hidden="true">
          {m.kind === "transfer" ? (
            <svg viewBox="0 0 40 40">
              <circle cx="20" cy="20" r="18" />
              <path d="M12 17h17M12 17l7-7M28 23H11M28 23l-7 7" />
            </svg>
          ) : (
            <span className="wechat-envelope">¥</span>
          )}
        </span>
        <div className="wechat-payment-copy">
          <strong>
            {m.kind === "transfer"
              ? r?.amount || "转账"
              : r?.description || "恭喜发财，大吉大利"}
          </strong>
          {m.kind === "transfer" && (
            <p>
              {r?.status === "转账通知"
                ? m.self
                  ? "待对方收款"
                  : "请收款"
                : r?.status || "转账通知"}
            </p>
          )}
        </div>
        <small>{m.kind === "transfer" ? "转账" : "微信红包"}</small>
      </div>
    );
  if (m.kind === "location")
    return (
      <div className="wechat-location">
        <strong>{r?.title || "位置"}</strong>
        <p>{r?.description}</p>
        {r?.latitude != null && r?.longitude != null && (
          <a
            href={`https://uri.amap.com/marker?position=${r.longitude},${r.latitude}&name=${encodeURIComponent(r.title || "位置")}`}
            target="_blank"
            rel="noopener noreferrer"
          >
            <span className="wechat-location-pin" aria-hidden="true">
              ⌖
            </span>
            <span>在地图中查看</span>
          </a>
        )}
      </div>
    );
  if (m.kind === "quote")
    return (
      <div className="wechat-quote">
        <WechatText text={m.content} />
        <blockquote>
          {r?.quote_url ? (
            <a href={r.quote_url} target="_blank" rel="noopener noreferrer">
              <strong>{r.author || "引用消息"}</strong>
              <WechatText text={r.quote || "查看链接"} />
            </a>
          ) : (
            <button
              className="wechat-quote-open"
              onClick={() => openQuote?.(m)}
              disabled={!openQuote || !r?.quote_id}
              aria-label="打开引用内容"
            >
              <strong>{r?.author || "引用消息"}</strong>
              <WechatText text={r?.quote || "引用内容不可用"} />
            </button>
          )}
        </blockquote>
      </div>
    );
  if (["image", "audio", "emoji"].includes(m.kind)) {
    const url = m.kind === "emoji" ? r?.image || m.media : src;
    if (!url || failed)
      return (
        <div className="wechat-media-unavailable">
          <span>
            {m.kind === "audio"
              ? "语音"
              : m.kind === "image"
                ? "图片"
                : r?.title || "表情包"}
            {failed ? "暂时无法加载" : "暂无可用图片"}
          </span>
          {url && (
            <button
              className="button small"
              onClick={() => {
                setFailed(false);
                setAttempt(attempt + 1);
              }}
            >
              重新加载
            </button>
          )}
        </div>
      );
    if (m.kind === "audio")
      return (
        <div className={`voice-message ${playing ? "playing" : ""}`}>
          {/* 使用原生音频解码，气泡按钮负责播放状态；不伪造未读红点。 */}
          <button
            className="wechat-voice-button"
            aria-label={playing ? "暂停语音" : "播放语音"}
            aria-pressed={playing}
            style={{ width: Math.min(220, 76 + (m.duration || 1) * 5) }}
            onClick={async () => {
              if (!audio.current) return;
              if (playing) audio.current.pause();
              else {
                try {
                  await audio.current.play();
                } catch {
                  setFailed(true);
                }
              }
            }}
          >
            <svg
              className="wechat-voice-waves"
              viewBox="0 0 24 24"
              aria-hidden="true"
            >
              <path d="M5 10v4M10 7a8 8 0 0 1 0 10M15 3a14 14 0 0 1 0 18" />
            </svg>
            <span>{m.duration || "—"}″</span>
          </button>
          <audio
            ref={audio}
            key={attempt}
            preload="none"
            src={url}
            onPlay={() => {
              // 同一时间仅播放一条语音，避免重叠。
              document.querySelectorAll("audio").forEach((player) => {
                if (player !== audio.current) player.pause();
              });
              setPlaying(true);
            }}
            onPause={() => setPlaying(false)}
            onEnded={() => setPlaying(false)}
            onError={() => {
              setPlaying(false);
              setFailed(true);
            }}
          />
        </div>
      );
    return (
      <button
        className={`image-open ${m.kind === "emoji" ? "wechat-sticker" : ""}`}
        onClick={() => preview(url)}
        aria-label={m.kind === "emoji" ? "查看表情包" : "查看图片"}
      >
        <img
          key={attempt}
          src={url}
          loading="lazy"
          alt={
            m.kind === "emoji"
              ? r?.description || r?.title || "表情包"
              : "聊天图片"
          }
          width={m.kind === "emoji" ? 140 : 260}
          height={m.kind === "emoji" ? 140 : 168}
          onError={() => setFailed(true)}
        />
      </button>
    );
  }
  return <WechatText text={m.content} />;
}
