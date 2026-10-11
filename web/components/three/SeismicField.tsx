"use client";

// The NE-PULSE wave field: a WebGL surface around the network with every
// public area rendered as a cluster of particles (one per phone, up to 36),
// data links to the network core, and the P and S wavefronts of an event
// expanding at the locator's velocities from its estimated epicentre.
//
// Everything on screen is driven by data:
//  - particle count per area  = phones listening in that 10 km square
//  - particle jitter           = the area's live motion relative to its own
//                                background (median of the phones' activity)
//  - orange particles          = phones in a triggered state (live) or areas
//                                whose first pick has arrived (replay)
//  - wavefront radii           = sqrt((v t)^2 - h^2) with Vp 5.8, Vs 3.36 km/s,
//                                h = 8 km, t from the event's estimated origin
//  - wave amplitude            = the event's measured peak acceleration

import { useEffect, useRef } from "react";
import * as THREE from "three";
import { DEPTH_KM, VP_KMS, VS_KMS, toLocalKm } from "@/lib/dsp";
import type { PublicCell, PublicEvent } from "@/lib/types";

export interface FieldProps {
  cells: PublicCell[];
  /** Event whose wavefronts are drawn (live or replay). */
  event: PublicEvent | null;
  /**
   * Seconds since the event's origin to draw. A function is polled every
   * frame so callers can drive live time, looping replays or a scrubber
   * without re-rendering React.
   */
  age: () => number | null;
  className?: string;
  /** Lets the page scroll over the canvas on touch devices (hero). */
  passive?: boolean;
  /** Camera distance multiplier (>1 = further away). */
  zoom?: number;
  /**
   * The network's median motion relative to each phone's own background
   * (trace p50; 1 = normal). Drives the ambient swell of the surface.
   */
  noise?: number;
}

const MAX_PER_CELL = 36;
const SPAN_UNITS = 120; // scene units across the field

const WAVE_GLSL = /* glsl */ `
uniform vec4 uWave;   // epicentre x, z (units), age (s), amplitude (units)
uniform float uScale; // scene units per km
uniform float uTime;
uniform float uNoise;
const float VP = ${VP_KMS.toFixed(2)};
const float VS = ${VS_KMS.toFixed(2)};
const float H = ${DEPTH_KM.toFixed(1)};
float ringf(float d, float r, float w) { float x = (d - r) / w; return exp(-x * x); }
// Returns (height, P glow, S glow) at a surface point in scene units.
vec3 waveAt(vec2 p) {
  vec3 o = vec3(0.0);
  // Ambient: a faint two-component swell whose size follows the network's
  // median background motion.
  o.x += uNoise * 0.18 * (sin(p.x * 0.21 + uTime * 0.9) * sin(p.y * 0.17 - uTime * 0.7));
  if (uWave.w <= 0.0 || uWave.z < 0.0) return o;
  float d = length(p - uWave.xy) / uScale;
  float t = uWave.z;
  float rp = sqrt(max(0.0, VP * VP * t * t - H * H));
  float rs = sqrt(max(0.0, VS * VS * t * t - H * H));
  float att = 1.0 / (1.0 + d / 22.0);
  float gp = rp > 0.0 ? ringf(d, rp, 2.6) : 0.0;
  float gs = rs > 0.0 ? ringf(d, rs, 4.2) : 0.0;
  // Coda behind the S front, decaying with time since it passed.
  float behind = rs - d;
  float coda = behind > 0.0 ? exp(-behind / 9.0) * 0.35 * sin(behind * 1.3 - uTime * 6.0) : 0.0;
  o.x += uWave.w * att * (0.28 * gp * sin((d - rp) * 2.4) + gs * sin((d - rs) * 1.5) + coda);
  o.y = gp * att;
  o.z = gs * att + max(coda, 0.0) * att * 0.6;
  return o;
}
`;

const surfaceVert = /* glsl */ `
${WAVE_GLSL}
varying vec3 vW;
varying vec3 vG;
void main() {
  vec3 p = position;
  vec3 w = waveAt(p.xz);
  p.y += w.x;
  vW = p;
  vG = w;
  gl_Position = projectionMatrix * modelViewMatrix * vec4(p, 1.0);
}
`;

