import type { Config } from "tailwindcss";

const config: Config = {
  content: ["./app/**/*.{ts,tsx}", "./components/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        // A clean, high-contrast data-platform palette — deep slate/
        // charcoal surfaces with crisp borders and one restrained accent,
        // deliberately not a neon/glow aesthetic.
        surface: {
          bg: "#020617", // slate-950
          panel: "#0f172a", // slate-900
          card: "#111827", // neutral-900-ish, slightly lifted off the panel
          border: "#1e293b", // slate-800
          text: "#f1f5f9", // slate-100
          muted: "#94a3b8", // slate-400
          accent: "#3b82f6", // blue-500 — used sparingly for live/interactive state
          danger: "#ef4444", // red-500 — used for active alerts only
        },
      },
      fontFamily: {
        sans: ["var(--font-inter)", "ui-sans-serif", "system-ui", "sans-serif"],
        mono: ["var(--font-mono)", "ui-monospace", "SFMono-Regular", "monospace"],
      },
      keyframes: {
        pulseDanger: {
          "0%, 100%": { opacity: "1" },
          "50%": { opacity: "0.5" },
        },
        ripple: {
          "0%": { transform: "scale(0.4)", opacity: "0.6" },
          "100%": { transform: "scale(2.6)", opacity: "0" },
        },
        // A crisp on/off hazard-light blink at exactly 1Hz (one full cycle
        // per second) — deliberately a hard step, not an eased fade like
        // pulseDanger, for the survival checklist's "DROP & COVER" state.
        flashRed: {
          "0%, 49%": { opacity: "1" },
          "50%, 100%": { opacity: "0.3" },
        },
      },
      animation: {
        pulseDanger: "pulseDanger 1.2s ease-in-out infinite",
        ripple: "ripple 1.6s ease-out infinite",
        flashRed: "flashRed 1s steps(1, jump-none) infinite",
      },
    },
  },
  plugins: [],
};

export default config;
