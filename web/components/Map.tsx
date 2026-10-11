"use client";

import dynamic from "next/dynamic";

// Leaflet touches `window` at import time, so the map is client-only.
const Map = dynamic(() => import("./LiveMap"), {
  ssr: false,
  loading: () => <div className="h-full min-h-[240px] w-full animate-pulse border border-rule bg-panel" />,
});

export default Map;
