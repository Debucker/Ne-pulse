import type { CellWeight } from "./types";

// Roughly Uzbekistan's bounding box — matches the territory CommandMap's
// tile layer actually shows, so injected nodes land on-screen instead of
// scattering into the Caspian or Kazakh steppe past the map's clamped bounds.
const LAT_RANGE: [number, number] = [37.0, 45.6];
const LNG_RANGE: [number, number] = [56.0, 73.2];

export const STRESS_TEST_NODE_COUNT = 320;

function randomInRange([min, max]: [number, number]): number {
  return min + Math.random() * (max - min);
}

/**
 * Generates a batch of synthetic CellWeight nodes shaped exactly like the
 * real telemetry WebSocket payload, so enabling Stress Test mode exercises
 * CommandMap's actual rendering path at scale rather than a mock layer —
 * that's what makes it useful for architectural performance verification.
 */
export function generateStressTestCells(count: number = STRESS_TEST_NODE_COUNT): CellWeight[] {
  return Array.from({ length: count }, (_, i) => ({
    cellId: `stress-${i}`,
    lat: randomInRange(LAT_RANGE),
    lng: randomInRange(LNG_RANGE),
    // A real CellWeight.weight is a device count — always a whole number.
    // Leaving this as a raw float leaked garbage digits (e.g.
    // "21.124234256135325 active readings") straight into the map's
    // tooltips.
    weight: Math.round(1 + Math.random() * 24),
  }));
}
