"use client";

import Link from "next/link";
import { useT } from "@/lib/i18n";

export default function SiteFooter() {
  const { t } = useT();
  return (
    <footer className="mt-24 border-t border-rule" style={{ paddingBottom: "env(safe-area-inset-bottom, 0px)" }}>
      <div className="mx-auto grid max-w-page gap-6 px-4 py-8 text-sm text-ink-3 sm:px-6 md:grid-cols-[1fr_auto]">
        <p className="max-w-[62ch]">{t("footer.disclaimer")}</p>
        <div className="flex flex-wrap gap-x-5 gap-y-2">
          <Link href="/method" className="hover:text-ink">
            {t("footer.method")}
          </Link>
          <Link href="/privacy" className="hover:text-ink">
            {t("footer.privacy")}
          </Link>
          <Link href="/events" className="hover:text-ink">
            {t("footer.log")}
          </Link>
          <a href="https://github.com/Debucker/Ne-pulse" className="hover:text-ink" rel="noopener">
            {t("footer.source")}
          </a>
        </div>
      </div>
    </footer>
  );
}
