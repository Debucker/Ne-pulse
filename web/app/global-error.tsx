"use client";

import { useState } from "react";
import Link from "next/link";

// Only fires if the root layout itself throws, so it can't rely on that
// layout's <html>/<body> wrapper or fonts — it has to bring its own, kept
// deliberately minimal so this fallback can't itself fail to render.
export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  const [showDetails, setShowDetails] = useState(false);

  return (
    <html lang="en">
      <body
        style={{
          margin: 0,
          minHeight: "100vh",
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          padding: "5rem 1rem",
          textAlign: "center",
          background: "#020617",
          color: "#f8fafc",
          fontFamily: "ui-sans-serif, system-ui, -apple-system, sans-serif",
        }}
      >
        <div
          style={{
            display: "flex",
            height: 64,
            width: 64,
            alignItems: "center",
            justifyContent: "center",
            borderRadius: "9999px",
            border: "1px solid rgba(239,68,68,0.3)",
            background: "rgba(239,68,68,0.1)",
            fontSize: 28,
          }}
        >
          ⚠
        </div>

        <h1 style={{ marginTop: 24, fontSize: 24, fontWeight: 700 }}>Something went wrong</h1>
        <p style={{ marginTop: 12, maxWidth: 420, color: "#94a3b8" }}>
          The app hit an unexpected error and couldn&apos;t recover on its own. Try reloading —
          if it keeps happening, it&apos;s on us, not you.
        </p>

        <div style={{ marginTop: 32, display: "flex", gap: 12 }}>
          <button
            onClick={() => reset()}
            style={{
              borderRadius: 6,
              background: "#3b82f6",
              color: "#fff",
              fontWeight: 500,
              padding: "10px 20px",
              border: "none",
              cursor: "pointer",
            }}
          >
            Try again
          </button>
          <Link
            href="/"
            style={{
              borderRadius: 6,
              border: "1px solid rgba(255,255,255,0.1)",
              color: "#f8fafc",
              fontWeight: 500,
              padding: "10px 20px",
              textDecoration: "none",
            }}
          >
            Go home
          </Link>
        </div>

        <button
          onClick={() => setShowDetails((v) => !v)}
          style={{
            marginTop: 40,
            background: "none",
            border: "none",
            color: "#94a3b8",
            fontSize: 12,
            cursor: "pointer",
          }}
        >
          {showDetails ? "Hide" : "Show"} technical details
        </button>

        {showDetails && (
          <pre
            style={{
              marginTop: 16,
              maxWidth: 640,
              overflowX: "auto",
              borderRadius: 8,
              border: "1px solid rgba(255,255,255,0.1)",
              background: "#0a0f1e",
              padding: 16,
              textAlign: "left",
              fontSize: 12,
              color: "#94a3b8",
            }}
          >
            {error.message || "No error message available."}
            {error.digest ? `\nDigest: ${error.digest}` : ""}
            {error.stack ? `\n\n${error.stack}` : ""}
          </pre>
        )}
      </body>
    </html>
  );
}
