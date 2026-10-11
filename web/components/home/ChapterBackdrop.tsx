"use client";

// Tinted backgrounds for the chapters: soft gradients in the chapter colour
// that drift slowly (CSS only, paused while inactive) and fade in when the
// chapter becomes active.

const rgb = (hex: string) => {
  const n = parseInt(hex.slice(1), 16);
  return `${(n >> 16) & 255},${(n >> 8) & 255},${n & 255}`;
};

export default function ChapterBackdrop({ color, accent = "#38e1ff", on }: { color: string; accent?: string; on: boolean }) {
  const a = rgb(color);
  const b = rgb(accent);
  const mask = "radial-gradient(ellipse 75% 70% at 50% 50%, #000 35%, transparent 100%)";
  return (
    <div
      aria-hidden
      data-on={on ? "1" : "0"}
      className="np-bd pointer-events-none absolute inset-0 overflow-hidden"
      style={{ opacity: on ? 1 : 0, WebkitMaskImage: mask, maskImage: mask }}
    >
      <div className="absolute inset-0" style={{ background: `radial-gradient(ellipse 65% 60% at 50% 52%, rgba(${a},0.14), transparent 70%)` }} />
      <div className="np-blob" style={{ left: "30%", top: "38%", width: "58%", background: `radial-gradient(circle, rgba(${a},0.22), transparent 65%)` }} />
      <div className="np-blob" style={{ left: "72%", top: "64%", width: "50%", background: `radial-gradient(circle, rgba(${b},0.15), transparent 65%)`, animationDelay: "-6s", animationDuration: "19s" }} />
    </div>
  );
}
