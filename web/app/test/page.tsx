"use client";

// "How sensitive is your phone?": the one-minute way to take part. The phone
// lies still for 30 s, the person taps five answers, and gets a result card
// to share. The raw 30 s and the answers go to the research.

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { Share2, RotateCcw, Smartphone } from "lucide-react";
import { API_URL, backendReady } from "@/lib/config";
import { inAppBrowser, isIOS, requestMotionPermission } from "@/lib/sensor/client";
import { useT } from "@/lib/i18n";

const SECONDS = 32;

const TEXT = {
  en: {
    kicker: "Earthquake research · 1 minute",
    title: "How sensitive is your phone?",
    lead: "Your phone has a motion sensor. Let's measure how small a shake it can feel, and use it for real earthquake research.",
    start: "Start the test",
    noSignup: "No sign-up · nothing to install · works on iPhone and Android",
    placeT: "Put the phone flat on a table",
    placeD: "Screen up. Don't touch it until the circle is full.",
    moved: "It moved, starting again…",
    keepStill: "Keep still…",
    qTitle: "5 quick questions",
    q: {
      night: "Where is your phone at night?",
      floor: "What floor do you live on?",
      overnight: "Would you leave your phone on a charger overnight to help detect earthquakes?",
      oldphone: "Do you have an old phone you no longer use?",
      felt: "Have you ever felt an earthquake?",
    },
    a: {
      night: { bedside: "Bedside table", bed: "In bed", desk: "Desk / shelf", floor: "Floor", pillow: "Under the pillow", other: "Other" },
      floor: { "0": "Ground / house", "1": "1st", "2-3": "2nd–3rd", "4-9": "4th–9th", "10+": "10th+" },
      overnight: { yes: "Yes", maybe: "Maybe", no: "No" },
      oldphone: { android: "Yes, Android", iphone: "Yes, iPhone", no: "No" },
      felt: { tashkent: "Yes, in Tashkent", elsewhere: "Yes, elsewhere", no: "No" },
    } as Record<string, Record<string, string>>,
    send: "See my result",
    sending: "Sending…",
    resultKicker: "Your phone",
    better: "More sensitive than {p}% of phones tested",
    first: "One of the first phones tested",
    feels: "It can feel shaking down to intensity {m}",
    mmiWords: ["", "not felt by people", "not felt by people", "like a truck passing outside", "felt indoors by many", "felt by everyone"],
    tested: "{n} phones tested so far",
    thanks: "Thank you! Your answers and your phone's 30 seconds are now part of the research.",
    notMeasured: "Your phone moved too much to measure, but your answers count. Try again on a table?",
    share: "Challenge your friends",
    shareText: "My phone is more sensitive than {p}% of phones in the NE-PULSE earthquake study. Test yours in 1 minute:",
    shareTextPlain: "I tested my phone for the NE-PULSE earthquake study. Test yours in 1 minute:",
    copied: "Link copied",
    again: "Test again",
    more: "Want to help every night? Turn an Android phone into a permanent sensor",
    errNoSensor: "No motion data from this browser. Open the link in Safari (iPhone) or Chrome (Android) on a phone.",
    errInApp: "Open this link in Safari or Chrome: tap ⋯ and choose “Open in browser”.",
    errDenied: "Motion access was refused. Close this tab, open the link again and tap Allow.",
    errSend: "Could not send. Check the internet and tap again.",
    privacy: "Saved without your name or number: the 30-second motion recording, the phone type and your answers.",
  },
  ru: {
    kicker: "Исследование землетрясений · 1 минута",
    title: "Насколько чувствителен твой телефон?",
    lead: "В телефоне есть датчик движения. Измерим, какой маленький толчок он чувствует, — и используем это в настоящем исследовании землетрясений.",
    start: "Начать тест",
    noSignup: "Без регистрации · ничего не устанавливать · iPhone и Android",
    placeT: "Положи телефон ровно на стол",
    placeD: "Экраном вверх. Не трогай, пока круг не заполнится.",
    moved: "Телефон сдвинулся, начинаем заново…",
    keepStill: "Не трогай…",
    qTitle: "5 быстрых вопросов",
    q: {
      night: "Где твой телефон ночью?",
      floor: "На каком этаже ты живёшь?",
      overnight: "Оставил(а) бы ты телефон на зарядке на ночь, чтобы помочь засекать землетрясения?",
      oldphone: "Есть ли у тебя старый телефон, которым ты не пользуешься?",
      felt: "Ты когда-нибудь чувствовал(а) землетрясение?",
    },
    a: {
      night: { bedside: "Тумбочка", bed: "В кровати", desk: "Стол / полка", floor: "Пол", pillow: "Под подушкой", other: "Другое" },
      floor: { "0": "Первый / дом", "1": "2-й", "2-3": "3–4-й", "4-9": "5–10-й", "10+": "Выше 10-го" },
      overnight: { yes: "Да", maybe: "Может быть", no: "Нет" },
      oldphone: { android: "Да, Android", iphone: "Да, iPhone", no: "Нет" },
      felt: { tashkent: "Да, в Ташкенте", elsewhere: "Да, в другом месте", no: "Нет" },
    } as Record<string, Record<string, string>>,
    send: "Узнать результат",
    sending: "Отправляем…",
    resultKicker: "Твой телефон",
    better: "Чувствительнее, чем {p}% проверенных телефонов",
    first: "Один из первых проверенных телефонов",
    feels: "Чувствует толчки от {m} баллов",
    mmiWords: ["", "люди не чувствуют", "люди не чувствуют", "как проезжающий грузовик", "многие чувствуют в доме", "чувствуют все"],
    tested: "Проверено телефонов: {n}",
    thanks: "Спасибо! Твои ответы и 30 секунд с датчика телефона теперь часть исследования.",
    notMeasured: "Телефон слишком двигался, измерить не получилось, но ответы засчитаны. Попробуешь ещё раз на столе?",
    share: "Брось вызов друзьям",
    shareText: "Мой телефон чувствительнее {p}% телефонов в исследовании землетрясений NE-PULSE. Проверь свой за минуту:",
    shareTextPlain: "Я проверил(а) свой телефон для исследования землетрясений NE-PULSE. Проверь свой за минуту:",
    copied: "Ссылка скопирована",
    again: "Ещё раз",
    more: "Хочешь помогать каждую ночь? Преврати Android в постоянный датчик",
    errNoSensor: "Браузер не даёт данные движения. Открой ссылку в Safari (iPhone) или Chrome (Android) на телефоне.",
    errInApp: "Открой ссылку в Safari или Chrome: нажми ⋯ и выбери «Открыть в браузере».",
    errDenied: "Доступ к датчику запрещён. Закрой вкладку, открой ссылку снова и нажми «Разрешить».",
    errSend: "Не удалось отправить. Проверь интернет и нажми ещё раз.",
    privacy: "Сохраняется без имени и номера: 30 секунд записи датчика, тип телефона и ответы.",
  },
};

