"use client";

import { useEffect, useState, type ReactNode } from "react";
import { AnimatePresence, motion } from "framer-motion";
import EarthquakeLoader from "./EarthquakeLoader";

// A floor on how long the loader stays up even if `load` fires instantly
// (e.g. from cache) — otherwise it can flash for a single frame, which
// reads as a glitch rather than a deliberate loading state.
const MIN_DISPLAY_MS = 600;

// Wraps the whole app once, at the root layout: children are always
// mounted underneath (so everything — including images — is already
// decoding/loading during the minimum display window), the loader just
// sits on top until `window.load` fires, then fades out.
export default function SiteLoadGate({ children }: { children: ReactNode }) {
  const [ready, setReady] = useState(false);

  useEffect(() => {
    const start = Date.now();
    let timer: ReturnType<typeof setTimeout>;

    function reveal() {
      const elapsed = Date.now() - start;
      timer = setTimeout(() => setReady(true), Math.max(0, MIN_DISPLAY_MS - elapsed));
    }

    if (document.readyState === "complete") {
      reveal();
      return () => clearTimeout(timer);
    }

    window.addEventListener("load", reveal, { once: true });
    return () => {
      window.removeEventListener("load", reveal);
      clearTimeout(timer);
    };
  }, []);

  return (
    <>
      <AnimatePresence>
        {!ready && (
          <motion.div key="site-loader" exit={{ opacity: 0 }} transition={{ duration: 0.4 }}>
            <EarthquakeLoader />
          </motion.div>
        )}
      </AnimatePresence>
      {children}
    </>
  );
}
