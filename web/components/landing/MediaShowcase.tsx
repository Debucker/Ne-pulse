import SeismographLoop from "./SeismographLoop";
import NetworkPulse from "./NetworkPulse";
import Reveal from "./Reveal";

export default function MediaShowcase() {
  return (
    <section className="mx-auto max-w-7xl px-4 py-20 sm:px-6 lg:px-8">
      <Reveal className="mb-10 text-center">
        <h2 className="text-3xl font-bold text-surface-text">See it in action</h2>
        <p className="mx-auto mt-3 max-w-2xl text-surface-muted">
          A live look at the sensor network in motion and the real-time seismic trace
          that drives every detection.
        </p>
      </Reveal>

      <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
        <Reveal className="aspect-video">
          <SeismographLoop />
        </Reveal>
        {/* Below sm, NetworkPulse's two node groups wrap onto two rows
            (see NetworkPulse.tsx) instead of one cramped line — that needs
            more height than a fixed 16:9 box allows, which was spilling
            content out past the rounded border. Only lock to video aspect
            from sm+, where the single-row layout comfortably fits it. */}
        <Reveal className="sm:aspect-video" delay={0.1}>
          <NetworkPulse />
        </Reveal>
      </div>
    </section>
  );
}
