import Link from "next/link";
import type { ReactNode } from "react";
import { ActivitySquare, ExternalLink, Github, Globe } from "lucide-react";

const GITHUB_REPO_URL = "https://github.com/Debucker/Ne-pulse.git";
const PRODUCTION_DOMAIN_URL = "https://ne-pulse.com";
const LIVE_APP_URL = "https://ne-pulse.com/dashboard";

export default function DashboardNav() {
  return (
    <header className="flex flex-wrap items-center justify-between gap-3 border-b border-white/10 bg-slate-900/60 px-4 py-3 sm:px-6 lg:px-8">
      <Link href="/" className="flex items-center gap-2 text-surface-text hover:text-surface-accent">
        <ActivitySquare size={20} className="text-surface-accent" />
        <span className="text-sm font-semibold tracking-tight">NE-PULSE</span>
        <span className="hidden text-xs font-normal text-surface-muted sm:inline">Command Dashboard</span>
      </Link>

      <nav className="flex items-center gap-1 text-sm">
        <NavLink href={LIVE_APP_URL} icon={<ExternalLink size={14} />} label="Live App" />
        <NavLink href={PRODUCTION_DOMAIN_URL} icon={<Globe size={14} />} label="ne-pulse.com" />
        <NavLink href={GITHUB_REPO_URL} icon={<Github size={14} />} label="Source" />
      </nav>
    </header>
  );
}

function NavLink({ href, icon, label }: { href: string; icon: ReactNode; label: string }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer noopener"
      className="flex items-center gap-1.5 rounded-md px-3 py-1.5 text-surface-muted transition hover:bg-slate-800/60 hover:text-surface-text"
    >
      {icon}
      {label}
    </a>
  );
}
