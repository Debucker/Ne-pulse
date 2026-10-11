"use client";

// A looping short film: one person, their phone, an ordinary day and three
// earthquakes — one while the phone lies on a table, one while it is held
// during a walk, one while it is in a pocket during a jog.
//
// Rendering: a real rigged human (Mixamo "Y Bot", three.js example asset,
// public/models/xbot.glb) driven by motion-captured idle / walk / run clips,
// synchronised to the scripted path, plus additive poses (crouch, head down,
// looking around). The right hand is placed on the phone by two-bone IK and
// turned so the palm lies on the phone's back — the phone never passes
// through the hand.
//
// Physics: the accelerometer reading comes from a pure function of time, the
// phone's scripted path p(t) and orientation R(t) (on the table, in front of
// the eyes, at the ear, in the front pocket): f = R^T (a + g + a_ground), with
// a = d^2p/dt^2 and a_ground the earthquake's ground acceleration — the whole
// world shakes, wherever the phone is. A simplified copy of the station stage
// decides live (internal/station): listen only when still, trigger on a
// STA/LTA rise, cancel on tilt, reject impulsive bursts, detrigger before a
// new pick. Timers are shortened (settling 3 s instead of 30 s).

import { useEffect, useRef, useState } from "react";
import * as THREE from "three";
import { GLTFLoader } from "three/examples/jsm/loaders/GLTFLoader.js";
import { RoundedBoxGeometry } from "three/examples/jsm/geometries/RoundedBoxGeometry.js";
import { bandpass, butterworth, StaLta, type Cascade } from "@/lib/dsp";
import { useT } from "@/lib/i18n";

const FS = 50;
const G = 9.81;
const LOOP = 61;
const QUAKE_VIS = 60;
const SETTLE_S = 3;
const TABLE_H = 0.75;
const QUAKES = [
  { p: 3.5, s: 5.6 },
  { p: 22.6, s: 24.0 },
  { p: 37.8, s: 39.2 },
];
const KNOCKS = [54.0, 56.9];
const SPOT = new THREE.Vector3(0.22, TABLE_H + 0.005, 0.2);
const KNOCK_AT = new THREE.Vector3(0.32, TABLE_H, 0.24);
const EPI = new THREE.Vector2(-9, -6);
const UP = new THREE.Vector3(0, 1, 0);

type Chapter = "quake" | "pickup" | "walk" | "walkquake" | "call" | "run" | "runquake" | "down" | "knock";
const CHAPTERS: { t: number; id: Chapter }[] = [
  { t: 0, id: "quake" },
  { t: 12.6, id: "pickup" },
  { t: 18.4, id: "walk" },
  { t: 22.2, id: "walkquake" },
  { t: 28.4, id: "call" },
  { t: 34.3, id: "run" },
  { t: 37.4, id: "runquake" },
  { t: 42, id: "down" },
  { t: 52.4, id: "knock" },
];
const chapterAt = (t: number) => [...CHAPTERS].reverse().find((c) => t >= c.t)!.id;

// ---- script ----------------------------------------------------------------

const smoother = (u: number) => {
  const x = Math.min(1, Math.max(0, u));
  return x * x * x * (x * (6 * x - 15) + 10);
};
const dSmoother = (u: number) => (u <= 0 || u >= 1 ? 0 : 30 * u * u * (1 - u) * (1 - u));
const ramp = (t: number, a: number, b: number, c: number, d: number) => smoother((t - a) / (b - a)) * (1 - smoother((t - c) / (d - c)));

const YA = Math.atan2(-1.0, -1.2);
const T_X = 0.15;
const T_Z = 0.62;
interface RK {
  t: number;
  x: number;
  z: number;
  yaw: number;
  run?: boolean;
}
const ROOT: RK[] = [
  { t: 0, x: 1.0, z: 1.2, yaw: YA },
  { t: 12.6, x: 1.0, z: 1.2, yaw: YA },
  { t: 14.6, x: T_X, z: T_Z, yaw: -Math.PI },
  { t: 17.6, x: T_X, z: T_Z, yaw: -Math.PI },
  { t: 18.4, x: T_X, z: T_Z, yaw: (-3 * Math.PI) / 2 },
  { t: 19.6, x: 1.0, z: 1.4, yaw: (-3 * Math.PI) / 2 },
  { t: 23.6, x: 5.4, z: 1.4, yaw: (-3 * Math.PI) / 2 },
  { t: 26.0, x: 5.4, z: 1.4, yaw: (-3 * Math.PI) / 2 },
  { t: 28.4, x: 8.2, z: 1.4, yaw: (-3 * Math.PI) / 2 },
  { t: 34.3, x: 8.2, z: 1.4, yaw: (-3 * Math.PI) / 2 },
  { t: 35.1, x: 8.2, z: 1.4, yaw: -Math.PI / 2 },
  { t: 41.2, x: -3.0, z: 1.4, yaw: -Math.PI / 2, run: true },
  { t: 42.0, x: -3.0, z: 1.4, yaw: Math.PI / 2 },
  { t: 44.6, x: T_X, z: T_Z, yaw: Math.PI },
  { t: 46.6, x: T_X, z: T_Z, yaw: Math.PI },
  { t: 47.6, x: 0.2, z: 1.0, yaw: Math.PI },
  { t: 52.4, x: 0.2, z: 1.0, yaw: Math.PI },
  { t: 53.0, x: 0.14, z: 0.6, yaw: Math.PI },
  { t: 58.0, x: 0.14, z: 0.6, yaw: Math.PI },
  { t: 58.6, x: 0.14, z: 0.6, yaw: 0.99 },
  { t: 60.0, x: 1.0, z: 1.2, yaw: 0.99 },
  { t: 60.6, x: 1.0, z: 1.2, yaw: YA + 2 * Math.PI },
  { t: 61, x: 1.0, z: 1.2, yaw: YA + 2 * Math.PI },
];
const WALK_CYCLE = 1.4; // m per stride of the walk clip
const RUN_CYCLE = 2.5;
const PHASE0: number[] = [0];
for (let i = 1; i < ROOT.length; i++) {
  const a = ROOT[i - 1];
  const b = ROOT[i];
  PHASE0.push(PHASE0[i - 1] + (2 * Math.PI * Math.hypot(b.x - a.x, b.z - a.z)) / (b.run ? RUN_CYCLE : WALK_CYCLE));
}

