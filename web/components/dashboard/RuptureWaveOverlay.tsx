"use client";

import { useCallback, useEffect, useRef } from "react";
import { useMap } from "react-leaflet";

const SVG_NS = "http://www.w3.org/2000/svg";

// Number of concentric rings drawn between the epicenter and the current
// wavefront radius — enough to read as a smooth decay gradient without
// spamming the DOM with elements re-created on every tick.
const RING_COUNT = 5;

// Standard Web Mercator (EPSG:3857) ground-resolution formula: meters per
// pixel at a given latitude/zoom. This is the same approximation used by
// most Leaflet overlays that draw raw SVG/canvas instead of a
// projection-aware Circle layer — accurate enough at the city/country zoom
// levels this map operates at.
function metersPerPixel(lat: number, zoom: number): number {
  return (156543.03392 * Math.cos((lat * Math.PI) / 180)) / Math.pow(2, zoom);
}

interface RuptureWaveOverlayProps {
  epicenterLat: number;
  epicenterLng: number;
  waveRadiusMeters: number;
}

/**
 * A hand-drawn SVG overlay layer for the rupture's expanding S-wave: several
 * concentric rings between the epicenter and the current wavefront radius,
 * fading in opacity and thickening in stroke with distance — a visual model
 * of inverse attenuation, since real seismic shaking intensity decays the
 * farther it travels from the rupture. Each ring also "wobbles" (a CSS
 * scale + opacity oscillation), reading as an active, still-propagating
 * wave rather than a static gradient.
 *
 * Appended directly to the Leaflet map container (the same pattern LiteMap's
 * GridOverlay uses for its own canvas) rather than rendered as react-leaflet
 * elements, since react-leaflet has no built-in primitive for a
 * physically-decaying multi-ring wavefront.
 */
export default function RuptureWaveOverlay({ epicenterLat, epicenterLng, waveRadiusMeters }: RuptureWaveOverlayProps) {
  const map = useMap();
  const svgRef = useRef<SVGSVGElement | null>(null);
  // Redraw reads from this ref (not from closed-over props) so the pan/zoom
  // event listeners registered once at mount always see the latest values.
  const stateRef = useRef({ epicenterLat, epicenterLng, waveRadiusMeters });
  stateRef.current = { epicenterLat, epicenterLng, waveRadiusMeters };

  const draw = useCallback(() => {
    const svg = svgRef.current;
    if (!svg) return;
    const { epicenterLat, epicenterLng, waveRadiusMeters } = stateRef.current;

    const size = map.getSize();
    svg.setAttribute("width", String(size.x));
    svg.setAttribute("height", String(size.y));
    while (svg.firstChild) svg.removeChild(svg.firstChild);

    const center = map.latLngToContainerPoint([epicenterLat, epicenterLng]);
    const mpp = metersPerPixel(epicenterLat, map.getZoom());
    const pixelRadius = waveRadiusMeters / mpp;
    if (!(pixelRadius > 0)) return;

    for (let i = 1; i <= RING_COUNT; i++) {
      const frac = i / RING_COUNT;
      const r = pixelRadius * frac;
      if (r < 1) continue;

      // Inverse-attenuation curve: near the epicenter (small frac) stays
      // bright/opaque and thin; farther out (frac -> 1) fades and thickens
      // — the same "closer = more intense" relationship the app's own MMI
      // formula models numerically (magnitude decaying with log distance).
      const opacity = Math.max(0.05, 0.85 * Math.pow(1 - frac, 1.4));
      const strokeWidth = 1.5 + frac * 5;

      const circle = document.createElementNS(SVG_NS, "circle");
      circle.setAttribute("cx", String(center.x));
      circle.setAttribute("cy", String(center.y));
      circle.setAttribute("r", String(r));
      circle.setAttribute("fill", "none");
      circle.setAttribute("stroke", "#ef4444");
      circle.setAttribute("stroke-width", String(strokeWidth));
      circle.setAttribute("class", "rupture-wobble-ring");
      circle.style.setProperty("--ring-opacity", opacity.toFixed(3));
      circle.style.animationDelay = `${i * 0.12}s`;
      svg.appendChild(circle);
    }
  }, [map]);

  // Create the SVG element and pan/zoom listeners once per mount (i.e. once
  // per active rupture, since the parent only mounts this component while
  // one exists) rather than on every tick — `elapsedSeconds` changes every
  // 100ms while a rupture is active, and recreating the DOM node/listeners
  // that often would be wasteful and could flicker.
  useEffect(() => {
    const svg = document.createElementNS(SVG_NS, "svg");
    svg.setAttribute("class", "rupture-wave-svg");
    Object.assign(svg.style, {
      position: "absolute",
      top: "0",
      left: "0",
      pointerEvents: "none",
      zIndex: "450",
    });
    map.getContainer().appendChild(svg);
    svgRef.current = svg;
    draw();
    map.on("move zoom resize", draw);
    return () => {
      map.off("move zoom resize", draw);
      svg.remove();
      svgRef.current = null;
    };
  }, [map, draw]);

  // Cheap redraw on every radius tick — reuses the existing SVG element and
  // listeners set up above instead of tearing them down.
  useEffect(() => {
    draw();
  }, [epicenterLat, epicenterLng, waveRadiusMeters, draw]);

  return null;
}
