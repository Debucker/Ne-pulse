"use client";

import { useEffect, useState } from "react";
import { API_URL } from "@/lib/config";
import { useT } from "@/lib/i18n";

interface Crit {
  MinUnits: number;
  MaxLog10PAdj: number;
  MinFootprint: number;
  MinSources: number;
  MinApertureKM: number;
  MinEstablished: number;
  MinEstablishedFrac: number;
  MaxRMSSec: number;
  MinConsistency: number;
  HoldSec: number;
  MinSweepVelKMS: number;
  MaxContradiction: number;
}

type Block = { h?: string; p?: string[]; eq?: string; list?: string[] };

const TEXT: Record<"en" | "ru", { kicker: string; title: string; simpleTitle: string; simple: string[]; glossTitle: string; gloss: [string, string][]; blocks: Block[]; levels: string; live: string; rows: string[]; limitsTitle: string; limits: string[] }> = {
  en: {
    kicker: "How it works",
    title: "How NE-PULSE decides",
    simpleTitle: "In simple words",
    simple: [
      "An earthquake sends two kinds of waves through the ground. The faster P wave arrives first; the slower, stronger S wave follows. Both spread outward from one point at several kilometres per second.",
      "A phone lying on a table moves a tiny bit when these waves pass. Its motion sensor notices. So does it notice a door slamming, someone typing on it, or being picked up. One phone alone cannot tell the difference reliably.",
      "Many phones can. If phones across a city start moving in the order a wave from one point would reach them, and far more of them than could by chance, that is strong evidence of an earthquake. NE-PULSE measures exactly that, and shows every number it used.",
    ],
    glossTitle: "Words used on this site",
    gloss: [
      ["P wave", "the first, faster seismic wave (≈ 6 km/s), a push-pull motion."],
      ["S wave", "the second, slower wave (≈ 3.4 km/s), a side-to-side motion; usually stronger."],
      ["Epicentre", "the point on the surface above where the earthquake started."],
      ["Intensity (MMI)", "how strongly shaking is felt at a place, from I (not felt) to X (extreme)."],
      ["PGA", "peak ground acceleration, the strongest acceleration measured, in m/s² or cm/s²."],
      ["STA/LTA", "the ratio of short-term to long-term average motion. A sudden rise means something started."],
      ["Area / unit", "a 1 km square. All phones in it count as one independent witness."],
      ["p-value", "the probability that the pattern happened by pure coincidence. Smaller is stronger evidence."],
    ],
    blocks: [
      { p: ["All detection runs on the server, on the raw samples phones send, with deterministic code. The same code runs in the simulator used to test it. The full derivation, with every constant and its source, is in docs/MODEL.md of the repository."] },
      { h: "1 · Each phone", p: ["Samples arrive with irregular browser timestamps. They are resampled to a 50 Hz grid shared by all phones, then band-passed with a 4th-order Butterworth high-pass at 0.5 Hz and a 2nd-order low-pass at 12 Hz. The high-pass removes gravity whatever the phone's orientation, so only the vector magnitude of the three axes is used."], eq: "|a|(t) = √(ax² + ay² + az²)   after band-pass, m/s²" },
      { p: ["A classic STA/LTA detector (1 s / 30 s) watches the energy |a|². A trigger needs:"], eq: "STA / max(LTA, A_min² / R) ≥ R = 4    and    √STA ≥ A_min\nA_min = max(0.02 m/s², 2 × measured sensor resolution)" },
      { p: ["The onset is refined with Maeda's AIC picker, and 2.56 s later the pick is classified. It is rejected if the phone fell (|a| < 0.3 g), turned by more than 2° (picked up), was already moving, or the motion was a spike (5–95 % energy duration under 0.4 s, or crest factor above 8). Phones only count after being still for 30 s. A phone that is walked around, held in the hand or kept in a pocket never becomes a sensor."] },
      { h: "2 · Each area", p: ["Phones are grouped into 1 km squares. A square votes when at least a third of its listening phones trigger within 2 s, so one building counts once. Each square's background vote rate is learned from its own history, starting from a deliberately pessimistic 2 votes per hour:"], eq: "λ_u = (α₀ + votes) / (β₀ + listening time)    α₀ = 1,  β₀ = 1800 s" },
      { h: "3 · The network", p: ["Votes are located by grid search for a point source at 8 km depth with IASP91 upper-crust velocities (P 5.80, S 3.36 km/s). Each vote may be a P or an S arrival. Hypotheses are scored with a robust kernel, so outliers are ignored:"], eq: "score = Σ_inliers 1 − (rᵢ/τᵢ)²      rᵢ = tᵢ − t₀ − Rᵢ/v\nτᵢ = 3 √(σ_pick² + σ_clock² + σ_model²)" },
      { p: ["Significance: how likely is it that at least k of the M sensitive squares inside the footprint triggered by chance inside their predicted windows? With pᵤ = 1 − exp(−λᵤ wᵤ), the exact Poisson-binomial tail is computed and multiplied by the number of independent hypotheses searched (look-elsewhere effect):"], eq: "p = P(K ≥ k),  K ~ PoissonBinomial(p₁…p_M)\np_adj = min(1, p × N_space × N_time)" },
      { p: ["The model assumes squares are independent. Correlated noise breaks that assumption, so it is defended separately. Silent squares inside the footprint count against an event. Quiet phones near places reporting strong shaking contradict it: a person shaking a phone, or a group doing it on a countdown, cannot make the ground move. Triggers the source cannot explain lower its consistency. A plane-wave fit catches fronts moving at the speed of sound or traffic. Evidence must come from several networks and from phones with a streaming history."] },
    ],
    levels: "Levels",
    live: "Read live from the running server. Changing a threshold changes this table and the configuration hash on the network page.",
    rows: [
      "Independent 1 km areas",
      "Chance coincidence (adjusted p)",
      "Sensitive areas in the footprint that triggered",
      "Nearby triggers explained by the source",
      "Quiet phones that should have felt the shaking (rejects deliberate shaking)",
      "Plane-wave sweep slower than (rejects sound and traffic)",
      "Distinct internet networks",
      "Spread of triggered areas",
      "Established phones (≥ 12 h old, ≥ 4 h streamed)",
      "Share of established phones",
      "Travel-time residual RMS",
      "Criteria must hold continuously for",
    ],
    limitsTitle: "What it cannot do",
    limits: [
      "Browsers stop sharing motion when the screen is off or the page is hidden. Continuous coverage needs a native app.",
      "Phones resolve roughly light shaking and above (intensity III–IV at best). Small or distant earthquakes will be missed.",
      "A phone's own vibration alert looks like shaking to that phone. Only agreement between areas filters it out.",
      "Thresholds were chosen from physics and simulation. Real detection rates must still be measured against catalogue earthquakes.",
      "It is too slow and sparse to be an early-warning system, and it does not claim to be one.",
    ],
  },
  ru: {
    kicker: "Как это работает",
    title: "Как NE-PULSE принимает решение",
    simpleTitle: "Простыми словами",
    simple: [
      "Землетрясение посылает сквозь землю волны двух видов. Первой приходит быстрая P-волна, за ней — более медленная и сильная S-волна. Обе расходятся из одной точки со скоростью несколько километров в секунду.",
      "Телефон на столе чуть-чуть сдвигается, когда проходят эти волны, и его датчик движения это замечает. Но так же он заметит хлопнувшую дверь, набор текста или то, что его взяли в руки. Один телефон не может надёжно отличить одно от другого.",
      "Много телефонов — могут. Если телефоны по всему городу начинают двигаться в том порядке, в каком до них дошла бы волна из одной точки, и их гораздо больше, чем могло бы совпасть случайно, это сильный признак землетрясения. NE-PULSE измеряет именно это и показывает все числа, на которых основано решение.",
    ],
    glossTitle: "Термины на этом сайте",
    gloss: [
      ["P-волна", "первая, более быстрая сейсмическая волна (≈ 6 км/с), колебания сжатия-растяжения."],
      ["S-волна", "вторая, более медленная волна (≈ 3,4 км/с), колебания из стороны в сторону; обычно сильнее."],
      ["Эпицентр", "точка на поверхности над местом, где началось землетрясение."],
      ["Интенсивность (MMI)", "насколько сильно ощущаются толчки в данном месте, от I (не ощущаются) до X (катастрофические)."],
      ["PGA", "пиковое ускорение грунта, самое сильное измеренное ускорение, в м/с² или см/с²."],
      ["STA/LTA", "отношение краткосрочного среднего движения к долгосрочному. Резкий рост значит, что что-то началось."],
      ["Участок", "квадрат 1 км. Все телефоны в нём считаются одним независимым свидетелем."],
      ["p-значение", "вероятность того, что картина возникла чисто случайно. Чем меньше, тем сильнее доказательство."],
    ],
    blocks: [
      { p: ["Всё обнаружение выполняется на сервере по необработанным измерениям телефонов, детерминированным кодом. Тот же код работает в симуляторе, которым его проверяют. Полный вывод со всеми константами и источниками — в файле docs/MODEL.md репозитория."] },
      { h: "1 · Каждый телефон", p: ["Измерения приходят с неравномерными метками времени браузера. Они пересчитываются на общую для всех телефонов сетку 50 Гц, затем фильтруются: фильтр Баттерворта верхних частот 4-го порядка на 0,5 Гц и нижних частот 2-го порядка на 12 Гц. Фильтр верхних частот убирает силу тяжести при любой ориентации телефона, поэтому используется только модуль вектора по трём осям."], eq: "|a|(t) = √(ax² + ay² + az²)   после фильтра, м/с²" },
      { p: ["Классический детектор STA/LTA (1 с / 30 с) следит за энергией |a|². Для срабатывания нужно:"], eq: "STA / max(LTA, A_min² / R) ≥ R = 4    и    √STA ≥ A_min\nA_min = max(0,02 м/с², 2 × измеренное разрешение датчика)" },
      { p: ["Момент вступления уточняется AIC-методом Маэды, и через 2,56 с срабатывание классифицируется. Оно отбрасывается, если телефон падал (|a| < 0,3 g), повернулся больше чем на 2° (его взяли в руки), уже двигался, или движение было коротким импульсом (длительность 5–95 % энергии меньше 0,4 с либо пик-фактор больше 8). Телефон учитывается только после 30 с неподвижности. Телефон в руке, в кармане или при ходьбе никогда не становится датчиком."] },
      { h: "2 · Каждый участок", p: ["Телефоны объединяются в квадраты 1 км. Квадрат голосует, когда в течение 2 с срабатывает хотя бы треть его слушающих телефонов, поэтому одно здание считается один раз. Фоновая частота голосов каждого квадрата обучается на его собственной истории, начиная с заведомо пессимистичных 2 голосов в час:"], eq: "λ_u = (α₀ + голоса) / (β₀ + время прослушивания)    α₀ = 1,  β₀ = 1800 с" },
      { h: "3 · Сеть", p: ["Голоса локализуются перебором по сетке: ищется точечный источник на глубине 8 км со скоростями верхней коры IASP91 (P 5,80, S 3,36 км/с). Каждый голос может быть приходом P- или S-волны. Гипотезы оцениваются устойчивым ядром, поэтому выбросы игнорируются:"], eq: "оценка = Σ 1 − (rᵢ/τᵢ)²      rᵢ = tᵢ − t₀ − Rᵢ/v\nτᵢ = 3 √(σ_вступл² + σ_часов² + σ_модели²)" },
      { p: ["Значимость: насколько вероятно, что хотя бы k из M чувствительных квадратов в зоне охвата сработали случайно в предсказанные для них окна? При pᵤ = 1 − exp(−λᵤ wᵤ) точно вычисляется хвост распределения Пуассона–бинома и умножается на число перебранных независимых гипотез (поправка на множественный поиск):"], eq: "p = P(K ≥ k),  K ~ ПуассонБином(p₁…p_M)\np_adj = min(1, p × N_пространство × N_время)" },
      { p: ["Модель предполагает, что квадраты независимы. Коррелированный шум нарушает это предположение, поэтому от него есть отдельные защиты. Молчащие квадраты в зоне охвата говорят против события. Спокойные телефоны рядом с местами, где якобы сильно трясёт, противоречат ему: человек, трясущий телефон, или группа, делающая это по команде, не может сдвинуть землю. Срабатывания, которые источник не объясняет, снижают согласованность. Подгонка плоской волны ловит фронты, идущие со скоростью звука или транспорта. Доказательства должны приходить из нескольких сетей и от телефонов с историей передачи данных."] },
    ],
    levels: "Уровни",
    live: "Значения берутся с работающего сервера. Изменение порога меняет эту таблицу и хэш конфигурации на странице сети.",
    rows: [
      "Независимые участки 1 км",
      "Шанс совпадения (скорректированное p)",
      "Доля чувствительных участков в зоне охвата, которые сработали",
      "Доля срабатываний рядом, объяснённых источником",
      "Спокойные телефоны, которые должны были почувствовать тряску (отсекает намеренную тряску)",
      "Фронт плоской волны медленнее (отсекает звук и транспорт)",
      "Разные интернет-сети",
      "Размах сработавших участков",
      "Проверенные телефоны (≥ 12 ч с подключения, ≥ 4 ч передачи)",
      "Доля проверенных телефонов",
      "Среднеквадратичная невязка времени прихода",
      "Критерии должны выполняться непрерывно",
    ],
    limitsTitle: "Чего система не умеет",
    limits: [
      "Браузер перестаёт передавать движение, когда экран выключен или страница скрыта. Для непрерывной работы нужно нативное приложение.",
      "Телефоны различают примерно лёгкие и более сильные толчки (в лучшем случае интенсивность III–IV). Слабые или далёкие землетрясения будут пропущены.",
      "Вибрация самого телефона выглядит для него как толчок. Отсеять её может только согласие между участками.",
      "Пороги выбраны по физике и моделированию. Реальную точность ещё предстоит измерить по каталогам землетрясений.",
      "Система слишком медленная и редкая, чтобы быть системой раннего оповещения, и не претендует на это.",
    ],
  },
};