type Mode = "table" | "view" | "ear" | "pocket";
const PHONE: [number, number, Mode, Mode][] = [
  [0, 15.1, "table", "table"],
  [15.1, 16.1, "table", "view"],
  [16.1, 28.4, "view", "view"],
  [28.4, 29.2, "view", "ear"],
  [29.2, 32.6, "ear", "ear"],
  [32.6, 33.4, "ear", "view"],
  [33.4, 34.3, "view", "pocket"],
  [34.3, 44.6, "pocket", "pocket"],
  [44.6, 45.4, "pocket", "view"],
  [45.4, 46.4, "view", "table"],
  [46.4, LOOP, "table", "table"],
];
const phoneSeg = (t: number) => {
  let k = 0;
  while (k + 1 < PHONE.length && PHONE[k + 1][0] <= t) k++;
  return PHONE[k];
};
// Right hand on the phone: from the pick-up until it goes into the pocket,
// and from taking it out until it is back on the table.
const holdW = (t: number) => {
  if (t < 34.3) return smoother((t - 14.5) / 0.6) * (1 - smoother((t - 33.9) / 0.5));
  if (t < 44.6) return smoother((t - 44.0) / 0.6);
  return 1 - smoother((t - 46.3) / 0.6);
};
const knockW = (t: number) => ramp(t, 52.6, 53.3, 57.3, 58.0);
const coverW = (t: number) => ramp(t, 5.8, 6.5, 10.8, 11.8);
const crouchW = (t: number) => ramp(t, 5.8, 6.7, 10.8, 12) + 0.55 * (ramp(t, 14.3, 14.9, 15.5, 16.2) + ramp(t, 45.0, 45.6, 46.4, 47.0)) + 0.3 * knockW(t);
const lookAroundW = (t: number) => ramp(t, 24.0, 24.4, 25.6, 26.0);
// Procedural posture: knee bend (rad) and forward bow of the spine (rad).
const braceW = (t: number) => ramp(t, 5.8, 6.7, 10.8, 12);
const reachW = (t: number) => ramp(t, 14.3, 14.9, 15.5, 16.2) + ramp(t, 45.0, 45.6, 46.4, 47.0);
const kneeAt = (t: number) => 1.0 * braceW(t) + 0.3 * reachW(t) + 0.15 * knockW(t);
const bowAt = (t: number) => 0.35 * braceW(t) + 0.7 * reachW(t) + 0.45 * knockW(t);

function groundAccel(t: number, out: THREE.Vector3) {
  out.set(0, 0, 0);
  for (const q of QUAKES) {
    const dp = t - q.p;
    if (dp > 0 && dp < 12) {
      const e = Math.min(1, dp / 0.15) * Math.exp(-dp / 1.6);
      out.y += 0.09 * e * Math.sin(2 * Math.PI * 5.2 * dp);
      out.x += 0.03 * e * Math.sin(2 * Math.PI * 6.1 * dp + 1);
    }
    const ds = t - q.s;
    if (ds > 0 && ds < 14) {
      const e = Math.min(1, ds / 0.4) * Math.exp(-ds / 2.8);
      out.x += 0.32 * e * (0.7 * Math.sin(2 * Math.PI * 2.2 * ds) + 0.3 * Math.sin(2 * Math.PI * 3.3 * ds + 2));
      out.z += 0.26 * e * (0.6 * Math.sin(2 * Math.PI * 2.7 * ds + 1) + 0.4 * Math.sin(2 * Math.PI * 1.8 * ds));
      out.y += 0.06 * e * Math.sin(2 * Math.PI * 3.9 * ds);
    }
  }
  return out;
}
function knockAccel(t: number) {
  let a = 0;
  for (const k of KNOCKS) {
    const d = t - k;
    if (d >= 0 && d < 0.4) a += 2.2 * Math.exp(-d / 0.035) * Math.sin(2 * Math.PI * 14 * d);
  }
  return a;
}

// ---- body state (pure) -------------------------------------------------------

interface Body {
  root: THREE.Vector3;
  yaw: number;
  f: THREE.Vector3;
  r: THREE.Vector3;
  speed: number;
  run: number;
  phase: number;
}
function body(t: number, b: Body) {
  t = ((t % LOOP) + LOOP) % LOOP;
  let i = 0;
  while (i + 1 < ROOT.length && ROOT[i + 1].t <= t) i++;
  const A = ROOT[i];
  const B = ROOT[Math.min(i + 1, ROOT.length - 1)];
  const span = Math.max(1e-6, B.t - A.t);
  const u = (t - A.t) / span;
  const w = B === A ? 0 : smoother(u);
  const segLen = Math.hypot(B.x - A.x, B.z - A.z);
  b.speed = B === A ? 0 : (segLen * dSmoother(u)) / span;
  b.run = B.run ? 1 : 0;
  b.phase = PHASE0[i] + (2 * Math.PI * segLen * w) / (B.run ? RUN_CYCLE : WALK_CYCLE);
  b.yaw = A.yaw + (B.yaw - A.yaw) * w;
  b.root.set(A.x + (B.x - A.x) * w, 0, A.z + (B.z - A.z) * w);
  b.f.set(Math.sin(b.yaw), 0, Math.cos(b.yaw));
  b.r.set(-Math.cos(b.yaw), 0, Math.sin(b.yaw)); // the person's right
}

const _m = new THREE.Matrix4();
function phoneBasis(n: THREE.Vector3, topHint: THREE.Vector3, out: THREE.Quaternion) {
  const y = n.clone().normalize();
  const top = topHint.clone().addScaledVector(y, -topHint.dot(y)).normalize();
  const z = top.negate(); // local -z = top of the phone, +y = screen normal
  const x = new THREE.Vector3().crossVectors(y, z);
  _m.makeBasis(x, y, z);
  return out.setFromRotationMatrix(_m);
}

interface Anchors {
  eye: THREE.Vector3;
  head: THREE.Vector3;
  hip: THREE.Vector3;
  knee: THREE.Vector3;
}
/** Anatomical anchors for the physics path (analytic, matched to the model). */
function anchors(t: number, b: Body, out: Anchors) {
  const amp = Math.min(1, b.speed / (b.run ? 2.6 : 1.3));
  const bob = amp * (b.run ? 0.045 : 0.02) * Math.sin(2 * b.phase);
  const crouch = crouchW(t);
  const breathe = 0.004 * Math.sin(2 * Math.PI * 0.25 * t);
  const nod = t > 29.2 && t < 32.6 ? 0.01 * Math.sin(2 * Math.PI * 0.8 * t) : 0;
  out.head.copy(b.root).addScaledVector(UP, 1.63 - 0.38 * crouch + bob + breathe).addScaledVector(b.f, 0.03 + nod + 0.15 * crouch);
  out.eye.copy(out.head).addScaledVector(b.f, 0.08).addScaledVector(UP, 0.02);
  const legSwing = amp * (b.run ? 0.7 : 0.4) * Math.sin(b.phase);
  out.hip.copy(b.root).addScaledVector(b.r, 0.1).addScaledVector(UP, 0.95 - 0.3 * crouch + bob);
  out.knee.copy(out.hip).addScaledVector(b.f, 0.45 * Math.sin(legSwing)).addScaledVector(UP, -0.45 * Math.cos(legSwing));
}

