import LandingNav from "@/components/landing/LandingNav";
import Hero from "@/components/landing/Hero";
import WaveTimeline from "@/components/landing/WaveTimeline";
import HoneycombExplainer from "@/components/landing/HoneycombExplainer";
import ScrollJourney from "@/components/landing/ScrollJourney";
import MediaShowcase from "@/components/landing/MediaShowcase";
import LandingFooter from "@/components/landing/LandingFooter";
import { DemoRuptureProvider } from "@/components/landing/DemoRuptureProvider";

export default function LandingPage() {
  return (
    <div className="min-h-screen">
      <LandingNav />
      <Hero />
      {/* WaveTimeline's "Simulate a rupture" button and MediaShowcase's
          seismic trace chart live in separate sections with no shared
          parent state of their own — this provider is the seam between
          them (see DemoRuptureProvider.tsx). */}
      <DemoRuptureProvider>
        <WaveTimeline />
        <HoneycombExplainer />
        <ScrollJourney />
        <MediaShowcase />
      </DemoRuptureProvider>
      <LandingFooter />
    </div>
  );
}