const KEYS: [keyof Crit, (v: number) => string][] = [
  ["MinUnits", (v) => `≥ ${v}`],
  ["MaxLog10PAdj", (v) => `≤ 10^${v}`],
  ["MinFootprint", (v) => (v ? `≥ ${Math.round(v * 100)}%` : "–")],
  ["MinConsistency", (v) => (v ? `≥ ${Math.round(v * 100)}%` : "–")],
  ["MaxContradiction", (v) => (v ? `≤ ${Math.round(v * 100)}%` : "–")],
  ["MinSweepVelKMS", (v) => (v ? `${v} km/s` : "–")],
  ["MinSources", (v) => (v ? `≥ ${v}` : "–")],
  ["MinApertureKM", (v) => (v ? `≥ ${v} km` : "–")],
  ["MinEstablished", (v) => (v ? `≥ ${v}` : "–")],
  ["MinEstablishedFrac", (v) => (v ? `≥ ${Math.round(v * 100)}%` : "–")],
  ["MaxRMSSec", (v) => (v ? `≤ ${v} s` : "–")],
  ["HoldSec", (v) => (v ? `${v} s` : "–")],
];

export default function Method() {
  const { lang, t } = useT();
  const T = TEXT[lang];
  const [lv, setLv] = useState<Record<string, Crit> | null>(null);
  useEffect(() => {
    fetch(API_URL + "/api/v1/config")
      .then((r) => r.json())
      .then((c) => setLv(c.levels))
      .catch(() => {});
  }, []);

  return (
    <main className="mx-auto max-w-[840px] px-4 pt-10 sm:px-6">
      <div className="label">{T.kicker}</div>
      <h1 className="mt-1 font-display text-3xl font-bold sm:text-5xl" style={{ fontStretch: "110%" }}>
        {T.title}
      </h1>

      <section className="panel mt-8 p-6">
        <h2 className="font-display text-2xl font-bold">{T.simpleTitle}</h2>
        <div className="mt-4 grid gap-4 text-lg leading-relaxed text-ink-2">
          {T.simple.map((p) => (
            <p key={p}>{p}</p>
          ))}
        </div>
      </section>

      <section className="mt-8">
        <h2 className="text-xl font-semibold">{T.glossTitle}</h2>
        <dl className="mt-3 divide-y divide-rule border-y border-rule">
          {T.gloss.map(([k, v]) => (
            <div key={k} className="grid gap-1 py-3 sm:grid-cols-[180px_1fr]">
              <dt className="mono text-sm text-trace">{k}</dt>
              <dd className="text-ink-2">{v}</dd>
            </div>
          ))}
        </dl>
      </section>

      <div className="prose-np mt-6">
        {T.blocks.map((b, i) => (
          <div key={i}>
            {b.h && <h2>{b.h}</h2>}
            {b.p?.map((p) => (
              <p key={p}>{p}</p>
            ))}
            {b.eq && <span className="eq">{b.eq}</span>}
          </div>
        ))}
        <h2>{T.levels}</h2>
      </div>
      <div className="mt-4 overflow-x-auto border border-rule">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-rule text-left">
              <th className="label p-2 font-normal" />
              <th className="label p-2 font-normal text-candidate">{t("level.candidate")}</th>
              <th className="label p-2 font-normal text-corroborated">{t("level.corroborated")}</th>
              <th className="label p-2 font-normal text-confirmed">{t("level.confirmed")}</th>
            </tr>
          </thead>
          <tbody>
            {KEYS.map(([k, f], i) => (
              <tr key={k} className="border-b border-rule last:border-0">
                <td className="p-2 text-ink-2">{T.rows[i]}</td>
                {(["candidate", "corroborated", "confirmed"] as const).map((l) => (
                  <td key={l} className="mono whitespace-nowrap p-2 text-ink">
                    {lv ? f(lv[l]?.[k] ?? 0) : "…"}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="mt-2 text-xs text-ink-3">{T.live}</p>

      <div className="prose-np mt-4">
        <h2>MMI</h2>
        <span className="eq">{"MMI = 1.78 + 1.55 log₁₀(PGA)   if log₁₀(PGA) ≤ 1.57\nMMI = −1.60 + 3.70 log₁₀(PGA)  otherwise     (PGA in cm/s², Worden et al., 2012)"}</span>
        <h2>{T.limitsTitle}</h2>
        <ul>
          {T.limits.map((l) => (
            <li key={l}>{l}</li>
          ))}
        </ul>
      </div>
    </main>
  );
}
