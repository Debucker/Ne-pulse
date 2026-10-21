"use client";

import Link from "next/link";
import { useT } from "@/lib/i18n";

function EN() {
  return (
    <>
      <div className="label">Privacy notice · version v1</div>
      <h1 className="mt-1 font-display text-3xl font-bold sm:text-4xl" style={{ fontStretch: "110%" }}>
        What NE-PULSE collects, and what it doesn&apos;t
      </h1>
      <div className="prose-np mt-6">
        <p>NE-PULSE is a research project studying whether ordinary phones can detect earthquakes together. It collects the minimum it needs.</p>
        <h2>Collected</h2>
        <ul>
          <li>
            <strong>Motion samples.</strong> Raw accelerometer readings (about 50–60 per second) while the NE-PULSE page is open and visible. They
            are analysed as they arrive and kept only in memory for about a minute.
          </li>
          <li>
            <strong>Recordings around events.</strong> When the network flags a possible earthquake, about a minute of motion from the phones that
            triggered is stored for research, for up to 365 days.
          </li>
          <li>
            <strong>Triggers.</strong> Each detected motion onset with its computed features (strength, duration, frequency), for up to 180 days.
          </li>
          <li>
            <strong>Your area.</strong> A 1 km square, computed on your phone. Your precise position is never sent.
          </li>
          <li>
            <strong>A random device ID</strong>, the date it joined, how long it has streamed, and the phone type (iPhone, Android, other).
          </li>
          <li>
            <strong>Class recordings (only with a class code).</strong> If you join with a code from your teacher, your phone&apos;s raw
            motion is recorded continuously while the page is open, for that class&apos;s research project, under a pseudonym. Kept up to 365
            days; deleted with your device. Anyone under 18 needs a parent&apos;s or guardian&apos;s agreement.
          </li>
          <li>
            <strong>Placement and labels (only if you fill them in).</strong> Where the phone lies (floor, surface, building type) and the moments
            you mark (&quot;door&quot;, &quot;steps&quot; ...) with their time. Never an address. Deleted with your device.
          </li>
          <li>
            <strong>Telegram alerts (only if you subscribe).</strong> Your Telegram chat ID, which 10 km area to alert and your language. A location
            you share with the bot is turned into that area at once and discarded. If you connect Telegram from the phone page, the bot also
            knows which phone is yours, only to tell you when it stops listening (daytime, at most every 6 hours). Send /stop to the bot to delete it.
          </li>
          <li>
            <strong>Your name (only if you add it).</strong> Shown on the public list of contributors and in the acknowledgements of the research
            paper once your phone has listened for an hour. It is stored apart from the motion recordings, which never contain it. Change or remove
            it on the phone page at any time; leaving the network deletes it. If you joined with a friend&apos;s invite link, we note who invited
            you so they are credited; nothing else about you is shared with them.
          </li>
          <li>
            <strong>Night team (only if you sign up after the phone test).</strong> Your Telegram username, phone type, age group and language,
            used only to add you to the volunteers&apos; Telegram group. It is stored apart from your test answers and deleted after the study.
          </li>
        </ul>
        <h2>Not collected</h2>
        <ul>
          <li>No email, phone number, account, contacts, photos, microphone or camera. No name unless you add one yourself.</li>
          <li>No precise location. If you share your browser&apos;s location, it is rounded on the phone and discarded.</li>
          <li>
            Your IP address is not stored. It is used in memory for rate limits and turned into a hashed network prefix whose key changes every
            day, so it cannot be linked across days or reversed. The detector uses it to count how many different networks an event came from.
          </li>
        </ul>
        <h2>What is public</h2>
        <ul>
          <li>Maps show phones grouped into 10 km squares. Squares with fewer than three phones are hidden.</li>
          <li>Event pages show triggered areas at the same 10 km resolution and motion averaged by distance. No device ID ever appears.</li>
          <li>Research datasets derived from NE-PULSE contain no device IDs and no location finer than 1 km.</li>
        </ul>
        <h2>Your control</h2>
        <ul>
          <li>Stop streaming at any time: close the page or press Stop.</li>
          <li>
            Delete everything from <Link href="/device">your phone page</Link>: the device ID, statistics, triggers and stored recordings.
            Anonymous totals of past events remain.
          </li>
          <li>Devices that do not connect for 180 days are deleted automatically.</li>
        </ul>
        <h2>Research use</h2>
        <p>
          Data is used to develop and evaluate the detection method and may be published in anonymised, aggregated form in research papers.
          NE-PULSE is not an official warning service.
        </p>
      </div>
    </>
  );
}

