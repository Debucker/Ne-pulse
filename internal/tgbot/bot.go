// Package tgbot is the NE-PULSE Telegram bot: anyone can subscribe to alerts
// for their area, and when the network reaches the alert level every
// subscriber within range gets a personal message with the honest time left
// before strong shaking (the S wave) reaches their area — or the statement
// that it already has.
//
// Privacy: the bot keeps the chat id (needed to deliver messages), a 10 km
// public grid area and a language. A shared location is converted to its
// area immediately and discarded. /stop deletes everything.
//
// The bot long-polls getUpdates, so it needs no public webhook URL and runs
// fine behind NAT, tunnels or a laptop.
package tgbot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // Asia/Tashkent on hosts without a zoneinfo database

	"ne-pulse/internal/alert"
	"ne-pulse/internal/geo"
	"ne-pulse/internal/network"
	"ne-pulse/internal/store"
)

// Store is the subset of the store the bot needs.
type Store interface {
	UpsertSubscriber(ctx context.Context, sub store.Subscriber) error
	SetSubscriberLang(ctx context.Context, chatID int64, lang string, now float64) error
	DeleteSubscriber(ctx context.Context, chatID int64) error
	GetSubscriber(ctx context.Context, chatID int64) (store.Subscriber, bool, error)
	Subscribers(ctx context.Context) ([]store.Subscriber, error)
}

// Devices lets the bot link a chat to a participant's phone (website deep
// link /start d<code>) and notice when that phone stops listening.
type Devices interface {
	ClaimTelegramLink(code string) (device string, lat, lng float64, ok bool)
	DeviceSeen(id string) (connected bool, lastSeen float64, exists bool)
	// NightSummary: how long the phone listened in a night and its participation code.
	NightSummary(device, night string) (hours float64, code string, eligible bool, minHours float64)
}

// Bot is both the update loop and an alert.Channel.
type Bot struct {
	Token    string
	API      string // default https://api.telegram.org
	Client   *http.Client
	Store    Store
	Min      network.Level
	BaseURL  string  // public site, for links
	RadiusKM float64 // subscribers farther than this are not alerted
	DepthKM  float64
	VsKMS    float64
	Loc      *time.Location
	PerSec   float64 // delivery rate limit (Telegram allows ~30 msg/s per bot)
	Now      func() time.Time

	Area geo.Grid // public area grid (10 km)

	// Devices, if set, enables phone linking and "stopped listening" reminders.
	Devices Devices
	// StoreNudge records when a reminder went out; nil disables reminders.
	StoreNudge func(ctx context.Context, chatID int64, at float64) error
	// StoreMorning records which night's summary a chat received; nil disables it.
	StoreMorning func(ctx context.Context, chatID int64, night string) error

	mu       sync.Mutex
	lastTest map[int64]time.Time
}

// New returns a bot with the network's defaults.
func New(token string, st Store, min network.Level, baseURL string, ncfg network.Config) *Bot {
	loc, err := time.LoadLocation("Asia/Tashkent")
	if err != nil {
		loc = time.UTC
	}
	return &Bot{
		Token: token, Store: st, Min: min, BaseURL: strings.TrimRight(baseURL, "/"),
		RadiusKM: 250, DepthKM: ncfg.DepthKM, VsKMS: ncfg.VsKMS, Loc: loc, PerSec: 25,
		Area: geo.Grid{CellKM: 10}, lastTest: map[int64]time.Time{},
	}
}

func (b *Bot) Name() string            { return "tgbot" }
func (b *Bot) MinLevel() network.Level { return b.Min }

func (b *Bot) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// ---- Telegram API ----

type apiResp struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// errBlocked means the user blocked the bot or deleted the chat.
var errBlocked = errors.New("tgbot: chat unavailable")

