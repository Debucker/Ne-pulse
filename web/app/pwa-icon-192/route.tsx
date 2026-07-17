import { ImageResponse } from "next/og";

// Same dark-square + pulse-waveform mark as app/icon.tsx, rendered at a
// PWA-installability-qualifying size (Chrome/Android's "Add to Home
// Screen" requires >=192x192) — a dynamically-rendered PNG route, not a
// static binary asset. Unlike icon.tsx's special file convention (which
// Next.js auto-prerenders), a plain Route Handler defaults to on-demand
// rendering — force-static makes it build once and serve as a static
// asset from then on, since the output never depends on the request.
export const dynamic = "force-static";
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
          width="128"
          height="128"
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
    { width: 192, height: 192 },
  );
}
