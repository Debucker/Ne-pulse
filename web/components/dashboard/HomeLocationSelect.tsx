"use client";

import { ChevronDown, MapPin } from "lucide-react";
import { UZBEKISTAN_REGIONS, type Region } from "@/lib/uzbekistanRegions";

interface HomeLocationSelectProps {
  value: Region;
  onChange: (region: Region) => void;
}

/**
 * Lets the user pick which of the 14 Uzbekistan regions represents "where
 * I am" — every downstream physics calculation (Haversine distance, S-wave
 * ETA, local MMI) in useDynamicRupture is computed against this point.
 */
export default function HomeLocationSelect({ value, onChange }: HomeLocationSelectProps) {
  return (
    <label className="flex w-full items-center gap-2 rounded-md border border-surface-border bg-surface-card px-3 py-2 text-sm text-surface-text lg:w-auto">
      <MapPin size={14} className="flex-none text-surface-accent" />
      <span className="hidden flex-none text-xs uppercase tracking-wide text-surface-muted sm:inline">Home:</span>
      <div className="relative min-w-0 flex-1 lg:flex-none">
        <select
          value={value.name}
          onChange={(e) => {
            const region = UZBEKISTAN_REGIONS.find((r) => r.name === e.target.value);
            if (region) onChange(region);
          }}
          className="w-full appearance-none bg-transparent pr-5 text-sm font-medium text-surface-text outline-none lg:w-auto"
        >
          {UZBEKISTAN_REGIONS.map((region) => (
            <option key={region.name} value={region.name} className="bg-surface-card text-surface-text">
              {region.name}
            </option>
          ))}
        </select>
        <ChevronDown size={12} className="pointer-events-none absolute right-0 top-1/2 -translate-y-1/2 text-surface-muted" />
      </div>
    </label>
  );
}
