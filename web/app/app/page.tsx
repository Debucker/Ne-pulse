"use client";

// The Android app: download, install, tap Start, forget. Written for someone
// who has never installed an app from outside a store.

import Link from "next/link";
import { useEffect, useState } from "react";
import { Download, Smartphone, Plug, ShieldCheck, ChevronDown } from "lucide-react";
import { useT } from "@/lib/i18n";

const APK = "https://github.com/Debucker/Ne-pulse/releases/latest/download/ne-pulse.apk";

const TEXT = {
  en: {
    kicker: "NE-PULSE for Android",
    title: "Turn your phone into an earthquake sensor",
    lead: "Install once, tap Start, put it on the charger. It keeps working with the screen off, even after a restart.",
    download: "Download for Android",
    size: "Free · under 1 MB · no account",
    s1: "Download",
    s1d: "Tap the button above. If Chrome asks, tap “Download anyway”.",
    s2: "Install",
    s2d: "Open the file and tap Install. If the phone says it is blocked, tap Settings, turn on “Allow from this source”, go back and tap Install.",
    s3: "Tap Start and forget",
    s3d: "Allow your area once. Put the phone on the charger, face up, on a table. That's all.",
    old: "Have an old Android phone in a drawer? It is the perfect sensor: plug it in, start the app and leave it there for good.",
    tipsTitle: "Phone stops at night? (Xiaomi, Samsung, Huawei)",
    tips: [
      "Xiaomi, Redmi, POCO: Settings › Apps › NE-PULSE › Autostart: on, Battery saver: No restrictions.",
      "Samsung: Settings › Battery › Background usage limits › Never sleeping apps › add NE-PULSE.",
      "Huawei, Honor: Settings › Battery › App launch › NE-PULSE › Manage manually, turn everything on.",
      "Any phone: the app asks once to run in the background. Tap Allow.",
    ],
    iphoneTitle: "iPhone?",
    iphone: "Apple does not allow this kind of app outside the App Store yet. You can still help: open the web page and leave it on overnight.",
    iphoneBtn: "Use the web page",
    privacy: "Only your phone's shaking and its 1 km area are sent. No name, no number, no account.",
    privacyLink: "Privacy",
  },
  ru: {
    kicker: "NE-PULSE для Android",
    title: "Превратите телефон в датчик землетрясений",
    lead: "Установите один раз, нажмите «Старт», поставьте на зарядку. Работает с выключенным экраном и даже после перезагрузки.",
    download: "Скачать для Android",
    size: "Бесплатно · меньше 1 МБ · без регистрации",
    s1: "Скачайте",
    s1d: "Нажмите кнопку выше. Если Chrome спросит — нажмите «Всё равно скачать».",
    s2: "Установите",
    s2d: "Откройте файл и нажмите «Установить». Если телефон пишет, что установка заблокирована, — нажмите «Настройки», включите «Разрешить из этого источника», вернитесь и нажмите «Установить».",
    s3: "Нажмите «Старт» и забудьте",
    s3d: "Один раз разрешите определить район. Поставьте телефон на зарядку экраном вверх на стол. Всё.",
    old: "Есть старый Android в ящике? Это идеальный датчик: подключите к зарядке, запустите приложение и оставьте навсегда.",
    tipsTitle: "Телефон отключает приложение ночью? (Xiaomi, Samsung, Huawei)",
    tips: [
      "Xiaomi, Redmi, POCO: Настройки › Приложения › NE-PULSE › Автозапуск: вкл, Контроль активности: Нет ограничений.",
      "Samsung: Настройки › Батарея › Ограничения в фоне › Приложения, которые никогда не спят › добавьте NE-PULSE.",
      "Huawei, Honor: Настройки › Батарея › Запуск приложений › NE-PULSE › Управлять вручную, включите всё.",
      "Любой телефон: приложение один раз попросит работать в фоне. Нажмите «Разрешить».",
    ],
    iphoneTitle: "iPhone?",
    iphone: "Apple пока не разрешает такие приложения вне App Store. Помочь всё равно можно: откройте веб-страницу и оставьте её включённой на ночь.",
    iphoneBtn: "Открыть веб-версию",
    privacy: "Отправляются только колебания телефона и его район (1 км). Без имени, номера и аккаунта.",
    privacyLink: "Приватность",
  },
};