const surfaceFrag = /* glsl */ `
uniform vec3 uGround;
uniform vec3 uLine;
uniform vec3 uP;
uniform vec3 uS;
uniform float uGridUnits;
uniform float uRadius;
varying vec3 vW;
varying vec3 vG;
void main() {
  vec2 g = vW.xz / uGridUnits;
  vec2 f = abs(fract(g - 0.5) - 0.5) / fwidth(g);
  float line = 1.0 - min(min(f.x, f.y), 1.0);
  vec2 g2 = vW.xz / (uGridUnits * 5.0);
  vec2 f2 = abs(fract(g2 - 0.5) - 0.5) / fwidth(g2);
  float major = 1.0 - min(min(f2.x, f2.y), 1.0);
  float r = length(vW.xz) / uRadius;
  float fade = smoothstep(1.0, 0.25, r);
  vec3 col = uGround * 1.6 + uLine * (line * 0.32 + major * 0.55);
  col += uP * vG.y * 0.8 + uS * vG.z * 0.85;
  col += uLine * clamp(vW.y, 0.0, 3.0) * 0.12;
  gl_FragColor = vec4(mix(uGround, col, fade), 1.0);
  #include <colorspace_fragment>
}
`;

const pointVert = /* glsl */ `
${WAVE_GLSL}
attribute float aAct;
attribute float aFirst;
attribute float aSeed;
attribute float aTrig;
uniform float uPx;
varying float vHot;
varying float vFade;
void main() {
  vec3 p = position;
  vec3 w = waveAt(p.xz);
  float jit = sin(uTime * (7.0 + aSeed * 9.0) + aSeed * 40.0) * aAct * 0.55;
  float since = uWave.z - aFirst;
  float flash = (uWave.w > 0.0 && since > 0.0) ? exp(-since / 7.0) : 0.0;
  vHot = max(flash, aTrig);
  p.y += w.x + 0.45 + jit + vHot * 0.9;
  vec4 mv = modelViewMatrix * vec4(p, 1.0);
  gl_PointSize = (3.4 + vHot * 4.0 + aAct * 1.5) * uPx * (90.0 / -mv.z);
  vFade = smoothstep(${(SPAN_UNITS * 0.62).toFixed(1)}, ${(SPAN_UNITS * 0.3).toFixed(1)}, length(p.xz));
  gl_Position = projectionMatrix * mv;
}
`;

const pointFrag = /* glsl */ `
uniform vec3 uCold;
uniform vec3 uHot;
varying float vHot;
varying float vFade;
void main() {
  vec2 c = gl_PointCoord - 0.5;
  float d = length(c);
  if (d > 0.5) discard;
  float core = smoothstep(0.5, 0.0, d);
  vec3 col = mix(uCold, uHot, vHot);
  gl_FragColor = vec4(col * (0.55 + core * 1.1), core * (0.75 + vHot * 0.25) * vFade);
  #include <colorspace_fragment>
}
`;

const linkVert = /* glsl */ `
attribute float aT;
attribute float aRate;
uniform float uTime;
varying float vT;
varying float vRate;
void main() {
  vT = aT;
  vRate = aRate;
  gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
}
`;

// Links carry moving pulses: one per second per link, which is the rate at
// which every phone ships a batch of samples.
const linkFrag = /* glsl */ `
uniform vec3 uCol;
uniform float uTime;
varying float vT;
varying float vRate;
void main() {
  float ph = fract(vT - uTime * 0.9 + vRate * 7.13);
  float pulse = smoothstep(0.1, 0.0, abs(ph - 0.5));
  gl_FragColor = vec4(uCol, 0.12 + pulse * 0.6 * (0.4 + vRate));
  #include <colorspace_fragment>
}
`;

function hash(n: number) {
  const x = Math.sin(n * 127.1 + 311.7) * 43758.5453;
  return x - Math.floor(x);
}

function cssColor(name: string, fallback: string) {
  if (typeof window === "undefined") return new THREE.Color(fallback);
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return new THREE.Color(v || fallback);
}

