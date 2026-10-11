"use client";

import { useEffect, useRef } from "react";
import type { PublicCell, PublicEvent } from "@/lib/types";
import { C } from "@/lib/colors";

/**
 * Live network view. Real data: node positions (public 10 km areas), the
 * number of particles per node (connected phones there), packet rate
 * (listening phones), red pulses (phones triggered right now) and event
 * rings (active events). Decorative: where a particle sits inside its node.
 */
export default function NetworkGraph({
  cells,
  events = [],
  height = 520,
  label,
  emptyText,
}: {
  cells: PublicCell[];
  events?: PublicEvent[];
  height?: number | string;
  label: string;
  emptyText: string;
}) {
  const ref = useRef<HTMLCanvasElement | null>(null);
  const data = useRef({ cells, events });
  data.current = { cells, events };

  useEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d")!;
    const reduce = matchMedia("(prefers-reduced-motion: reduce)").matches;
    let raf = 0;
    let visible = true;
    const io = new IntersectionObserver(([e]) => {
      visible = e.isIntersecting;
      if (visible && !raf) raf = requestAnimationFrame(frame);
    });
    io.observe(canvas);

    type Packet = { node: number; p: number; speed: number };
    let packets: Packet[] = [];
    const spawnAcc: number[] = [];
    let last = performance.now();
    const t0 = last;
    const font = getComputedStyle(document.body).getPropertyValue("--font-mono") + ", monospace";

    function frame(now: number) {
      raf = 0;
      const dt = Math.min(0.05, (now - last) / 1000);
      last = now;
      const t = (now - t0) / 1000;
      const dpr = window.devicePixelRatio || 1;
      const w = canvas!.clientWidth;
      const h = canvas!.clientHeight;
      if (canvas!.width !== Math.round(w * dpr) || canvas!.height !== Math.round(h * dpr)) {
        canvas!.width = Math.round(w * dpr);
        canvas!.height = Math.round(h * dpr);
      }
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, w, h);

      const { cells, events } = data.current;
      // background grid
      ctx.strokeStyle = "rgba(47,63,74,0.35)";
      ctx.lineWidth = 1;
      const g = 48;
      for (let x = (w / 2) % g; x < w; x += g) {
        ctx.beginPath();
        ctx.moveTo(x, 0);
        ctx.lineTo(x, h);
        ctx.stroke();
      }
      for (let y = (h / 2) % g; y < h; y += g) {
        ctx.beginPath();
        ctx.moveTo(0, y);
        ctx.lineTo(w, y);
        ctx.stroke();
      }

      // projection of node positions into the canvas (keeping aspect)
      const lat0 = cells.length ? cells.reduce((a, c) => a + c.lat, 0) / cells.length : 41.3;
      const k = Math.cos((lat0 * Math.PI) / 180);
      const xs = cells.map((c) => c.lng * k);
      const ys = cells.map((c) => -c.lat);
      const minX = Math.min(...xs, Infinity), maxX = Math.max(...xs, -Infinity);
      const minY = Math.min(...ys, Infinity), maxY = Math.max(...ys, -Infinity);
      const spanX = Math.max(0.15, maxX - minX), spanY = Math.max(0.15, maxY - minY);
      const pad = Math.min(w, h) * 0.2;
      const scale = Math.min((w - 2 * pad) / spanX, (h - 2 * pad) / spanY);
      const cx = (minX + maxX) / 2, cy = (minY + maxY) / 2;
      const P = (lat: number, lng: number) => [w / 2 + (lng * k - cx) * scale, h / 2 + (-lat - cy) * scale] as const;
      const nodes = cells.map((c) => {
        const [x, y] = P(c.lat, c.lng);
        return { c, x, y, r: 6 + Math.sqrt(c.sensors) * 2.2 };
      });
      // network core: centroid of the nodes, lifted towards the top
      const hubX = w / 2;
      const hubY = nodes.length ? Math.max(40, Math.min(...nodes.map((n) => n.y)) - Math.min(90, h * 0.16)) : h / 2;

      // mesh: each node to its two nearest neighbours
      ctx.strokeStyle = "rgba(242,167,59,0.12)";
      ctx.lineWidth = 1;
      nodes.forEach((a, i) => {
        nodes
          .map((b, j) => ({ j, d: Math.hypot(a.x - b.x, a.y - b.y) }))
          .filter((o) => o.j !== i)
          .sort((p, q) => p.d - q.d)
          .slice(0, 2)
          .forEach(({ j }) => {
            ctx.beginPath();
            ctx.moveTo(a.x, a.y);
            ctx.lineTo(nodes[j].x, nodes[j].y);
            ctx.stroke();
          });
      });

      // links to the core (quadratic curves)
      const ctrl = (n: { x: number; y: number }) => [(n.x + hubX) / 2 + (n.y - hubY) * 0.15, (n.y + hubY) / 2 - 30] as const;
      nodes.forEach((n) => {
        const [qx, qy] = ctrl(n);
        ctx.strokeStyle = n.c.triggered ? "rgba(255,93,69,0.45)" : `rgba(242,167,59,${0.12 + 0.3 * Math.min(1, n.c.eligible / 20)})`;
        ctx.lineWidth = n.c.triggered ? 1.6 : 1;
        ctx.beginPath();
        ctx.moveTo(n.x, n.y);
        ctx.quadraticCurveTo(qx, qy, hubX, hubY);
        ctx.stroke();
      });

      // packets: listening phones stream once per second; draw a sampled flow
      if (!reduce) {
        nodes.forEach((n, i) => {
          spawnAcc[i] = (spawnAcc[i] ?? 0) + dt * Math.min(12, 0.4 + n.c.eligible * 0.35);
          while (spawnAcc[i] >= 1) {
            spawnAcc[i] -= 1;
            packets.push({ node: i, p: 0, speed: 0.35 + Math.random() * 0.25 });
          }
        });
        packets = packets.filter((pk) => (pk.p += dt * pk.speed) < 1 && pk.node < nodes.length);
        for (const pk of packets) {
          const n = nodes[pk.node];
          const [qx, qy] = ctrl(n);
          const u = pk.p;
          const x = (1 - u) * (1 - u) * n.x + 2 * (1 - u) * u * qx + u * u * hubX;
          const y = (1 - u) * (1 - u) * n.y + 2 * (1 - u) * u * qy + u * u * hubY;
          ctx.fillStyle = n.c.triggered ? C.confirmed : C.trace;
          ctx.globalAlpha = 0.9 * Math.sin(Math.PI * u);
          ctx.beginPath();
          ctx.arc(x, y, 1.8, 0, Math.PI * 2);
          ctx.fill();
        }
        ctx.globalAlpha = 1;
      }

      // event rings
      for (const e of events) {
        if (!e.active) continue;
        const [ex, ey] = P(e.lat, e.lng);
        for (let r = 0; r < 3; r++) {
          const ph = ((t * 0.6 + r / 3) % 1);
          ctx.strokeStyle = e.level === "confirmed" ? C.confirmed : e.level === "corroborated" ? C.corroborated : C.candidate;
          ctx.globalAlpha = 1 - ph;
          ctx.lineWidth = 1.5;
          ctx.beginPath();
          ctx.arc(ex, ey, 8 + ph * Math.min(w, h) * 0.35, 0, Math.PI * 2);
          ctx.stroke();
        }
        ctx.globalAlpha = 1;
      }

      // nodes with orbiting phone particles
      nodes.forEach((n, i) => {
        const hot = n.c.triggered > 0;
        const pulse = hot ? 0.5 + 0.5 * Math.sin(t * 6) : 0;
        const grd = ctx.createRadialGradient(n.x, n.y, 0, n.x, n.y, n.r * 3);
        grd.addColorStop(0, hot ? `rgba(255,93,69,${0.35 + 0.3 * pulse})` : "rgba(242,167,59,0.22)");
        grd.addColorStop(1, "rgba(0,0,0,0)");
        ctx.fillStyle = grd;
        ctx.beginPath();
        ctx.arc(n.x, n.y, n.r * 3, 0, Math.PI * 2);
        ctx.fill();
        ctx.fillStyle = hot ? C.confirmed : C.trace;
        ctx.beginPath();
        ctx.arc(n.x, n.y, 3.5, 0, Math.PI * 2);
        ctx.fill();
        const count = Math.min(80, n.c.sensors);
        for (let j = 0; j < count; j++) {
          const a = (j / count) * Math.PI * 2 + t * (0.25 + (j % 3) * 0.08) * (reduce ? 0 : 1) + i;
          const rr = n.r * (0.7 + ((j * 37) % 10) / 22);
          const listening = j < n.c.eligible;
          ctx.fillStyle = hot && j < n.c.triggered ? C.confirmed : listening ? "rgba(233,238,240,0.85)" : "rgba(114,131,141,0.6)";
          ctx.beginPath();
          ctx.arc(n.x + Math.cos(a) * rr, n.y + Math.sin(a) * rr * 0.8, 1.3, 0, Math.PI * 2);
          ctx.fill();
        }
        ctx.fillStyle = "rgba(170,183,190,0.9)";
        ctx.font = `10px ${font}`;
        ctx.fillText(String(n.c.sensors), n.x + n.r + 4, n.y + 3);
      });

      // core
      const hp = 0.5 + 0.5 * Math.sin(t * 2);
      ctx.strokeStyle = C.trace;
      ctx.lineWidth = 1.5;
      ctx.beginPath();
      ctx.arc(hubX, hubY, 13 + hp * 2, 0, Math.PI * 2);
      ctx.stroke();
      ctx.fillStyle = C.ground;
      ctx.beginPath();
      ctx.arc(hubX, hubY, 10, 0, Math.PI * 2);
      ctx.fill();
      ctx.strokeStyle = C.trace;
      ctx.beginPath();
      ctx.moveTo(hubX - 7, hubY + 1);
      ctx.lineTo(hubX - 3, hubY + 1);
      ctx.lineTo(hubX - 1, hubY - 5);
      ctx.lineTo(hubX + 2, hubY + 5);
      ctx.lineTo(hubX + 4, hubY);
      ctx.lineTo(hubX + 7, hubY);
      ctx.stroke();
      ctx.fillStyle = "rgba(170,183,190,0.9)";
      ctx.font = `10px ${font}`;
      ctx.textAlign = "center";
      ctx.fillText(label.toUpperCase(), hubX, hubY - 22);
      ctx.textAlign = "left";

      if (!nodes.length) {
        ctx.fillStyle = C.ink3;
        ctx.textAlign = "center";
        ctx.fillText(emptyText, w / 2, h / 2 + 40);
        ctx.textAlign = "left";
      }
      if (visible && !reduce) raf = requestAnimationFrame(frame);
    }
    raf = requestAnimationFrame(frame);
    return () => {
      io.disconnect();
      cancelAnimationFrame(raf);
    };
  }, [label, emptyText]);

  return <canvas ref={ref} style={{ width: "100%", height, display: "block" }} role="img" aria-label={label} />;
}
