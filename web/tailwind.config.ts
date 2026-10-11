import type { Config } from "tailwindcss";

// Colours resolve to rgb channel variables so opacity modifiers (bg-ok/10) work.
const v = (name: string) => `rgb(var(--${name}-rgb) / <alpha-value>)`;

const config: Config = {
  content: ["./app/**/*.{ts,tsx}", "./components/**/*.{ts,tsx}", "./lib/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        ground: v("ground"),
        panel: v("panel"),
        raised: v("raised"),
        rule: v("rule"),
        "rule-strong": v("rule-strong"),
        ink: v("ink"),
        "ink-2": v("ink-2"),
        "ink-3": v("ink-3"),
        trace: v("trace"),
        cyan: v("cyan"),
        hot: v("hot"),
        "trace-dim": v("trace-dim"),
        candidate: v("candidate"),
        corroborated: v("corroborated"),
        confirmed: v("confirmed"),
        ok: v("ok"),
        warn: v("warn"),
        bad: v("bad"),
      },
      fontFamily: {
        display: ["var(--font-display)", "var(--font-display-cyr)", "sans-serif"],
        body: ["var(--font-body)", "sans-serif"],
        mono: ["var(--font-mono)", "monospace"],
      },
      maxWidth: { page: "1180px" },
    },
  },
  plugins: [],
};

export default config;
