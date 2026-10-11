"use client";

import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { STRINGS, type Lang } from "./strings";

const K = "nepulse.lang";

interface Ctx {
  lang: Lang;
  setLang: (l: Lang) => void;
  t: (key: string, vars?: Record<string, string | number>) => string;
}

const LangCtx = createContext<Ctx>({ lang: "en", setLang: () => {}, t: (k) => k });

function lookup(lang: Lang, key: string): string | undefined {
  let cur: unknown = STRINGS[lang];
  for (const part of key.split(".")) {
    if (cur && typeof cur === "object" && part in (cur as Record<string, unknown>)) cur = (cur as Record<string, unknown>)[part];
    else return undefined;
  }
  return typeof cur === "string" ? cur : undefined;
}

export function LangProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>("en");
  useEffect(() => {
    let l: Lang | null = null;
    try {
      l = localStorage.getItem(K) as Lang | null;
    } catch {}
    if (l !== "en" && l !== "ru") l = navigator.language?.toLowerCase().startsWith("ru") || navigator.language?.toLowerCase().startsWith("uz") ? "ru" : "en";
    setLangState(l);
  }, []);
  useEffect(() => {
    document.documentElement.lang = lang;
  }, [lang]);
  const setLang = useCallback((l: Lang) => {
    setLangState(l);
    try {
      localStorage.setItem(K, l);
    } catch {}
  }, []);
  const t = useCallback(
    (key: string, vars?: Record<string, string | number>) => {
      let s = lookup(lang, key) ?? lookup("en", key) ?? key;
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [lang],
  );
  return <LangCtx.Provider value={{ lang, setLang, t }}>{children}</LangCtx.Provider>;
}

export function useT() {
  return useContext(LangCtx);
}

export function LangSwitch({ className = "" }: { className?: string }) {
  const { lang, setLang } = useT();
  return (
    <div className={`mono flex overflow-hidden rounded-[3px] border border-rule-strong text-[11px] ${className}`} role="group" aria-label="Language">
      {(["en", "ru"] as Lang[]).map((l) => (
        <button
          key={l}
          onClick={() => setLang(l)}
          aria-pressed={lang === l}
          className={`px-2 py-1.5 uppercase tracking-widest transition-colors ${lang === l ? "bg-ink text-ground" : "text-ink-3 hover:text-ink"}`}
        >
          {l}
        </button>
      ))}
    </div>
  );
}
