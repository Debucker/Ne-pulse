"use client";

/**
 * NE-PULSE LITE — a 100% offline-first local earthquake alarm. No network
 * calls, no backend dependency, no map, no region picker: this page reads
 * this device's own accelerometer directly and reacts entirely client-side,
 * on the theory that a phone on a nightstand needs to alarm the instant
 * *it* feels violent shaking, not wait on a network round-trip to confirm
 * what's already obvious. (The main /dashboard still owns the
 * network-aggregated, many-devices-agree detection pipeline — this page
 * used to mirror a stripped-down version of that, but it's been fully
 * replaced by this alarm instead, per this phase's brief.)
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { Moon, ShieldCheck, Siren, Sun, TriangleAlert } from "lucide-react";
import DashboardNav from "@/components/dashboard/DashboardNav";
import { useWakeLock } from "@/lib/useWakeLock";

// A real, sudden earthquake jolt reliably exceeds 1.2g of *raw*
// accelerometer magnitude (including gravity's own resting ~1g).
// Deliberately simple and orientation-independent, unlike a
// gravity-cancelled/EMA-baselined reading: this alarm's whole purpose is to
// keep working correctly on a phone sitting still on a nightstand, where
// never missing a real violent shake matters far more than a perfectly
// tuned false-positive rate.
const EARTHQUAKE_THRESHOLD_G = 1.2;
const METERS_PER_SECOND_SQUARED_PER_G = 9.80665;
const THRESHOLD_MS2 = EARTHQUAKE_THRESHOLD_G * METERS_PER_SECOND_SQUARED_PER_G;

const SIREN_LOW_HZ = 500;
const SIREN_HIGH_HZ = 1400;
const SIREN_SWEEP_MS = 600;

// ~2.9 full black/white cycles per second — fast enough to read as an
// urgent emergency strobe, but deliberately kept out of the ~15-25Hz band
// most strongly associated with photosensitive-seizure risk.
const STROBE_CYCLE_MS = 350;

const LITE_STYLES = `
@keyframes ne-pulse-strobe { 0%, 49% { background-color: #000000; } 50%, 100% { background-color: #ffffff; } }
.ne-pulse-strobe-bg { animation: ne-pulse-strobe ${STROBE_CYCLE_MS}ms steps(1, jump-none) infinite; }
`;

type Permission = "idle" | "granted" | "denied";

/**
 * Web Audio-synthesized emergency siren — a sweeping sawtooth oscillator,
 * no external audio file to load (or fail to load offline).
 */
