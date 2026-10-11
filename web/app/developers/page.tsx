"use client";

// Developers: the free public API, the live feed, and how to connect your
// own sensor. Everything here describes the API as it actually exists.

import Link from "next/link";
import { useState, type ReactNode } from "react";
import { Check, Copy } from "lucide-react";
import { API_URL, WS_URL } from "@/lib/config";
import { useT } from "@/lib/i18n";

function Code({ children, lang = "" }: { children: string; lang?: string }) {
  const [ok, setOk] = useState(false);
  return (
    <div className="group relative mt-3">
      <pre className="overflow-x-auto rounded-xl border border-rule bg-[#070b0e] p-4 text-[12.5px] leading-relaxed text-ink-2">
        {lang && <span className="mono mb-2 block text-[10px] uppercase tracking-[0.16em] text-ink-3">{lang}</span>}
        <code className="mono">{children}</code>
      </pre>
      <button
        className="absolute right-2 top-2 flex items-center gap-1 rounded-md border border-rule-strong bg-ground/80 px-2 py-1 text-[11px] text-ink-3 opacity-80 hover:text-ink"
        onClick={() => {
          navigator.clipboard?.writeText(children);
          setOk(true);
          setTimeout(() => setOk(false), 1200);
        }}
        aria-label="Copy"
      >
        {ok ? <Check size={12} /> : <Copy size={12} />} {ok ? "ok" : "copy"}
      </button>
    </div>
  );
}

function H2({ id, children }: { id: string; children: ReactNode }) {
  return (
    <h2 id={id} className="mt-14 scroll-mt-24 font-display text-2xl font-bold sm:text-3xl" style={{ fontStretch: "106%" }}>
      {children}
    </h2>
  );
}

