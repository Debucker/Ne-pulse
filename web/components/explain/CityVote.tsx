"use client";

// "One building is one vote", as a small 3D city. The city sits on a grid of
// 1 km squares (the network's independent units); lit windows are phones.
// A unit votes when at least a third of its listening phones trigger within
// 2 s, and the network needs votes from at least four units, in the order a
// wave would reach them (internal/network). Two stories alternate:
//   furniture: one tower's phones all trigger -> one beacon -> not enough
//   earthquake: a shock ring crosses the city -> beacons rise in wave order

import { useEffect, useRef, useState } from "react";
import * as THREE from "three";
import { useT } from "@/lib/i18n";

type Story = "building" | "quake";
const COLS = 7;
const ROWS = 5;
const SQ = 2; // scene units per 1 km square
const NEED = 4;
const DUR: Record<Story, number> = { building: 10, quake: 11 };
const TOWER = { c: 3, r: 2 };
const EPI = new THREE.Vector2(-COLS * SQ * 0.5 - 5, 0.8); // west of the city
const WAVE = 3.0; // units per second (slowed for viewing)

const clamp = (v: number, a = 0, b = 1) => Math.min(b, Math.max(a, v));
const ease = (u: number) => {
  const x = clamp(u);
  return x < 0.5 ? 4 * x * x * x : 1 - (-2 * x + 2) ** 3 / 2;
};

interface Building {
  c: number;
  r: number;
  x: number;
  z: number;
  w: number;
  d: number;
  h: number;
  reach: number; // time (s after the wave starts) the ring reaches it
}

const buildingVert = /* glsl */ `
attribute float aHeat;
varying vec2 vUv;
varying float vHeat;
varying vec3 vN;
void main() {
  vUv = uv;
  vHeat = aHeat;
  vN = normalize(mat3(modelMatrix * instanceMatrix) * normal);
  gl_Position = projectionMatrix * viewMatrix * modelMatrix * instanceMatrix * vec4(position, 1.0);
}
`;
const buildingFrag = /* glsl */ `
uniform vec3 uCyan;
uniform vec3 uHot;
varying vec2 vUv;
varying float vHeat;
varying vec3 vN;
void main() {
  vec2 e = min(vUv, 1.0 - vUv);
  float edge = 1.0 - smoothstep(0.0, 0.035, min(e.x, e.y));
  float top = step(0.5, vN.y);
  vec3 body = vec3(0.035, 0.055, 0.07) + vec3(0.02, 0.04, 0.05) * top;
  vec3 line = mix(uCyan * 0.55, uHot, vHeat);
  vec3 col = body + line * edge * (0.7 + vHeat * 0.8) + uHot * vHeat * 0.12;
  gl_FragColor = vec4(col, 1.0);
  #include <colorspace_fragment>
}
`;
const beamVert = /* glsl */ `
varying float vY;
void main() { vY = uv.y; gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }
`;
const beamFrag = /* glsl */ `
uniform vec3 uCol;
uniform float uA;
varying float vY;
void main() {
  float a = uA * pow(1.0 - vY, 1.6);
  gl_FragColor = vec4(uCol * (0.6 + 0.8 * (1.0 - vY)), a);
  #include <colorspace_fragment>
}
`;
const groundFrag = /* glsl */ `
uniform vec3 uCyan;
uniform vec3 uHot;
uniform vec2 uEpi;
uniform float uR;
uniform float uRA;
uniform vec2 uSel;
uniform float uSelA;
varying vec3 vW;
void main() {
  vec2 half_ = vec2(${((COLS * SQ) / 2).toFixed(1)}, ${((ROWS * SQ) / 2).toFixed(1)});
  vec2 p = vW.xz + half_;
  vec2 g = p / ${SQ.toFixed(1)};
  vec2 f = abs(fract(g - 0.5) - 0.5) / fwidth(g);
  float line = 1.0 - min(min(f.x, f.y), 1.0);
  float inside = step(0.0, p.x) * step(0.0, p.y) * step(p.x, half_.x * 2.0) * step(p.y, half_.y * 2.0);
  vec3 col = vec3(0.02, 0.035, 0.045) * inside + uCyan * line * 0.35 * inside;
  // Selected square glow.
  vec2 cell = floor(g);
  float sel = (cell.x == uSel.x && cell.y == uSel.y) ? 1.0 : 0.0;
  col += uHot * sel * uSelA * 0.25 * inside;
  // Shock ring.
  float r = length(vW.xz - uEpi);
  float ring = exp(-pow((r - uR) / 0.9, 2.0)) * uRA;
  float trail = r < uR ? exp(-(uR - r) / 3.0) * 0.25 * uRA : 0.0;
  col += uHot * (ring + trail);
  float fade = smoothstep(26.0, 10.0, length(vW.xz));
  gl_FragColor = vec4(col * fade, 1.0);
  #include <colorspace_fragment>
}
`;

