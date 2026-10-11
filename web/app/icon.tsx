import { ImageResponse } from "next/og";

export const size = { width: 32, height: 32 };
export const contentType = "image/png";

export default function Icon() {
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
          borderRadius: 7,
        }}
      >
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#38e1ff" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
          <path d="M1 13h5l2-5 3 11 3-15 2.5 9H23" />
        </svg>
      </div>
    ),
    { ...size }
  );
}