func (b *Bot) call(ctx context.Context, method string, payload any, out any) error {
	api := b.API
	if api == "" {
		api = "https://api.telegram.org"
	}
	body, _ := json.Marshal(payload)
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, api+"/bot"+b.Token+"/"+method, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		c := b.Client
		if c == nil {
			c = &http.Client{Timeout: 70 * time.Second}
		}
		resp, err := c.Do(req)
		if err != nil {
			return err
		}
		var r apiResp
		err = json.NewDecoder(resp.Body).Decode(&r)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("tgbot: %s: HTTP %d: %w", method, resp.StatusCode, err)
		}
		if r.OK {
			if out != nil {
				return json.Unmarshal(r.Result, out)
			}
			return nil
		}
		if r.ErrorCode == 429 && attempt < 3 {
			wait := time.Duration(max(1, r.Parameters.RetryAfter)) * time.Second
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if r.ErrorCode == 403 || (r.ErrorCode == 400 && strings.Contains(r.Description, "chat not found")) {
			return errBlocked
		}
		return fmt.Errorf("tgbot: %s: %d %s", method, r.ErrorCode, r.Description)
	}
}

func (b *Bot) send(ctx context.Context, chatID int64, text string, markup any) error {
	p := map[string]any{"chat_id": chatID, "text": text, "parse_mode": "HTML", "disable_web_page_preview": true}
	if markup != nil {
		p["reply_markup"] = markup
	}
	return b.call(ctx, "sendMessage", p, nil)
}

// Username asks Telegram for the bot's @username (for deep links).
func (b *Bot) Username(ctx context.Context) (string, error) {
	var me struct {
		Username string `json:"username"`
	}
	if err := b.call(ctx, "getMe", map[string]any{}, &me); err != nil {
		return "", err
	}
	return me.Username, nil
}

// ---- update loop ----