export default function AppPage() {
  const { lang } = useT();
  const L = TEXT[lang === "ru" ? "ru" : "en"];
  const [os, setOs] = useState<"android" | "ios" | "other">("other");
  const [tips, setTips] = useState(false);
  useEffect(() => {
    const ua = navigator.userAgent;
    setOs(/Android/i.test(ua) ? "android" : /iPhone|iPad|iPod/i.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1) ? "ios" : "other");
  }, []);

  const steps = [
    { icon: Download, t: L.s1, d: L.s1d },
    { icon: ShieldCheck, t: L.s2, d: L.s2d },
    { icon: Plug, t: L.s3, d: L.s3d },
  ];

  return (
    <main className="mx-auto max-w-[640px] px-4 pb-20 pt-10 sm:px-6">
      <div className="label">{L.kicker}</div>
      <h1 className="mt-2 font-display text-[clamp(2.1rem,8vw,3.2rem)] font-extrabold leading-[1.02]" style={{ fontStretch: "108%" }}>
        {L.title}
      </h1>
      <p className="mt-4 text-lg leading-relaxed text-ink-2">{L.lead}</p>

      {os === "ios" ? (
        <div className="glass mt-8 p-5">
          <div className="text-xl font-semibold">{L.iphoneTitle}</div>
          <p className="mt-2 text-lg text-ink-2">{L.iphone}</p>
          <Link href="/join" className="btn btn-primary mt-4 w-full justify-center !min-h-[60px] text-lg">
            {L.iphoneBtn}
          </Link>
        </div>
      ) : (
        <>
          <a href={APK} className="btn btn-primary mt-8 flex w-full items-center justify-center gap-3 !min-h-[68px] text-xl">
            <Download size={24} aria-hidden /> {L.download}
          </a>
          <p className="mt-3 text-center text-sm text-ink-3">{L.size}</p>
        </>
      )}

      <ol className="mt-10 grid gap-4">
        {steps.map((s, i) => (
          <li key={i} className="glass flex gap-4 p-5">
            <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-full bg-cyan/15 text-cyan">
              <s.icon size={24} aria-hidden />
            </div>
            <div>
              <div className="text-xl font-semibold">
                {i + 1}. {s.t}
              </div>
              <p className="mt-1 text-lg leading-relaxed text-ink-2">{s.d}</p>
            </div>
          </li>
        ))}
      </ol>

      <div className="mt-6 flex gap-4 rounded-xl border border-cyan/40 bg-cyan/5 p-5">
        <Smartphone size={26} className="mt-0.5 shrink-0 text-cyan" aria-hidden />
        <p className="text-lg leading-relaxed text-ink">{L.old}</p>
      </div>

      <button className="mt-6 flex w-full items-center justify-between gap-3 rounded-xl border border-rule p-4 text-left text-lg text-ink" onClick={() => setTips(!tips)} aria-expanded={tips}>
        {L.tipsTitle}
        <ChevronDown size={20} className={`shrink-0 transition-transform ${tips ? "rotate-180" : ""}`} aria-hidden />
      </button>
      {tips && (
        <ul className="mt-3 grid gap-3 px-1 text-base leading-relaxed text-ink-2">
          {L.tips.map((x) => (
            <li key={x}>• {x}</li>
          ))}
        </ul>
      )}

      {os !== "ios" && (
        <div className="mt-8 border-t border-rule pt-6">
          <div className="text-lg font-semibold">{L.iphoneTitle}</div>
          <p className="mt-1 text-ink-2">{L.iphone}</p>
          <Link href="/join" className="mt-2 inline-block text-cyan underline underline-offset-4">
            {L.iphoneBtn}
          </Link>
        </div>
      )}

      <p className="mt-10 text-sm text-ink-3">
        {L.privacy}{" "}
        <Link href="/privacy" className="underline underline-offset-4">
          {L.privacyLink}
        </Link>
      </p>
    </main>
  );
}