export default function CityVote() {
  const { t } = useT();
  const host = useRef<HTMLDivElement>(null);
  const bar = useRef<HTMLDivElement>(null);
  const [story, setStory] = useState<Story>("building");
  const storyRef = useRef(story);
  storyRef.current = story;
  const [ui, setUi] = useState({ phones: 0, votes: 0, cap: "vote.b1", verdict: "" as "" | "ignored" | "quake" });

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    let renderer: THREE.WebGLRenderer;
    try {
      renderer = new THREE.WebGLRenderer({ antialias: true, alpha: false });
    } catch {
      return;
    }
    renderer.setPixelRatio(Math.min(devicePixelRatio || 1, 2));
    const css = (n: string, f: string) => new THREE.Color(getComputedStyle(document.documentElement).getPropertyValue(n).trim() || f);
    const ground = css("--panel", "#0a1015");
    renderer.setClearColor(ground);
    el.appendChild(renderer.domElement);
    Object.assign(renderer.domElement.style, { display: "block", width: "100%", height: "100%" });
    const cyan = css("--cyan", "#38e1ff");
    const hot = css("--hot", "#ff7a2f");

    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(30, 1, 0.1, 200);

    // Ground with 1 km squares and the shock ring.
    const gUni = {
      uCyan: { value: cyan },
      uHot: { value: hot },
      uEpi: { value: EPI },
      uR: { value: 0 },
      uRA: { value: 0 },
      uSel: { value: new THREE.Vector2(TOWER.c, TOWER.r) },
      uSelA: { value: 0 },
    };
    const groundMesh = new THREE.Mesh(
      new THREE.PlaneGeometry(70, 70).rotateX(-Math.PI / 2),
      new THREE.ShaderMaterial({
        uniforms: gUni,
        vertexShader: `varying vec3 vW; void main(){ vW = (modelMatrix*vec4(position,1.)).xyz; gl_Position = projectionMatrix*viewMatrix*vec4(vW,1.); }`,
        fragmentShader: groundFrag,
      }),
    );
    scene.add(groundMesh);

    // Buildings.
    let seed = 5;
    const rnd = () => (seed = (seed * 16807) % 2147483647) / 2147483647;
    const blds: Building[] = [];
    const ox = (-COLS * SQ) / 2;
    const oz = (-ROWS * SQ) / 2;
    for (let c = 0; c < COLS; c++)
      for (let r = 0; r < ROWS; r++) {
        if (c === TOWER.c && r === TOWER.r) {
          blds.push({ c, r, x: ox + (c + 0.5) * SQ, z: oz + (r + 0.5) * SQ, w: 0.9, d: 0.9, h: 3.4, reach: 0 });
          continue;
        }
        const n = 2 + Math.floor(rnd() * 3);
        for (let i = 0; i < n; i++) {
          const w = 0.35 + rnd() * 0.35;
          const d = 0.35 + rnd() * 0.35;
          blds.push({ c, r, x: ox + c * SQ + 0.35 + rnd() * (SQ - 0.7), z: oz + r * SQ + 0.35 + rnd() * (SQ - 0.7), w, d, h: 0.25 + rnd() * rnd() * 1.4, reach: 0 });
        }
      }
    for (const b of blds) b.reach = Math.hypot(b.x - EPI.x, b.z - EPI.y) / WAVE;
    const box = new THREE.BoxGeometry(1, 1, 1);
    box.translate(0, 0.5, 0);
    const heat = new Float32Array(blds.length);
    box.setAttribute("aHeat", new THREE.InstancedBufferAttribute(heat, 1));
    const bMat = new THREE.ShaderMaterial({ uniforms: { uCyan: { value: cyan }, uHot: { value: hot } }, vertexShader: buildingVert, fragmentShader: buildingFrag });
    const inst = new THREE.InstancedMesh(box, bMat, blds.length);
    scene.add(inst);

    // Phones = lit windows on the facades.
    const phones: { b: number; local: THREE.Vector3; seed: number }[] = [];
    blds.forEach((b, bi) => {
      const n = b.c === TOWER.c && b.r === TOWER.r ? 100 : 2 + Math.floor(rnd() * 5);
      for (let i = 0; i < n; i++) {
        const side = Math.floor(rnd() * 4);
        const u = rnd() - 0.5;
        const y = 0.12 + rnd() * (b.h - 0.2);
        const local =
          side === 0
            ? new THREE.Vector3(u * b.w, y, b.d / 2 + 0.01)
            : side === 1
              ? new THREE.Vector3(u * b.w, y, -b.d / 2 - 0.01)
              : side === 2
                ? new THREE.Vector3(b.w / 2 + 0.01, y, u * b.d)
                : new THREE.Vector3(-b.w / 2 - 0.01, y, u * b.d);
        phones.push({ b: bi, local, seed: rnd() });
      }
    });
    const pPos = new Float32Array(phones.length * 3);
    const pCol = new Float32Array(phones.length * 3);
    const pGeo = new THREE.BufferGeometry();
    pGeo.setAttribute("position", new THREE.BufferAttribute(pPos, 3));
    pGeo.setAttribute("color", new THREE.BufferAttribute(pCol, 3));
    const pts = new THREE.Points(
      pGeo,
      new THREE.PointsMaterial({ size: 0.09, vertexColors: true, transparent: true, depthWrite: false, blending: THREE.AdditiveBlending, sizeAttenuation: true }),
    );
    scene.add(pts);

    // Beacons: one per square.
    const beams: { mesh: THREE.Mesh; uni: { uCol: { value: THREE.Color }; uA: { value: number } }; ring: THREE.Mesh; ringMat: THREE.MeshBasicMaterial }[] = [];
    for (let c = 0; c < COLS; c++)
      for (let r = 0; r < ROWS; r++) {
        const uni = { uCol: { value: hot.clone() }, uA: { value: 0 } };
        const mesh = new THREE.Mesh(
          new THREE.CylinderGeometry(0.1, 0.18, 1, 16, 1, true).translate(0, 0.5, 0),
          new THREE.ShaderMaterial({ uniforms: uni, vertexShader: beamVert, fragmentShader: beamFrag, transparent: true, depthWrite: false, blending: THREE.AdditiveBlending, side: THREE.DoubleSide }),
        );
        mesh.position.set(ox + (c + 0.5) * SQ, 0, oz + (r + 0.5) * SQ);
        const ringMat = new THREE.MeshBasicMaterial({ color: hot, transparent: true, opacity: 0, depthWrite: false, blending: THREE.AdditiveBlending });
        const ring = new THREE.Mesh(new THREE.RingGeometry(0.5, 0.62, 48).rotateX(-Math.PI / 2), ringMat);
        ring.position.copy(mesh.position).setY(0.02);
        scene.add(mesh, ring);
        beams.push({ mesh, uni, ring, ringMat });
      }
    const beamIndex = (c: number, r: number) => c * ROWS + r;

    const resize = () => {
      renderer.setSize(el.clientWidth, el.clientHeight, false);
      camera.aspect = el.clientWidth / el.clientHeight;
      camera.updateProjectionMatrix();
    };
    const ro = new ResizeObserver(resize);
    ro.observe(el);
    resize();
    let visible = true;
    const io = new IntersectionObserver(([e]) => (visible = e.isIntersecting));
    io.observe(el);

    const m4 = new THREE.Matrix4();
    const q = new THREE.Quaternion();
    const e3 = new THREE.Euler();
    const sc = new THREE.Vector3();
    const pv = new THREE.Vector3();
    const camPos = new THREE.Vector3(14, 12, 16);
    const camLook = new THREE.Vector3();
    const wantPos = new THREE.Vector3();
    const wantLook = new THREE.Vector3();
    const cCyan = cyan.clone();
    const cHot = hot.clone();
    const cGrey = new THREE.Color(0x6d808b);

    let cur = storyRef.current;
    let t0 = performance.now();
    let raf = 0;
    let lastUi = 0;
    let orbit = 0;
    let last = performance.now();
    const frame = (now: number) => {
      raf = requestAnimationFrame(frame);
      const dt = Math.min((now - last) / 1000, 0.1);
      last = now;
      if (!visible || document.hidden) return;
      if (storyRef.current !== cur) {
        cur = storyRef.current;
        t0 = now;
      }
      let T = (now - t0) / 1000;
      if (T > DUR[cur]) {
        const next: Story = cur === "building" ? "quake" : "building";
        storyRef.current = next;
        setStory(next);
        cur = next;
        t0 = now;
        T = 0;
      }
      orbit += dt * 0.05;

      // Story state.
      const quake = cur === "quake";
      const wT = T - 1; // wave clock
      const ringR = quake && wT > 0 ? wT * WAVE : 0;
      gUni.uR.value = ringR;
      gUni.uRA.value = quake ? ease(wT / 0.3) * (1 - ease((T - 9) / 1.5)) : 0;
      const furn = !quake ? ease((T - 1.2) / 0.4) * (1 - ease((T - 4.4) / 0.5)) : 0;
      gUni.uSelA.value = !quake ? ease((T - 1.2) / 0.4) * (1 - ease((T - 9) / 0.8)) : 0;

      // Buildings: sway as the wave passes; heat = phones triggered inside.
      blds.forEach((b, i) => {
        let sway = 0;
        let h = 0;
        if (quake) {
          const since = wT - b.reach;
          if (since > 0) {
            const env = Math.exp(-since / 1.4) * ease(since / 0.15);
            sway = 0.09 * env * Math.sin(since * 11) * (0.6 + b.h * 0.3);
            h = env;
          }
        } else if (b.c === TOWER.c && b.r === TOWER.r) {
          h = furn;
          sway = 0.012 * furn * Math.sin(T * 30);
        }
        heat[i] = h;
        // Rotate about the base, perpendicular to the wave direction.
        const dx = b.x - EPI.x;
        const dz = b.z - EPI.y;
        const L = Math.hypot(dx, dz) || 1;
        e3.set((dz / L) * sway, 0, (-dx / L) * sway);
        q.setFromEuler(e3);
        sc.set(b.w, b.h, b.d);
        pv.set(b.x, 0, b.z);
        m4.compose(pv, q, sc);
        inst.setMatrixAt(i, m4);
      });
      inst.instanceMatrix.needsUpdate = true;
      (box.getAttribute("aHeat") as THREE.InstancedBufferAttribute).needsUpdate = true;

      // Phones.
      let trig = 0;
      phones.forEach((p, i) => {
        const b = blds[p.b];
        inst.getMatrixAt(p.b, m4);
        pv.copy(p.local).set(p.local.x / b.w, p.local.y / b.h, p.local.z / b.d).applyMatrix4(m4);
        pPos[i * 3] = pv.x;
        pPos[i * 3 + 1] = pv.y;
        pPos[i * 3 + 2] = pv.z;
        const on = heat[p.b] > 0.3 && p.seed < 0.9;
        if (on) trig++;
        const flick = on ? 0.75 + 0.25 * Math.sin(now / 40 + p.seed * 50) : 0.55;
        const col = on ? cHot : cCyan;
        pCol[i * 3] = col.r * flick;
        pCol[i * 3 + 1] = col.g * flick;
        pCol[i * 3 + 2] = col.b * flick;
      });
      pGeo.attributes.position.needsUpdate = true;
      pGeo.attributes.color.needsUpdate = true;

      // Beacons.
      let votes = 0;
      for (let c = 0; c < COLS; c++)
        for (let r = 0; r < ROWS; r++) {
          const bm = beams[beamIndex(c, r)];
          let v = 0;
          let col = cHot;
          if (quake) {
            const reach = Math.hypot(ox + (c + 0.5) * SQ - EPI.x, oz + (r + 0.5) * SQ - EPI.y) / WAVE;
            v = ease((wT - reach - 0.35) / 0.5) * (1 - ease((T - 10.3) / 0.7));
          } else if (c === TOWER.c && r === TOWER.r) {
            v = ease((T - 3.2) / 0.7) * (1 - ease((T - 9) / 0.8));
            // Not enough on its own: the beacon cools to grey.
            col = T > 5.8 ? cGrey : cHot;
          }
          if (v > 0.5) votes++;
          bm.uni.uCol.value.lerp(col, Math.min(1, dt * 3));
          bm.uni.uA.value = v * 0.9;
          bm.mesh.scale.set(1, 0.01 + v * (quake ? 3.2 : 5.2), 1);
          bm.mesh.visible = v > 0.005;
          const since = quake ? wT - Math.hypot(ox + (c + 0.5) * SQ - EPI.x, oz + (r + 0.5) * SQ - EPI.y) / WAVE - 0.35 : T - 3.2;
          const pulse = since > 0 && since < 1.2 && v > 0 ? 1 - since / 1.2 : 0;
          bm.ringMat.opacity = pulse * 0.9;
          bm.ring.scale.setScalar(1 + (1 - pulse) * 1.6);
        }

      // Camera: orbit; push in on the tower during the furniture story.
      const portrait = camera.aspect < 1;
      const k = portrait ? 1.55 : 1;
      if (!quake && T > 0.8 && T < 7.5) {
        const tw = new THREE.Vector3(ox + (TOWER.c + 0.5) * SQ, 1.6, oz + (TOWER.r + 0.5) * SQ);
        wantLook.copy(tw);
        const a = 0.7 + orbit;
        wantPos.set(tw.x + Math.sin(a) * 7.5 * k, 5.2 * k, tw.z + Math.cos(a) * 7.5 * k);
      } else {
        wantLook.set(0, 0.4, 0);
        const a = 0.55 + orbit;
        wantPos.set(Math.sin(a) * 17 * k, 13 * k, Math.cos(a) * 17 * k);
      }
      const kk = 1 - Math.exp(-dt * 1.4);
      camPos.lerp(wantPos, kk);
      camLook.lerp(wantLook, kk);
      camera.position.copy(camPos);
      camera.lookAt(camLook);
      renderer.render(scene, camera);
      if (bar.current) bar.current.style.width = `${(T / DUR[cur]) * 100}%`;

      if (now - lastUi > 120) {
        lastUi = now;
        let cap = "vote.b1";
        let verdict: "" | "ignored" | "quake" = "";
        if (!quake) {
          cap = T < 1.2 ? "vote.b1" : T < 3.2 ? "vote.b2" : T < 5.8 ? "vote.b3" : "vote.b4";
          if (T > 5.8) verdict = "ignored";
        } else {
          cap = T < 1 ? "vote.q1" : T < 7.8 ? "vote.q2" : "vote.q3";
          if (T > 7.8) verdict = "quake";
        }
        setUi({ phones: trig, votes, cap, verdict });
      }
    };
    raf = requestAnimationFrame(frame);
    return () => {
      cancelAnimationFrame(raf);
      ro.disconnect();
      io.disconnect();
      renderer.dispose();
      renderer.domElement.remove();
    };
  }, []);

  return (
    <div className="glass overflow-hidden">
      <div className="relative h-[320px] sm:h-[440px]">
        <div ref={host} className="absolute inset-0" />
        <div className="pointer-events-none absolute inset-x-0 top-0 h-20 bg-gradient-to-b from-panel/90 to-transparent" />
        <div key={ui.cap} className="pointer-events-none absolute left-4 top-3 max-w-[80%] animate-[npIn_0.6s_cubic-bezier(0.16,1,0.3,1)]">
          <div className="mono text-[10px] uppercase tracking-[0.16em] text-cyan">{t(`vote.${story}`)}</div>
          <div className="mt-0.5 font-display text-lg font-bold text-ink sm:text-xl">{t(ui.cap)}</div>
        </div>
        <div className="absolute bottom-3 right-3 flex gap-1">
          {(["building", "quake"] as Story[]).map((s) => (
            <button key={s} onClick={() => setStory(s)} className={`chip !min-h-[28px] !px-2.5 !text-[11px] ${story === s ? "chip-on" : ""}`}>
              {t(`vote.${s}`)}
            </button>
          ))}
        </div>
        <div className="absolute inset-x-0 bottom-0 h-[3px] bg-rule/60">
          <div ref={bar} className="h-full bg-cyan/70" style={{ width: 0 }} />
        </div>
      </div>
      <div className="grid grid-cols-3 gap-px border-t border-rule bg-rule text-center">
        <div className="bg-panel px-3 py-3">
          <div className="label">{t("vote.phones")}</div>
          <div className="mono mt-1 text-xl text-ink">{ui.phones}</div>
        </div>
        <div className="bg-panel px-3 py-3">
          <div className="label">{t("vote.votes")}</div>
          <div className="mono mt-1 text-xl">
            <span className={ui.votes >= NEED ? "text-hot" : "text-ink"}>{ui.votes}</span>
            <span className="text-sm text-ink-3"> / {t("vote.need", { n: NEED })}</span>
          </div>
        </div>
        <div className="flex items-center justify-center bg-panel px-3 py-3 text-sm leading-snug">
          {ui.verdict === "ignored" && <span className="text-warn">{t("vote.local")}</span>}
          {ui.verdict === "quake" && <span className="font-semibold text-hot">{t("vote.wave")}</span>}
          {ui.verdict === "" && <span className="text-ink-3">{t("vote.watching")}</span>}
        </div>
      </div>
    </div>
  );
}
