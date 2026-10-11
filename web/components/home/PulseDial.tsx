"use client";

// A seismograph drum cut in half by the left edge of the screen. The drum
// carries the chapters as wedges (twice round, so the visible half always
// shows all of them) and a seismogram ring. The drum is one GPU layer that
// only rotates; the core, needle and stylus sit still on top of it.

import { useEffect, useMemo, useRef } from "react";

export type DialChapter = { label: string; color: string };
export type DialHandle = (x: number) => void;

const R_IN = 200;
const R_OUT = 452;
const R_TRACE = 500;
const AMP = 30;
const VB = 560; // viewBox half-size

/** The seismogram of chapter c, u in 0..1 across its wedge. */
export function traceAt(c: number, u: number): number {
  let v = 0.05 * Math.sin(u * 140 + c * 3) + 0.035 * Math.sin(u * 260 + c);
  const g = (x: number, w: number) => Math.exp(-(x * x) / (w * w));
  switch (c % 5) {
    case 0: // a quake: small P, big rolling S
      v += 0.3 * g(u - 0.27, 0.05) * Math.sin(u * 120);
      if (u > 0.42) v += 0.95 * Math.exp(-(u - 0.42) * 6) * Math.sin((u - 0.42) * 70);
      break;
    case 1: // one spike per area
      for (let j = 0; j < 5; j++) v += (j % 2 ? -0.75 : 0.85) * g(u - (0.16 + j * 0.17), 0.02);
      break;
    case 2: // three arrivals, later and stronger
      [0.28, 0.48, 0.68].forEach((u0, j) => {
        if (u > u0) v += (0.45 + 0.2 * j) * Math.exp(-(u - u0) * 22) * Math.sin((u - u0) * 150);
      });
      break;
    case 3: // shaking grows until it arrives
      if (u < 0.86) v += 0.95 * u * u * Math.sin(u * 110);
      break;
    case 4: { // the NE-PULSE pulse
      const P: [number, number][] = [[0, 0], [0.34, 0], [0.42, 0.85], [0.53, -0.95], [0.62, 0.55], [0.68, 0], [1, 0]];
      for (let i = 1; i < P.length; i++)
        if (u <= P[i][0]) {
          const a = P[i - 1], b = P[i];
          v += a[1] + ((u - a[0]) / (b[0] - a[0])) * (b[1] - a[1]);
          break;
        }
      break;
    }
  }
  return v;
}

const pt = (r: number, deg: number) => {
  const a = (deg * Math.PI) / 180;
  return [r * Math.cos(a), r * Math.sin(a)] as const;
};
const f1 = (n: number) => n.toFixed(1);
function sector(r0: number, r1: number, a0: number, a1: number) {
  const [x0, y0] = pt(r1, a0), [x1, y1] = pt(r1, a1), [x2, y2] = pt(r0, a1), [x3, y3] = pt(r0, a0);
  return `M${f1(x0)} ${f1(y0)}A${r1} ${r1} 0 0 1 ${f1(x1)} ${f1(y1)}L${f1(x2)} ${f1(y2)}A${r0} ${r0} 0 0 0 ${f1(x3)} ${f1(y3)}Z`;
}

