"use client";

export default function Error({ reset }: { error: Error; reset: () => void }) {
  return (
    <main className="mx-auto max-w-[640px] px-4 pt-20 sm:px-6">
      <div className="label">Error</div>
      <h1 className="mt-2 font-display text-3xl font-bold">Something broke on this page</h1>
      <p className="mt-3 text-ink-2">If you were contributing, your phone stopped streaming. Reload to continue.</p>
      <button className="btn btn-primary mt-6" onClick={reset}>Try again</button>
    </main>
  );
}