const _pa = new THREE.Vector3();
const _qa = new THREE.Quaternion();
const _pb = new THREE.Vector3();
const _qb = new THREE.Quaternion();
function modePose(mode: Mode, b: Body, a: Anchors, p: THREE.Vector3, q: THREE.Quaternion) {
  switch (mode) {
    case "table":
      p.copy(SPOT);
      phoneBasis(UP, new THREE.Vector3(0, 0, -1), q);
      break;
    case "view":
      p.copy(a.eye).addScaledVector(b.f, 0.3).addScaledVector(UP, -0.21).addScaledVector(b.r, 0.04);
      phoneBasis(new THREE.Vector3().subVectors(a.eye, p), UP, q);
      break;
    case "ear":
      p.copy(a.head).addScaledVector(b.r, 0.095).addScaledVector(b.f, 0.035).addScaledVector(UP, -0.035);
      phoneBasis(new THREE.Vector3().subVectors(a.head, p).setY(0), new THREE.Vector3().copy(UP).addScaledVector(b.f, -0.35), q);
      break;
    case "pocket":
      p.copy(a.hip).lerp(a.knee, 0.3).addScaledVector(b.f, 0.085).addScaledVector(b.r, 0.02);
      phoneBasis(new THREE.Vector3().copy(b.f).negate(), new THREE.Vector3().subVectors(a.hip, a.knee), q);
      break;
  }
}
/** Phone pose at t from a set of anchors (blended between modes). */
function phonePose(t: number, b: Body, a: Anchors, p: THREE.Vector3, q: THREE.Quaternion): Mode | "move" {
  const [t0, t1, fm, tm] = phoneSeg(t);
  if (fm === tm) {
    modePose(fm, b, a, p, q);
    return fm;
  }
  const w = smoother((t - t0) / (t1 - t0));
  modePose(fm, b, a, _pa, _qa);
  modePose(tm, b, a, _pb, _qb);
  p.copy(_pa).lerp(_pb, w).addScaledVector(UP, 0.05 * Math.sin(Math.PI * w));
  q.copy(_qa).slerp(_qb, w);
  return "move";
}

// ---- verdicts ----------------------------------------------------------------

type Verdict = "listening" | "moving" | "settling" | "triggered" | "vote" | "cancelled" | "knock";
const VCOL: Record<Verdict, string> = {
  listening: "#4fd18b",
  settling: "#f2c14e",
  moving: "#f2c14e",
  triggered: "#38e1ff",
  vote: "#ff7a2f",
  cancelled: "#6d808b",
  knock: "#6d808b",
};

