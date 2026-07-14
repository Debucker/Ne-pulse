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
                title="Virtual honeycomb zones"
                body="Instead of comparing raw GPS coordinates against each other, we snap every device into one of millions of small hexagonal zones that tile the entire globe — like graph paper for the planet. Two phones in the same hexagon are, by definition, close together."
              />
            </Reveal>
            <Reveal delay={0.2}>
              <Feature
                icon={<Search size={20} />}
                title="One instant lookup, not a search"
                body="Asking 'how many devices are in this hexagon right now?' is a single, instant lookup — not a search through every device's coordinates. That's what makes it fast enough to run in real time, at any scale."
              />
            </Reveal>
            <Reveal delay={0.3}>
              <Feature
                icon={<Gauge size={20} />}
                title="Built for speed from the ground up"
                body="The whole detection pipeline is written in Go and designed to never block on a slow operation — every incoming reading is handled the moment it arrives."
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
