import type { Metadata } from "next";
import Link from "next/link";
import { ArrowRight, MapPinOff } from "lucide-react";

export const metadata: Metadata = {
  title: "Page not found",
};

export default function NotFound() {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center px-4 py-20 text-center">
      <div className="flex h-16 w-16 items-center justify-center rounded-full border border-white/10 bg-slate-900/60">
        <MapPinOff size={28} className="text-surface-accent" />
      </div>

      <h1 className="mt-6 text-2xl font-bold text-surface-text">Page not found</h1>
      <p className="mt-3 max-w-md text-surface-muted">
        This page doesn&apos;t exist, or it may have moved. Check the link you followed, or head
        back somewhere that does.
      </p>

      <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
        <Link
          href="/"
          className="flex items-center gap-2 rounded-md bg-surface-accent px-5 py-2.5 font-medium text-white transition hover:bg-blue-600"
        >
          Go home
          <ArrowRight size={16} />
        </Link>
        <Link
          href="/dashboard"
          className="rounded-md border border-white/10 px-5 py-2.5 font-medium text-surface-text transition hover:bg-slate-900/60"
        >
          Open Dashboard
        </Link>
      </div>
    </div>
  );
}
