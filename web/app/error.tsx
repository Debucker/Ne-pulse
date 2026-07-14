"use client";

import { useState } from "react";
import Link from "next/link";
import { ChevronDown, RefreshCw, TriangleAlert } from "lucide-react";

export default function Error({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  const [showDetails, setShowDetails] = useState(false);

  return (
    <div className="flex min-h-screen flex-col items-center justify-center px-4 py-20 text-center">
      <div className="flex h-16 w-16 items-center justify-center rounded-full border border-surface-danger/30 bg-surface-danger/10">
        <TriangleAlert size={28} className="text-surface-danger" />
      </div>

      <h1 className="mt-6 text-2xl font-bold text-surface-text">Something went wrong</h1>
      <p className="mt-3 max-w-md text-surface-muted">
        This page hit an unexpected error. It isn&apos;t something you did — try again, or head
        back home while we look into it.
      </p>

      <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
        <button
          onClick={() => reset()}
          className="flex items-center gap-2 rounded-md bg-surface-accent px-5 py-2.5 font-medium text-white transition hover:bg-blue-600"
        >
          <RefreshCw size={16} />
          Try again
        </button>
        <Link
          href="/"
          className="rounded-md border border-white/10 px-5 py-2.5 font-medium text-surface-text transition hover:bg-slate-900/60"
        >
          Go home
        </Link>
      </div>

      <button
        onClick={() => setShowDetails((v) => !v)}
        className="mt-10 flex items-center gap-1.5 text-xs text-surface-muted transition hover:text-surface-text"
      >
        <ChevronDown size={14} className={`transition-transform ${showDetails ? "rotate-180" : ""}`} />
        {showDetails ? "Hide" : "Show"} technical details
      </button>

      {showDetails && (
        <pre className="mt-4 max-w-2xl overflow-x-auto rounded-lg border border-white/10 bg-slate-950 p-4 text-left text-xs text-surface-muted">
          {error.message || "No error message available."}
          {error.digest ? `\nDigest: ${error.digest}` : ""}
          {error.stack ? `\n\n${error.stack}` : ""}
        </pre>
      )}
    </div>
  );
}
