// Shared types between page.tsx and LiteMap.tsx.

export interface Region {
  name: string;
  lat: number;
  lng: number;
}

export interface Rupture {
  lat: number;
  lng: number;
  magnitude: number;
  triggeredAt: number; // Date.now() ms — origin for every ticking value derived from it
}
