"use client";

import { useEffect, useState } from "react";
import { motion } from "framer-motion";
import { Smartphone, TriangleAlert } from "lucide-react";

// A single persistent scene, not five swapped-in icons — every element
// (grid, hex cells, phone, rings, monitor) stays mounted the whole time and
// just animates its own opacity/scale/position as `active` changes, so
// nothing ever hard-cuts or remounts between steps.
const SPRING = { type: "spring" as const, stiffness: 80, damping: 15 };

const VB = 300;
const HEX_R = 34;
const CENTER = { x: 150, y: 150 };
const NEIGHBOR_DIST = HEX_R * Math.sqrt(3);

function hexPoints(cx: number, cy: number, r: number) {
  return Array.from({ length: 6 }, (_, i) => {
    const angle = (Math.PI / 180) * (60 * i);
    return `${(cx + r * Math.cos(angle)).toFixed(1)},${(cy + r * Math.sin(angle)).toFixed(1)}`;
  }).join(" ");
}

const NEIGHBORS = Array.from({ length: 6 }, (_, i) => {
  const angle = (Math.PI / 180) * (30 + 60 * i);
  return {
    x: CENTER.x + NEIGHBOR_DIST * Math.cos(angle),
    y: CENTER.y + NEIGHBOR_DIST * Math.sin(angle),
  };
});
const VERIFIED_NEIGHBOR_INDICES = [0, 2, 4];

const COUNTDOWN_START_CENTIS = 1000; // 10.00s
const ALERT_DURATION_MS = 2500;

// Ticks a real 10-second countdown down to zero, then holds an "earthquake
// live" alert state for a couple seconds before resetting and looping —
// the impact isn't just a number hitting zero, it's a visible event.
function useCountdown(running: boolean) {
  const [centis, setCentis] = useState(COUNTDOWN_START_CENTIS);
  const [alert, setAlert] = useState(false);

  useEffect(() => {
    if (!running) {
      setCentis(COUNTDOWN_START_CENTIS);
      setAlert(false);
      return;
    }
    const interval = setInterval(() => {
      setCentis((c) => {
        if (c <= 0) {
          setAlert(true);
          return 0;
        }
        return c - 5;
      });
    }, 50);
    return () => clearInterval(interval);
  }, [running]);

  useEffect(() => {
    if (!alert) return;
    const timeout = setTimeout(() => {
      setAlert(false);
      setCentis(COUNTDOWN_START_CENTIS);
    }, ALERT_DURATION_MS);
    return () => clearTimeout(timeout);
  }, [alert]);

  const seconds = Math.floor(centis / 100);
  const hundredths = centis % 100;
  const display = `00:${String(seconds).padStart(2, "0")}.${String(hundredths).padStart(2, "0")}`;
  return { display, alert };
}

