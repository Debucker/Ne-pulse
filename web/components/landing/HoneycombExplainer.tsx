import type { ReactNode } from "react";
import Image from "next/image";
import { Gauge, Hexagon, Search } from "lucide-react";
import Reveal from "./Reveal";

export default function HoneycombExplainer() {
  return (
    <section className="mx-auto max-w-7xl px-4 py-20 sm:px-6 lg:px-8">
      <div className="grid grid-cols-1 items-center gap-12 lg:grid-cols-2">
        <div>
          <Reveal>
            <h2 className="text-3xl font-bold text-surface-text">The backend, demystified</h2>
            <p className="mt-4 text-surface-muted">
              Checking whether ten different phones are all reporting shaking from the{" "}
              <em>same neighborhood</em>, at the <em>same instant</em>, sounds like it should
              require slow, complicated geographic math. It doesn&apos;t have to.
            </p>
          </Reveal>

          <div className="mt-8 flex flex-col gap-6">
            <Reveal delay={0.1}>
              <Feature
                icon={<Hexagon size={20} />}
                title="Discrete spatial partitioning via Uber H3 indexing"
                body="Instead of running heavy, non-linear geometric boundary computations against raw GPS coordinates, every reading's lat/lng is indexed instantly into a discrete, hierarchical hexagonal cell key via Uber's H3 library (github.com/uber/h3-go) — with an automatic pure-Go equirectangular-grid fallback when a C toolchain isn't available at build time. Two phones in the same cell are, by definition, close together."
              />
            </Reveal>
            <Reveal delay={0.2}>
              <Feature
                icon={<Search size={20} />}
                title="O(1) spatial bucket resolution"
                body="Routing a new reading to its cell is a single hash-map lookup keyed on its H3 index — never an iterative comparison against every other active device's coordinates, the approach that would otherwise force scan time to grow with total device count. Confirming coincidence within that cell then checks only its own small, fixed-capacity buffer of recent readings, bounded regardless of how many devices are online system-wide."
              />
            </Reveal>
            <Reveal delay={0.3}>
              <Feature
                icon={<Gauge size={20} />}
                title="Concurrent, lock-free ingestion engine"
                body="The ingestion pipeline is written in Go: goroutines and channel-based handoff move every reading from the gRPC hot path to background workers with zero application-level mutexes — each worker owns its own consumer state exclusively, coordinating only through a buffered channel and atomic counters, so no incoming reading ever blocks on lock contention."
              />
            </Reveal>
          </div>
        </div>

        <Reveal y={32} delay={0.15} className="overflow-hidden rounded-xl border border-white/10 bg-slate-950">
          <Image
            src="/images/device-network-globe.png"
            alt="A glowing hexagonal grid of connected devices — phones, laptops, and sensors — tiling a globe, representing the honeycomb zone network"
            width={1695}
            height={928}
            className="h-auto w-full"
          />
        </Reveal>
      </div>
    </section>
  );
}

function Feature({ icon, title, body }: { icon: ReactNode; title: string; body: string }) {
  return (
    <div className="flex gap-4">
      <div className="flex h-10 w-10 flex-none items-center justify-center rounded-full border border-white/10 bg-slate-900/60 text-surface-accent">
        {icon}
      </div>
      <div>
        <div className="font-semibold text-surface-text">{title}</div>
        <p className="mt-1 text-sm text-surface-muted">{body}</p>
      </div>
    </div>
  );
}
