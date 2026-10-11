// Run with: node --test lib/sensor/sensor.test.ts  (Node >= 23 strips types)
import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { Grid } from "./grid.ts";
import { sha256, toHex, leadingZeroBits } from "./sha256.ts";

// Same golden cells as internal/geo/geo_test.go TestGridGolden.
test("grid matches Go implementation", () => {
  const g = new Grid(1);
  const golden: [number, number, number, number][] = [
    [41.2995, 69.2401, 14599, 20821],
    [39.627, 66.975, 14413, 21152],
    [-33.8688, 151.2093, 6241, 30579],
    [0, 0, 10007, 20015],
  ];
  for (const [lat, lng, row, col] of golden) {
    assert.deepEqual(g.cellOf(lat, lng), { row, col }, `${lat},${lng}`);
  }
});

test("grid centre maps back to its cell", () => {
  const g = new Grid(1);
  for (let i = 0; i < 2000; i++) {
    const lat = Math.random() * 150 - 75;
    const lng = Math.random() * 360 - 180;
    const c = g.cellOf(lat, lng);
    const ctr = g.center(c);
    assert.deepEqual(g.cellOf(ctr.lat, ctr.lng), c);
  }
});

test("sha256 matches node:crypto", () => {
  for (const s of ["", "abc", "a".repeat(55), "a".repeat(56), "a".repeat(64), "1234567890.18.nonce.sig:98765"]) {
    assert.equal(toHex(sha256(s)), createHash("sha256").update(s, "latin1").digest("hex"), JSON.stringify(s));
  }
});

test("leading zero bits", () => {
  assert.equal(leadingZeroBits(new Uint8Array([0, 0, 0x10, 0xff])), 19);
  assert.equal(leadingZeroBits(new Uint8Array([0x80])), 0);
});
