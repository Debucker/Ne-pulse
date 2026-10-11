"use client";

// A sphere whose surface deforms with this phone's live band-passed motion.
// Deformation = log10(rms / background): calm (rms at its own background)
// is a smooth sphere; ten times the background is clearly rippled.

import { useEffect, useRef, type MutableRefObject } from "react";
import * as THREE from "three";

const vert = /* glsl */ `
uniform float uTime;
uniform float uAmp;
varying float vD;
varying vec3 vN;
// Cheap 3D value noise.
float h(vec3 p) { return fract(sin(dot(p, vec3(127.1, 311.7, 74.7))) * 43758.5453); }
float n3(vec3 p) {
  vec3 i = floor(p), f = fract(p);
  f = f * f * (3.0 - 2.0 * f);
  return mix(mix(mix(h(i), h(i + vec3(1,0,0)), f.x), mix(h(i + vec3(0,1,0)), h(i + vec3(1,1,0)), f.x), f.y),
             mix(mix(h(i + vec3(0,0,1)), h(i + vec3(1,0,1)), f.x), mix(h(i + vec3(0,1,1)), h(i + vec3(1,1,1)), f.x), f.y), f.z);
}
void main() {
  float d = (n3(normal * 2.2 + uTime * 1.7) - 0.5) * uAmp + sin(normal.y * 14.0 + uTime * 9.0) * uAmp * 0.12;
  vD = d;
  vN = normalize(normalMatrix * normal);
  gl_Position = projectionMatrix * modelViewMatrix * vec4(position + normal * d, 1.0);
}
`;
const frag = /* glsl */ `
uniform vec3 uCold;
uniform vec3 uHot;
uniform float uAmp;
varying float vD;
varying vec3 vN;
void main() {
  float rim = pow(1.0 - abs(vN.z), 2.2);
  vec3 c = mix(uCold, uHot, clamp(uAmp * 1.6 + vD * 2.0, 0.0, 1.0));
  gl_FragColor = vec4(c * (0.18 + rim * 1.2), 1.0);
  #include <colorspace_fragment>
}
`;

export default function SensorOrb({ level, base }: { level: MutableRefObject<number>; base: MutableRefObject<number> }) {
  const host = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = host.current;
    if (!el) return;
    let r: THREE.WebGLRenderer;
    try {
      r = new THREE.WebGLRenderer({ antialias: true, alpha: true });
    } catch {
      return;
    }
    r.setPixelRatio(Math.min(devicePixelRatio || 1, 2));
    el.appendChild(r.domElement);
    Object.assign(r.domElement.style, { display: "block", width: "100%", height: "100%" });
    const css = (n: string, f: string) => new THREE.Color(getComputedStyle(document.documentElement).getPropertyValue(n).trim() || f);
    const scene = new THREE.Scene();
    const cam = new THREE.PerspectiveCamera(35, 1, 0.1, 50);
    cam.position.set(0, 0, 5.2);
    const u = { uTime: { value: 0 }, uAmp: { value: 0 }, uCold: { value: css("--cyan", "#38e1ff") }, uHot: { value: css("--hot", "#ff7a2f") } };
    const orb = new THREE.Mesh(new THREE.IcosahedronGeometry(1.15, 24), new THREE.ShaderMaterial({ uniforms: u, vertexShader: vert, fragmentShader: frag }));
    scene.add(orb);
    const wire = new THREE.Mesh(new THREE.IcosahedronGeometry(1.5, 2), new THREE.MeshBasicMaterial({ color: u.uCold.value, wireframe: true, transparent: true, opacity: 0.08 }));
    scene.add(wire);
    const ro = new ResizeObserver(() => {
      const w = el.clientWidth;
      const hh = el.clientHeight;
      r.setSize(w, hh, false);
      cam.aspect = w / hh;
      cam.updateProjectionMatrix();
    });
    ro.observe(el);
    const clock = new THREE.Clock();
    let raf = 0;
    let amp = 0;
    const frame = () => {
      raf = requestAnimationFrame(frame);
      if (document.hidden) return;
      const dt = Math.min(clock.getDelta(), 0.1);
      u.uTime.value += dt;
      const b = Math.max(base.current, 1e-4);
      const target = Math.min(0.55, Math.max(0, Math.log10(Math.max(level.current, 1e-6) / b)) * 0.35 + 0.02);
      amp += (target - amp) * Math.min(1, dt * 8);
      u.uAmp.value = amp;
      orb.rotation.y += dt * 0.25;
      wire.rotation.y -= dt * 0.08;
      wire.rotation.x += dt * 0.05;
      r.render(scene, cam);
    };
    frame();
    return () => {
      cancelAnimationFrame(raf);
      ro.disconnect();
      r.dispose();
      r.domElement.remove();
    };
  }, [level, base]);
  return <div ref={host} className="absolute inset-0" aria-hidden="true" />;
}
