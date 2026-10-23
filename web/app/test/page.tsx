"use client";

// "How sensitive is your phone?": the one-minute way to take part, written
// for someone who has never done anything like it. One thing per screen:
// start → (iPhone: allow motion) → put the phone down → 30 s still →
// eight one-tap questions → result to share. The raw 30 s and the answers
// go to the research.

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { ArrowLeft, Hand, ListChecks, Share2, RotateCcw, Smartphone, Timer, Trophy } from "lucide-react";
import { inAppBrowser, isIOS, requestMotionPermission } from "@/lib/sensor/client";
import { API_URL, backendReady } from "@/lib/config";
import { useT } from "@/lib/i18n";

const SECONDS = 32;

type Q = { key: string; options: string[] };
const QUESTIONS: Q[] = [
  { key: "age", options: ["u14", "14-17", "18+"] },
  { key: "surface", options: ["table", "floor", "bed", "other"] },
  { key: "night", options: ["bedside", "desk", "floor", "bed", "pillow", "other"] },
  { key: "charging", options: ["always", "sometimes", "never"] },
  { key: "storey", options: ["ground", "2", "3-4", "5-9", "10+"] },
  { key: "overnight", options: ["yes", "maybe", "no"] },
  { key: "oldphone", options: ["android", "iphone", "no"] },
  { key: "felt", options: ["tashkent", "elsewhere", "no"] },
];