type update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
		From *struct {
			LanguageCode string `json:"language_code"`
		} `json:"from"`
		Text     string `json:"text"`
		Location *struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"location"`
	} `json:"message"`
}

// Run long-polls for updates until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) {
	if b.Devices != nil && b.StoreNudge != nil {
		go b.remindLoop(ctx)
	}
	var offset int64
	backoff := time.Second
	for ctx.Err() == nil {
		var ups []update
		err := b.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 50, "allowed_updates": []string{"message"}}, &ups)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("tgbot: getUpdates: %v", err)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			}
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = time.Second
		for _, u := range ups {
			offset = u.UpdateID + 1
			if u.Message == nil || u.Message.Chat.Type != "private" {
				continue
			}
			hctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			if err := b.handle(hctx, u); err != nil {
				log.Printf("tgbot: update %d: %v", u.UpdateID, err)
			}
			cancel()
		}
	}
}

func langOf(code string) string {
	switch strings.ToLower(strings.SplitN(code, "-", 2)[0]) {
	case "ru", "uz", "kk", "ky", "tg", "be", "uk", "az":
		return "ru"
	}
	return "en"
}

func (b *Bot) handle(ctx context.Context, u update) error {
	m := u.Message
	chat := m.Chat.ID
	now := float64(b.now().Unix())
	sub, have, err := b.Store.GetSubscriber(ctx, chat)
	if err != nil {
		return err
	}
	lang := sub.Lang
	if !have {
		lang = "en"
		if m.From != nil {
			lang = langOf(m.From.LanguageCode)
		}
	}
	tr := text[lang]

	if m.Location != nil {
		c := b.Area.CellOf(m.Location.Latitude, m.Location.Longitude)
		return b.subscribe(ctx, chat, c, lang, sub, have, now)
	}
	cmd, arg, _ := strings.Cut(strings.TrimSpace(m.Text), " ")
	cmd = strings.ToLower(strings.SplitN(cmd, "@", 2)[0])
	switch cmd {
	case "/start":
		arg = strings.TrimSpace(arg)
		if strings.HasPrefix(arg, "d") && b.Devices != nil {
			dev, lat, lng, ok := b.Devices.ClaimTelegramLink(arg[1:])
			if !ok {
				return b.send(ctx, chat, tr.linkExpired, nil)
			}
			c := b.Area.CellOf(lat, lng)
			created := now
			if have {
				created = sub.Created
			}
			if err := b.Store.UpsertSubscriber(ctx, store.Subscriber{ChatID: chat, Row: c.Row, Col: c.Col, Lang: lang, Created: created, Updated: now, DeviceID: dev}); err != nil {
				return err
			}
			alat, alng := b.Area.Center(c)
			return b.send(ctx, chat, fmt.Sprintf(tr.linked, alat, alng, b.RadiusKM), map[string]any{"remove_keyboard": true})
		}
		if c, ok := b.parseArea(arg); ok {
			return b.subscribe(ctx, chat, c, lang, sub, have, now)
		}
		return b.send(ctx, chat, tr.welcome, locationKeyboard(tr))
	case "/stop":
		if err := b.Store.DeleteSubscriber(ctx, chat); err != nil {
			return err
		}
		return b.send(ctx, chat, tr.stopped, map[string]any{"remove_keyboard": true})
	case "/status":
		if !have {
			return b.send(ctx, chat, tr.notSubscribed, locationKeyboard(tr))
		}
		lat, lng := b.Area.Center(geo.Cell{Row: sub.Row, Col: sub.Col})
		return b.send(ctx, chat, fmt.Sprintf(tr.status, lat, lng, b.RadiusKM), nil)
	case "/lang":
		nl := "ru"
		if lang == "ru" {
			nl = "en"
		}
		if have {
			if err := b.Store.SetSubscriberLang(ctx, chat, nl, now); err != nil {
				return err
			}
		}
		return b.send(ctx, chat, text[nl].langSet, nil)
	case "/test":
		if !have {
			return b.send(ctx, chat, tr.notSubscribed, locationKeyboard(tr))
		}
		b.mu.Lock()
		last := b.lastTest[chat]
		ok := b.now().Sub(last) > time.Minute
		if ok {
			b.lastTest[chat] = b.now()
		}
		b.mu.Unlock()
		if !ok {
			return nil
		}
		// A drill: an imaginary quake 120 km away that happened 25 s ago.
		lat, lng := b.Area.Center(geo.Cell{Row: sub.Row, Col: sub.Col})
		elat, elng := geo.Destination(lat, lng, 90, 120)
		m := alert.Message{EventID: "TEST", Level: network.LevelConfirmed, Lat: elat, Lng: elng, OriginT: float64(b.now().Unix()) - 25}
		return b.send(ctx, chat, tr.testPrefix+"\n\n"+b.compose(m, sub, b.now()), nil)
	default:
		if have {
			return b.send(ctx, chat, tr.help, nil)
		}
		return b.send(ctx, chat, tr.welcome, locationKeyboard(tr))
	}
}

// parseArea accepts "a<row>x<col>" (10 km public grid) from website deep links.
func (b *Bot) parseArea(s string) (geo.Cell, bool) {
	if !strings.HasPrefix(s, "a") {
		return geo.Cell{}, false
	}
	rs, cs, ok := strings.Cut(s[1:], "x")
	if !ok {
		return geo.Cell{}, false
	}
	r, err1 := strconv.ParseInt(rs, 10, 32)
	c, err2 := strconv.ParseInt(cs, 10, 32)
	if err1 != nil || err2 != nil {
		return geo.Cell{}, false
	}
	cell := geo.Cell{Row: int32(r), Col: int32(c)}
	return cell, b.Area.Valid(cell)
}

func (b *Bot) subscribe(ctx context.Context, chat int64, c geo.Cell, lang string, prev store.Subscriber, had bool, now float64) error {
	created := now
	if had {
		created = prev.Created
	}
	if err := b.Store.UpsertSubscriber(ctx, store.Subscriber{ChatID: chat, Row: c.Row, Col: c.Col, Lang: lang, Created: created, Updated: now}); err != nil {
		return err
	}
	lat, lng := b.Area.Center(c)
	return b.send(ctx, chat, fmt.Sprintf(text[lang].subscribed, lat, lng, b.RadiusKM), map[string]any{"remove_keyboard": true})
}

func locationKeyboard(tr strs) any {
	return map[string]any{
		"keyboard":          [][]map[string]any{{{"text": tr.shareButton, "request_location": true}}},
		"resize_keyboard":   true,
		"one_time_keyboard": true,
	}
}

// ---- alerts ----

type target struct {
	sub   store.Subscriber
	dist  float64
	sWave float64 // unix time the S wave reaches the area centre
}

// Send is the alert.Channel hook: one personal message per subscriber in
// range, those with the least (but still positive) warning time first.
func (b *Bot) Send(ctx context.Context, m alert.Message) error {
	subs, err := b.Store.Subscribers(ctx)
	if err != nil {
		return err
	}
	var ts []target
	for _, s := range subs {
		lat, lng := b.Area.Center(geo.Cell{Row: s.Row, Col: s.Col})
		d := geo.HaversineKM(lat, lng, m.Lat, m.Lng)
		if d > b.RadiusKM {
			continue
		}
		ts = append(ts, target{sub: s, dist: d, sWave: m.OriginT + math.Hypot(d, b.DepthKM)/b.VsKMS})
	}
	now := float64(b.now().UnixNano()) / 1e9
	sort.SliceStable(ts, func(i, j int) bool {
		li, lj := ts[i].sWave-now, ts[j].sWave-now
		if (li > 0) != (lj > 0) {
			return li > 0
		}
		return li < lj
	})
	// Delivery outlives the dispatcher's short context: a big list at
	// PerSec messages per second needs longer.
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(float64(len(ts))/b.PerSec+30)*time.Second)
	defer cancel()
	tick := time.NewTicker(time.Duration(float64(time.Second) / b.PerSec))
	defer tick.Stop()
	var failed int
	for i, t := range ts {
		if i > 0 {
			select {
			case <-tick.C:
			case <-dctx.Done():
				return fmt.Errorf("tgbot: delivery stopped after %d of %d: %w", i, len(ts), dctx.Err())
			}
		}
		err := b.send(dctx, t.sub.ChatID, b.compose(m, t.sub, b.now()), nil)
		if errors.Is(err, errBlocked) {
			_ = b.Store.DeleteSubscriber(dctx, t.sub.ChatID)
			continue
		}
		if err != nil {
			failed++
			log.Printf("tgbot: alert %s to a subscriber: %v", m.EventID, err)
		}
	}
	log.Printf("tgbot: alert %s delivered to %d subscribers (%d failed, %d in range of %d)", m.EventID, len(ts)-failed, failed, len(ts), len(subs))
	if failed > 0 && failed == len(ts) {
		return fmt.Errorf("tgbot: all %d deliveries failed", failed)
	}
	return nil
}

// compose builds one subscriber's message. The warning time is computed at
// the moment of sending, from the area centre's hypocentral distance.
func (b *Bot) compose(m alert.Message, s store.Subscriber, at time.Time) string {
	tr := text[s.Lang]
	if tr.title == "" {
		tr = text["en"]
	}
	lat, lng := b.Area.Center(geo.Cell{Row: s.Row, Col: s.Col})
	d := geo.HaversineKM(lat, lng, m.Lat, m.Lng)
	lead := m.OriginT + math.Hypot(d, b.DepthKM)/b.VsKMS - float64(at.UnixNano())/1e9
	var sb strings.Builder
	sb.WriteString("<b>" + tr.title + "</b>\n")
	origin := time.Unix(int64(m.OriginT), 0).In(b.Loc).Format("15:04:05")
	sb.WriteString(fmt.Sprintf(tr.where, math.Round(d), origin) + "\n\n")
	if lead >= 3 {
		sb.WriteString("⏱ <b>" + fmt.Sprintf(tr.lead, math.Floor(lead)) + "</b>\n")
	} else {
		sb.WriteString(tr.passed + "\n")
	}
	sb.WriteString(tr.unknown + "\n\n")
	sb.WriteString("<i>" + tr.disclaimer + "</i>")
	if b.BaseURL != "" && m.EventID != "TEST" {
		sb.WriteString("\n" + html.EscapeString(b.BaseURL+"/event?id="+url.QueryEscape(m.EventID)))
	}
	return sb.String()
}

// ---- texts ----

type strs struct {
	welcome, shareButton, subscribed, stopped, notSubscribed, status, help, langSet, testPrefix string
	linked, linkExpired, stoppedListening, morningOK, morningShort                              string
	title, where, lead, passed, unknown, disclaimer                                             string
}

var text = map[string]strs{
	"en": {
		welcome: "<b>NE-PULSE earthquake alerts</b>\nA research network of phones in Uzbekistan. Share your location once and I will message you when the network confirms an earthquake near you.\n\n" +
			"Only a 10 km area is kept; your exact position is discarded immediately. /stop deletes everything.",
		shareButton:      "Share my location",
		subscribed:       "<b>Subscribed.</b> Your area: near %.1f°, %.1f° (10 km square).\nI will message you when the network confirms an earthquake within %.0f km.\n\n/test — send a drill message\n/status — your subscription\n/lang — Русский\n/stop — unsubscribe and delete",
		stopped:          "Unsubscribed. Your data is deleted.",
		notSubscribed:    "You are not subscribed. Share your location to get alerts.",
		status:           "Subscribed. Area: near %.1f°, %.1f°. Alerts for confirmed earthquakes within %.0f km.",
		help:             "/test — drill message\n/status — your subscription\n/lang — Русский\n/stop — unsubscribe",
		langSet:          "Language: English.",
		testPrefix:       "<b>TEST · this is a drill, not a real earthquake.</b>",
		linked:           "<b>Phone linked.</b> Area: near %.1f°, %.1f° (10 km square).\nI will message you when the network confirms an earthquake within %.0f km, and if your phone stops listening.\n\n/test — drill message\n/status — your subscription\n/stop — unsubscribe and delete",
		morningOK:        "<b>Your night of %s</b>\nYour phone listened %.1f h: a full research night. Thank you!\n\nEveryone who took part: %s/contributors\nYour night chart and invite link: %s/device",
		morningShort:     "<b>Your night of %s</b>\nYour phone listened %.1f h; a full night needs %.0f h.\nTonight: charger in, phone flat on a table, NE-PULSE open, tap Dim screen.\n%s/device",
		linkExpired:      "This link has expired or was already used. Open NE-PULSE on your phone and tap “Connect Telegram” again.",
		stoppedListening: "<b>Your phone stopped listening</b> about %d min ago. Open NE-PULSE, tap Start, put the phone flat on a table and plug it in.\n%s/device",
		title:            "NE-PULSE · EARTHQUAKE CONFIRMED by the phone network",
		where:            "Epicentre about %.0f km from your area. Started %s (Tashkent time).",
		lead:             "Strong shaking may reach your area in about %.0f s.",
		passed:           "Strong shaking has probably already reached your area.",
		unknown:          "How strong it is where you are is unknown — the network does not estimate magnitude. If it shakes: drop, cover, hold on.",
		disclaimer:       "Experimental research network, not an official warning. Follow the Ministry of Emergency Situations.",
	},
	"ru": {
		welcome: "<b>NE-PULSE: оповещения о землетрясениях</b>\nИсследовательская сеть телефонов в Узбекистане. Отправьте местоположение один раз — я напишу, когда сеть подтвердит землетрясение рядом с вами.\n\n" +
			"Хранится только квадрат 10 км; точные координаты сразу удаляются. /stop удаляет всё.",
		shareButton:      "Отправить местоположение",
		subscribed:       "<b>Подписка оформлена.</b> Ваш район: около %.1f°, %.1f° (квадрат 10 км).\nЯ напишу, когда сеть подтвердит землетрясение в пределах %.0f км.\n\n/test — учебное сообщение\n/status — ваша подписка\n/lang — English\n/stop — отписаться и удалить данные",
		stopped:          "Подписка отменена. Ваши данные удалены.",
		notSubscribed:    "Вы не подписаны. Отправьте местоположение, чтобы получать оповещения.",
		status:           "Подписка активна. Район: около %.1f°, %.1f°. Оповещения о подтверждённых землетрясениях в пределах %.0f км.",
		help:             "/test — учебное сообщение\n/status — ваша подписка\n/lang — English\n/stop — отписаться",
		langSet:          "Язык: русский.",
		testPrefix:       "<b>ТЕСТ · это учебная тревога, не настоящее землетрясение.</b>",
		linked:           "<b>Телефон привязан.</b> Ваш район: около %.1f°, %.1f° (квадрат 10 км).\nЯ напишу, когда сеть подтвердит землетрясение в пределах %.0f км, и если ваш телефон перестанет слушать.\n\n/test — учебное сообщение\n/status — ваша подписка\n/stop — отписаться и удалить данные",
		morningOK:        "<b>Ваша ночь %s</b>\nТелефон слушал %.1f ч: полная исследовательская ночь. Спасибо!\n\nВсе участники: %s/contributors\nГрафик ночи и ссылка-приглашение: %s/device",
		morningShort:     "<b>Ваша ночь %s</b>\nТелефон слушал %.1f ч; для полной ночи нужно %.0f ч.\nСегодня: зарядка, телефон ровно на столе, NE-PULSE открыт, «Затемнить экран».\n%s/device",
		linkExpired:      "Ссылка устарела или уже использована. Откройте NE-PULSE на телефоне и снова нажмите «Подключить Telegram».",
		stoppedListening: "<b>Ваш телефон перестал слушать</b> примерно %d мин назад. Откройте NE-PULSE, нажмите «Старт», положите телефон на стол и поставьте на зарядку.\n%s/device",
		title:            "NE-PULSE · ЗЕМЛЕТРЯСЕНИЕ: сеть телефонов подтвердила",
		where:            "Эпицентр примерно в %.0f км от вашего района. Начало в %s (по Ташкенту).",
		lead:             "Сильная тряска может дойти до вашего района примерно через %.0f с.",
		passed:           "Сильная тряска, вероятно, уже дошла до вашего района.",
		unknown:          "Сила тряски у вас неизвестна — сеть не оценивает магнитуду. Если трясёт: пригнитесь, укройтесь, держитесь.",
		disclaimer:       "Экспериментальная исследовательская сеть, не официальное оповещение. Следуйте указаниям МЧС.",
	},
}

// remindLoop tells a person, once per gap, that their linked phone stopped
// listening: only for phones seen in the last day, after 20 minutes of
// silence, between 08:00 and 21:59 Tashkent time, and at most every 6 hours.
func (b *Bot) remindLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		b.remindOnce(ctx)
		b.morningOnce(ctx)
	}
}

// morningOnce sends each linked person, once, between 08:00 and 11:59, how
// long their phone listened last night, with a thank-you for a full night.
func (b *Bot) morningOnce(ctx context.Context) {
	if b.StoreMorning == nil {
		return
	}
	now := b.now().In(b.Loc)
	if h := now.Hour(); h < 8 || h >= 12 {
		return
	}
	night := now.Add(-12 * time.Hour).Format("2006-01-02")
	subs, err := b.Store.Subscribers(ctx)
	if err != nil {
		return
	}
	for _, s := range subs {
		if s.DeviceID == "" || s.Morning == night {
			continue
		}
		hours, _, ok, min := b.Devices.NightSummary(s.DeviceID, night)
		if hours < 0.1 {
			continue
		}
		tr := text[s.Lang]
		if tr.morningOK == "" {
			tr = text["en"]
		}
		date, _ := time.ParseInLocation("2006-01-02", night, b.Loc)
		day := date.Format("02.01")
		var msg string
		if ok {
			msg = fmt.Sprintf(tr.morningOK, day, hours, b.BaseURL, b.BaseURL)
		} else {
			msg = fmt.Sprintf(tr.morningShort, day, hours, min, b.BaseURL)
		}
		err := b.send(ctx, s.ChatID, msg, nil)
		if errors.Is(err, errBlocked) {
			b.Store.DeleteSubscriber(ctx, s.ChatID)
			continue
		}
		if err == nil {
			b.StoreMorning(ctx, s.ChatID, night)
		}
	}
}

// remindOnce sends the due "stopped listening" reminders.
func (b *Bot) remindOnce(ctx context.Context) {
	now := b.now()
	if h := now.In(b.Loc).Hour(); h < 8 || h >= 22 {
		return
	}
	subs, err := b.Store.Subscribers(ctx)
	if err != nil {
		return
	}
	for _, s := range subs {
		if s.DeviceID == "" {
			continue
		}
		conn, last, exists := b.Devices.DeviceSeen(s.DeviceID)
		gap := float64(now.Unix()) - last
		if !exists || conn || last <= 0 || gap < 20*60 || gap > 24*3600 || s.Nudged > last || float64(now.Unix())-s.Nudged < 6*3600 {
			continue
		}
		tr := text[s.Lang]
		if tr.stoppedListening == "" {
			tr = text["en"]
		}
		err := b.send(ctx, s.ChatID, fmt.Sprintf(tr.stoppedListening, int(gap/60), b.BaseURL), nil)
		if errors.Is(err, errBlocked) {
			b.Store.DeleteSubscriber(ctx, s.ChatID)
			continue
		}
		if err == nil {
			b.StoreNudge(ctx, s.ChatID, float64(now.Unix()))
		}
	}
}
