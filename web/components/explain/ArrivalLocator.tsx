"use client";

// Finding the source from arrival times, in 3D. A grid of points is the
// ground; a P wave (cyan) and an S wave (orange) spread from the epicentre at
// 5.8 and 3.36 km/s with the hypocentre 8 km deep (the locator's model).
// Three areas record when the P wave reaches them. Then each area's distance
// is traced back as a circle (radius = Vp x travel time); the circles meet at
// the source. The network does the same with many areas and an unknown
// origin time (internal/network/locate.go). Click the ground to move the
// earthquake; drag the timeline to scrub through it.

import { useEffect, useRef, useState } from "react";
import * as THREE from "three";
import { DEPTH_KM, travel, VP_KMS, VS_KMS } from "@/lib/dsp";
import { useT } from "@/lib/i18n";

const SPAN = 64; // km
const PINS: [number, number][] = [
  [-13, 9],
  [15, 11],
  [5, -16],
];
const NAMES = ["A", "B", "C"];
const TRACE = 3.2; // seconds of the trace-back phase
const HOLD = 1.6;

const vert = /* glsl */ `
uniform vec2 uEpi;
uniform float uT;
uniform float uFade;
varying float vGlow;
varying float vHot;
varying float vEdge;
const float VP = ${VP_KMS.toFixed(2)};
const float VS = ${VS_KMS.toFixed(2)};
const float H = ${DEPTH_KM.toFixed(1)};
void main() {
  vec3 p = position;
  float d = length(p.xz - uEpi);
  float rp = sqrt(max(0.0, VP*VP*uT*uT - H*H));
  float rs = sqrt(max(0.0, VS*VS*uT*uT - H*H));
  float gp = rp > 0.0 ? exp(-pow((d - rp) / 1.6, 2.0)) : 0.0;
  float gs = rs > 0.0 ? exp(-pow((d - rs) / 2.4, 2.0)) : 0.0;
  float behind = rs - d;
  float coda = behind > 0.0 && rs > 0.0 ? exp(-behind / 6.0) * sin(behind * 1.4) * 0.5 : 0.0;
  float att = 1.0 / (1.0 + d / 30.0);
  p.y += uFade * att * (gp * 0.9 * sin((d - rp) * 2.2) + gs * 2.6 * sin((d - rs) * 1.2) + coda);
  vGlow = uFade * att * gp;
  vHot = uFade * att * (gs + max(coda, 0.0) * 0.5);
  vEdge = smoothstep(${(SPAN / 2).toFixed(1)}, ${(SPAN / 2 - 10).toFixed(1)}, length(p.xz));
  vec4 mv = modelViewMatrix * vec4(p, 1.0);
  gl_PointSize = (1.6 + vGlow * 2.5 + vHot * 3.5) * (110.0 / -mv.z);
  gl_Position = projectionMatrix * mv;
}
`;
const frag = /* glsl */ `
uniform vec3 uCyan;
uniform vec3 uHot;
uniform vec3 uBase;
varying float vGlow;
varying float vHot;
varying float vEdge;
void main() {
  vec2 c = gl_PointCoord - 0.5;
  float r = length(c);
  if (r > 0.5) discard;
  float core = smoothstep(0.5, 0.0, r);
  vec3 col = uBase + uCyan * vGlow * 1.4 + uHot * vHot * 1.6;
  gl_FragColor = vec4(col, core * vEdge * (0.55 + vGlow + vHot));
  #include <colorspace_fragment>
}
`;

const ease = (u: number) => {
  const x = Math.min(1, Math.max(0, u));
  return x < 0.5 ? 4 * x * x * x : 1 - (-2 * x + 2) ** 3 / 2;
};