export default function PhoneExplainer() {
  const { t } = useT();
  const tRef = useRef(t);
  tRef.current = t;
  const host = useRef<HTMLDivElement>(null);
  const trace = useRef<HTMLCanvasElement>(null);
  const [loaded, setLoaded] = useState(false);
  const [ui, setUi] = useState<{ v: Verdict; tilt: number; rms: number; settle: number; ch: Chapter }>({
    v: "listening",
    tilt: 0,
    rms: 0,
    settle: 0,
    ch: "quake",
  });

  useEffect(() => {
    const el = host.current;
    const cv = trace.current;
    if (!el || !cv) return;
    let renderer: THREE.WebGLRenderer;
    try {
      renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true });
    } catch {
      return;
    }
    renderer.setPixelRatio(Math.min(devicePixelRatio || 1, 2));
    renderer.toneMapping = THREE.ACESFilmicToneMapping;
    el.appendChild(renderer.domElement);
    Object.assign(renderer.domElement.style, { display: "block", width: "100%", height: "100%" });
    let disposed = false;

    const css = (n: string, f: string) => new THREE.Color(getComputedStyle(document.documentElement).getPropertyValue(n).trim() || f);
    const cyan = css("--cyan", "#38e1ff");
    const hot = css("--hot", "#ff7a2f");
    const rule = css("--rule-strong", "#25343e");

    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(36, 1, 0.02, 80);
    scene.add(new THREE.HemisphereLight(0xd6ecff, 0x0a0f13, 1.3));
    const key = new THREE.DirectionalLight(0xffffff, 2.2);
    key.position.set(3, 6, 4);
    scene.add(key);
    const rim = new THREE.DirectionalLight(0x38e1ff, 2.4);
    rim.position.set(-4, 3, -5);
    scene.add(rim);

    // Floor with wave rings.
    const floorMat = new THREE.ShaderMaterial({
      uniforms: { uCol: { value: cyan.clone().multiplyScalar(0.3) }, uRing: { value: new THREE.Vector4(0, 0, -1, 0) }, uEpi: { value: EPI }, uHot: { value: hot }, uCyan: { value: cyan } },
      vertexShader: `varying vec3 vW; void main(){ vW = (modelMatrix*vec4(position,1.)).xyz; gl_Position = projectionMatrix*viewMatrix*vec4(vW,1.); }`,
      fragmentShader: `uniform vec3 uCol; uniform vec4 uRing; uniform vec2 uEpi; uniform vec3 uHot; uniform vec3 uCyan; varying vec3 vW;
        void main(){
          vec2 g = vW.xz/0.5; vec2 f = abs(fract(g-.5)-.5)/fwidth(g);
          float l = 1.-min(min(f.x,f.y),1.);
          float fade = smoothstep(18.,3.,length(vW.xz - vec2(2.5,0.)));
          float r = length(vW.xz - uEpi);
          float ring = uRing.z > 0. ? exp(-pow((r-uRing.x)/0.6,2.))*uRing.y : 0.;
          vec3 rc = uRing.w > 0.5 ? uHot : uCyan;
          gl_FragColor = vec4(uCol*l*fade + rc*ring*fade, max(l*fade*0.9, ring*fade));
          #include <colorspace_fragment>
        }`,
      transparent: true,
      depthWrite: false,
    });
    const floor = new THREE.Mesh(new THREE.PlaneGeometry(60, 60).rotateX(-Math.PI / 2), floorMat);
    floor.position.set(2.5, 0, 0);
    scene.add(floor);

    // Table.
    const table = new THREE.Group();
    const top = new THREE.Mesh(new RoundedBoxGeometry(1.2, 0.04, 0.72, 3, 0.012), new THREE.MeshStandardMaterial({ color: 0x17222a, roughness: 0.7 }));
    top.position.y = TABLE_H - 0.02;
    top.add(new THREE.LineSegments(new THREE.EdgesGeometry(new THREE.BoxGeometry(1.2, 0.04, 0.72)), new THREE.LineBasicMaterial({ color: rule })));
    table.add(top);
    const legMat = new THREE.MeshStandardMaterial({ color: 0x10171c, roughness: 0.8 });
    for (const [x, z] of [
      [-0.54, -0.3],
      [0.54, -0.3],
      [-0.54, 0.3],
      [0.54, 0.3],
    ]) {
      const leg = new THREE.Mesh(new THREE.BoxGeometry(0.04, TABLE_H - 0.04, 0.04), legMat);
      leg.position.set(x, (TABLE_H - 0.04) / 2, z);
      table.add(leg);
    }
    scene.add(table);

    // Phone with the app on its screen.
    const phone = new THREE.Group();
    phone.add(new THREE.Mesh(new RoundedBoxGeometry(0.075, 0.009, 0.155, 4, 0.004), new THREE.MeshStandardMaterial({ color: 0x33424d, metalness: 0.7, roughness: 0.3 })));
    const app = document.createElement("canvas");
    app.width = 240;
    app.height = 496;
    const actx = app.getContext("2d")!;
    const tex = new THREE.CanvasTexture(app);
    tex.colorSpace = THREE.SRGBColorSpace;
    const screenMat = new THREE.MeshBasicMaterial({ map: tex, toneMapped: false });
    const screen = new THREE.Mesh(new THREE.PlaneGeometry(0.069, 0.147), screenMat);
    screen.rotation.x = -Math.PI / 2;
    screen.position.y = 0.0047;
    phone.add(screen);
    scene.add(phone);

    // ---- the person ----
    type Rig = {
      model: THREE.Object3D;
      mixer: THREE.AnimationMixer;
      acts: Record<string, THREE.AnimationAction>;
      bone: (n: string) => THREE.Bone;
    };
    let person: Rig | null = null;
    new GLTFLoader().load("/models/xbot.glb", (gltf) => {
      if (disposed) return;
      const model = gltf.scene;
      model.traverse((o) => {
        const m = o as THREE.SkinnedMesh;
        if (m.isSkinnedMesh) {
          m.frustumCulled = false;
          m.material =
            m.name === "Beta_Joints"
              ? new THREE.MeshStandardMaterial({ color: 0x1b5a66, emissive: cyan, emissiveIntensity: 0.08, roughness: 0.45, metalness: 0.2 })
              : new THREE.MeshStandardMaterial({ color: 0x33434e, metalness: 0.3, roughness: 0.45 });
        }
      });
      scene.add(model);
      const mixer = new THREE.AnimationMixer(model);
      const acts: Record<string, THREE.AnimationAction> = {};
      for (let clip of gltf.animations) {
        if (clip.name === "walk" || clip.name === "run") {
          // In place: remove horizontal hip travel, the script moves the root.
          for (const tr of clip.tracks) {
            if (tr.name.endsWith("Hips.position")) {
              for (let k = 0; k < tr.values.length; k += 3) {
                tr.values[k] = 0;
                tr.values[k + 2] = 0;
              }
            }
          }
        }
        if (["sneak_pose", "sad_pose", "headShake", "agree"].includes(clip.name)) {
          THREE.AnimationUtils.makeClipAdditive(clip);
          if (clip.name.endsWith("_pose")) clip = THREE.AnimationUtils.subclip(clip, clip.name, 2, 3, 30);
        }
        const a = mixer.clipAction(clip);
        a.play();
        a.setEffectiveWeight(0);
        acts[clip.name] = a;
      }
      const bones: Record<string, THREE.Bone> = {};
      model.traverse((o) => {
        if ((o as THREE.Bone).isBone) bones[o.name.replace("mixamorig", "").replace(":", "")] = o as THREE.Bone;
      });
      person = { model, mixer, acts, bone: (n) => bones[n] };
      setLoaded(true);
    });

    // ---- IK helpers ----
    const wp = (o: THREE.Object3D, v = new THREE.Vector3()) => o.getWorldPosition(v);
    const rotateWorld = (bone: THREE.Object3D, delta: THREE.Quaternion) => {
      const wq = bone.getWorldQuaternion(new THREE.Quaternion());
      const pq = bone.parent!.getWorldQuaternion(new THREE.Quaternion()).invert();
      bone.quaternion.copy(pq.multiply(delta.multiply(wq)));
      bone.updateMatrixWorld(true);
    };
    const rotAxis = (bone: THREE.Object3D, axis: THREE.Vector3, angle: number) => {
      if (Math.abs(angle) > 1e-5) rotateWorld(bone, new THREE.Quaternion().setFromAxisAngle(axis, angle));
    };
    const aim = (bone: THREE.Object3D, from: THREE.Vector3, to: THREE.Vector3, amount = 1) => {
      if (from.lengthSq() < 1e-10 || to.lengthSq() < 1e-10) return;
      const q = new THREE.Quaternion().setFromUnitVectors(from.clone().normalize(), to.clone().normalize());
      if (amount < 1) q.slerp(new THREE.Quaternion(), 1 - amount);
      rotateWorld(bone, q);
    };
    const twoBone = (arm: THREE.Bone, fore: THREE.Bone, hand: THREE.Bone, target: THREE.Vector3, pole: THREE.Vector3, amount: number) => {
      if (amount <= 0.001) return;
      const A = wp(arm);
      const B = wp(fore);
      const C = wp(hand);
      const a = A.distanceTo(B);
      const b = B.distanceTo(C);
      const T = C.clone().lerp(target, amount);
      const dir = T.clone().sub(A);
      let d = dir.length();
      d = Math.min(Math.max(d, Math.abs(a - b) + 1e-3), a + b - 1e-3);
      dir.normalize();
      const x = (a * a - b * b + d * d) / (2 * d);
      const y = Math.sqrt(Math.max(0, a * a - x * x));
      const perp = pole.clone().addScaledVector(dir, -pole.dot(dir)).normalize();
      const E = A.clone().addScaledVector(dir, x).addScaledVector(perp, y);
      aim(arm, B.clone().sub(A), E.clone().sub(A));
      const B2 = wp(fore);
      const C2 = wp(hand);
      aim(fore, C2.clone().sub(B2), A.clone().addScaledVector(dir, d).sub(B2));
    };
    /** Turn the right hand so the palm faces along n and the fingers along fdir. */
    const orientHand = (hand: THREE.Bone, middle: THREE.Bone, thumb: THREE.Bone, fdir: THREE.Vector3, n: THREE.Vector3, amount = 1) => {
      const H = wp(hand);
      const fing = wp(middle).sub(H).normalize();
      const th = wp(thumb).sub(H).normalize();
      // Right hand: palm normal = thumb-side x fingers.
      const palm = new THREE.Vector3().crossVectors(th, fing).normalize();
      const cur = new THREE.Matrix4().makeBasis(fing, palm, new THREE.Vector3().crossVectors(fing, palm));
      const f2 = fdir.clone().normalize();
      const n2 = n.clone().addScaledVector(f2, -n.dot(f2)).normalize();
      const des = new THREE.Matrix4().makeBasis(f2, n2, new THREE.Vector3().crossVectors(f2, n2));
      const q = new THREE.Quaternion().setFromRotationMatrix(des).multiply(new THREE.Quaternion().setFromRotationMatrix(cur).invert());
      if (amount < 1) q.slerp(new THREE.Quaternion(), 1 - amount);
      rotateWorld(hand, q);
    };

    // ---- detector ----
    const H = FS * 8;
    const hist = new Float32Array(H);
    const histState = new Uint8Array(H);
    let hi = 0;
    let bp: Cascade[] = [];
    let grav: Cascade[] = [];
    let sl = new StaLta(1, 30, FS);
    const rmsWin = new Float32Array(FS);
    let wi = 0;
    const ref = new THREE.Vector3();
    let verdict: Verdict = "listening";
    let verdictUntil = 0;
    let stillFor = 0;
    let trig: { t: number; e: number[] } | null = null;
    let seed = 7;
    const noise = () => {
      seed = (seed * 16807) % 2147483647;
      const u = seed / 2147483647;
      seed = (seed * 16807) % 2147483647;
      return Math.sqrt(-2 * Math.log(u + 1e-12)) * Math.cos((2 * Math.PI * seed) / 2147483647);
    };
    const mkB = (): Body => ({ root: new THREE.Vector3(), yaw: 0, f: new THREE.Vector3(), r: new THREE.Vector3(), speed: 0, run: 0, phase: 0 });
    const mkA = (): Anchors => ({ eye: new THREE.Vector3(), head: new THREE.Vector3(), hip: new THREE.Vector3(), knee: new THREE.Vector3() });
    const B0 = mkB();
    const A0 = mkA();
    const P0 = new THREE.Vector3();
    const Q0 = new THREE.Quaternion();
    const P1 = new THREE.Vector3();
    const P2 = new THREE.Vector3();
    const Qx = new THREE.Quaternion();
    const physPose = (tt: number, p: THREE.Vector3, q: THREE.Quaternion) => {
      body(tt, B0);
      anchors(tt, B0, A0);
      return phonePose(tt, B0, A0, p, q);
    };
    const aW = new THREE.Vector3();
    const gA = new THREE.Vector3();
    const f = new THREE.Vector3();
    const gq = new THREE.Vector3();
    const qi = new THREE.Quaternion();

    const resetDetector = () => {
      physPose(0, P0, Q0);
      const g0 = new THREE.Vector3(0, G, 0).applyQuaternion(qi.copy(Q0).invert());
      bp = [0, 1, 2].map(() => bandpass(0.5, 12, FS));
      grav = [0, 1, 2].map(() => butterworth("lp", 2, 1, FS));
      [g0.x, g0.y, g0.z].forEach((v, i) => {
        bp[i].resetTo(v);
        grav[i].resetTo(v);
      });
      sl = new StaLta(1, 30, FS);
      sl.seed(0.006 ** 2);
      rmsWin.fill(0.005);
      ref.copy(g0);
      verdict = "listening";
      stillFor = 0;
      trig = null;
    };
    let simT = 0;
    const startAt = Number(new URLSearchParams(window.location.search).get("film"));
    if (startAt > 0 && startAt < LOOP) simT = startAt;
    resetDetector();

    const step = () => {
      const tt = simT;
      const mode = physPose(tt, P0, Q0);
      if (mode === "table") {
        aW.set(0, knockAccel(tt), 0);
      } else {
        const h = 0.01;
        physPose(tt - h, P1, Qx);
        physPose(tt + h, P2, Qx);
        physPose(tt, P0, Q0);
        aW.set(P2.x - 2 * P0.x + P1.x, P2.y - 2 * P0.y + P1.y, P2.z - 2 * P0.z + P1.z).multiplyScalar(1 / (h * h));
      }
      // The ground shakes under the table, the feet and everything else.
      aW.add(groundAccel(tt, gA));
      f.set(aW.x, aW.y + G, aW.z).applyQuaternion(qi.copy(Q0).invert());
      const ax = f.x + noise() * 0.006;
      const ay = f.y + noise() * 0.006;
      const az = f.z + noise() * 0.006;
      const m = Math.hypot(bp[0].process(ax), bp[1].process(ay), bp[2].process(az));
      gq.set(grav[0].process(ax), grav[1].process(ay), grav[2].process(az));
      const tilt = (gq.angleTo(ref) * 180) / Math.PI;
      rmsWin[wi] = m;
      wi = (wi + 1) % FS;
      let r2 = 0;
      for (let i = 0; i < FS; i++) r2 += rmsWin[i] * rmsWin[i];
      const rms = Math.sqrt(r2 / FS);
      const ratio = sl.update(m * m);

      if (verdict === "moving" || verdict === "settling") {
        if (rms < 0.15 && tilt < 1) {
          stillFor += 1 / FS;
          verdict = "settling";
          if (stillFor >= SETTLE_S) {
            verdict = "listening";
            sl.seed(Math.max(rms * rms, 0.006 ** 2));
          }
        } else {
          ref.copy(gq);
          stillFor = 0;
          verdict = "moving";
        }
      } else if (verdict === "listening") {
        if (tilt > 2 || rms > 0.6) {
          verdict = "moving";
          ref.copy(gq);
          stillFor = 0;
        } else if (ratio > 4 && Math.sqrt(sl.sta) > 0.02) {
          verdict = "triggered";
          sl.frozen = true;
          trig = { t: tt, e: [] };
        }
      } else if (verdict === "triggered" && trig) {
        trig.e.push(m * m);
        if (tilt > 2) {
          verdict = "cancelled";
          verdictUntil = tt + 2.5;
          sl.frozen = false;
          trig = null;
        } else if (tt - trig.t >= 2.56) {
          const e = trig.e;
          const tot = e.reduce((x, y) => x + y, 0);
          let c = 0;
          let i5 = -1;
          let i95 = e.length - 1;
          for (let i = 0; i < e.length; i++) {
            c += e[i];
            if (i5 < 0 && c >= 0.05 * tot) i5 = i;
            if (c >= 0.95 * tot) {
              i95 = i;
              break;
            }
          }
          const crest = Math.sqrt(Math.max(...e)) / Math.sqrt(tot / e.length);
          verdict = (i95 - Math.max(0, i5)) / FS < 0.4 || crest > 8 ? "knock" : "vote";
          verdictUntil = tt + 3;
          sl.frozen = false;
          trig = null;
        }
      } else if ((verdict === "vote" || verdict === "knock" || verdict === "cancelled") && tt >= verdictUntil && (verdict === "cancelled" || ratio < 1.5)) {
        if (tilt > 2 || rms > 0.6) {
          verdict = "moving";
          ref.copy(gq);
          stillFor = 0;
        } else verdict = "listening";
      }
      hist[hi % H] = m;
      histState[hi % H] = verdict === "moving" || verdict === "settling" ? 1 : verdict === "vote" ? 3 : 0;
      hi++;
      simT += 1 / FS;
      if (simT >= LOOP) {
        simT -= LOOP;
        resetDetector();
      }
      return { tilt, rms };
    };

    // ---- the app on the phone's screen ----
    const drawApp = () => {
      const W = app.width;
      actx.fillStyle = "#05080b";
      actx.fillRect(0, 0, W, app.height);
      actx.fillStyle = "#38e1ff";
      actx.font = "bold 20px sans-serif";
      actx.textAlign = "left";
      actx.fillText("NE-PULSE", 22, 44);
      const col = VCOL[verdict];
      const g = actx.createRadialGradient(W / 2, 170, 10, W / 2, 170, 90);
      g.addColorStop(0, col + "cc");
      g.addColorStop(1, col + "00");
      actx.fillStyle = g;
      actx.beginPath();
      actx.arc(W / 2, 170, 90, 0, Math.PI * 2);
      actx.fill();
      actx.fillStyle = col;
      actx.beginPath();
      actx.arc(W / 2, 170, 26, 0, Math.PI * 2);
      actx.fill();
      actx.fillStyle = "#eaf1f4";
      actx.font = "bold 22px sans-serif";
      actx.textAlign = "center";
      const words = tRef.current(`ex.v_${verdict}`).split(" ");
      let line = "";
      let y = 300;
      for (const wd of words) {
        if (actx.measureText(line + wd).width > W - 30 && line) {
          actx.fillText(line.trim(), W / 2, y);
          y += 28;
          line = "";
        }
        line += wd + " ";
      }
      actx.fillText(line.trim(), W / 2, y);
      actx.strokeStyle = "#ff7a2f";
      actx.lineWidth = 2;
      actx.beginPath();
      const n = Math.min(hi, FS * 4);
      for (let i = 0; i < n; i++) {
        const v = hist[(hi - n + i) % H];
        const yy = 450 - Math.min(1, Math.log10(Math.max(v, 0.003) / 0.003) / 3.8) * 60;
        const xx = 16 + (i / (FS * 4)) * (W - 32);
        if (i) actx.lineTo(xx, yy);
        else actx.moveTo(xx, yy);
      }
      actx.stroke();
      tex.needsUpdate = true;
    };

    const resize = () => {
      renderer.setSize(el.clientWidth, el.clientHeight, false);
      camera.aspect = el.clientWidth / el.clientHeight;
      camera.updateProjectionMatrix();
      cv.width = Math.round(cv.clientWidth * devicePixelRatio);
      cv.height = Math.round(cv.clientHeight * devicePixelRatio);
    };
    const ro = new ResizeObserver(resize);
    ro.observe(el);
    resize();
    let visible = true;
    const io = new IntersectionObserver(([e]) => (visible = e.isIntersecting));
    io.observe(el);

    const ctx = cv.getContext("2d")!;
    const drawTrace = () => {
      const W = cv.width;
      const Hh = cv.height;
      ctx.clearRect(0, 0, W, Hh);
      const n = Math.min(hi, H);
      const x0 = W - (n / H) * W;
      for (let i = 0; i < n; i++) {
        const s = histState[(hi - n + i) % H];
        if (s) {
          ctx.fillStyle = s === 1 ? "rgba(242,193,78,0.07)" : "rgba(255,122,47,0.12)";
          ctx.fillRect(x0 + (i / H) * W, 0, W / H + 1, Hh);
        }
      }
      const y = (v: number) => Hh - 3 - (Math.log10(Math.max(v, 0.003) / 0.003) / Math.log10(20 / 0.003)) * (Hh - 6);
      ctx.setLineDash([4 * devicePixelRatio, 4 * devicePixelRatio]);
      ctx.strokeStyle = `#${rule.getHexString()}`;
      ctx.lineWidth = devicePixelRatio;
      ctx.beginPath();
      ctx.moveTo(0, y(0.15));
      ctx.lineTo(W, y(0.15));
      ctx.stroke();
      ctx.setLineDash([]);
      ctx.lineWidth = 1.6 * devicePixelRatio;
      ctx.strokeStyle = `#${hot.getHexString()}`;
      ctx.beginPath();
      for (let i = 0; i < n; i++) {
        const xx = x0 + (i / H) * W;
        const v = hist[(hi - n + i) % H];
        if (i) ctx.lineTo(xx, y(v));
        else ctx.moveTo(xx, y(v));
      }
      ctx.stroke();
    };

    // ---- camera ----
    const camPos = new THREE.Vector3(-2.2, 1.7, 3.2);
    const camLook = new THREE.Vector3(0.3, 0.8, 0.4);
    const wantPos = new THREE.Vector3();
    const wantLook = new THREE.Vector3();
    const shot = (tt: number, b: Body, phonePos: THREE.Vector3, chest: THREE.Vector3, headPos: THREE.Vector3) => {
      const far = camera.aspect < 1 ? 1.5 : 1;
      const at = (look: THREE.Vector3, off: THREE.Vector3) => {
        wantLook.copy(look);
        wantPos.copy(look).addScaledVector(off, far);
      };
      const rel = (x: number, y: number, z: number) => new THREE.Vector3().copy(b.r).multiplyScalar(x).addScaledVector(UP, y).addScaledVector(b.f, z);
      const closeUp = new THREE.Vector3(-0.32, 0.3, 0.38);
      const wide = () => at(new THREE.Vector3(0.4, 0.8, 0.5), new THREE.Vector3(-2.3, 1.0, 2.6));
      if (tt < 2.6 || (tt > 6.3 && tt < 9.6)) at(phonePos, closeUp);
      else if (tt < 13.4) wide();
      else if (tt < 16.4) at(new THREE.Vector3().copy(b.root).setY(1.0), new THREE.Vector3(-1.5, 0.45, -1.1));
      else if (tt < 18.4) at(phonePos, new THREE.Vector3().copy(b.f).multiplyScalar(-0.3).addScaledVector(UP, 0.16).addScaledVector(b.r, 0.16));
      else if (tt < 28.4) at(new THREE.Vector3().copy(chest), rel(1.3, 0.25, 2.6));
      else if (tt < 33.4) at(new THREE.Vector3().copy(headPos).addScaledVector(UP, -0.1), rel(1.0, 0.05, 0.75));
      else if (tt < 42) at(new THREE.Vector3().copy(chest).setY(0.95), rel(1.6, 0.4, 3.4));
      else if (tt < 46.8) at(phonePos, new THREE.Vector3(-1.1, 0.45, -0.6));
      else if (tt < 52.4) at(phonePos, closeUp);
      else if (tt < 58.4) at(new THREE.Vector3(0.25, 0.9, 0.35), new THREE.Vector3(-1.3, 0.5, -1.0));
      else wide();
    };

    const Bv = mkB();
    const Av = mkA();
    const Pv = new THREE.Vector3();
    const Qv = new THREE.Quaternion();
    const gv = new THREE.Vector3();
    const clock = new THREE.Clock();
    let raf = 0;
    let acc = 0;
    let lastUi = 0;
    let lastApp = 0;
    let lastM = { tilt: 0, rms: 0 };
    const smEye = new THREE.Vector3();
    const smHead = new THREE.Vector3();
    let smT = -1;
    const frame = () => {
      raf = requestAnimationFrame(frame);
      if (!visible || document.hidden) return;
      const dt = Math.min(clock.getDelta(), 0.1);
      acc += dt;
      while (acc >= 1 / FS) {
        lastM = step();
        acc -= 1 / FS;
      }
      const tt = (simT + acc) % LOOP;
      body(tt, Bv);
      anchors(tt, Bv, Av);

      // Ground motion made visible (exaggerated); the detector uses true values.
      groundAccel(tt, gv);
      const dx = (-gv.x / (2 * Math.PI * 2.4) ** 2) * QUAKE_VIS;
      const dy = (-gv.y / (2 * Math.PI * 5) ** 2) * QUAKE_VIS + knockAccel(tt) * 0.0015;
      const dz = (-gv.z / (2 * Math.PI * 2.4) ** 2) * QUAKE_VIS;
      table.position.set(dx, dy, dz);

      let chest = new THREE.Vector3().copy(Bv.root).setY(1.3);
      let headPos = Av.head.clone();
      if (person) {
        const { model, mixer, acts, bone } = person;
        model.position.copy(Bv.root).add(new THREE.Vector3(dx * 0.5, 0, dz * 0.5));
        model.rotation.y = Bv.yaw;
        // Locomotion blend from speed; clips follow the scripted stride.
        const v = Bv.speed;
        const idleW = 1 - Math.min(1, v / 0.45);
        const loco = 1 - idleW;
        const runW = loco * Math.min(1, Math.max(0, (v - 1.5) / 0.7));
        const walkW = loco - runW;
        const cyc = (Bv.phase / (2 * Math.PI)) % 1;
        const set = (name: string, w: number, time?: number) => {
          const a = acts[name];
          if (!a) return;
          a.setEffectiveWeight(w);
          if (time !== undefined) a.time = time;
        };
        set("idle", idleW, (tt % acts.idle.getClip().duration + acts.idle.getClip().duration) % acts.idle.getClip().duration);
        set("walk", walkW, cyc * acts.walk.getClip().duration);
        set("run", runW, cyc * acts.run.getClip().duration);
        const viewing = phoneSeg(tt)[2] === "view" && phoneSeg(tt)[3] === "view";
        set("headShake", lookAroundW(tt), ((tt - 24) % acts.headShake.getClip().duration + acts.headShake.getClip().duration) % acts.headShake.getClip().duration);
        set("agree", tt > 29.4 && tt < 32.4 ? 0.3 : 0, ((tt - 29.4) % acts.agree.getClip().duration + acts.agree.getClip().duration) % acts.agree.getClip().duration);
        mixer.update(0);
        model.updateMatrixWorld(true);

        // Posture on top of the motion capture: bend the knees (feet stay on
        // the floor) and bow the spine to reach the table or brace.
        const knee = kneeAt(tt);
        const bow = bowAt(tt);
        if (knee > 0.001 || bow > 0.001) {
          model.position.y -= 0.88 * (1 - Math.cos(knee));
          model.updateMatrixWorld(true);
          for (const side of ["Left", "Right"]) {
            rotAxis(bone(side + "UpLeg"), Bv.r, knee);
            rotAxis(bone(side + "Leg"), Bv.r, -2 * knee);
            rotAxis(bone(side + "Foot"), Bv.r, knee);
          }
          for (const sp of ["Spine", "Spine1", "Spine2"]) rotAxis(bone(sp), Bv.r, -bow / 3);
        }

        // Phone pose from the model's own head and thigh (so it sits exactly
        // at the face, the ear or the pocket), blended like the physics path.
        const head = bone("Head");
        const le = bone("LeftEye");
        const re = bone("RightEye");
        const eyes = wp(le).add(wp(re)).multiplyScalar(0.5);
        // A held phone is steadied against the head's stride bob: smooth the
        // eye and head anchors relative to the body (the pocket stays rigid).
        const relE = eyes.clone().sub(Bv.root);
        const relH = wp(head).addScaledVector(UP, 0.07).sub(Bv.root);
        if (Math.abs(tt - smT) > 0.5 || smT < 0) {
          smEye.copy(relE);
          smHead.copy(relH);
        } else {
          const ks = 1 - Math.exp(-dt / 0.14);
          smEye.lerp(relE, ks);
          smHead.lerp(relH, ks);
        }
        smT = tt;
        const ha: Anchors = { eye: smEye.clone().add(Bv.root), head: smHead.clone().add(Bv.root), hip: wp(bone("RightUpLeg")), knee: wp(bone("RightLeg")) };
        headPos = ha.head.clone();
        chest = wp(bone("Spine2"));
        const mode = phonePose(tt, Bv, ha, Pv, Qv);
        if (mode === "table") Pv.add(new THREE.Vector3(dx, dy, dz));

        // Right hand on the phone: palm on its back, fingers across it.
        const hold = holdW(tt);
        const arm = bone("RightArm");
        const fore = bone("RightForeArm");
        const hand = bone("RightHand");
        const mid = bone("RightHandMiddle1");
        const thumb = bone("RightHandThumb1");
        const px = new THREE.Vector3(1, 0, 0).applyQuaternion(Qv);
        const py = new THREE.Vector3(0, 1, 0).applyQuaternion(Qv);
        const pz = new THREE.Vector3(0, 0, 1).applyQuaternion(Qv);
        const pole = new THREE.Vector3().copy(UP).multiplyScalar(-1).addScaledVector(Bv.r, 0.8).addScaledVector(Bv.f, -0.3);
        const palmTarget = Pv.clone().addScaledVector(py, -0.022).addScaledVector(px, 0.012).addScaledVector(pz, 0.03);
        const kw = knockW(tt);
        const cw = coverW(tt);
        if (hold > 0.001 || kw > 0.001 || cw > 0.001) {
          const tap = Math.min(...KNOCKS.map((k) => Math.abs(tt - k)));
          const knockT = KNOCK_AT.clone().addScaledVector(UP, 0.05 + 0.1 * Math.min(1, tap / 0.2));
          const coverT = (side: number) => ha.head.clone().addScaledVector(UP, 0.08).addScaledVector(Bv.r, 0.08 * side).addScaledVector(Bv.f, -0.02);
          for (let pass = 0; pass < 2; pass++) {
            const H0 = wp(hand);
            const palmOff = wp(mid).sub(H0).multiplyScalar(0.55);
            const target = cw > 0.001 ? coverT(1) : kw > 0.001 ? knockT : palmTarget.clone().sub(palmOff);
            twoBone(arm, fore, hand, target, pole, Math.max(hold, kw, cw));
            const ow = smoother(Math.min(1, Math.max(0, (hold - 0.1) / 0.6))) * (1 - kw) * (1 - cw);
            if (ow > 0.001) orientHand(hand, mid, thumb, px.clone().negate(), py, ow);
          }
          if (cw > 0.001) {
            const poleL = new THREE.Vector3().copy(UP).multiplyScalar(-1).addScaledVector(Bv.r, -0.8).addScaledVector(Bv.f, -0.3);
            twoBone(bone("LeftArm"), bone("LeftForeArm"), bone("LeftHand"), coverT(-1), poleL, cw);
          }
        }
        // Look at the phone while reading it.
        const look = viewing ? 0.6 : 0;
        if (look > 0) {
          const fwd = eyes.clone().sub(wp(head)).setY(0).normalize();
          aim(head, fwd, Pv.clone().sub(eyes), look * 0.5);
        }
      } else {
        phonePose(tt, Bv, Av, Pv, Qv);
      }
      phone.position.copy(Pv);
      phone.quaternion.copy(Qv);
      const mode = phoneSeg(tt);
      screenMat.color.setScalar(mode[2] === "pocket" && mode[3] === "pocket" ? 0.02 : 1);

      // Wave rings for whichever quake is running.
      let ringT = -1;
      let isS = 0;
      for (const q of QUAKES) {
        if (tt > q.s && tt - q.s < 9) {
          ringT = tt - q.s;
          isS = 1;
        } else if (tt > q.p && tt < q.s) ringT = tt - q.p;
      }
      floorMat.uniforms.uRing.value.set(ringT > 0 ? Math.hypot(EPI.x, EPI.y) + (ringT - 0.3) * (isS ? 3.4 : 5.8) : 0, ringT > 0 ? Math.exp(-ringT / 3) : 0, ringT > 0 ? 1 : -1, isS);

      // Camera: soft cuts, and never inside the person.
      shot(tt, Bv, phone.position, chest, headPos);
      const k = 1 - Math.exp(-dt * 1.7);
      camPos.lerp(wantPos, k);
      camLook.lerp(wantLook, k);
      const body0 = new THREE.Vector3().copy(Bv.root).setY(Math.min(1.2, camPos.y));
      const away = camPos.clone().sub(body0);
      if (away.length() < 0.55) camPos.copy(body0).addScaledVector(away.normalize(), 0.55);
      camera.position.copy(camPos);
      camera.lookAt(camLook);
      renderer.render(scene, camera);
      drawTrace();

      const now = performance.now();
      if (now - lastApp > 120) {
        lastApp = now;
        drawApp();
      }
      if (now - lastUi > 150) {
        lastUi = now;
        el.dataset.t = tt.toFixed(1);
        el.dataset.v = verdict;
        setUi({ v: verdict, tilt: lastM.tilt, rms: lastM.rms, settle: stillFor, ch: chapterAt(tt) });
      }
    };
    frame();
    return () => {
      disposed = true;
      cancelAnimationFrame(raf);
      ro.disconnect();
      io.disconnect();
      tex.dispose();
      renderer.dispose();
      renderer.domElement.remove();
    };
  }, []);

  const idx = CHAPTERS.findIndex((c) => c.id === ui.ch);
  return (
    <div className="glass overflow-hidden">
      <div className="relative h-[360px] sm:h-[480px]">
        <div ref={host} className="absolute inset-0" />
        {!loaded && <div className="mono absolute inset-0 grid place-items-center text-xs text-ink-3">{t("common.loading")}</div>}
        <div className="pointer-events-none absolute inset-x-0 top-0 h-28 bg-gradient-to-b from-ground/85 to-transparent" />
        <div key={ui.ch} className="pointer-events-none absolute left-4 top-3 max-w-[82%] animate-[npIn_0.7s_cubic-bezier(0.16,1,0.3,1)]">
          <div className="mono text-[10px] uppercase tracking-[0.16em] text-cyan">
            {idx + 1} / {CHAPTERS.length}
          </div>
          <div className="font-display text-lg font-bold text-ink sm:text-xl">{t(`ex.ch_${ui.ch}`)}</div>
          <div className="mt-0.5 text-xs leading-snug text-ink-2 sm:text-sm">{t(`ex.cs_${ui.ch}`)}</div>
        </div>
        {(ui.ch === "quake" || ui.ch === "walkquake" || ui.ch === "runquake") && (
          <span className="hud mono pointer-events-none absolute bottom-3 right-3 px-2 py-0.5 text-[10px] text-ink-3">{t("ex.exaggerated", { k: QUAKE_VIS })}</span>
        )}
        <div className="absolute inset-x-0 bottom-0 flex h-[3px] gap-[2px] bg-transparent">
          {CHAPTERS.map((c, i) => {
            const end = i + 1 < CHAPTERS.length ? CHAPTERS[i + 1].t : LOOP;
            return <div key={c.id} className={`h-full ${i <= idx ? "bg-cyan/70" : "bg-rule/70"}`} style={{ flex: end - c.t }} />;
          })}
        </div>
      </div>
      <div className="grid grid-cols-[1fr_auto] items-center gap-3 border-t border-rule px-4 py-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2 font-display text-lg font-bold" style={{ color: VCOL[ui.v] }}>
            <span className="inline-block h-2.5 w-2.5 shrink-0 rounded-full" style={{ background: VCOL[ui.v], boxShadow: `0 0 12px ${VCOL[ui.v]}` }} />
            {t(`ex.v_${ui.v}`)}
          </div>
          <p className="mt-0.5 text-sm leading-snug text-ink-2">{t(`ex.w_${ui.v}`, { s: Math.max(0, SETTLE_S - ui.settle).toFixed(0) })}</p>
        </div>
        <div className="mono grid gap-0.5 text-right text-[11px] text-ink-3">
          <span>
            {t("ex.tilt")} <b className="text-ink">{ui.tilt.toFixed(1)}°</b>
          </span>
          <span>
            {t("ex.motionRms")} <b className="text-ink">{ui.rms.toFixed(3)}</b>
          </span>
        </div>
      </div>
      <div className="relative border-t border-rule">
        <canvas ref={trace} className="block h-14 w-full" aria-hidden="true" />
        <span className="mono pointer-events-none absolute left-2 top-1 text-[10px] text-ink-3">{t("ex.traceLabel")}</span>
      </div>
    </div>
  );
}