export default function SeismicField({ cells, event, age, className, passive = true, zoom = 1, noise = 1 }: FieldProps) {
  const host = useRef<HTMLDivElement>(null);
  const api = useRef<{ setData: (c: PublicCell[], e: PublicEvent | null) => void } | null>(null);
  const ageRef = useRef(age);
  ageRef.current = age;
  const noiseRef = useRef(noise);
  noiseRef.current = noise;

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    let renderer: THREE.WebGLRenderer;
    try {
      renderer = new THREE.WebGLRenderer({ antialias: true, alpha: false, powerPreference: "high-performance" });
    } catch {
      el.dataset.webgl = "off";
      return;
    }
    const mobile = window.matchMedia("(max-width: 700px)").matches;
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, mobile ? 1.75 : 2));
    const ground = cssColor("--ground", "#05080b");
    renderer.setClearColor(ground, 1);
    el.appendChild(renderer.domElement);
    renderer.domElement.style.display = "block";
    renderer.domElement.style.width = "100%";
    renderer.domElement.style.height = "100%";
    if (passive) renderer.domElement.style.touchAction = "pan-y";

    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(38, 1, 0.5, 800);

    const shared = {
      uWave: { value: new THREE.Vector4(0, 0, -1, 0) },
      uScale: { value: 1 },
      uTime: { value: 0 },
      uNoise: { value: 0.12 },
    };
    const cold = cssColor("--cyan", "#38e1ff");
    const hot = cssColor("--hot", "#ff7a2f");

    // Surface.
    const seg = mobile ? 150 : 220;
    const planeGeo = new THREE.PlaneGeometry(SPAN_UNITS * 1.3, SPAN_UNITS * 1.3, seg, seg);
    planeGeo.rotateX(-Math.PI / 2);
    const surface = new THREE.Mesh(
      planeGeo,
      new THREE.ShaderMaterial({
        uniforms: {
          ...shared,
          uGround: { value: ground },
          uLine: { value: cold.clone().multiplyScalar(0.55) },
          uP: { value: cold },
          uS: { value: hot },
          uGridUnits: { value: 4 },
          uRadius: { value: SPAN_UNITS * 0.62 },
        },
        vertexShader: surfaceVert,
        fragmentShader: surfaceFrag,
      }),
    );
    scene.add(surface);

    // Phones.
    const pointMat = new THREE.ShaderMaterial({
      uniforms: { ...shared, uPx: { value: renderer.getPixelRatio() }, uCold: { value: cold }, uHot: { value: hot } },
      vertexShader: pointVert,
      fragmentShader: pointFrag,
      transparent: true,
      depthWrite: false,
      blending: THREE.AdditiveBlending,
    });
    const points = new THREE.Points(new THREE.BufferGeometry(), pointMat);
    scene.add(points);

    // Links to the core.
    const linkMat = new THREE.ShaderMaterial({
      uniforms: { uTime: shared.uTime, uCol: { value: cold } },
      vertexShader: linkVert,
      fragmentShader: linkFrag,
      transparent: true,
      depthWrite: false,
      blending: THREE.AdditiveBlending,
    });
    const links = new THREE.LineSegments(new THREE.BufferGeometry(), linkMat);
    scene.add(links);

    // Core: where the phones' data goes. A ring and a vertical beam.
    const core = new THREE.Group();
    const coreRing = new THREE.Mesh(new THREE.TorusGeometry(2.2, 0.12, 8, 48), new THREE.MeshBasicMaterial({ color: cold, transparent: true, opacity: 0.9 }));
    coreRing.rotation.x = Math.PI / 2;
    const coreDot = new THREE.Mesh(new THREE.SphereGeometry(0.7, 16, 12), new THREE.MeshBasicMaterial({ color: cold }));
    core.add(coreRing, coreDot);
    scene.add(core);

    // Epicentre beam.
    const beam = new THREE.Mesh(
      new THREE.CylinderGeometry(0.18, 0.18, 26, 8, 1, true),
      new THREE.MeshBasicMaterial({ color: hot, transparent: true, opacity: 0.0, blending: THREE.AdditiveBlending, depthWrite: false }),
    );
    scene.add(beam);

    let centre: [number, number] = [41.3, 69.25];
    let scale = 1;
    const coreY = 18;

    const setData = (cellsIn: PublicCell[], ev: PublicEvent | null) => {
      // Nodes: live public areas, plus the areas that triggered for the event
      // (they may have gone quiet since).
      type Node = { lat: number; lng: number; n: number; act: number; trig: number; first: number };
      const nodes = new Map<string, Node>();
      const key = (la: number, lo: number) => `${la.toFixed(2)},${lo.toFixed(2)}`;
      for (const c of cellsIn) {
        // Jitter follows the area's motion relative to its own background:
        // 1x (normal sensor noise) is a faint tremble, 8x and above is strong.
        const act = Math.min(1, 0.14 * Math.pow(Math.max(c.activity, 0), 0.85));
        nodes.set(key(c.lat, c.lng), { lat: c.lat, lng: c.lng, n: c.sensors, act, trig: c.sensors ? c.triggered / c.sensors : 0, first: 1e9 });
      }
      for (const c of ev?.cells ?? []) {
        const k = key(c.lat, c.lng);
        const cur = nodes.get(k);
        const first = ev ? c.firstT - ev.originT : 1e9;
        if (cur) cur.first = first;
        else nodes.set(k, { lat: c.lat, lng: c.lng, n: Math.max(c.stations, 3), act: 0, trig: 0, first });
      }
      const list = [...nodes.values()];
      if (list.length) {
        const lat0 = ev ? ev.lat : list.reduce((s, n) => s + n.lat, 0) / list.length;
        const lng0 = ev ? ev.lng : list.reduce((s, n) => s + n.lng, 0) / list.length;
        centre = [lat0, lng0];
      } else if (ev) centre = [ev.lat, ev.lng];
      let ext = 30;
      for (const n of list) {
        const [x, y] = toLocalKm(n.lat, n.lng, centre[0], centre[1]);
        ext = Math.max(ext, Math.abs(x) + 8, Math.abs(y) + 8);
      }
      scale = SPAN_UNITS / 2 / (ext * 1.25);
      shared.uScale.value = scale;

      const pos: number[] = [];
      const aAct: number[] = [];
      const aFirst: number[] = [];
      const aSeed: number[] = [];
      const aTrig: number[] = [];
      const lpos: number[] = [];
      const lt: number[] = [];
      const lrate: number[] = [];
      const cellKm = 10;
      list.forEach((n, ni) => {
        const [x, y] = toLocalKm(n.lat, n.lng, centre[0], centre[1]);
        const count = Math.min(MAX_PER_CELL, Math.max(1, n.n));
        const trigCount = Math.round(n.trig * count);
        for (let i = 0; i < count; i++) {
          const s = ni * 97 + i * 13;
          // Phones are placed inside their 10 km square; within it positions
          // are illustrative (true positions are never published).
          const px = (x + (hash(s) - 0.5) * cellKm * 0.8) * scale;
          const pz = -(y + (hash(s + 5) - 0.5) * cellKm * 0.8) * scale;
          pos.push(px, 0, pz);
          aAct.push(n.act);
          aFirst.push(n.first + hash(s + 9) * 0.6);
          aSeed.push(hash(s + 3));
          aTrig.push(i < trigCount ? 1 : 0);
        }
        // Link from area centre up to the core.
        const cx = x * scale;
        const cz = -y * scale;
        const rate = Math.min(1, n.n / 30);
        lpos.push(cx, 0.6, cz, 0, coreY, 0);
        lt.push(0, 1);
        lrate.push(rate, rate);
      });
      const g = new THREE.BufferGeometry();
      g.setAttribute("position", new THREE.Float32BufferAttribute(pos, 3));
      g.setAttribute("aAct", new THREE.Float32BufferAttribute(aAct, 1));
      g.setAttribute("aFirst", new THREE.Float32BufferAttribute(aFirst, 1));
      g.setAttribute("aSeed", new THREE.Float32BufferAttribute(aSeed, 1));
      g.setAttribute("aTrig", new THREE.Float32BufferAttribute(aTrig, 1));
      points.geometry.dispose();
      points.geometry = g;
      const lg = new THREE.BufferGeometry();
      lg.setAttribute("position", new THREE.Float32BufferAttribute(lpos, 3));
      lg.setAttribute("aT", new THREE.Float32BufferAttribute(lt, 1));
      lg.setAttribute("aRate", new THREE.Float32BufferAttribute(lrate, 1));
      links.geometry.dispose();
      links.geometry = lg;
      core.position.set(0, coreY, 0);

      if (ev) {
        const [ex, ey] = toLocalKm(ev.lat, ev.lng, centre[0], centre[1]);
        shared.uWave.value.x = ex * scale;
        shared.uWave.value.y = -ey * scale;
        // Amplitude from the measured peak acceleration, log-scaled so a
        // 0.1 m/s^2 event is visible and a 5 m/s^2 one does not tear the mesh.
        const pga = Math.max(ev.metrics.maxPga, 0.05);
        shared.uWave.value.w = Math.min(5.5, 1.2 + 1.6 * Math.log10(pga / 0.05));
        beam.position.set(ex * scale, 13, -ey * scale);
      } else {
        shared.uWave.value.w = 0;
      }
    };
    api.current = { setData };

    const resize = () => {
      const w = el.clientWidth;
      const h = el.clientHeight;
      if (!w || !h) return;
      renderer.setSize(w, h, false);
      camera.aspect = w / h;
      camera.updateProjectionMatrix();
    };
    const ro = new ResizeObserver(resize);
    ro.observe(el);
    resize();

    let visible = true;
    const io = new IntersectionObserver(([e]) => (visible = e.isIntersecting), { threshold: 0 });
    io.observe(el);

    // Pointer parallax (desktop) — never captures touch.
    let px = 0;
    let py = 0;
    const onMove = (e: PointerEvent) => {
      if (e.pointerType !== "mouse") return;
      const r = el.getBoundingClientRect();
      px = ((e.clientX - r.left) / r.width - 0.5) * 2;
      py = ((e.clientY - r.top) / r.height - 0.5) * 2;
    };
    el.addEventListener("pointermove", onMove);

    const clock = new THREE.Clock();
    let raf = 0;
    let orbit = 0.6;
    const frame = () => {
      raf = requestAnimationFrame(frame);
      if (!visible || document.hidden) return;
      const dt = Math.min(clock.getDelta(), 0.1);
      shared.uTime.value += dt;
      if (!reduce) orbit += dt * 0.035;
      shared.uNoise.value += (Math.min(1, 0.12 * noiseRef.current) - shared.uNoise.value) * Math.min(1, dt);
      const a = ageRef.current();
      shared.uWave.value.z = a ?? -1;
      beam.material.opacity = a !== null && a >= 0 ? Math.max(0.15, 0.85 * Math.exp(-a / 30)) : 0;

      // Frame the field: portrait screens need more distance to fit width.
      // Portrait screens: let the field bleed off the sides rather than
      // shrinking it to a thin band.
      const fit = camera.aspect < 1 ? Math.min(1.45, 0.75 / camera.aspect) : 1;
      const dist = 150 * zoom * fit;
      const tilt = 0.62 + py * 0.05;
      camera.position.set(Math.sin(orbit + px * 0.12) * dist * Math.cos(tilt), dist * Math.sin(tilt), Math.cos(orbit + px * 0.12) * dist * Math.cos(tilt));
      // Aim below the field so it sits in the upper part of the frame,
      // clear of the headline.
      camera.lookAt(0, camera.aspect < 1 ? -26 : -14, 0);
      coreRing.rotation.z += dt * 0.6;
      renderer.render(scene, camera);
    };
    frame();

    return () => {
      cancelAnimationFrame(raf);
      ro.disconnect();
      io.disconnect();
      el.removeEventListener("pointermove", onMove);
      api.current = null;
      scene.traverse((o) => {
        const m = o as THREE.Mesh;
        m.geometry?.dispose();
        const mat = m.material as THREE.Material | THREE.Material[] | undefined;
        if (Array.isArray(mat)) mat.forEach((x) => x.dispose());
        else mat?.dispose();
      });
      renderer.dispose();
      renderer.domElement.remove();
    };
  }, [passive, zoom]);

  useEffect(() => {
    api.current?.setData(cells, event);
  }, [cells, event]);

  return <div ref={host} className={className} />;
}
