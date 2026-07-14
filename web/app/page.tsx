import LandingNav from "@/components/landing/LandingNav";
import Hero from "@/components/landing/Hero";
import WaveTimeline from "@/components/landing/WaveTimeline";
import HoneycombExplainer from "@/components/landing/HoneycombExplainer";
import ScrollJourney from "@/components/landing/ScrollJourney";
import MediaShowcase from "@/components/landing/MediaShowcase";
import LandingFooter from "@/components/landing/LandingFooter";

export default function LandingPage() {
  return (
    <div className="min-h-screen">
      <LandingNav />
      <Hero />
      <WaveTimeline />
      <HoneycombExplainer />
      <ScrollJourney />
      <MediaShowcase />
      <LandingFooter />
    </div>
  );
}
