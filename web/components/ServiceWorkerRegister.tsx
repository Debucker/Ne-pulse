"use client";

import { useEffect } from "react";

// Registers public/sw.js once the app shell has loaded. Production-only —
// in dev, a service worker caching JS chunks fights with hot reload and
// makes every code change look like it isn't taking effect.
export default function ServiceWorkerRegister() {
  useEffect(() => {
    if (process.env.NODE_ENV !== "production") return;
    if (typeof window === "undefined" || !("serviceWorker" in navigator)) return;

    navigator.serviceWorker.register("/sw.js").catch((err) => {
      console.error("[SW] registration failed:", err);
    });
  }, []);

  return null;
}