const TEXT = {
  en: {
    kicker: "Earthquake research · 1 minute",
    title: "How sensitive is your phone?",
    lead: "Your phone has a motion sensor. We measure how small a shake it can feel. Your result helps a real research project about earthquakes.",
    how: ["Put the phone on a table for 30 seconds", "Answer 8 quick questions (just tap)", "See how your phone ranks"],
    consent: "Anonymous: no name, no phone number. By tapping Start you agree that your phone's motion recording and your answers are used for a school research project about earthquakes.",
    start: "Start",
    inAppT: "First, open this page in your browser",
    inAppAndroid: "Tap ⋮ in the top right corner, then “Open in Chrome” (or “Open in browser”).",
    inAppIos: "Tap ⋯ or the compass icon at the bottom, then “Open in Safari”.",
    inAppNote: "The test may not work inside Telegram or Instagram.",
    permT: "Your iPhone will ask a question",
    permD: "A window will say “Allow access to Motion and Orientation”. Tap Allow.",
    permBtn: "OK, continue",
    placeT: "Put your phone down",
    placeD: "Lay it on a table, screen up. Then take your hands away.",
    placeWait: "Waiting until the phone is still…",
    measureT: "Don't touch it",
    measureD: "Don't touch the phone or the table. Just wait until the circle is full.",
    moved: "The phone moved. Starting again, keep your hands away.",
    hiddenT: "The screen turned off",
    hiddenD: "The test stops when the screen goes dark. Tap the button and try again, the screen will stay on.",
    retry: "Try again",
    qOf: "Question {i} of {n}",
    back: "Back",
    q: {
      age: "How old are you?",
      surface: "Where was your phone lying during the test?",
      night: "Where is your phone at night?",
      charging: "Is your phone charging at night?",
      storey: "Which floor do you live on?",
      overnight: "Would you leave your phone on a charger overnight to help detect earthquakes?",
      oldphone: "Do you have an old phone you don't use anymore?",
      felt: "Have you ever felt an earthquake?",
    } as Record<string, string>,
    a: {
      age: { u14: "Under 14", "14-17": "14 to 17", "18+": "18 or older" },
      surface: { table: "On a table or desk", floor: "On the floor", bed: "On a bed or sofa", other: "Somewhere else" },
      night: { bedside: "Bedside table", desk: "Desk or shelf", floor: "On the floor", bed: "In bed with me", pillow: "Under the pillow", other: "Somewhere else" },
      charging: { always: "Yes, every night", sometimes: "Sometimes", never: "No" },
      storey: { ground: "1st floor or a house", "2": "2nd floor", "3-4": "3rd–4th floor", "5-9": "5th–9th floor", "10+": "10th or higher" },
      overnight: { yes: "Yes", maybe: "Maybe", no: "No" },
      oldphone: { android: "Yes, an Android", iphone: "Yes, an iPhone", no: "No" },
      felt: { tashkent: "Yes, in Tashkent", elsewhere: "Yes, somewhere else", no: "No, never" },
    } as Record<string, Record<string, string>>,
    sending: "Sending your result…",
    resultKicker: "Your phone",
    better: "More sensitive than {p}% of phones tested",
    first: "One of the first phones tested",
    feels: "It can feel shaking from intensity {m} ({w}).",
    mmiWords: ["", "people don't feel it", "people don't feel it", "like a truck passing outside", "felt indoors by many people", "felt by everyone"],
    tested: "Phones tested so far: {n}",
    thanks: "Thank you! You are now part of the research.",
    notMeasured: "The phone moved too much to measure it, but your answers are saved. You can try again on a table.",
    share: "Send to friends",
    shareText: "My phone is more sensitive than {p}% of phones in the NE-PULSE earthquake study. Test yours in 1 minute:",
    shareTextPlain: "I tested my phone for the NE-PULSE earthquake study. Test yours in 1 minute:",
    copied: "Link copied, paste it in a chat",
    again: "Test again",
    more: "Want to help every night? Turn an Android phone into a permanent sensor",
    errNoSensor: "This browser gives no motion data. Open the link on a phone, in Safari (iPhone) or Chrome (Android).",
    errDenied: "Motion access was refused. Close this page completely, open the link again and tap Allow.",
    errSend: "Could not send. Check the internet and tap the button again.",
    sendAgain: "Send again",
    teamT: "Join the night team?",
    teamLead: "You said you would leave your phone on overnight. Here is how it works:",
    teamPoints: [
      "I will add you to a Telegram group with the other volunteers.",
      "We do it together on one night: phone on the charger, on a table, until morning. I explain every step in the group.",
      "Everyone who takes part gets a certificate of participation in the research.",
    ],
    teamLabel: "Your Telegram username",
    teamPh: "@username",
    teamHow: "Find it in Telegram › Settings, under your name (it starts with @).",
    teamNote: "Only used to add you to the group. Stored separately from your test answers and deleted after the study. Under 18? Tell a parent.",
    teamJoin: "Join the night team",
    teamSkip: "No thanks",
    teamBad: "Check the username: letters, numbers and _, at least 5 characters, from Telegram › Settings.",
    teamOk: "You're on the night team! You'll be added to the Telegram group soon.",
  },
  ru: {
    kicker: "Исследование землетрясений · 1 минута",
    title: "Насколько чувствителен твой телефон?",
    lead: "В телефоне есть датчик движения. Мы измерим, какой маленький толчок он чувствует. Твой результат поможет настоящему исследованию землетрясений.",
    how: ["Положи телефон на стол на 30 секунд", "Ответь на 8 быстрых вопросов (просто нажимай)", "Узнай, какое место у твоего телефона"],
    consent: "Анонимно: без имени и номера. Нажимая «Начать», ты соглашаешься, что запись датчика и ответы используются в школьном исследовании землетрясений.",
    start: "Начать",
    inAppT: "Сначала открой эту страницу в браузере",
    inAppAndroid: "Нажми ⋮ в правом верхнем углу, затем «Открыть в Chrome» (или «Открыть в браузере»).",
    inAppIos: "Нажми ⋯ или значок компаса внизу, затем «Открыть в Safari».",
    inAppNote: "Внутри Telegram или Instagram тест может не работать.",
    permT: "iPhone задаст вопрос",
    permD: "Появится окно «Разрешить доступ к движению и ориентации». Нажми «Разрешить».",
    permBtn: "Понятно, дальше",
    placeT: "Положи телефон",
    placeD: "На стол, экраном вверх. Потом убери руки.",
    placeWait: "Ждём, пока телефон замрёт…",
    measureT: "Не трогай",
    measureD: "Не трогай телефон и стол. Просто подожди, пока круг заполнится.",
    moved: "Телефон сдвинулся. Начинаем заново, убери руки.",
    hiddenT: "Экран погас",
    hiddenD: "Когда экран гаснет, тест останавливается. Нажми кнопку и попробуй снова, экран не будет гаснуть.",
    retry: "Попробовать снова",
    qOf: "Вопрос {i} из {n}",
    back: "Назад",
    q: {
      age: "Сколько тебе лет?",
      surface: "Где лежал телефон во время теста?",
      night: "Где твой телефон ночью?",
      charging: "Телефон заряжается ночью?",
      storey: "На каком этаже ты живёшь?",
      overnight: "Оставил(а) бы ты телефон на зарядке на ночь, чтобы помочь засекать землетрясения?",
      oldphone: "Есть ли у тебя старый телефон, которым ты не пользуешься?",
      felt: "Ты когда-нибудь чувствовал(а) землетрясение?",
    } as Record<string, string>,
    a: {
      age: { u14: "Меньше 14", "14-17": "От 14 до 17", "18+": "18 или больше" },
      surface: { table: "На столе", floor: "На полу", bed: "На кровати или диване", other: "В другом месте" },
      night: { bedside: "На тумбочке", desk: "На столе или полке", floor: "На полу", bed: "В кровати рядом со мной", pillow: "Под подушкой", other: "В другом месте" },
      charging: { always: "Да, каждую ночь", sometimes: "Иногда", never: "Нет" },
      storey: { ground: "1-й этаж или свой дом", "2": "2-й этаж", "3-4": "3–4-й этаж", "5-9": "5–9-й этаж", "10+": "10-й и выше" },
      overnight: { yes: "Да", maybe: "Может быть", no: "Нет" },
      oldphone: { android: "Да, Android", iphone: "Да, iPhone", no: "Нет" },
      felt: { tashkent: "Да, в Ташкенте", elsewhere: "Да, в другом месте", no: "Нет, никогда" },
    } as Record<string, Record<string, string>>,
    sending: "Отправляем результат…",
    resultKicker: "Твой телефон",
    better: "Чувствительнее, чем {p}% проверенных телефонов",
    first: "Один из первых проверенных телефонов",
    feels: "Он чувствует толчки от {m} баллов ({w}).",
    mmiWords: ["", "люди не чувствуют", "люди не чувствуют", "как проезжающий грузовик", "многие чувствуют в доме", "чувствуют все"],
    tested: "Проверено телефонов: {n}",
    thanks: "Спасибо! Теперь ты участник исследования.",
    notMeasured: "Телефон слишком двигался, измерить не получилось, но ответы сохранены. Можно попробовать ещё раз на столе.",
    share: "Отправить друзьям",
    shareText: "Мой телефон чувствительнее {p}% телефонов в исследовании землетрясений NE-PULSE. Проверь свой за минуту:",
    shareTextPlain: "Я проверил(а) свой телефон для исследования землетрясений NE-PULSE. Проверь свой за минуту:",
    copied: "Ссылка скопирована, вставь её в чат",
    again: "Ещё раз",
    more: "Хочешь помогать каждую ночь? Преврати Android в постоянный датчик",
    errNoSensor: "Этот браузер не даёт данные движения. Открой ссылку на телефоне в Safari (iPhone) или Chrome (Android).",
    errDenied: "Доступ к датчику запрещён. Полностью закрой страницу, открой ссылку снова и нажми «Разрешить».",
    errSend: "Не удалось отправить. Проверь интернет и нажми кнопку ещё раз.",
    sendAgain: "Отправить ещё раз",
    teamT: "Присоединишься к ночной команде?",
    teamLead: "Ты ответил(а), что оставил(а) бы телефон на ночь. Вот как это будет:",
    teamPoints: [
      "Я добавлю тебя в группу в Telegram вместе с другими участниками.",
      "Мы сделаем это вместе в одну ночь: телефон на зарядке, на столе, до утра. Каждый шаг я объясню в группе.",
      "Все, кто участвует, получат сертификат участника исследования.",
    ],
    teamLabel: "Твой username в Telegram",
    teamPh: "@username",
    teamHow: "Его можно найти в Telegram › Настройки, под твоим именем (начинается с @).",
    teamNote: "Нужен только чтобы добавить тебя в группу. Хранится отдельно от ответов теста и удаляется после исследования. Тебе меньше 18? Предупреди родителей.",
    teamJoin: "Вступить в ночную команду",
    teamSkip: "Нет, спасибо",
    teamBad: "Проверь username: буквы, цифры и _, минимум 5 символов, из Telegram › Настройки.",
    teamOk: "Ты в ночной команде! Скоро тебя добавят в группу в Telegram.",
  },
};

