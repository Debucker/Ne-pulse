import type { Metadata } from "next";
import Link from "next/link";

export const metadata: Metadata = { title: "Not found" };

export default function NotFound() {
  return (
    <main className="mx-auto max-w-[640px] px-4 pt-20 sm:px-6">
      <div className="label">404</div>
      <h1 className="mt-2 font-display text-3xl font-bold">No signal at this address</h1>
      <p className="mt-3 text-ink-2">The page does not exist or has moved.</p>
      <div className="mt-6 flex flex-wrap gap-3">
        <Link href="/" className="btn btn-primary">Home</Link>
        <Link href="/network" className="btn btn-ghost">Live network</Link>
      </div>
    </main>
  );
}
