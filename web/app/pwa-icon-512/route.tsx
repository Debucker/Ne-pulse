import { ImageResponse } from "next/og";

// force-static — see pwa-icon-192/route.tsx for why (fixed output,
// prerendered once at build time instead of re-run per request).
export const dynamic = "force-static";

// Same mark as pwa-icon-192, at the larger size some install surfaces
// (splash screens, higher-density home screens) request. The waveform
// is kept well inside the center ~80% "safe zone" — this same PNG is also
// referenced with purpose:"maskable" in manifest.ts, and a maskable icon
// gets cropped to a circle/squircle by the OS, so content too close to the
// edge would otherwise get clipped.
export async function GET() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          background: "#0f172a",
        }}
      >
        <svg
          width="300"
          height="300"
          viewBox="0 0 24 24"
          fill="none"
          stroke="#3b82f6"
          strokeWidth="2.5"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M2 12h4l2 7 4-14 2 7h8" />
        </svg>
      </div>
    ),
    { width: 512, height: 512 },
  );
}