type Phase = "intro" | "perm" | "place" | "measure" | "hidden" | "questions" | "send" | "team" | "result";
type Result = { measured: boolean; betterThanPct: number; tested: number; detMmi: number };
type WakeLock = { release: () => Promise<void> };

function model(): string {
  const ua = navigator.userAgent;
  const m = ua.match(/Android [\d.]+; ([^;)]+)/);
  if (m) return m[1].replace(/ Build\/.*/, "").trim();
  if (/iPhone/.test(ua)) return "iPhone";
  if (/iPad/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1)) return "iPad";
  return "";
}

/**
 * Saves to the website's own storage; if that is unavailable, to the NE-PULSE
 * network server instead (same request, same answer), so no test is lost.
 */
async function postResearch(kind: "survey" | "volunteer", body: unknown): Promise<Response> {
  const init = { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) };
  try {
    const r = await fetch(`/api/${kind}`, init);
    if (r.status < 500) return r;
  } catch {
    // fall through to the network server
  }
  await backendReady;
  return fetch(`${API_URL}/api/v1/${kind}`, init);
}

function testerId(): string {
  try {
    let id = localStorage.getItem("nepulse.tester");
    if (!id) {
      const b = new Uint8Array(8);
      crypto.getRandomValues(b);
      id = Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
      localStorage.setItem("nepulse.tester", id);
    }
    return id;
  } catch {
    return "";
  }
}

