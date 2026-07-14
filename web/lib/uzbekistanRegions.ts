// The 14 top-level Uzbekistan administrative divisions (12 regions, the
// Republic of Karakalpakstan, and Tashkent city), each represented by its
// administrative center. Mirrors internal/solver/cities.go on the Go side
// — keep the two lists in sync if either changes.

export interface Region {
  name: string;
  lat: number;
  lng: number;
}

export const UZBEKISTAN_REGIONS: Region[] = [
  { name: "Tashkent", lat: 41.2995, lng: 69.2401 },
  { name: "Nurafshon", lat: 41.0167, lng: 69.3417 },
  { name: "Nukus", lat: 42.4531, lng: 59.6103 },
  { name: "Andijan", lat: 40.7821, lng: 72.3442 },
  { name: "Bukhara", lat: 39.7747, lng: 64.4286 },
  { name: "Fergana", lat: 40.3894, lng: 71.7978 },
  { name: "Jizzakh", lat: 40.1158, lng: 67.8422 },
  { name: "Namangan", lat: 40.9983, lng: 71.6726 },
  { name: "Navoiy", lat: 40.0844, lng: 65.3792 },
  { name: "Qarshi", lat: 38.8606, lng: 65.7891 },
  { name: "Samarkand", lat: 39.6542, lng: 66.9597 },
  { name: "Gulistan", lat: 40.4897, lng: 68.7842 },
  { name: "Termez", lat: 37.2242, lng: 67.2783 },
  { name: "Urgench", lat: 41.5506, lng: 60.6317 },
];

export const DEFAULT_HOME_REGION = UZBEKISTAN_REGIONS[0];