function useSiren() {
  const ctxRef = useRef<AudioContext | null>(null);
  const oscRef = useRef<OscillatorNode | null>(null);
  const gainRef = useRef<GainNode | null>(null);
  const sweepTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Web Audio requires a real user gesture to produce sound the *first*
  // time — a devicemotion event doesn't count as one. Call this from
  // inside the "Arm the Alarm" button's click handler, well before any real
  // alert, so a later *programmatic* start() (triggered by a shake, not a
  // click) actually produces sound instead of silently doing nothing.
  const unlock = useCallback(() => {
    if (!ctxRef.current) {
      const Ctx =
        window.AudioContext ||
        (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext;
      ctxRef.current = new Ctx();
    }
    if (ctxRef.current.state === "suspended") {
      ctxRef.current.resume().catch(() => {});
    }
  }, []);

  const start = useCallback(() => {
    unlock();
    const ctx = ctxRef.current;
    if (!ctx || oscRef.current) return;

    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.type = "sawtooth";
    gain.gain.value = 0.25;
    osc.connect(gain);
    gain.connect(ctx.destination);
    osc.start();
    oscRef.current = osc;
    gainRef.current = gain;

    let rising = true;
    function sweep() {
      if (!oscRef.current) return;
      // ctx is guaranteed non-null by the guard above, but TS doesn't
      // preserve that narrowing across this closure's boundary.
      const now = ctx!.currentTime;
      osc.frequency.cancelScheduledValues(now);
      osc.frequency.setValueAtTime(rising ? SIREN_LOW_HZ : SIREN_HIGH_HZ, now);
      osc.frequency.linearRampToValueAtTime(rising ? SIREN_HIGH_HZ : SIREN_LOW_HZ, now + SIREN_SWEEP_MS / 1000);
      rising = !rising;
      sweepTimerRef.current = setTimeout(sweep, SIREN_SWEEP_MS);
    }
    sweep();
  }, [unlock]);

  const stop = useCallback(() => {
    if (sweepTimerRef.current) {
      clearTimeout(sweepTimerRef.current);
      sweepTimerRef.current = null;
    }
    if (oscRef.current) {
      oscRef.current.stop();
      oscRef.current.disconnect();
      oscRef.current = null;
    }
    if (gainRef.current) {
      gainRef.current.disconnect();
      gainRef.current = null;
    }
  }, []);

  useEffect(() => stop, [stop]);

  return { unlock, start, stop };
}

export default function LiteDashboardPage() {
  const [permission, setPermission] = useState<Permission>("idle");
  const [alertActive, setAlertActive] = useState(false);
  const [isTest, setIsTest] = useState(false);
  const [peakG, setPeakG] = useState(0);
  const [currentG, setCurrentG] = useState(0);
  const [wakeLockEnabled, setWakeLockEnabled] = useState(false);

  const wakeLock = useWakeLock();
  const siren = useSiren();
  const alertActiveRef = useRef(false);
  alertActiveRef.current = alertActive;

  const triggerAlert = useCallback(
    (test: boolean, magnitude: number) => {
      if (alertActiveRef.current) return; // already alerting — don't restart the siren/strobe mid-alert
      setIsTest(test);
      setPeakG(magnitude);
      setAlertActive(true);
      siren.start();
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [siren.start],
  );

  const dismissAlert = useCallback(() => {
    setAlertActive(false);
    setIsTest(false);
    siren.stop();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [siren.stop]);

  const handleMotion = useCallback(
    (event: DeviceMotionEvent) => {
      const acc = event.accelerationIncludingGravity;
      if (!acc) return;
      const x = acc.x ?? 0;
      const y = acc.y ?? 0;
      const z = acc.z ?? 0;
      const magnitudeMs2 = Math.sqrt(x * x + y * y + z * z);
      const g = magnitudeMs2 / METERS_PER_SECOND_SQUARED_PER_G;
      setCurrentG(g);
      if (magnitudeMs2 > THRESHOLD_MS2) {
        triggerAlert(false, g);
      }
    },
    [triggerAlert],
  );

  useEffect(() => {
    return () => window.removeEventListener("devicemotion", handleMotion);
  }, [handleMotion]);

  async function activate() {
    // A real user gesture, right here — unlocks Web Audio for the later
    // programmatic siren playback a devicemotion event triggers.
    siren.unlock();
    try {
      const DME = window.DeviceMotionEvent as unknown as {
        requestPermission?: () => Promise<"granted" | "denied">;
      };
      if (typeof DME?.requestPermission === "function") {
        const result = await DME.requestPermission();
        if (result !== "granted") {
          setPermission("denied");
          return;
        }
      }
      window.addEventListener("devicemotion", handleMotion);
      setPermission("granted");
    } catch {
      setPermission("denied");
    }
  }

  async function toggleWakeLock() {
    if (wakeLockEnabled) {
      wakeLock.disable();
      setWakeLockEnabled(false);
    } else {
      await wakeLock.enable();
      setWakeLockEnabled(true);
    }
  }

  return (
    <div className="flex h-full flex-col bg-[#020617] text-slate-100">
      <style>{LITE_STYLES}</style>

      <DashboardNav
        right={
          <button
            type="button"
            onClick={toggleWakeLock}
            disabled={!wakeLock.supported}
            title={wakeLock.supported ? "Keep the screen on while armed" : "Wake Lock isn't supported in this browser"}
            className={`flex items-center gap-2 rounded-md border px-2.5 py-1.5 text-xs font-medium transition sm:px-3 ${
              wakeLockEnabled
                ? "border-cyan-500/60 bg-cyan-500/10 text-cyan-300"
                : "border-slate-700 bg-slate-900 text-slate-300 hover:border-cyan-500/60 hover:text-cyan-400"
            } ${!wakeLock.supported ? "cursor-not-allowed opacity-40" : ""}`}
          >
            {wakeLockEnabled ? <Sun size={14} /> : <Moon size={14} />}
            <span className="hidden sm:inline">Screen Lock {wakeLockEnabled ? "On" : "Off"}</span>
          </button>
        }
      />

      <div className="flex-1 overflow-y-auto">
        {permission !== "granted" ? (
          <SetupScreen permission={permission} onActivate={activate} />
        ) : (
          <ArmedScreen currentG={currentG} onTest={() => triggerAlert(true, currentG)} />
        )}
      </div>

      {alertActive && <AlertOverlay isTest={isTest} peakG={peakG} onDismiss={dismissAlert} />}
    </div>
  );
}

function SetupScreen({ permission, onActivate }: { permission: Permission; onActivate: () => void }) {
  return (
    <main className="mx-auto flex h-full max-w-md flex-col items-center justify-center gap-6 p-6 text-center">
      <ShieldCheck size={64} className="text-cyan-400" />
      <div>
        <h1 className="text-2xl font-bold text-slate-100">Local Earthquake Alarm</h1>
        <p className="mt-2 text-sm text-slate-400">
          Runs entirely on this device — no network, no server, no account. Once armed, leave this tab
          open (installed as an app works best) and this phone becomes a nightstand seismic alarm: a
          strong shake triggers a siren, a strobe, and on-screen survival instructions immediately, with
          zero dependency on connectivity.
        </p>
      </div>
      <button
        type="button"
        onClick={onActivate}
        className="w-full rounded-lg border border-cyan-500/50 bg-cyan-500/10 px-6 py-3 text-sm font-semibold text-cyan-300 transition hover:bg-cyan-500/20"
      >
        Arm the Alarm
      </button>
      {permission === "denied" && (
        <p className="text-xs text-red-400">
          Motion sensor permission was denied. Enable it in your browser/device settings and try again.
        </p>
      )}
    </main>
  );
}

function ArmedScreen({ currentG, onTest }: { currentG: number; onTest: () => void }) {
  return (
    <main className="mx-auto flex h-full max-w-md flex-col items-center justify-center gap-6 p-6 text-center">
      <div className="flex items-center gap-2 rounded-full border border-emerald-500/40 bg-emerald-500/10 px-4 py-1.5 text-xs font-semibold uppercase tracking-wide text-emerald-400">
        <span className="h-2 w-2 animate-pulse rounded-full bg-emerald-400" />
        Armed — Monitoring
      </div>

      <div>
        <div className="text-5xl font-bold tabular-nums text-slate-100">{currentG.toFixed(2)}g</div>
        <div className="mt-1 text-xs uppercase tracking-wide text-slate-500">
          Live acceleration · alarm fires above {EARTHQUAKE_THRESHOLD_G}g
        </div>
      </div>

      <button
        type="button"
        onClick={onTest}
        className="flex w-full items-center justify-center gap-2 rounded-lg border border-amber-500/50 bg-amber-500/10 px-6 py-3 text-sm font-semibold text-amber-300 transition hover:bg-amber-500/20"
      >
        <Siren size={16} /> Test Alarm (Siren + Strobe)
      </button>

      <p className="text-xs text-slate-500">
        Keep this device plugged in and the screen-lock toggle on (top right) so the OS never dims or
        suspends this tab overnight.
      </p>
    </main>
  );
}

function AlertOverlay({ isTest, peakG, onDismiss }: { isTest: boolean; peakG: number; onDismiss: () => void }) {
  return (
    <div className="ne-pulse-strobe-bg fixed inset-0 z-[9999] flex items-center justify-center p-6">
      <div className="w-full max-w-md rounded-2xl border-4 border-red-600 bg-black/90 p-6 text-center shadow-2xl">
        {isTest && (
          <div className="mb-3 inline-block rounded-full bg-amber-500/20 px-3 py-1 text-xs font-bold uppercase tracking-wide text-amber-400">
            Test Mode — Not A Real Alert
          </div>
        )}
        <TriangleAlert size={56} className="mx-auto mb-4 text-red-500" />
        <h1 className="text-3xl font-black uppercase leading-tight tracking-tight text-red-500 sm:text-4xl">
          Earthquake Detected
        </h1>
        <p className="mt-3 text-xl font-bold uppercase tracking-wide text-white sm:text-2xl">
          Drop, Cover, and Hold On
        </p>
        <p className="mt-1 text-xs uppercase tracking-wide text-slate-400">Peak reading: {peakG.toFixed(2)}g</p>

        <ul className="mt-5 space-y-2 text-left text-sm text-slate-200">
          <li>• Get under a sturdy desk or table.</li>
          <li>• Protect your head and neck.</li>
          <li>• Stay away from windows and heavy furniture.</li>
          <li>• Do not run outside during shaking.</li>
        </ul>

        <button
          type="button"
          onClick={onDismiss}
          className="mt-6 w-full rounded-lg bg-white px-4 py-3 text-sm font-bold uppercase tracking-wide text-black transition hover:bg-slate-200"
        >
          {isTest ? "End Test" : "I'm Safe — Dismiss"}
        </button>
      </div>
    </div>
  );
}
