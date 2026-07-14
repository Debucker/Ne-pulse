import Link from "next/link";
import { ActivitySquare, Github } from "lucide-react";

const GITHUB_REPO_URL = "https://github.com/Debucker/Ne-pulse.git";

export default function LandingNav() {
  return (
    <header className="mx-auto flex max-w-7xl items-center justify-between px-4 py-6 sm:px-6 lg:px-8">
      <div className="flex items-center gap-2">
        <ActivitySquare size={22} className="text-surface-accent" />
        <span className="text-base font-semibold tracking-tight text-surface-text">NE-PULSE</span>
      </div>
      <nav className="flex items-center gap-2 text-sm">
        <a
          href={GITHUB_REPO_URL}
          target="_blank"
          rel="noreferrer noopener"
          className="flex items-center gap-1.5 rounded-md px-3 py-1.5 text-surface-muted transition hover:text-surface-text"
        >
          <Github size={16} />
          <span className="hidden sm:inline">Source</span>
        </a>
        <Link
          href="/dashboard"
          className="rounded-md bg-surface-accent px-4 py-1.5 font-medium text-white transition hover:bg-blue-600"
        >
          Open Dashboard
        </Link>
      </nav>
    </header>
  );
}
