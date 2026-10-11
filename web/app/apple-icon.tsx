import { ImageResponse } from "next/og";

export const size = { width: 180, height: 180 };
export const contentType = "image/png";

export default function AppleIcon() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          background: "#05080b",
        }}
      >
        <svg width="108" height="108" viewBox="0 0 24 24" fill="none" stroke="#38e1ff" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
          <path d="M1 13h5l2-5 3 11 3-15 2.5 9H23" />
        </svg>
      </div>
    ),
    { ...size }
  );
}
