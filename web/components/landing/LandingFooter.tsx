import Link from "next/link";
import { Github } from "lucide-react";

const GITHUB_REPO_URL = "https://github.com/Debucker/Ne-pulse.git";

export default function LandingFooter() {
  return (
    <footer className="mx-auto flex max-w-7xl flex-wrap items-center justify-between gap-4 border-t border-white/10 px-4 py-8 text-sm text-surface-muted sm:px-6 lg:px-8">
      <span>&copy; 2026 NE-PULSE. Built by D_craft for public safety.</span>
      <div className="flex items-center gap-4">
        <Link href="/dashboard" className="hover:text-surface-text">
          Dashboard
        </Link>
        <a
          href={GITHUB_REPO_URL}
          target="_blank"
          rel="noreferrer noopener"
          className="flex items-center gap-1.5 hover:text-surface-text"
        >
          <Github size={14} />
          Source
        </a>
      </div>
    </footer>
  );
}