export default function PhoneTest() {
  const { lang } = useT();
  const L = TEXT[lang === "ru" ? "ru" : "en"];
  const [phase, setPhase] = useState<Phase>("intro");
  const [err, setErr] = useState<string | null>(null);
  const [left, setLeft] = useState(SECONDS);
  const [moved, setMoved] = useState(false);
  const [qi, setQi] = useState(0);
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const [res, setRes] = useState<Result | null>(null);
  const [copied, setCopied] = useState(false);
  const [inApp, setInApp] = useState<string | null>(null);
  const [android, setAndroid] = useState(false);
  const [handle, setHandle] = useState("");
  const [teamErr, setTeamErr] = useState(false);
  const [joined, setJoined] = useState(false);
  const rec = useRef({ t0: 0, dt: [] as number[], x: [] as number[], y: [] as number[], z: [] as number[] });
  const meta = useRef({ restarts: 0, wakeLock: false, settleSec: 0, screenOff: 0 });
  const stopRef = useRef<() => void>(() => {});
  const wake = useRef<WakeLock | null>(null);

  useEffect(() => {
    setInApp(inAppBrowser());
    setAndroid(/Android/i.test(navigator.userAgent));
    return () => stopRef.current();
  }, []);

  useEffect(() => {
    window.scrollTo(0, 0);
  }, [phase, qi]);

  async function keepAwake() {
    try {
      const nav = navigator as Navigator & { wakeLock?: { request: (t: "screen") => Promise<WakeLock> } };
      if (nav.wakeLock) {
        wake.current = await nav.wakeLock.request("screen");
        meta.current.wakeLock = true;
      }
    } catch {
      // not supported: the 30 s still fits most screen timeouts
    }
  }

  function releaseAwake() {
    wake.current?.release().catch(() => {});
    wake.current = null;
  }

  async function begin() {
    setErr(null);
    if (isIOS()) {
      setPhase("perm");
      return;
    }
    requestMotionPermission().catch(() => "granted");
    keepAwake();
    listen();
  }

  async function afterPerm() {
    const perm = await requestMotionPermission(); // must be the first await inside the tap (iPhone)
    if (perm === "denied") {
      setPhase("intro");
      return setErr(L.errDenied);
    }
    keepAwake();
    listen();
  }

  /** Waits for the phone to lie still, then records 32 s. */
  function listen() {
    setPhase("place");
    setMoved(false);
    setLeft(SECONDS);
    const r = rec.current;
    r.t0 = 0;
    r.dt = [];
    r.x = [];
    r.y = [];
    r.z = [];
    let got = false;
    let measuring = false;
    let calmSince = 0;
    const placedAt = performance.now();
    let prev: number[] | null = null;
    let jump = 0;

    const startMeasure = (now: number) => {
      measuring = true;
      r.t0 = now;
      r.dt = [];
      r.x = [];
      r.y = [];
      r.z = [];
      setPhase("measure");
    };

    const onMotion = (e: DeviceMotionEvent) => {
      const a = e.accelerationIncludingGravity;
      if (!a || a.x == null || a.y == null || a.z == null) return;
      got = true;
      const now = performance.now();
      const v = [a.x, a.y, a.z];
      if (prev) {
        const d = Math.hypot(v[0] - prev[0], v[1] - prev[1], v[2] - prev[2]);
        jump = jump * 0.9 + d * 0.1;
      }
      prev = v;
      if (!measuring) {
        // Start once the phone has been calm for 2 s (or after 20 s anyway).
        if (jump < 0.12) {
          if (!calmSince) calmSince = now;
        } else calmSince = 0;
        if ((calmSince && now - calmSince > 2000) || now - placedAt > 20000) {
          meta.current.settleSec = Math.round((now - placedAt) / 100) / 10;
          startMeasure(now);
        }
        return;
      }
      // Picked up or bumped in the first 20 s: start over (3 times at most).
      if (jump > 0.5 && now - r.t0 > 800 && now - r.t0 < 20000 && meta.current.restarts < 3) {
        meta.current.restarts++;
        jump = 0;
        setMoved(true);
        setTimeout(() => setMoved(false), 3500);
        startMeasure(now);
        return;
      }
      r.dt.push(Math.round((now - r.t0) * 10) / 10);
      r.x.push(Math.round(a.x * 1000) / 1000);
      r.y.push(Math.round(a.y * 1000) / 1000);
      r.z.push(Math.round(a.z * 1000) / 1000);
    };

    const onHidden = () => {
      if (document.visibilityState === "hidden") {
        meta.current.screenOff++;
        stop();
        releaseAwake();
        setPhase("hidden");
      }
    };

    window.addEventListener("devicemotion", onMotion);
    document.addEventListener("visibilitychange", onHidden);
    const timer = setInterval(() => {
      if (!measuring) return;
      const l = Math.max(0, SECONDS - Math.floor((performance.now() - r.t0) / 1000));
      setLeft(l);
      if (l === 0) {
        stop();
        releaseAwake();
        setQi(0);
        setPhase("questions");
      }
    }, 250);
    const noData = setTimeout(() => {
      if (!got) {
        stop();
        releaseAwake();
        setPhase("intro");
        setErr(L.errNoSensor);
      }
    }, 4000);
    function stop() {
      window.removeEventListener("devicemotion", onMotion);
      document.removeEventListener("visibilitychange", onHidden);
      clearInterval(timer);
      clearTimeout(noData);
    }
    stopRef.current = stop;
  }

  function answer(key: string, v: string) {
    const next = { ...answers, [key]: v };
    setAnswers(next);
    if (qi + 1 < QUESTIONS.length) setQi(qi + 1);
    else send(next);
  }

  async function send(a = answers) {
    setPhase("send");
    setErr(null);
    try {
      const r = rec.current;
      const cohort = new URLSearchParams(window.location.search).get("class") ?? "";
      const resp = await postResearch("survey", {
          platform: /Android/i.test(navigator.userAgent) ? "android" : isIOS() ? "ios" : "other",
          model: model(),
          lang,
          cohort,
          t0: Date.now() - (r.dt[r.dt.length - 1] ?? 0),
          dt: r.dt,
          x: r.x,
          y: r.y,
          z: r.z,
          answers: a,
          tester: testerId(),
          meta: { ...meta.current, inApp: !!inApp, samples: r.dt.length },
      });
      if (!resp.ok) throw new Error();
      setRes(await resp.json());
      setPhase(a.overnight === "yes" && !joined ? "team" : "result");
    } catch {
      setErr(L.errSend);
    }
  }

  async function joinTeam() {
    const h = handle.trim().replace(/^(https?:\/\/)?t\.me\//, "").replace(/^@/, "");
    if (!/^[A-Za-z][A-Za-z0-9_]{3,31}$/.test(h)) return setTeamErr(true);
    setTeamErr(false);
    try {
      const r = await postResearch("volunteer", { handle: h, platform: /Android/i.test(navigator.userAgent) ? "android" : isIOS() ? "ios" : "other", age: answers.age ?? "", lang });
      if (r.status === 400) return setTeamErr(true);
      if (!r.ok) throw new Error();
      setJoined(true);
      setPhase("result");
    } catch {
      setErr(L.errSend);
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
    setTimeout(() => setCopied(false), 2500);
  }

  function restart() {
    setAnswers({});
    setQi(0);
    setRes(null);
    meta.current = { restarts: 0, wakeLock: false, settleSec: 0, screenOff: 0 };
    setPhase("intro");
  }

  const pct = ((SECONDS - left) / SECONDS) * 100;
  const mmi = res ? Math.max(1, Math.min(5, Math.round(res.detMmi))) : 0;
  const roman = ["", "I", "II", "III", "IV", "V"][mmi];
  const howIcons = [Timer, ListChecks, Trophy];
  const big = "btn btn-primary flex w-full items-center justify-center gap-2 !min-h-[64px] text-xl";

  return (
    <main className="mx-auto max-w-[560px] px-4 pb-20 pt-8 sm:px-6">
      {phase === "intro" && (
        <>
          <div className="label">{L.kicker}</div>
          <h1 className="mt-2 font-display text-[clamp(2.1rem,9vw,3.3rem)] font-extrabold leading-[1.03]" style={{ fontStretch: "106%" }}>
            {L.title}
          </h1>
          <p className="mt-4 text-lg leading-relaxed text-ink-2">{L.lead}</p>

          {inApp && (
            <div className="mt-6 rounded-2xl border border-warn/60 bg-warn/5 p-5">
              <div className="text-lg font-semibold text-ink">{L.inAppT}</div>
              <p className="mt-2 text-lg text-ink-2">{android ? L.inAppAndroid : L.inAppIos}</p>
              <p className="mt-2 text-sm text-ink-3">{L.inAppNote}</p>
            </div>
          )}

          <ol className="mt-7 grid gap-3">
            {L.how.map((h, i) => {
              const Icon = howIcons[i];
              return (
                <li key={h} className="flex items-center gap-4 rounded-xl border border-rule p-4">
                  <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-full bg-cyan/15 text-cyan">
                    <Icon size={22} aria-hidden />
                  </span>
                  <span className="text-lg text-ink">
                    {i + 1}. {h}
                  </span>
                </li>
              );
            })}
          </ol>

          <button className={`${big} mt-7`} onClick={begin}>
            {L.start}
          </button>
          <p className="mt-4 text-sm leading-relaxed text-ink-3">{L.consent}</p>
          {err && <p className="mt-5 rounded-xl border border-warn/50 bg-warn/5 p-4 text-lg text-ink">{err}</p>}
        </>
      )}

      {phase === "perm" && (
        <div className="flex min-h-[70vh] flex-col justify-center">
          <h1 className="text-3xl font-bold">{L.permT}</h1>
          <p className="mt-4 text-xl leading-relaxed text-ink-2">{L.permD}</p>
          <button className={`${big} mt-8`} onClick={afterPerm}>
            {L.permBtn}
          </button>
        </div>
      )}

      {phase === "place" && (
        <div className="flex min-h-[70vh] flex-col items-center justify-center text-center">
          <Hand size={56} className="text-cyan" aria-hidden />
          <h1 className="mt-6 text-3xl font-bold">{L.placeT}</h1>
          <p className="mt-3 text-xl leading-relaxed text-ink-2">{L.placeD}</p>
          <p className="mt-8 text-base text-ink-3">{L.placeWait}</p>
        </div>
      )}

      {phase === "measure" && (
        <div className="flex min-h-[70vh] flex-col items-center justify-center text-center">
          <h1 className="text-3xl font-bold">{L.measureT}</h1>
          <p className="mt-3 text-xl leading-relaxed text-ink-2">{L.measureD}</p>
          <div
            className="relative mt-10 flex h-56 w-56 items-center justify-center rounded-full"
            style={{ background: `conic-gradient(var(--cyan) ${pct}%, rgba(255,255,255,0.08) 0)` }}
          >
            <div className="flex h-48 w-48 items-center justify-center rounded-full bg-ground">
              <span className="mono text-6xl font-semibold text-ink">{left}</span>
            </div>
          </div>
          <p className={`mt-8 min-h-[3.5rem] text-lg ${moved ? "font-semibold text-warn" : "text-ink-3"}`}>{moved ? L.moved : ""}</p>
        </div>
      )}

      {phase === "hidden" && (
        <div className="flex min-h-[70vh] flex-col justify-center">
          <h1 className="text-3xl font-bold">{L.hiddenT}</h1>
          <p className="mt-4 text-xl leading-relaxed text-ink-2">{L.hiddenD}</p>
          <button className={`${big} mt-8`} onClick={() => (isIOS() ? afterPerm() : (keepAwake(), listen()))}>
            {L.retry}
          </button>
        </div>
      )}

      {phase === "questions" && (
        <div className="min-h-[70vh]">
          <div className="flex items-center justify-between">
            <span className="mono text-sm text-ink-3">{L.qOf.replace("{i}", String(qi + 1)).replace("{n}", String(QUESTIONS.length))}</span>
            {qi > 0 && (
              <button className="flex items-center gap-1 py-2 text-base text-ink-3" onClick={() => setQi(qi - 1)}>
                <ArrowLeft size={18} aria-hidden /> {L.back}
              </button>
            )}
          </div>
          <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-rule">
            <div className="h-full rounded-full bg-cyan transition-all" style={{ width: `${(qi / QUESTIONS.length) * 100}%` }} />
          </div>
          <h1 className="mt-8 text-[1.7rem] font-bold leading-snug">{L.q[QUESTIONS[qi].key]}</h1>
          <div className="mt-6 grid gap-3">
            {QUESTIONS[qi].options.map((v) => (
              <button
                key={v}
                onClick={() => answer(QUESTIONS[qi].key, v)}
                className={`min-h-[60px] w-full rounded-2xl border px-5 text-left text-xl ${
                  answers[QUESTIONS[qi].key] === v ? "border-cyan bg-cyan/15 text-ink" : "border-rule-strong text-ink"
                }`}
              >
                {L.a[QUESTIONS[qi].key][v]}
              </button>
            ))}
          </div>
        </div>
      )}

      {phase === "send" && (
        <div className="flex min-h-[60vh] flex-col items-center justify-center text-center">
          {err ? (
            <>
              <p className="text-xl text-ink">{err}</p>
              <button className={`${big} mt-6`} onClick={() => send()}>
                {L.sendAgain}
              </button>
            </>
          ) : (
            <p className="text-xl text-ink-2">{L.sending}</p>
          )}
        </div>
      )}

      {phase === "team" && (
        <div className="min-h-[70vh]">
          <h1 className="text-3xl font-bold">🌙 {L.teamT}</h1>
          <p className="mt-4 text-lg text-ink-2">{L.teamLead}</p>
          <ol className="mt-4 grid gap-3">
            {L.teamPoints.map((t, i) => (
              <li key={t} className="flex gap-3 rounded-xl border border-rule p-4 text-lg text-ink">
                <span className="mono text-cyan">{i + 1}</span>
                <span>{t}</span>
              </li>
            ))}
          </ol>
          <label className="mt-6 grid gap-2">
            <span className="text-lg font-medium text-ink">{L.teamLabel}</span>
            <input
              value={handle}
              onChange={(e) => {
                setHandle(e.target.value);
                setTeamErr(false);
              }}
              placeholder={L.teamPh}
              autoCapitalize="none"
              autoCorrect="off"
              autoComplete="off"
              spellCheck={false}
              maxLength={40}
              className="h-14 rounded-xl border border-rule-strong bg-ground px-4 text-xl text-ink outline-none focus:border-cyan"
            />
            <span className="text-sm text-ink-3">{L.teamHow}</span>
          </label>
          {teamErr && <p className="mt-3 text-base text-warn">{L.teamBad}</p>}
          {err && <p className="mt-3 text-base text-bad">{err}</p>}
          <button className={`${big} mt-6`} disabled={!handle.trim()} onClick={joinTeam}>
            {L.teamJoin}
          </button>
          <button className="mt-3 w-full py-3 text-lg text-ink-3 underline underline-offset-4" onClick={() => setPhase("result")}>
            {L.teamSkip}
          </button>
          <p className="mt-4 text-sm leading-relaxed text-ink-3">{L.teamNote}</p>
        </div>
      )}

      {phase === "result" && res && (
        <>
          {joined && <p className="mb-4 rounded-xl border border-ok/50 bg-ok/10 p-4 text-lg text-ink">✓ {L.teamOk}</p>}
          <div className="glass mt-4 p-6 text-center">
            <div className="label">{L.resultKicker}</div>
            {res.measured ? (
              <>
                <div className="mt-3 font-display text-[clamp(2rem,8vw,2.8rem)] font-extrabold leading-tight text-cyan">
                  {res.tested > 1 ? L.better.replace("{p}", String(res.betterThanPct)) : L.first}
                </div>
                <p className="mt-4 text-lg text-ink">{L.feels.replace("{m}", roman).replace("{w}", L.mmiWords[mmi])}</p>
              </>
            ) : (
              <p className="mt-3 text-lg text-ink">{L.notMeasured}</p>
            )}
            <p className="mono mt-4 text-sm text-ink-3">{L.tested.replace("{n}", String(Math.max(res.tested, 1)))}</p>
          </div>
          <p className="mt-5 text-center text-lg text-ink-2">{L.thanks}</p>
          <button className={`${big} mt-6`} onClick={share}>
            <Share2 size={22} aria-hidden /> {copied ? L.copied : L.share}
          </button>
          <button className="btn mt-3 flex w-full items-center justify-center gap-2 !min-h-[52px]" onClick={restart}>
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