export default function PulseDial({
  chapters,
  active,
  register,
  mini = false,
}: {
  chapters: DialChapter[];
  active: number;
  register: (fn: DialHandle) => () => void;
  mini?: boolean;
}) {
  const n = chapters.length;
  const span = 360 / (2 * n);
  const drum = useRef<HTMLDivElement>(null);
  const stylus = useRef<SVGCircleElement>(null);

  const wedges = useMemo(
    () =>
      Array.from({ length: 2 * n }, (_, k) => {
        const c = k % n;
        const a = k * span;
        let d = "";
        for (let i = 0; i <= 72; i++) {
          const u = i / 72;
          const [x, y] = pt(R_TRACE + AMP * traceAt(c, u), a - span / 2 + u * span);
          d += (i ? "L" : "M") + f1(x) + " " + f1(y);
        }
        return { k, c, a, path: sector(R_IN, R_OUT, a - span / 2 + 0.8, a + span / 2 - 0.8), trace: d };
      }),
    [n, span],
  );

  useEffect(
    () =>
      register((x) => {
        if (drum.current) drum.current.style.transform = `translate(-50%, -50%) rotate(${(-x * span).toFixed(2)}deg)`;
        // The stylus rides the trace under it.
        const k = Math.round(x);
        const u = Math.min(1, Math.max(0, x - k + 0.5));
        const r = R_TRACE + AMP * traceAt(((k % n) + n) % n, u);
        stylus.current?.setAttribute("cx", r.toFixed(1));
      }),
    [register, span, n],
  );

  const col = chapters[active]?.color ?? "#38e1ff";
  // Radius: half the column height, but never wider than the column.
  const size = mini ? "100%" : "min(92cqw, 47cqh)";
  return (
    <div className="relative h-full w-full overflow-hidden [container-type:size]" style={{ color: col, transition: "color .6s ease" }}>
      {!mini && (
        <div
          className="pointer-events-none absolute inset-0"
          style={{ background: `radial-gradient(circle at 0% 50%, ${col}2e, transparent 60%)`, transition: "background .6s ease" }}
        />
      )}
      {/* the drum: one composited layer, rotated by the scroll loop */}
      <div ref={drum} className="absolute left-0 top-1/2 aspect-square will-change-transform" style={{ width: `calc(${size} * 2)`, transform: "translate(-50%, -50%)" }}>
        <svg viewBox={`${-VB} ${-VB} ${2 * VB} ${2 * VB}`} className="block h-full w-full" aria-hidden>
          {wedges.map(({ k, c, a, path, trace }) => {
            const on = c === active;
            return (
              <g key={k} style={{ transition: "opacity .5s" }} opacity={on ? 1 : 0.55}>
                <path d={path} fill={chapters[c].color} fillOpacity={on ? 0.22 : 0.06} stroke={chapters[c].color} strokeOpacity={on ? 0.95 : 0.3} strokeWidth={on ? 3 : 1.5} />
                <path d={trace} fill="none" stroke={chapters[c].color} strokeOpacity={on ? 1 : 0.35} strokeWidth={on ? 4 : 2} strokeLinejoin="round" />
                {!mini && (
                  <text
                    transform={`rotate(${a})`}
                    x={(R_IN + R_OUT) / 2 + 12}
                    y="0"
                    textAnchor="middle"
                    dominantBaseline="central"
                    className="font-display"
                    fontWeight={700}
                    fontSize={on ? 46 : 34}
                    fill={on ? "#ffffff" : "#a9b8c0"}
                  >
                    {chapters[c].label}
                  </text>
                )}
              </g>
            );
          })}
        </svg>
      </div>
      {/* still parts: core, shockwave, needle and stylus */}
      <div className="pointer-events-none absolute left-0 top-1/2 aspect-square -translate-x-1/2 -translate-y-1/2" style={{ width: `calc(${size} * 2)` }}>
        <svg viewBox={`${-VB} ${-VB} ${2 * VB} ${2 * VB}`} className="block h-full w-full overflow-visible" aria-hidden>
          <circle r={R_IN - 16} fill="#05080b" stroke="currentColor" strokeOpacity="0.6" strokeWidth="3" />
          <circle key={`a${active}`} r={R_IN - 16} fill="none" stroke="currentColor" strokeWidth="4" className="np-shock" />
          <circle key={`b${active}`} r={R_IN - 16} fill="none" stroke="currentColor" strokeWidth="3" className="np-shock np-shock-late" />
          {/* only the right half of the core is on screen: keep the number inside it */}
          <text x={mini ? 34 : 22} y={mini ? 0 : 6} dominantBaseline={mini ? "central" : "auto"} className="font-display" fontWeight={800} fontSize={mini ? 170 : 84} fill="currentColor">
            {mini ? active + 1 : String(active + 1).padStart(2, "0")}
          </text>
          {!mini && (
            <text x="26" y="48" className="mono" fontSize="22" letterSpacing="3" fill="#6d808b">
              / {String(n).padStart(2, "0")}
            </text>
          )}
          <line x1={R_TRACE + AMP + 8} y1="0" x2={VB} y2="0" stroke="currentColor" strokeWidth="4" strokeLinecap="round" />
          <circle ref={stylus} cx={R_TRACE} cy="0" r="11" fill="currentColor" />
        </svg>
      </div>
    </div>
  );
}
