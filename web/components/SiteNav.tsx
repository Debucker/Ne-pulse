"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";
import { storage } from "@/lib/sensor/client";
import { LangSwitch, useT } from "@/lib/i18n";

export function Mark({ size = 22 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true">
      <path d="M1 13h5l2-5 3 11 3-15 2.5 9H23" fill="none" stroke="var(--trace)" strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
}

export default function SiteNav() {
  const path = usePathname();
  const { t } = useT();
  const [joined, setJoined] = useState(false);
  useEffect(() => setJoined(!!storage.creds()), [path]);
  const links = [
    { href: "/network", label: t("nav.network") },
    { href: "/events", label: t("nav.events") },
    { href: "/how", label: t("nav.learn") },
    { href: "/contributors", label: t("nav.contributors") },
    { href: "/developers", label: "API" },
    { href: "/privacy", label: t("nav.privacy") },
  ];
  return (
    <header className="sticky top-0 z-[1000] border-b border-rule bg-ground/85 backdrop-blur-md" style={{ paddingTop: "env(safe-area-inset-top, 0px)" }}>
      <nav className="mx-auto flex h-14 max-w-page items-center gap-3 px-4 sm:gap-5 sm:px-6">
        <Link href="/" className="flex shrink-0 items-center gap-2" aria-label="NE-PULSE">
          <Mark />
          <span className="whitespace-nowrap font-display text-[15px] font-bold tracking-wide max-[340px]:hidden" style={{ fontStretch: "125%" }}>
            NE-PULSE
          </span>
        </Link>
        <div className="hidden items-center gap-5 md:flex">
          {links.map((l) => (
            <Link key={l.href} href={l.href} className={`text-sm transition-colors ${path.startsWith(l.href) ? "text-ink" : "text-ink-3 hover:text-ink"}`}>
              {l.label}
            </Link>
          ))}
        </div>
        <div className="ml-auto flex items-center gap-2 sm:gap-3">
          <LangSwitch />
          {joined ? (
            <Link href="/device" className="btn btn-ghost !min-h-[36px] !px-3 text-sm">
              <span className="live-dot" /> <span className="hidden sm:inline">{t("nav.yours")}</span>
            </Link>
          ) : (
            <Link href="/join" className="btn btn-primary !min-h-[36px] whitespace-nowrap !px-3.5 text-sm">
              <span className="sm:hidden">{t("nav.joinShort")}</span>
              <span className="hidden sm:inline">{t("nav.join")}</span>
            </Link>
          )}
        </div>
      </nav>
      <div className="flex gap-4 overflow-x-auto border-t border-rule px-4 py-2 [mask-image:linear-gradient(to_right,black_82%,transparent)] md:hidden">
        {links.map((l) => (
          <Link key={l.href} href={l.href} className={`whitespace-nowrap text-sm ${path.startsWith(l.href) ? "text-ink" : "text-ink-3"}`}>
            {l.label}
          </Link>
        ))}
      </div>
    </header>
  );
}