export default function ArrivalLocator() {
  const { t } = useT();
  const host = useRef<HTMLDivElement>(null);
  const labels = useRef<(HTMLDivElement | null)[]>([]);
  const epiLabel = useRef<HTMLDivElement>(null);
  const track = useRef<HTMLDivElement>(null);
  const fill = useRef<HTMLDivElement>(null);
  const handle = useRef<HTMLDivElement>(null);
  const bubble = useRef<HTMLSpanElement>(null);
  const marks = useRef<(HTMLDivElement | null)[]>([]);
  const [epi, setEpi] = useState<[number, number]>([-12, -4]);
  const epiRef = useRef(epi);
  epiRef.current = epi;
  // Scrubbing: while a target is set the scene glides to it; dragging keeps it set.
  const scrub = useRef<{ active: boolean; target: number | null }>({ active: false, target: null });
  const clock = useRef({ time: 0, len: 1 });
  const unit = useRef("s");
  unit.current = t("common.sec");
  const [phase, setPhase] = useState(0);

  const arr = PINS.map(([x, z]) => travel(Math.hypot(x - epi[0], z - epi[1]), VP_KMS));
  const order = arr.map((a, i) => [a, i] as const).sort((a, b) => a[0] - b[0]);
  const loop = Math.max(...arr) + 1.2 + TRACE + HOLD;

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    let renderer: THREE.WebGLRenderer;
    try {
      renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true });
    } catch {
      return;
    }
    renderer.setPixelRatio(Math.min(devicePixelRatio || 1, 2));
    el.appendChild(renderer.domElement);
    Object.assign(renderer.domElement.style, { display: "block", width: "100%", height: "100%", cursor: "crosshair" });
    const css = (n: string, f: string) => new THREE.Color(getComputedStyle(document.documentElement).getPropertyValue(n).trim() || f);
    const cyan = css("--cyan", "#38e1ff");
    const hot = css("--hot", "#ff7a2f");
    const ink = css("--ink", "#eaf1f4");

    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(36, 1, 0.5, 400);

    // Ground points.
    const N = 90;
    const pos = new Float32Array(N * N * 3);
    for (let i = 0; i < N; i++)
      for (let j = 0; j < N; j++) {
        const k = (i * N + j) * 3;
        pos[k] = -SPAN / 2 + (i / (N - 1)) * SPAN;
        pos[k + 2] = -SPAN / 2 + (j / (N - 1)) * SPAN;
      }
    const g = new THREE.BufferGeometry();
    g.setAttribute("position", new THREE.BufferAttribute(pos, 3));
    const uni = {
      uEpi: { value: new THREE.Vector2(epiRef.current[0], epiRef.current[1]) },
      uT: { value: 0 },
      uFade: { value: 1 },
      uCyan: { value: cyan },
      uHot: { value: hot },
      uBase: { value: cyan.clone().multiplyScalar(0.28) },
    };
    const pts = new THREE.Points(g, new THREE.ShaderMaterial({ uniforms: uni, vertexShader: vert, fragmentShader: frag, transparent: true, depthWrite: false, blending: THREE.AdditiveBlending }));
    scene.add(pts);

    // Pins.
    const pins = PINS.map(([x, z]) => {
      const grp = new THREE.Group();
      const beamMat = new THREE.MeshBasicMaterial({ color: ink, transparent: true, opacity: 0.6 });
      const beam = new THREE.Mesh(new THREE.CylinderGeometry(0.12, 0.12, 5, 8), beamMat);
      beam.position.y = 2.5;
      const headMat = new THREE.MeshBasicMaterial({ color: ink });
      const head = new THREE.Mesh(new THREE.SphereGeometry(0.55, 16, 12), headMat);
      head.position.y = 5.2;
      const pulseMat = new THREE.MeshBasicMaterial({ color: cyan, transparent: true, opacity: 0, side: THREE.DoubleSide });
      const pulse = new THREE.Mesh(new THREE.RingGeometry(0.8, 1.1, 48), pulseMat);
      pulse.rotation.x = -Math.PI / 2;
      pulse.position.y = 0.05;
      grp.add(beam, head, pulse);
      grp.position.set(x, 0, z);
      scene.add(grp);
      return { grp, headMat, beamMat, pulse, pulseMat };
    });

    // Trace-back circles: one plane, three glowing bands drawn analytically.
    const circUni = {
      uPins: { value: PINS.map(([x, z]) => new THREE.Vector2(x, z)) },
      uR: { value: [0, 0, 0] },
      uA: { value: 0 },
      uCol: { value: cyan },
    };
    const circPlane = new THREE.Mesh(
      new THREE.PlaneGeometry(SPAN * 1.6, SPAN * 1.6).rotateX(-Math.PI / 2),
      new THREE.ShaderMaterial({
        uniforms: circUni,
        vertexShader: `varying vec2 vP; void main(){ vP = position.xz; gl_Position = projectionMatrix*modelViewMatrix*vec4(position,1.); }`,
        fragmentShader: `uniform vec2 uPins[3]; uniform float uR[3]; uniform float uA; uniform vec3 uCol; varying vec2 vP;
          void main(){
            float a = 0.;
            for (int i = 0; i < 3; i++) {
              float d = abs(length(vP - uPins[i]) - uR[i]);
              a += uR[i] > 0.01 ? exp(-d*d/0.08) + 0.35*exp(-d/1.6) : 0.;
            }
            float edge = smoothstep(${(SPAN * 0.78).toFixed(1)}, ${(SPAN * 0.5).toFixed(1)}, length(vP));
            gl_FragColor = vec4(uCol * a, a * uA * edge);
            #include <colorspace_fragment>
          }`,
        transparent: true,
        depthWrite: false,
        blending: THREE.AdditiveBlending,
      }),
    );
    circPlane.position.y = 0.3;
    scene.add(circPlane);

    // Epicentre marker.
    const epiGrp = new THREE.Group();
    const epiRingMat = new THREE.MeshBasicMaterial({ color: hot, transparent: true, opacity: 0, side: THREE.DoubleSide });
    const epiRing = new THREE.Mesh(new THREE.RingGeometry(0.9, 1.3, 48), epiRingMat);
    epiRing.rotation.x = -Math.PI / 2;
    const epiDotMat = new THREE.MeshBasicMaterial({ color: hot, transparent: true, opacity: 1 });
    const epiDot = new THREE.Mesh(new THREE.SphereGeometry(0.45, 16, 12), epiDotMat);
    epiDot.position.y = 0.4;
    epiGrp.add(epiRing, epiDot);
    scene.add(epiGrp);
    const epiNow = new THREE.Vector2(epiRef.current[0], epiRef.current[1]);
    let epiFrom = epiNow.clone();
    const epiTo = epiNow.clone();
    let moveT = 1;

    const resize = () => {
      const w = el.clientWidth;
      const h = el.clientHeight;
      renderer.setSize(w, h, false);
      camera.aspect = w / h;
      camera.updateProjectionMatrix();
    };
    const ro = new ResizeObserver(resize);
    ro.observe(el);
    resize();
    let visible = true;
    const io = new IntersectionObserver(([e]) => (visible = e.isIntersecting));
    io.observe(el);

    // Click the ground to move the earthquake.
    const ray = new THREE.Raycaster();
    const plane = new THREE.Plane(new THREE.Vector3(0, 1, 0), 0);
    const hit = new THREE.Vector3();
    const onDown = (e: PointerEvent) => {
      const r = renderer.domElement.getBoundingClientRect();
      const ndc = new THREE.Vector2(((e.clientX - r.left) / r.width) * 2 - 1, -((e.clientY - r.top) / r.height) * 2 + 1);
      ray.setFromCamera(ndc, camera);
      if (!ray.ray.intersectPlane(plane, hit)) return;
      const x = Math.max(-26, Math.min(26, hit.x));
      const z = Math.max(-26, Math.min(26, hit.z));
      epiFrom = epiNow.clone();
      epiTo.set(x, z);
      moveT = 0;
      setEpi([x, z]);
    };
    renderer.domElement.addEventListener("pointerdown", onDown);

    let time = 0;
    let raf = 0;
    let last = performance.now();
    let lastPhase = -1;
    let orbit = 0;
    const proj = new THREE.Vector3();
    const frame = (now: number) => {
      raf = requestAnimationFrame(frame);
      if (!visible || document.hidden) {
        last = now;
        return;
      }
      const dt = Math.min((now - last) / 1000, 0.1);
      last = now;
      orbit += dt * 0.05;

      // Epicentre glide after a click; the loop restarts when it lands.
      if (moveT < 1) {
        moveT = Math.min(1, moveT + dt / 0.9);
        epiNow.lerpVectors(epiFrom, epiTo, ease(moveT));
        time = -0.6;
      }
      const e0 = epiNow;
      const ar = PINS.map(([x, z]) => travel(Math.hypot(x - e0.x, z - e0.y), VP_KMS));
      const tMax = Math.max(...ar) + 1.2;
      const L = tMax + TRACE + HOLD;
      const sc = scrub.current;
      if (sc.target !== null) {
        const goal = sc.target * L;
        time += (goal - time) * (1 - Math.exp(-dt * 14));
        if (!sc.active && Math.abs(goal - time) < 0.01) sc.target = null;
      } else if (moveT >= 1) time += dt;
      if (time > L) time = 0;
      clock.current = { time, len: L };

      const fadeOut = 1 - ease((time - (L - 0.6)) / 0.6);
      uni.uEpi.value.copy(e0);
      uni.uT.value = Math.max(0, time);
      uni.uFade.value = time < tMax + 0.8 ? 1 : Math.max(0.25, 1 - (time - tMax - 0.8) / 1.5) * fadeOut;
      epiGrp.position.set(e0.x, 0, e0.y);

      // Pins light up as the P wave arrives.
      pins.forEach((p, i) => {
        const since = time - ar[i];
        const on = since >= 0;
        p.headMat.color.copy(on ? cyan : ink);
        p.beamMat.opacity = on ? 0.9 : 0.45;
        const pulse = on ? Math.max(0, 1 - since / 1.2) : 0;
        p.pulseMat.opacity = pulse;
        p.pulse.scale.setScalar(1 + (1 - pulse) * 3);
        // Trace back: circle radius grows to the epicentral distance
        // implied by the travel time (Vp x t, minus the depth).
        const tb = ease((time - tMax) / (TRACE * 0.8));
        circUni.uR.value[i] = Math.sqrt(Math.max(0, (VP_KMS * ar[i]) ** 2 - DEPTH_KM ** 2)) * tb;
        // Label.
        const lab = labels.current[i];
        if (lab) {
          proj.set(PINS[i][0], 6.4, PINS[i][1]).project(camera);
          lab.style.transform = `translate(-50%,-100%) translate(${((proj.x + 1) / 2) * el.clientWidth}px, ${((1 - proj.y) / 2) * el.clientHeight}px)`;
          lab.dataset.on = on ? "1" : "0";
          const tspan = lab.querySelector("span[data-t]");
          if (tspan) tspan.textContent = on ? `+${ar[i].toFixed(1)} ${unit.current}` : "…";
        }
      });
      circUni.uA.value = time > tMax ? fadeOut : 0;
      const found = time > tMax + TRACE * 0.8;
      epiRingMat.opacity = found ? 0.9 * fadeOut : 0;
      epiRing.scale.setScalar(found ? 1 + Math.sin(time * 5) * 0.15 : 1);
      epiDotMat.opacity = time < 0.3 || found ? fadeOut : 0.35;
      if (epiLabel.current) {
        proj.set(e0.x, 1.2, e0.y).project(camera);
        epiLabel.current.style.transform = `translate(-50%,-120%) translate(${((proj.x + 1) / 2) * el.clientWidth}px, ${((1 - proj.y) / 2) * el.clientHeight}px)`;
        epiLabel.current.style.opacity = found ? String(fadeOut) : "0";
      }
      const u = Math.min(1, Math.max(0, time / L));
      if (fill.current) fill.current.style.transform = `scaleX(${u})`;
      if (handle.current) handle.current.style.left = `${u * 100}%`;
      if (bubble.current) bubble.current.textContent = `${Math.max(0, time).toFixed(1)} ${unit.current}`;
      marks.current.forEach((m, i) => {
        if (m) m.dataset.on = time >= ar[i] ? "1" : "0";
      });
      const ph = time < 0.4 ? 0 : time < tMax ? 1 : 2;
      if (ph !== lastPhase) {
        lastPhase = ph;
        setPhase(ph);
      }

      const portrait = camera.aspect < 1;
      const dist = portrait ? 118 : 78;
      camera.position.set(Math.sin(0.5 + Math.sin(orbit) * 0.15) * dist * 0.62, dist * 0.72, Math.cos(0.5 + Math.sin(orbit) * 0.15) * dist * 0.62);
      camera.lookAt(0, -2, 0);
      renderer.render(scene, camera);
    };
    raf = requestAnimationFrame(frame);
    return () => {
      cancelAnimationFrame(raf);
      ro.disconnect();
      io.disconnect();
      renderer.domElement.removeEventListener("pointerdown", onDown);
      renderer.dispose();
      renderer.domElement.remove();
    };
  }, []);

  const setFrom = (clientX: number) => {
    const r = track.current?.getBoundingClientRect();
    if (r) scrub.current.target = Math.min(1, Math.max(0, (clientX - r.left) / r.width));
  };
  const caption = phase === 0 ? t("loc.p0") : phase === 1 ? t("loc.p1") : t("loc.p2");

  return (
    <div className="glass overflow-hidden">
      <div className="relative h-[300px] overflow-hidden sm:h-[380px]">
        <div ref={host} className="absolute inset-0" />
        {NAMES.map((n, i) => (
          <div
            key={n}
            ref={(el) => {
              labels.current[i] = el;
            }}
            className="mono pointer-events-none absolute left-0 top-0 whitespace-nowrap rounded-md border border-rule-strong bg-ground/80 px-1.5 py-0.5 text-[11px] text-ink-2 backdrop-blur data-[on='1']:border-cyan/60 data-[on='1']:text-cyan"
          >
            {n} <span data-t="">…</span>
          </div>
        ))}
        <div ref={epiLabel} className="mono pointer-events-none absolute left-0 top-0 whitespace-nowrap rounded-md bg-hot px-1.5 py-0.5 text-[11px] font-semibold text-[#1a0b03] opacity-0">
          {t("loc.found")}
        </div>
        <span className="hud mono pointer-events-none absolute left-3 top-3 max-w-[85%] px-2.5 py-1 text-[11px] text-ink-2">{caption}</span>
        <span className="mono pointer-events-none absolute bottom-2 right-3 text-[10px] text-ink-3">{t("loc.tap")}</span>
      </div>
      {/* Timeline: loops by itself; drag to scrub, the scene follows smoothly. */}
      <div
        className="relative h-[68px] cursor-grab touch-none select-none border-t border-rule px-5 active:cursor-grabbing"
        onPointerDown={(e) => {
          e.currentTarget.setPointerCapture(e.pointerId);
          scrub.current.active = true;
          setFrom(e.clientX);
        }}
        onPointerMove={(e) => {
          if (scrub.current.active) setFrom(e.clientX);
        }}
        onPointerUp={() => (scrub.current.active = false)}
        onPointerCancel={() => (scrub.current.active = false)}
        onKeyDown={(e) => {
          if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
          e.preventDefault();
          const { time, len } = clock.current;
          scrub.current.target = Math.min(1, Math.max(0, (time + (e.key === "ArrowRight" ? 0.5 : -0.5)) / len));
        }}
        role="slider"
        aria-label={t("loc.drag")}
        aria-valuemin={0}
        aria-valuemax={100}
        tabIndex={0}
      >
        <div ref={track} className="absolute inset-x-5 top-[30px] h-1.5 rounded-full bg-rule-strong">
          {/* trace-back phase */}
          <div className="absolute inset-y-0 right-0 rounded-r-full bg-cyan/15" style={{ left: `${((Math.max(...arr) + 1.2) / loop) * 100}%` }}>
            <span className="mono absolute -top-[18px] right-0 text-[10px] uppercase tracking-[0.12em] text-ink-3">{t("loc.traceBack")}</span>
          </div>
          <div ref={fill} className="absolute inset-0 origin-left rounded-full bg-gradient-to-r from-cyan to-hot" style={{ transform: "scaleX(0)" }} />
          {order.map(([a, i]) => (
            <div
              key={i}
              ref={(el) => {
                marks.current[i] = el;
              }}
              data-on="0"
              className="group absolute top-1/2 -translate-x-1/2 -translate-y-1/2"
              style={{ left: `${(a / loop) * 100}%` }}
            >
              <div className="h-3.5 w-3.5 rounded-full border-2 border-cyan bg-ground transition-all duration-200 group-data-[on='1']:scale-110 group-data-[on='1']:bg-cyan group-data-[on='1']:shadow-[0_0_12px_var(--cyan)]" />
              <div className="mono absolute left-1/2 top-[18px] -translate-x-1/2 whitespace-nowrap text-[10px] text-ink-3 transition-colors group-data-[on='1']:text-cyan">
                {NAMES[i]} +{a.toFixed(1)}
              </div>
            </div>
          ))}
          <div ref={handle} className="pointer-events-none absolute top-1/2 -translate-x-1/2 -translate-y-1/2" style={{ left: 0 }}>
            <span ref={bubble} className="mono absolute bottom-[14px] left-1/2 -translate-x-1/2 whitespace-nowrap rounded bg-ink px-1.5 py-0.5 text-[10px] font-semibold text-ground">
              0.0
            </span>
            <div className="h-[18px] w-[18px] rounded-full border-[3px] border-hot bg-ink shadow-[0_0_14px_var(--hot)]" />
          </div>
        </div>
      </div>
    </div>
  );
}