type Phase = "intro" | "measure" | "questions" | "result";
type Result = { measured: boolean; betterThanPct: number; tested: number; detMmi: number };

function model(): string {
  const ua = navigator.userAgent;
  const m = ua.match(/Android [\d.]+; ([^;)]+)/);
  if (m) return m[1].replace(/ Build\/.*/, "").trim();
  if (/iPhone/.test(ua)) return "iPhone";
  if (/iPad/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1)) return "iPad";
  return "";
}

export default function PhoneTest() {
  const { lang } = useT();
  const L = TEXT[lang === "ru" ? "ru" : "en"];
  const [phase, setPhase] = useState<Phase>("intro");
  const [err, setErr] = useState<string | null>(null);
  const [left, setLeft] = useState(SECONDS);
  const [moved, setMoved] = useState(false);
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [res, setRes] = useState<Result | null>(null);
  const [copied, setCopied] = useState(false);
  const rec = useRef({ t0: 0, dt: [] as number[], x: [] as number[], y: [] as number[], z: [] as number[] });
  const stopRef = useRef<() => void>(() => {});

  useEffect(() => () => stopRef.current(), []);

  async function start() {
    setErr(null);
    const perm = await requestMotionPermission(); // first thing inside the tap (iPhone)
    if (perm === "denied" && isIOS()) return setErr(L.errDenied);
    if (perm === "unsupported") return setErr(inAppBrowser() ? L.errInApp : L.errNoSensor);
    measure();
  }

  function measure() {
    setPhase("measure");
    setMoved(false);
    setLeft(SECONDS);
    const r = rec.current;
    r.t0 = 0;
    r.dt = [];
    r.x = [];
    r.y = [];
    r.z = [];
    let restarts = 0;
    let got = false;
    // Movement check: a fast-reacting average of how much the reading jumps.
    let prev: number[] | null = null;
    let jump = 0;
    const onMotion = (e: DeviceMotionEvent) => {
      const a = e.accelerationIncludingGravity;
      if (!a || a.x == null || a.y == null || a.z == null) return;
      got = true;
      const now = performance.now();
      if (!r.t0) r.t0 = now;
      const v = [a.x, a.y, a.z];
      if (prev) {
        const d = Math.hypot(v[0] - prev[0], v[1] - prev[1], v[2] - prev[2]);
        jump = jump * 0.9 + d * 0.1;
      }
      prev = v;
      // Picked up or bumped hard during the first 20 s: start over (3 times at most).
      if (jump > 0.6 && now - r.t0 > 1000 && now - r.t0 < 20000 && restarts < 3) {
        restarts++;
        r.t0 = now;
        r.dt = [];
        r.x = [];
        r.y = [];
        r.z = [];
        jump = 0;
        setMoved(true);
        return;
      }
      r.dt.push(Math.round((now - r.t0) * 10) / 10);
      r.x.push(Math.round(a.x * 1000) / 1000);
      r.y.push(Math.round(a.y * 1000) / 1000);
      r.z.push(Math.round(a.z * 1000) / 1000);
    };
    window.addEventListener("devicemotion", onMotion);
    const timer = setInterval(() => {
      if (!r.t0) return;
      const l = Math.max(0, SECONDS - Math.floor((performance.now() - r.t0) / 1000));
      setLeft(l);
      if (l === 0) {
        stop();
        setPhase("questions");
      }
    }, 250);
    const noData = setTimeout(() => {
      if (!got) {
        stop();
        setPhase("intro");
        setErr(inAppBrowser() ? L.errInApp : L.errNoSensor);
      }
    }, 4000);
    function stop() {
      window.removeEventListener("devicemotion", onMotion);
      clearInterval(timer);
      clearTimeout(noData);
    }
    stopRef.current = stop;
    // Hide the "moved" note again after a moment.
    setTimeout(() => setMoved(false), 2500);
  }

  async function send() {
    setBusy(true);
    setErr(null);
    try {
      await backendReady;
      const r = rec.current;
      const cohort = new URLSearchParams(window.location.search).get("class") ?? "";
      const resp = await fetch(API_URL + "/api/v1/survey", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          platform: /Android/i.test(navigator.userAgent) ? "android" : isIOS() ? "ios" : "other",
          model: model(),
          lang,
          cohort,
          t0: Date.now() - (r.dt[r.dt.length - 1] ?? 0),
          dt: r.dt,
          x: r.x,
          y: r.y,
          z: r.z,
          answers,
        }),
      });
      if (!resp.ok) throw new Error();
      setRes(await resp.json());
      setPhase("result");
    } catch {
      setErr(L.errSend);
    } finally {
      setBusy(false);
    }
  }

  async function share() {
    const url = window.location.origin + "/test";
    const text = res?.measured && res.tested > 1 ? L.shareText.replace("{p}", String(res.betterThanPct)) : L.shareTextPlain;
    try {
      if (navigator.share) return await navigator.share({ text, url });
    } catch {
      return;
    }
    await navigator.clipboard?.writeText(`${text} ${url}`).catch(() => {});
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }

  const qKeys = ["night", "floor", "overnight", "oldphone", "felt"] as const;
  const allAnswered = qKeys.every((k) => answers[k]);
  const pct = ((SECONDS - left) / SECONDS) * 100;
  const mmi = res ? Math.max(1, Math.min(5, Math.round(res.detMmi))) : 0;
  const roman = ["", "I", "II", "III", "IV", "V"][mmi];

  return (
    <main className="mx-auto max-w-[560px] px-4 pb-20 pt-8 sm:px-6">
      {phase === "intro" && (
        <>
          <div className="label">{L.kicker}</div>
          <h1 className="mt-2 font-display text-[clamp(2.2rem,9vw,3.4rem)] font-extrabold leading-[1.02]" style={{ fontStretch: "106%" }}>
            {L.title}
          </h1>
          <p className="mt-4 text-lg leading-relaxed text-ink-2">{L.lead}</p>
          <button className="btn btn-primary mt-8 flex w-full justify-center !min-h-[68px] text-xl" onClick={start}>
            {L.start}
          </button>
          <p className="mt-3 text-center text-sm text-ink-3">{L.noSignup}</p>
          {err && <p className="mt-5 rounded-xl border border-warn/50 bg-warn/5 p-4 text-base text-ink">{err}</p>}
        </>
      )}

      {phase === "measure" && (
        <div className="flex min-h-[70vh] flex-col items-center justify-center text-center">
          <h1 className="text-2xl font-semibold">{L.placeT}</h1>
          <p className="mt-2 text-lg text-ink-2">{L.placeD}</p>
          <div
            className="relative mt-10 flex h-56 w-56 items-center justify-center rounded-full"
            style={{ background: `conic-gradient(var(--cyan) ${pct}%, rgba(255,255,255,0.08) 0)` }}
          >
            <div className="flex h-48 w-48 items-center justify-center rounded-full bg-ground">
              <span className="mono text-6xl font-semibold text-ink">{left}</span>
            </div>
          </div>
          <p className={`mt-8 text-lg ${moved ? "text-warn" : "text-ink-3"}`}>{moved ? L.moved : L.keepStill}</p>
        </div>
      )}

      {phase === "questions" && (
        <>
          <h1 className="text-3xl font-bold">{L.qTitle}</h1>
          <div className="mt-6 grid gap-7">
            {qKeys.map((k) => (
              <fieldset key={k}>
                <legend className="mb-3 text-lg font-medium text-ink">{L.q[k]}</legend>
                <div className="flex flex-wrap gap-2">
                  {Object.entries(L.a[k]).map(([v, label]) => (
                    <button
                      key={v}
                      type="button"
                      onClick={() => setAnswers((a) => ({ ...a, [k]: v }))}
                      className={`min-h-[48px] rounded-full border px-4 text-base ${
                        answers[k] === v ? "border-cyan bg-cyan/15 text-ink" : "border-rule-strong text-ink-2"
                      }`}
                    >
                      {label}
                    </button>
                  ))}
                </div>
              </fieldset>
            ))}
          </div>
          <button className="btn btn-primary mt-8 flex w-full justify-center !min-h-[64px] text-xl" disabled={!allAnswered || busy} onClick={send}>
            {busy ? L.sending : L.send}
          </button>
          {err && <p className="mt-4 text-base text-bad">{err}</p>}
          <p className="mt-4 text-sm text-ink-3">{L.privacy}</p>
        </>
      )}

      {phase === "result" && res && (
        <>
          <div className="glass mt-4 p-6 text-center">
            <div className="label">{L.resultKicker}</div>
            {res.measured ? (
              <>
                <div className="mt-3 font-display text-[clamp(2rem,8vw,2.8rem)] font-extrabold leading-tight text-cyan">
                  {res.tested > 1 ? L.better.replace("{p}", String(res.betterThanPct)) : L.first}
                </div>
                <p className="mt-4 text-lg text-ink">
                  {L.feels.replace("{m}", roman)} ({L.mmiWords[mmi]})
                </p>
              </>
            ) : (
              <p className="mt-3 text-lg text-ink">{L.notMeasured}</p>
            )}
            <p className="mono mt-4 text-sm text-ink-3">{L.tested.replace("{n}", String(res.tested))}</p>
          </div>
          <p className="mt-5 text-center text-base text-ink-2">{L.thanks}</p>
          <button className="btn btn-primary mt-6 flex w-full items-center justify-center gap-2 !min-h-[64px] text-xl" onClick={share}>
            <Share2 size={22} aria-hidden /> {copied ? L.copied : L.share}
          </button>
          <button className="btn mt-3 flex w-full items-center justify-center gap-2 !min-h-[52px]" onClick={() => { setAnswers({}); setPhase("intro"); }}>
            <RotateCcw size={18} aria-hidden /> {L.again}
          </button>
          <Link href="/app" className="mt-8 flex items-center gap-3 rounded-xl border border-cyan/40 bg-cyan/5 p-4 text-base text-ink">
            <Smartphone size={22} className="shrink-0 text-cyan" aria-hidden /> {L.more}
          </Link>
        </>
      )}
    </main>
  );
}