function RU() {
  return (
    <>
      <div className="label">Уведомление о приватности · версия v1</div>
      <h1 className="mt-1 font-display text-3xl font-bold sm:text-4xl">Что NE-PULSE собирает, а что нет</h1>
      <div className="prose-np mt-6">
        <p>NE-PULSE — исследовательский проект: могут ли обычные телефоны вместе обнаруживать землетрясения. Мы собираем только необходимое.</p>
        <h2>Собирается</h2>
        <ul>
          <li>
            <strong>Измерения движения.</strong> Показания акселерометра (примерно 50–60 в секунду), пока страница NE-PULSE открыта и видна.
            Они анализируются сразу и хранятся только в памяти около минуты.
          </li>
          <li>
            <strong>Записи вокруг событий.</strong> Когда сеть отмечает возможное землетрясение, около минуты движения от сработавших телефонов
            сохраняется для исследования, до 365 дней.
          </li>
          <li>
            <strong>Срабатывания.</strong> Каждое замеченное начало движения с рассчитанными признаками (сила, длительность, частота), до 180 дней.
          </li>
          <li>
            <strong>Ваш район.</strong> Квадрат 1 км, который вычисляет сам телефон. Точное местоположение никогда не отправляется.
          </li>
          <li>
            <strong>Случайный идентификатор устройства</strong>, дата подключения, сколько времени оно передавало данные и тип телефона (iPhone,
            Android, другой).
          </li>
          <li>
            <strong>Записи класса (только с кодом класса).</strong> Если вы подключились с кодом от учителя, данные движения телефона
            постоянно записываются, пока открыта страница, для исследовательского проекта класса, под псевдонимом. Хранятся до 365 дней;
            удаляются вместе с устройством. Участникам младше 18 лет нужно согласие родителей или опекунов.
          </li>
          <li>
            <strong>Расположение и отметки (только если вы их заполнили).</strong> Где лежит телефон (этаж, поверхность, тип здания) и моменты,
            которые вы отметили («дверь», «шаги» ...), со временем. Никогда не адрес. Удаляются вместе с устройством.
          </li>
          <li>
            <strong>Оповещения в Telegram (только если вы подписались).</strong> Ваш ID чата в Telegram, квадрат 10 км для оповещений и язык.
            Местоположение, отправленное боту, сразу превращается в этот квадрат и удаляется. Если вы подключили Telegram на странице телефона,
            бот также знает, какой телефон ваш, — только чтобы сообщить, когда он перестал слушать (днём, не чаще раза в 6 часов). Команда /stop
            в боте удаляет данные.
          </li>
          <li>
            <strong>Ваше имя (только если вы его добавили).</strong> Показывается в публичном списке участников и в благодарностях научной работы,
            когда телефон проработал час. Хранится отдельно от записей движения и никогда в них не попадает. Изменить или удалить имя можно на
            странице телефона в любой момент; выход из сети удаляет его. Если вы подключились по ссылке друга, мы отмечаем, кто вас пригласил,
            чтобы засчитать это ему; больше ничего о вас ему не сообщается.
          </li>
          <li>
            <strong>Ночная команда (только если вы записались после теста телефона).</strong> Ваш username в Telegram, тип телефона, возрастная
            группа и язык — только чтобы добавить вас в группу участников. Хранится отдельно от ответов теста и удаляется после исследования.
          </li>
        </ul>
        <h2>Не собирается</h2>
        <ul>
          <li>Почта, номер телефона, аккаунт, контакты, фото, микрофон и камера. Имя — только если вы сами его добавили.</li>
          <li>Точное местоположение. Если вы разрешили геолокацию, телефон округляет её и отбрасывает координаты.</li>
          <li>
            IP-адрес не хранится. Он используется в памяти для ограничения частоты запросов и превращается в хэш сетевого префикса с ключом,
            который меняется каждый день, поэтому его нельзя связать между днями или восстановить. Детектор считает по нему, из скольких разных
            сетей пришло событие.
          </li>
        </ul>
        <h2>Что публично</h2>
        <ul>
          <li>Карты показывают телефоны квадратами по 10 км. Квадраты, где меньше трёх телефонов, скрыты.</li>
          <li>На страницах событий — участки с той же точностью 10 км и движение, усреднённое по расстоянию. Идентификаторы устройств не показываются.</li>
          <li>Исследовательские наборы данных не содержат идентификаторов устройств и координат точнее 1 км.</li>
        </ul>
        <h2>Ваш контроль</h2>
        <ul>
          <li>Остановить передачу можно в любой момент: закройте страницу или нажмите «Остановить».</li>
          <li>
            На <Link href="/device">странице вашего телефона</Link> можно удалить всё: идентификатор, статистику, срабатывания и записи. В прошлых
            событиях остаются только анонимные итоги.
          </li>
          <li>Устройства, которые не подключались 180 дней, удаляются автоматически.</li>
        </ul>
        <h2>Использование в исследованиях</h2>
        <p>
          Данные используются для разработки и проверки метода обнаружения и могут публиковаться в научных работах в обезличенном и
          обобщённом виде. NE-PULSE не является официальной службой оповещения.
        </p>
      </div>
    </>
  );
}

export default function Privacy() {
  const { lang } = useT();
  return <main className="mx-auto max-w-[760px] px-4 pt-10 sm:px-6">{lang === "ru" ? <RU /> : <EN />}</main>;
}