const T = {
  en: {
    kicker: "Developers",
    title: "Build on NE-PULSE",
    lead: "The network's earthquake events and live state are open: no key, no sign-up, free. Use them for an alert bot, a school display, a map, a research notebook, or connect your own sensor and make the network denser.",
    honest: "Experimental research network, not an official warning service. Events can be wrong or retracted; show that to your users. For safety decisions follow the national seismological service and the Ministry of Emergency Situations.",
    toc: ["Quick start", "Endpoints", "Live feed", "Event object", "Examples", "Connect your own sensor", "Rules", "Coming next"],
    qs: "Everything is plain HTTPS + JSON. Try it:",
    epHead: ["Method", "Path", "What you get"],
    eps: [
      ["GET", "/api/v1/events?limit=50", "Recent events, newest first (limit 1–200; add &all=1 to include candidates)."],
      ["GET", "/api/v1/events/{id}", "One event with its full decision log: every check the network made and why."],
      ["GET", "/api/v1/events/{id}/traces", "Short anonymous waveform snippets recorded around that event."],
      ["GET", "/api/v1/network", "Live state: connected and listening phones, active areas (10 km, only if ≥3 phones), noise trace, active events."],
      ["GET", "/api/v1/history?hours=24", "Per-minute network statistics (phones, samples, triggers, events)."],
      ["GET", "/api/v1/config", "The detection settings in force (levels, thresholds, algorithm version)."],
      ["WS", "/ws/public", "Live push: the network state about every second, and every event update the moment it happens."],
    ],
    feed: "Open one WebSocket and you get two message types: `network` (the same body as /api/v1/network) and `event` (an event was created or changed level). No polling needed.",
    evIntro: "The fields you will use most:",
    evFields: [
      ["id", "Stable event id, e.g. NP-20261011-091500-1"],
      ["level", "candidate → corroborated → confirmed (only confirmed should alert people)"],
      ["originT", "Estimated start time, Unix seconds (±originUncSec)"],
      ["lat, lng", "Estimated epicentre, rounded (±locUncKm); depthKmAssumed is a model value, not measured"],
      ["levelTimes", "When each level was reached, Unix seconds"],
      ["cells", "Areas (10 km) that felt it: first arrival time, peak acceleration, intensity"],
      ["retracted", "true if the event was withdrawn; show it, don't hide it"],
    ],
    exAlert: "A tiny alert bot: print (or post to Discord/Telegram) every newly confirmed earthquake.",
    exPy: "Research: load the last day of network statistics into pandas.",
    sensorLead: "Anything with an accelerometer can join: an ESP32 + MPU6050 (~$5), a Raspberry Pi, an old phone. It must lie still and fixed; that is what makes a good sensor. Three steps:",
    sensorSteps: [
      "Register once: GET /api/v1/challenge, solve the small proof-of-work (find a nonce so that SHA-256(challenge + \":\" + nonce) starts with `difficulty` zero bits), then POST /api/v1/devices. Keep the deviceId and secret.",
      "Open a WebSocket to /ws/device and send a hello with your 1 km area (row/col of the 1 km grid; the exact location is never sent).",
      "Send a batch every second: sample times as offsets from t0 (0.1 ms units) and x, y, z in mm/s² with gravity included, at 50–100 Hz. The server filters, detects and votes exactly as for phones.",
    ],
    sensorFw: "Ready firmware for ESP32 + MPU6050 is in the source code under firmware/esp32-station.",
    rules: [
      "Free. No key needed for reading. Please cache: events change at most every few seconds.",
      "Rate limits apply per IP; if you get HTTP 429, slow down.",
      "Privacy is built in: positions are only published as 10 km areas with at least 3 phones; no device ids, no raw phone data are public.",
      "Say where the data comes from (\"NE-PULSE phone network\") and that it is experimental.",
      "Don't connect a sensor that moves, and don't fake data; the network rejects inconsistent sources and that only hurts everyone's alerts.",
    ],
    next: [
      "Webhooks: register a URL and get a POST the moment an event is confirmed (for school bells, displays, channels), with a free key.",
      "Open research datasets: anonymised noise statistics per area and labelled recordings from participants who agreed.",
    ],
    nextNote: "Not built yet. Tell us what you would make with them.",
    tgNote: "Just want alerts? The Telegram bot does it already, no code needed:",
    source: "Source code",
    method: "How detection works",
  },
  ru: {
    kicker: "Разработчикам",
    title: "Стройте на NE-PULSE",
    lead: "События землетрясений и состояние сети открыты: без ключа, без регистрации, бесплатно. Сделайте бота-оповещателя, табло для школы, карту, исследовательский блокнот — или подключите свой датчик и сделайте сеть плотнее.",
    honest: "Экспериментальная исследовательская сеть, не официальная служба оповещения. События могут быть ошибочными или отозванными — показывайте это пользователям. Для решений о безопасности следуйте национальной сейсмологической службе и МЧС.",
    toc: ["Быстрый старт", "Методы API", "Живой поток", "Объект события", "Примеры", "Подключить свой датчик", "Правила", "Скоро"],
    qs: "Всё — обычный HTTPS + JSON. Попробуйте:",
    epHead: ["Метод", "Путь", "Что вы получаете"],
    eps: [
      ["GET", "/api/v1/events?limit=50", "Последние события, сначала новые (limit 1–200; &all=1 — включая кандидатов)."],
      ["GET", "/api/v1/events/{id}", "Одно событие с полным журналом решений: каждая проверка сети и её причина."],
      ["GET", "/api/v1/events/{id}/traces", "Короткие анонимные фрагменты сигналов, записанные вокруг события."],
      ["GET", "/api/v1/network", "Состояние сейчас: подключённые и слушающие телефоны, активные участки (10 км, только если ≥3 телефонов), шум, активные события."],
      ["GET", "/api/v1/history?hours=24", "Поминутная статистика сети (телефоны, измерения, срабатывания, события)."],
      ["GET", "/api/v1/config", "Действующие настройки обнаружения (уровни, пороги, версия алгоритма)."],
      ["WS", "/ws/public", "Живая передача: состояние сети примерно раз в секунду и каждое изменение события сразу."],
    ],
    feed: "Откройте один WebSocket — придут два типа сообщений: `network` (то же, что /api/v1/network) и `event` (событие создано или сменило уровень). Опрашивать не нужно.",
    evIntro: "Самые нужные поля:",
    evFields: [
      ["id", "Постоянный id события, например NP-20261011-091500-1"],
      ["level", "candidate → corroborated → confirmed (оповещать людей — только при confirmed)"],
      ["originT", "Оценка времени начала, Unix-секунды (±originUncSec)"],
      ["lat, lng", "Оценка эпицентра, округлена (±locUncKm); depthKmAssumed — значение модели, не измерение"],
      ["levelTimes", "Когда достигнут каждый уровень, Unix-секунды"],
      ["cells", "Участки (10 км), которые его почувствовали: первое прибытие, пиковое ускорение, интенсивность"],
      ["retracted", "true, если событие отозвано — показывайте это, не скрывайте"],
    ],
    exAlert: "Маленький бот-оповещатель: печатает (или шлёт в Discord/Telegram) каждое новое подтверждённое землетрясение.",
    exPy: "Исследование: загрузить статистику сети за сутки в pandas.",
    sensorLead: "Подключиться может всё, где есть акселерометр: ESP32 + MPU6050 (~$5), Raspberry Pi, старый телефон. Главное — он должен лежать неподвижно и закреплённо. Три шага:",
    sensorSteps: [
      "Один раз зарегистрируйтесь: GET /api/v1/challenge, решите небольшую задачу (найдите nonce, чтобы SHA-256(challenge + \":\" + nonce) начинался с `difficulty` нулевых бит), затем POST /api/v1/devices. Сохраните deviceId и secret.",
      "Откройте WebSocket на /ws/device и отправьте hello со своим участком 1 км (row/col сетки 1 км; точное место никогда не передаётся).",
      "Каждую секунду отправляйте batch: время измерений как смещения от t0 (единицы 0,1 мс) и x, y, z в мм/с² с учётом гравитации, 50–100 Гц. Сервер фильтрует, обнаруживает и голосует так же, как для телефонов.",
    ],
    sensorFw: "Готовая прошивка для ESP32 + MPU6050 — в исходном коде, в firmware/esp32-station.",
    rules: [
      "Бесплатно. Для чтения ключ не нужен. Пожалуйста, кешируйте: события меняются не чаще раза в несколько секунд.",
      "Действуют ограничения по IP; при ответе HTTP 429 замедлитесь.",
      "Приватность встроена: места публикуются только как участки 10 км, где не меньше 3 телефонов; id устройств и сырые данные телефонов не публикуются.",
      "Указывайте источник («сеть телефонов NE-PULSE») и что она экспериментальная.",
      "Не подключайте датчик, который двигается, и не подделывайте данные: сеть отбрасывает противоречивые источники, а это вредит оповещениям для всех.",
    ],
    next: [
      "Вебхуки: укажите URL и получайте POST в момент подтверждения события (для школьного звонка, табло, каналов), с бесплатным ключом.",
      "Открытые исследовательские данные: анонимная статистика шума по участкам и размеченные записи участников, которые согласились.",
    ],
    nextNote: "Пока не сделано. Напишите, что бы вы с этим построили.",
    tgNote: "Нужны только оповещения? Это уже умеет Telegram-бот, без кода:",
    source: "Исходный код",
    method: "Как работает обнаружение",
  },
};