export default function JourneyPanel({ active }: { active: number }) {
  const { display: countdown, alert } = useCountdown(active === 4);

  const phoneVisible = active <= 2;
  const phoneScale = active === 0 ? 1.6 : 1;
  const hexVisible = active === 1 || active === 2;
  const shieldVisible = active === 2;
  const broadcastVisible = active === 3;
  const monitorVisible = active === 4;
  const tilt = active === 3 ? 8 : active === 4 ? 4 : 0;

  return (
    <div
      className="relative hidden aspect-square items-center justify-center overflow-hidden rounded-xl border border-white/10 bg-slate-950 lg:flex"
      style={{ perspective: 800 }}
    >
      {/* persistent moving grid floor */}
      <motion.div
        className="pointer-events-none absolute inset-[-20%]"
        style={{
          backgroundImage:
            "linear-gradient(rgba(59,130,246,0.18) 1px, transparent 1px), linear-gradient(90deg, rgba(59,130,246,0.18) 1px, transparent 1px)",
          backgroundSize: "28px 28px",
        }}
        animate={{ backgroundPositionX: active * 14, backgroundPositionY: active * 10, rotateX: tilt }}
        transition={SPRING}
      />
      <div
        className="pointer-events-none absolute inset-0"
        style={{ background: "radial-gradient(circle, transparent 40%, #020617 92%)" }}
      />

      {/* step 1: faint red seismic ripples along the grid */}
      {[0, 1, 2].map((i) => (
        <motion.span
          key={`ripple-${i}`}
          className="pointer-events-none absolute rounded-full border border-surface-danger/60"
          style={{ left: "50%", top: "50%", x: "-50%", y: "-50%" }}
          animate={
            active === 0
              ? { width: [0, 220], height: [0, 220], opacity: [0.6, 0] }
              : { width: 0, height: 0, opacity: 0 }
          }
          transition={{
            duration: 2,
            repeat: active === 0 ? Infinity : 0,
            delay: i * 0.6,
            ease: "easeOut",
          }}
        />
      ))}

      {/* hex grid: fades in for steps 2-3 */}
      <motion.svg
        viewBox={`0 0 ${VB} ${VB}`}
        className="absolute inset-0 h-full w-full"
        animate={{ opacity: hexVisible ? 1 : 0 }}
        transition={SPRING}
      >
        {NEIGHBORS.map((n, i) => {
          const verified = shieldVisible && VERIFIED_NEIGHBOR_INDICES.includes(i);
          return (
            <motion.polygon
              key={i}
              points={hexPoints(n.x, n.y, HEX_R)}
              strokeWidth={1.5}
              animate={{
                fill: verified ? "rgba(34,211,238,0.1)" : "rgba(0,0,0,0)",
                stroke: verified ? "rgba(34,211,238,0.9)" : "rgba(51,65,85,1)",
              }}
              transition={SPRING}
            />
          );
        })}

        <motion.polygon
          points={hexPoints(CENTER.x, CENTER.y, HEX_R)}
          animate={{
            fill: "rgba(59,130,246,0.08)",
            stroke: active >= 1 ? "rgba(34,211,238,0.95)" : "rgba(59,130,246,0.4)",
            strokeWidth: active >= 1 ? 2 : 1.25,
          }}
          transition={SPRING}
        />

        {shieldVisible &&
          VERIFIED_NEIGHBOR_INDICES.map((idx) => {
            const n = NEIGHBORS[idx];
            return (
              <motion.circle
                key={idx}
                r={3}
                fill="rgb(103 232 249)"
                animate={{ cx: [n.x, CENTER.x], cy: [n.y, CENTER.y], opacity: [1, 0] }}
                transition={{ duration: 0.9, repeat: Infinity, repeatDelay: 0.6, ease: "easeIn" }}
              />
            );
          })}
      </motion.svg>

      {/* verification shield ring around the device */}
      <motion.div
        className="pointer-events-none absolute rounded-full border-2 border-cyan-400/70"
        style={{
          left: "50%",
          top: "50%",
          x: "-50%",
          y: "-50%",
          boxShadow: "0 0 24px 4px rgba(34,211,238,0.35)",
        }}
        animate={
          shieldVisible ? { width: 108, height: 108, opacity: 1 } : { width: 0, height: 0, opacity: 0 }
        }
        transition={SPRING}
      />

      {/* the device itself: micro-vibrates continuously, scales down as it snaps in, erased for the broadcast/countdown steps */}
      <motion.div
        className="absolute flex items-center justify-center text-surface-accent"
        animate={{ scale: phoneVisible ? phoneScale : 0, opacity: phoneVisible ? 1 : 0 }}
        transition={SPRING}
      >
        <motion.div
          animate={{ rotate: [0, -3, 3, -2, 0] }}
          transition={{ duration: 1.4, repeat: Infinity, ease: "easeInOut" }}
          style={{ filter: "drop-shadow(0 0 10px rgba(59,130,246,0.6))" }}
        >
          <Smartphone size={56} strokeWidth={1.5} />
        </motion.div>
      </motion.div>

      {/* network broadcast: concentric cyan rings expanding across the grid.
          A native CSS animation (see .animate-broadcast-ring), not a
          JS-driven repeat — the rings are only mounted while broadcasting,
          so there's no state to reset and no flash at the loop seam. */}
      {broadcastVisible &&
        [0, 1, 2, 3].map((i) => (
          <span
            key={`wave-${i}`}
            className="pointer-events-none absolute left-1/2 top-1/2 h-[520px] w-[520px] rounded-full border-2 border-cyan-400/50 animate-broadcast-ring"
            style={{ animationDelay: `${i * 0.45}s` }}
          />
        ))}

      {/* command monitor: the broadcast resolves into a live countdown HUD,
          which itself resolves into a red "live earthquake" alert the
          instant it hits zero, before resetting and looping. */}
      <motion.div
        className="absolute flex flex-col items-center gap-3"
        animate={{ opacity: monitorVisible ? 1 : 0, scale: monitorVisible ? 1 : 0.7 }}
        transition={SPRING}
      >
        <motion.div
          className="relative flex h-32 w-44 flex-col items-center justify-center overflow-hidden rounded-lg border-2 bg-slate-900/90"
          animate={{
            borderColor: alert ? "rgba(239,68,68,1)" : "rgba(34,211,238,1)",
            boxShadow: alert
              ? "0 0 30px rgba(239,68,68,0.45)"
              : "0 0 30px rgba(34,211,238,0.35)",
          }}
          transition={SPRING}
        >
          {monitorVisible && !alert && (
            <motion.div
              className="pointer-events-none absolute inset-x-0 h-6 bg-gradient-to-t from-green-400/40 to-transparent"
              animate={{ top: ["120%", "-20%"] }}
              transition={{ duration: 1.6, repeat: Infinity, ease: "linear" }}
            />
          )}

          {alert ? (
            <motion.div
              className="flex flex-col items-center gap-1.5"
              animate={{ opacity: [1, 0.35, 1] }}
              transition={{ duration: 0.5, repeat: Infinity }}
            >
              <TriangleAlert size={30} className="text-surface-danger" />
              <span className="font-mono text-sm font-bold tracking-wide text-surface-danger">
                EARTHQUAKE
              </span>
            </motion.div>
          ) : (
            <>
              <span className="font-mono text-2xl font-bold text-green-400">{countdown}</span>
              <span className="mt-1 text-[10px] uppercase tracking-wide text-surface-muted">
                Time to impact
              </span>
            </>
          )}
        </motion.div>
        <div className="h-1 w-20 rounded-full bg-slate-800" />
      </motion.div>
    </div>
  );
}
