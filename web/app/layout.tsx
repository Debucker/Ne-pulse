import type { Metadata, Viewport } from "next";
import type { ReactNode } from "react";
import { Inter } from "next/font/google";
import SiteLoadGate from "@/components/SiteLoadGate";
import "./globals.css";

const inter = Inter({
  subsets: ["latin"],
  variable: "--font-inter",
  display: "swap",
});

const SITE_URL = "https://ne-pulse.com";
const SITE_DESCRIPTION =
  "NE-PULSE turns everyday phones and low-cost sensors into a decentralized earthquake early-warning network, giving cities precious seconds before destructive shaking arrives.";

export const metadata: Metadata = {
  metadataBase: new URL(SITE_URL),
  title: {
    default: "NE-PULSE — Real-Time Earthquake Early Warning",
    template: "%s · NE-PULSE",
  },
  description: SITE_DESCRIPTION,
  keywords: [
    "earthquake early warning",
    "seismic detection network",
    "decentralized sensor network",
    "disaster alert system",
    "structural motion detection",
    "P-wave S-wave detection",
    "Uzbekistan earthquake monitoring",
  ],
  authors: [{ name: "D_craft" }],
  creator: "D_craft",
  category: "technology",
  alternates: { canonical: "/" },
  openGraph: {
    type: "website",
    url: SITE_URL,
    siteName: "NE-PULSE",
    title: "NE-PULSE — Real-Time Earthquake Early Warning",
    description: "Every phone is a sensor. Every second is a life.",
    images: [{ url: "/opengraph-image", width: 1200, height: 630, alt: "NE-PULSE" }],
  },
  twitter: {
    card: "summary_large_image",
    title: "NE-PULSE — Real-Time Earthquake Early Warning",
    description: "Every phone is a sensor. Every second is a life.",
    images: ["/opengraph-image"],
  },
  robots: {
    index: true,
    follow: true,
    googleBot: { index: true, follow: true },
  },
};

export const viewport: Viewport = {
  themeColor: "#020617",
  width: "device-width",
  initialScale: 1,
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" className={inter.variable}>
      <body className="bg-surface-bg font-sans text-surface-text antialiased">
        <SiteLoadGate>{children}</SiteLoadGate>
      </body>
    </html>
  );
}