export default function Developers() {
  const { lang } = useT();
  const L = T[lang === "ru" ? "ru" : "en"];
  const ids = ["start", "endpoints", "feed", "event", "examples", "sensor", "rules", "next"];

  const curl = `curl ${API_URL}/api/v1/events?limit=5
curl ${API_URL}/api/v1/network`;
  const js = `// Live: every event update, the moment it happens
const ws = new WebSocket("${WS_URL}/ws/public");
const seen = new Set();
ws.onmessage = (m) => {
  const msg = JSON.parse(m.data);
  if (msg.type !== "event") return;           // "network" = live state
  const e = msg.event;
  if (e.level === "confirmed" && !e.retracted && !seen.has(e.id)) {
    seen.add(e.id);
    const t = new Date(e.originT * 1000).toLocaleTimeString();
    console.log(\`Earthquake confirmed \${t} near \${e.lat}, \${e.lng} (±\${e.locUncKm} km)\`);
    // post to your Discord webhook / Telegram channel / school display here
  }
};`;
  const py = `import requests, pandas as pd

API = "${API_URL}"
h = requests.get(f"{API}/api/v1/history", params={"hours": 24}).json()
df = pd.DataFrame(h["minutes"])
df["time"] = pd.to_datetime(df["t"], unit="s")
print(df[["time", "connected", "eligible", "samples", "picksAccepted", "events"]].tail())

events = requests.get(f"{API}/api/v1/events", params={"limit": 200}).json()["events"]
print(pd.DataFrame(events)[["id", "level", "originT", "lat", "lng", "locUncKm"]])`;
  const sensor = `# pip install requests websockets
import asyncio, hashlib, json, math, time, requests, websockets

API = "${API_URL}"

def solve(challenge, bits):                       # proof-of-work, ~1 s
    n = 0
    while True:
        h = hashlib.sha256(f"{challenge}:{n}".encode()).digest()
        if int.from_bytes(h, "big") >> (256 - bits) == 0:
            return str(n)
        n += 1

def cell_1km(lat, lng):                           # same grid as the site
    kmdeg = 2 * math.pi * 6371.0088 / 360
    dlat = 1 / kmdeg
    row = math.floor((lat + 90) / dlat)
    latc = -90 + (row + 0.5) * dlat
    n = max(1, round(360 * kmdeg * max(math.cos(math.radians(latc)), 1e-3)))
    return row, min(n - 1, math.floor((lng + 180) / (360 / n)))

# 1. register once (store these two values)
ch = requests.get(f"{API}/api/v1/challenge").json()
dev = requests.post(f"{API}/api/v1/devices", json={
    "challenge": ch["challenge"], "nonce": solve(ch["challenge"], ch["difficulty"]),
    "platform": "other", "consent": "v1"}).json()

def read_accel():                                 # <- your sensor here, m/s^2 incl. gravity
    return 0.0, 0.0, 9.81

async def main(lat, lng):
    row, col = cell_1km(lat, lng)
    async with websockets.connect(API.replace("http", "ws", 1) + "/ws/device") as ws:
        # 2. hello with the 1 km area
        await ws.send(json.dumps({"type": "hello", "deviceId": dev["deviceId"], "secret": dev["secret"],
            "platform": "other", "client": "my-sensor/0.1", "location": {"grid": 1, "row": row, "col": col}}))
        seq = 0
        while True:                               # 3. one batch per second, ~100 Hz
            t0 = time.time() * 1000
            dt, x, y, z = [], [], [], []
            while time.time() * 1000 - t0 < 1000:
                ax, ay, az = read_accel()
                dt.append(round((time.time() * 1000 - t0) * 10))
                x.append(round(ax * 1000)); y.append(round(ay * 1000)); z.append(round(az * 1000))
                await asyncio.sleep(0.01)
            seq += 1
            await ws.send(json.dumps({"type": "batch", "seq": seq, "t0": t0, "dt": dt,
                                      "x": x, "y": y, "z": z, "clock": {"offset": 0, "rtt": 0}}))

asyncio.run(main(41.31, 69.28))                   # your approximate location`;

  return (
    <main className="mx-auto max-w-[920px] px-4 pb-24 pt-10 sm:px-6">
      <div className="label">{L.kicker}</div>
      <h1 className="mt-2 font-display text-[clamp(2.1rem,6vw,3.6rem)] font-extrabold leading-[1]" style={{ fontStretch: "110%" }}>
        {L.title}
      </h1>
      <p className="mt-4 max-w-[64ch] text-lg leading-relaxed text-ink-2">{L.lead}</p>
      <p className="mt-4 rounded-xl border border-warn/40 bg-warn/5 p-3 text-sm text-ink-2">{L.honest}</p>

      <nav className="mt-6 flex flex-wrap gap-1.5">
        {L.toc.map((x, i) => (
          <a key={x} href={`#${ids[i]}`} className="chip">
            {x}
          </a>
        ))}
      </nav>

      <H2 id="start">{L.toc[0]}</H2>
      <p className="mt-3 text-ink-2">{L.qs}</p>
      <Code lang="shell">{curl}</Code>
      <p className="mono mt-3 text-xs text-ink-3">base URL: {API_URL}</p>

      <H2 id="endpoints">{L.toc[1]}</H2>
      <div className="mt-4 overflow-x-auto rounded-xl border border-rule">
        <table className="w-full min-w-[620px] text-left text-sm">
          <thead className="bg-panel text-ink-3">
            <tr>
              {L.epHead.map((h) => (
                <th key={h} className="label px-3 py-2.5 font-normal">
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {L.eps.map(([m, p, d]) => (
              <tr key={p} className="border-t border-rule align-top">
                <td className="mono px-3 py-2.5 text-cyan">{m}</td>
                <td className="mono whitespace-nowrap px-3 py-2.5 text-ink">{p}</td>
                <td className="px-3 py-2.5 text-ink-2">{d}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <H2 id="feed">{L.toc[2]}</H2>
      <p className="mt-3 text-ink-2">{L.feed}</p>
      <Code lang="javascript">{js}</Code>

      <H2 id="event">{L.toc[3]}</H2>
      <p className="mt-3 text-ink-2">{L.evIntro}</p>
      <dl className="mt-3 divide-y divide-rule rounded-xl border border-rule">
        {L.evFields.map(([k, v]) => (
          <div key={k} className="grid gap-1 px-4 py-2.5 sm:grid-cols-[150px_1fr]">
            <dt className="mono text-sm text-cyan">{k}</dt>
            <dd className="text-sm text-ink-2">{v}</dd>
          </div>
        ))}
      </dl>

      <H2 id="examples">{L.toc[4]}</H2>
      <p className="mt-3 text-ink-2">{L.exAlert}</p>
      <p className="mono mt-1 text-xs text-ink-3">↑ {L.toc[2]}</p>
      <p className="mt-6 text-ink-2">{L.exPy}</p>
      <Code lang="python">{py}</Code>
      <p className="mt-6 text-ink-2">
        {L.tgNote}{" "}
        <a href="https://t.me/ne_pulse_bot" className="text-cyan underline underline-offset-2">
          @ne_pulse_bot
        </a>
      </p>

      <H2 id="sensor">{L.toc[5]}</H2>
      <p className="mt-3 text-ink-2">{L.sensorLead}</p>
      <ol className="mt-4 grid gap-3">
        {L.sensorSteps.map((x, i) => (
          <li key={i} className="flex gap-3 text-ink-2">
            <span className="mono flex h-6 w-6 shrink-0 items-center justify-center rounded-full border border-cyan/50 text-xs text-cyan">{i + 1}</span>
            <span>{x}</span>
          </li>
        ))}
      </ol>
      <Code lang="python">{sensor}</Code>
      <p className="mt-3 text-sm text-ink-3">{L.sensorFw}</p>

      <H2 id="rules">{L.toc[6]}</H2>
      <ul className="mt-4 grid gap-2 text-ink-2">
        {L.rules.map((x) => (
          <li key={x} className="flex gap-2">
            <Check size={16} className="mt-1 shrink-0 text-ok" aria-hidden /> {x}
          </li>
        ))}
      </ul>

      <H2 id="next">{L.toc[7]}</H2>
      <ul className="mt-4 grid gap-2 text-ink-2">
        {L.next.map((x) => (
          <li key={x}>• {x}</li>
        ))}
      </ul>
      <p className="mt-3 text-sm text-ink-3">{L.nextNote}</p>

      <div className="mt-14 flex flex-wrap gap-3 border-t border-rule pt-6 text-sm">
        <Link href="/method" className="text-cyan underline underline-offset-2">
          {L.method}
        </Link>
        <a href="https://github.com/Debucker/Ne-pulse" className="text-cyan underline underline-offset-2">
          {L.source}
        </a>
      </div>
    </main>
  );
}
