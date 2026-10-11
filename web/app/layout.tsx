import type { Metadata, Viewport } from "next";
import type { ReactNode } from "react";
import { Archivo, IBM_Plex_Mono, IBM_Plex_Sans, Onest } from "next/font/google";
import SiteNav from "@/components/SiteNav";
import SiteFooter from "@/components/SiteFooter";
import { LangProvider } from "@/lib/i18n";
import "./globals.css";

// Archivo has no Cyrillic; Onest (a Cyrillic grotesque of similar build)
// supplies Russian glyphs through the --font-display-cyr fallback.
const display = Archivo({ subsets: ["latin"], axes: ["wdth"], variable: "--font-display", display: "swap", adjustFontFallback: false });
const displayCyr = Onest({ subsets: ["cyrillic"], weight: ["600", "700", "800"], variable: "--font-display-cyr", display: "swap" });
const body = IBM_Plex_Sans({ subsets: ["latin", "cyrillic"], weight: ["400", "500", "600"], variable: "--font-body", display: "swap" });
const mono = IBM_Plex_Mono({ subsets: ["latin", "cyrillic"], weight: ["400", "500"], variable: "--font-mono", display: "swap" });

const SITE_URL = "https://ne-pulse.com";
const DESCRIPTION =
  "An open experiment in distributed seismic sensing: phones lying still around a city measure ground motion, and the network decides together whether it was an earthquake.";

export const metadata: Metadata = {
  metadataBase: new URL(SITE_URL),
  title: { default: "NE-PULSE — a seismic network made of phones", template: "%s · NE-PULSE" },
  description: DESCRIPTION,
  openGraph: {
    type: "website",
    url: SITE_URL,
    siteName: "NE-PULSE",
    title: "NE-PULSE — a seismic network made of phones",
    description: "Lend your phone's motion sensor to a research earthquake network.",
    images: [{ url: "/opengraph-image", width: 1200, height: 630, alt: "NE-PULSE" }],
  },
  twitter: { card: "summary_large_image", images: ["/opengraph-image"] },
  robots: { index: true, follow: true },
};

export const viewport: Viewport = {
  themeColor: "#05080b",
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" className={`${display.variable} ${displayCyr.variable} ${body.variable} ${mono.variable}`}>
      <body className="flex min-h-screen flex-col">
        <LangProvider>
          <SiteNav />
          <div className="flex-1">{children}</div>
          <SiteFooter />
        </LangProvider>
      </body>
    </html>
  );
}
