// NE-PULSE fixed station firmware: an ESP32 reads an MPU-6050 at 100 Hz and
// streams the raw accelerometer to the NE-PULSE network over the same
// WebSocket protocol the phones use (docs/API.md). Detection runs on the
// server; the board only samples, timestamps and ships.
//
// First boot: registers itself (proof of work, like a phone) and stores its
// credentials in flash. Location: computes its own 1 km grid cell from
// STATION_LAT/LNG and sends only the cell.
//
// Status LED (GPIO 2): fast blink = no Wi-Fi/server, slow blink = streaming,
// solid = the server reports motion (triggered).

#include <Arduino.h>
#include <ArduinoJson.h>
#include <HTTPClient.h>
#include <Preferences.h>
#include <WebSocketsClient.h>
#include <WiFi.h>
#include <WiFiClientSecure.h>
#include <Wire.h>
#include <esp_timer.h>
#include <mbedtls/sha256.h>
#include <sys/time.h>

#include <vector>

#if __has_include("config.h")
#include "config.h"
#else
#error "Copy src/config.example.h to src/config.h and fill in your Wi-Fi and station position."
#endif

static const char *FW = "esp32/1.0.0";
static const double G0 = 9.80665;
static const int LED = 2;

// ---------------------------------------------------------------- time ----
// Epoch milliseconds = monotonic microsecond timer + offset learned from NTP.
static double bootEpochMs = 0;
static double nowMs() { return esp_timer_get_time() / 1000.0 + bootEpochMs; }

static void syncNtp() {
  configTime(0, 0, "pool.ntp.org", "time.google.com", "time.cloudflare.com");
  for (int i = 0; i < 100 && time(nullptr) < 1700000000; i++) delay(100);
  struct timeval tv;
  gettimeofday(&tv, nullptr);
  bootEpochMs = tv.tv_sec * 1000.0 + tv.tv_usec / 1000.0 - esp_timer_get_time() / 1000.0;
}

// ------------------------------------------------------------- MPU-6050 ----
static const uint8_t MPU = 0x68;

static bool mpuWrite(uint8_t reg, uint8_t v) {
  Wire.beginTransmission(MPU);
  Wire.write(reg);
  Wire.write(v);
  return Wire.endTransmission() == 0;
}

static int mpuWhoAmI() {
  Wire.beginTransmission(MPU);
  Wire.write(0x75);
  if (Wire.endTransmission(false) != 0) return -1;
  if (Wire.requestFrom(MPU, (uint8_t)1) != 1) return -1;
  return Wire.read();
}

static bool mpuInit() {
  int who = mpuWhoAmI();
  if (who < 0) return false;
  Serial.printf("MPU WHO_AM_I = 0x%02X\n", who);  // 0x68 genuine; clones report 0x70/0x72/0x98
  return mpuWrite(0x6B, 0x01)     // wake, clock = PLL (gyro X)
         && mpuWrite(0x19, 9)     // sample rate 1 kHz / (1 + 9) = 100 Hz
         && mpuWrite(0x1A, 0x03)  // digital low-pass ~44 Hz (anti-alias for 100 Hz)
         && mpuWrite(0x1C, 0x00); // accelerometer +/-2 g: 16384 LSB/g
}

static bool mpuRead(int16_t &x, int16_t &y, int16_t &z) {
  Wire.beginTransmission(MPU);
  Wire.write(0x3B);
  if (Wire.endTransmission(false) != 0) return false;
  if (Wire.requestFrom(MPU, (uint8_t)6) != 6) return false;
  x = (Wire.read() << 8) | Wire.read();
  y = (Wire.read() << 8) | Wire.read();
  z = (Wire.read() << 8) | Wire.read();
  return true;
}

// --------------------------------------------------------- sample ring ----
struct Sample {
  double t;  // epoch ms
  int32_t x, y, z;  // mm/s^2
};
static const int RING = 4096;
static Sample ring[RING];
static volatile uint32_t head = 0, tail = 0;
static portMUX_TYPE ringMux = portMUX_INITIALIZER_UNLOCKED;
static volatile uint32_t readErrors = 0, overruns = 0;

static void samplerTask(void *) {
  TickType_t last = xTaskGetTickCount();
  const TickType_t period = pdMS_TO_TICKS(10);  // 100 Hz
  for (;;) {
    vTaskDelayUntil(&last, period);
    int16_t rx, ry, rz;
    if (!mpuRead(rx, ry, rz)) {
      readErrors++;
      continue;
    }
    Sample s;
    s.t = nowMs();
    const double k = G0 * 1000.0 / 16384.0;  // raw -> mm/s^2
    s.x = lround(rx * k);
    s.y = lround(ry * k);
    s.z = lround(rz * k);
    portENTER_CRITICAL(&ringMux);
    ring[head % RING] = s;
    head++;
    if (head - tail > RING) {
      tail = head - RING;
      overruns++;
    }
    portEXIT_CRITICAL(&ringMux);
  }
}

// ------------------------------------------------ grid (internal/geo) ----
// Identical arithmetic to internal/geo/geo.go and web/lib/sensor/grid.ts.
static const double EARTH_R = 6371.0088;
static const double KM_PER_DEG_LAT = 2 * M_PI * EARTH_R / 360.0;

static int32_t gridCols(int32_t row, double cellKm) {
  double dLat = cellKm / KM_PER_DEG_LAT;
  double latC = -90 + (row + 0.5) * dLat;
  double c = cos(latC * M_PI / 180);
  if (c < 1e-3) c = 1e-3;
  double n = round(360 * KM_PER_DEG_LAT * c / cellKm);
  return n < 1 ? 1 : (int32_t)n;
}

static void cellOf(double lat, double lng, double cellKm, int32_t &row, int32_t &col) {
  double dLat = cellKm / KM_PER_DEG_LAT;
  if (lat >= 90) lat = 89.999999;
  if (lat < -90) lat = -90;
  row = (int32_t)floor((lat + 90) / dLat);
  int32_t maxRow = (int32_t)ceil(180 / dLat) - 1;
  if (row > maxRow) row = maxRow;
  int32_t n = gridCols(row, cellKm);
  lng = fmod(fmod(lng + 540, 360) + 360, 360) - 180;
  col = (int32_t)floor((lng + 180) / (360.0 / n));
  if (col >= n) col = n - 1;
  if (col < 0) col = 0;
}

// ---------------------------------------------------------- enrolment ----
static Preferences prefs;
static String deviceId, secret;

static String baseUrl() { return String(API_TLS ? "https://" : "http://") + API_HOST + ":" + API_PORT; }

static int leadingZeroBits(const uint8_t *h) {
  int n = 0;
  for (int i = 0; i < 32; i++) {
    if (h[i] == 0) {
      n += 8;
      continue;
    }
    n += __builtin_clz((unsigned)h[i]) - 24;
    break;
  }
  return n;
}

static String solvePow(const String &challenge, int difficulty) {
  char buf[256];
  size_t pre = snprintf(buf, sizeof(buf), "%s:", challenge.c_str());
  uint8_t out[32];
  for (uint32_t i = 0;; i++) {
    size_t len = pre + snprintf(buf + pre, sizeof(buf) - pre, "%lu", (unsigned long)i);
    mbedtls_sha256((const unsigned char *)buf, len, out, 0);
    if (leadingZeroBits(out) >= difficulty) return String(i);
    if ((i & 0x3FFF) == 0) delay(1);  // feed the watchdog
  }
}

// TLS note: certificate checking is off (setInsecure) so the board works
// behind any CA without updates. The traffic is still encrypted; a station
// secret stolen by an attacker on the same Wi-Fi gives them one station,
// which is one vote at most. Pin a CA with setCACert() for stricter setups.
static bool httpJson(const char *method, const String &path, const String &body, int &status, JsonDocument &doc) {
  HTTPClient http;
  WiFiClientSecure tls;
  WiFiClient plain;
  if (API_TLS) {
    tls.setInsecure();
    http.begin(tls, baseUrl() + path);
  } else {
    http.begin(plain, baseUrl() + path);
  }
  http.addHeader("Content-Type", "application/json");
  http.setTimeout(15000);
  status = strcmp(method, "POST") == 0 ? http.POST(body) : http.GET();
  String resp = http.getString();
  http.end();
  return status > 0 && deserializeJson(doc, resp) == DeserializationError::Ok;
}

static bool enroll() {
  JsonDocument ch;
  int st;
  if (!httpJson("GET", "/api/v1/challenge", "", st, ch) || st != 200) {
    Serial.printf("challenge failed: HTTP %d\n", st);
    return false;
  }
  String challenge = ch["challenge"].as<String>();
  int diff = ch["difficulty"] | 18;
  Serial.printf("solving proof of work (difficulty %d)...\n", diff);
  uint32_t t0 = millis();
  String nonce = solvePow(challenge, diff);
  Serial.printf("solved in %lu ms\n", (unsigned long)(millis() - t0));
  JsonDocument req;
  req["challenge"] = challenge;
  req["nonce"] = nonce;
  req["platform"] = "esp32";
  req["consent"] = "v1";
  req["cohort"] = CLASS_CODE;
  String body;
  serializeJson(req, body);
  JsonDocument res;
  if (!httpJson("POST", "/api/v1/devices", body, st, res) || st != 201) {
    Serial.printf("enrolment failed: HTTP %d %s\n", st, res["error"] | "");
    return false;
  }
  deviceId = res["deviceId"].as<String>();
  secret = res["secret"].as<String>();
  prefs.putString("id", deviceId);
  prefs.putString("secret", secret);
  Serial.printf("registered as %s (class %s)\n", deviceId.c_str(), CLASS_CODE);
  return true;
}

// ---------------------------------------------------------- streaming ----
static WebSocketsClient ws;
static bool welcomed = false;
static int32_t cellRow = 0, cellCol = 0;
static struct {
  double rtt, off;
} syncs[8];
static int nSyncs = 0;
static double clockOff = 0, clockRtt = -1;
static int64_t seq = 0;
static uint32_t acked = 0, sent = 0;
static String state = "connecting";

struct Batch {
  double t0;
  std::vector<int32_t> dt, x, y, z;
};
static std::vector<Batch> queue;  // oldest first, at most 30 s
static uint32_t nextSyncAt[3] = {0, 0, 0};

static void sendSync() {
  char b[80];
  snprintf(b, sizeof(b), "{\"type\":\"sync\",\"c0\":%.1f}", nowMs());
  ws.sendTXT(b);
}

static void onWs(WStype_t type, uint8_t *payload, size_t len) {
  switch (type) {
    case WStype_CONNECTED: {
      JsonDocument h;
      h["type"] = "hello";
      h["deviceId"] = deviceId;
      h["secret"] = secret;
      h["platform"] = "esp32";
      h["client"] = FW;
      JsonObject loc = h["location"].to<JsonObject>();
      loc["grid"] = 1;
      loc["row"] = cellRow;
      loc["col"] = cellCol;
      String out;
      serializeJson(h, out);
      ws.sendTXT(out);
      break;
    }
    case WStype_DISCONNECTED:
      welcomed = false;
      state = "reconnecting";
      break;
    case WStype_TEXT: {
      JsonDocument m;
      if (deserializeJson(m, payload, len) != DeserializationError::Ok) return;
      const char *t = m["type"] | "";
      if (!strcmp(t, "welcome")) {
        welcomed = true;
        nSyncs = 0;
        uint32_t now = millis();
        nextSyncAt[0] = now + 1;
        nextSyncAt[1] = now + 300;
        nextSyncAt[2] = now + 700;
      } else if (!strcmp(t, "sync")) {
        double c0 = m["c0"], s = m["s"], c2 = nowMs();
        double rtt = c2 - c0, off = s - (c0 + c2) / 2;
        if (nSyncs < 8) {
          syncs[nSyncs++] = {rtt, off};
        } else {
          memmove(syncs, syncs + 1, sizeof(syncs[0]) * 7);
          syncs[7] = {rtt, off};
        }
        int best = 0;
        for (int i = 1; i < nSyncs; i++)
          if (syncs[i].rtt < syncs[best].rtt) best = i;
        clockOff = syncs[best].off;
        clockRtt = syncs[best].rtt;
      } else if (!strcmp(t, "ack")) {
        acked++;
      } else if (!strcmp(t, "status")) {
        state = m["state"] | "unknown";
      } else if (!strcmp(t, "error")) {
        const char *code = m["code"] | "";
        Serial.printf("server error: %s %s\n", code, m["message"] | "");
        if (!strcmp(code, "auth")) {  // deleted on the server: register again
          prefs.remove("id");
          prefs.remove("secret");
          delay(1000);
          ESP.restart();
        }
      }
      break;
    }
    default:
      break;
  }
}

static void collectBatch() {
  Batch b;
  portENTER_CRITICAL(&ringMux);
  uint32_t h = head, t = tail;
  portEXIT_CRITICAL(&ringMux);
  uint32_t n = h - t;
  if (n == 0) return;
  if (n > 300) n = 300;
  for (uint32_t i = 0; i < n; i++) {
    Sample s = ring[(t + i) % RING];
    if (i == 0) b.t0 = s.t;
    b.dt.push_back(lround((s.t - b.t0) * 10));  // 0.1 ms units
    b.x.push_back(s.x);
    b.y.push_back(s.y);
    b.z.push_back(s.z);
  }
  portENTER_CRITICAL(&ringMux);
  tail = t + n;
  portEXIT_CRITICAL(&ringMux);
  queue.push_back(std::move(b));
  if (queue.size() > 30) queue.erase(queue.begin());
}

static void appendArr(String &s, const char *name, const std::vector<int32_t> &v) {
  s += ",\"";
  s += name;
  s += "\":[";
  for (size_t i = 0; i < v.size(); i++) {
    if (i) s += ',';
    s += v[i];
  }
  s += ']';
}

static void drain() {
  if (!welcomed || clockRtt < 0) return;
  while (!queue.empty()) {
    Batch &b = queue.front();
    String s;
    s.reserve(64 + b.dt.size() * 26);
    char head[96];
    snprintf(head, sizeof(head), "{\"type\":\"batch\",\"seq\":%lld,\"t0\":%.1f", (long long)(seq + 1), b.t0);
    s += head;
    appendArr(s, "dt", b.dt);
    appendArr(s, "x", b.x);
    appendArr(s, "y", b.y);
    appendArr(s, "z", b.z);
    char tail[80];
    snprintf(tail, sizeof(tail), ",\"clock\":{\"offset\":%.1f,\"rtt\":%.1f}}", clockOff, clockRtt);
    s += tail;
    if (!ws.sendTXT(s)) break;
    seq++;
    sent++;
    queue.erase(queue.begin());
  }
}

// ------------------------------------------------------------ main ----
static void connectWifi() {
  if (WiFi.status() == WL_CONNECTED) return;
  WiFi.mode(WIFI_STA);
  WiFi.setSleep(false);  // steady latency
  WiFi.begin(WIFI_SSID, WIFI_PASS);
  Serial.printf("Wi-Fi %s", WIFI_SSID);
  for (int i = 0; i < 60 && WiFi.status() != WL_CONNECTED; i++) {
    delay(500);
    Serial.print('.');
    digitalWrite(LED, i & 1);
  }
  Serial.println(WiFi.status() == WL_CONNECTED ? " connected" : " failed");
}

void setup() {
  Serial.begin(115200);
  pinMode(LED, OUTPUT);
  Wire.begin(PIN_SDA, PIN_SCL, 400000);
  if (!mpuInit()) {
    Serial.println("MPU-6050 not found: check wiring (VCC 3V3, GND, SDA, SCL).");
    for (;;) {
      digitalWrite(LED, !digitalRead(LED));
      delay(100);
    }
  }
  connectWifi();
  syncNtp();
  cellOf(STATION_LAT, STATION_LNG, 1.0, cellRow, cellCol);
  Serial.printf("1 km cell %ld:%ld\n", (long)cellRow, (long)cellCol);

  prefs.begin("nepulse", false);
  deviceId = prefs.getString("id", "");
  secret = prefs.getString("secret", "");
  while (deviceId.isEmpty() || secret.isEmpty()) {
    if (!enroll()) delay(30000);
  }

  xTaskCreatePinnedToCore(samplerTask, "sampler", 4096, nullptr, 3, nullptr, 1);

  if (API_TLS)
    ws.beginSSL(API_HOST, API_PORT, "/ws/device");
  else
    ws.begin(API_HOST, API_PORT, "/ws/device");
  ws.onEvent(onWs);
  ws.setReconnectInterval(5000);
}

void loop() {
  // NTP only anchors the clock at boot. Crystal drift (~20 ppm) is tracked
  // by the server sync every 30 s, whose offset travels with every batch;
  // re-stepping the clock here could make sample times jump backwards.
  static uint32_t lastBatch = 0, lastSync = 0, lastLog = 0;
  uint32_t now = millis();
  if (WiFi.status() != WL_CONNECTED) connectWifi();
  ws.loop();
  for (auto &at : nextSyncAt) {
    if (at && (int32_t)(now - at) >= 0) {
      at = 0;
      if (welcomed) sendSync();
    }
  }
  if (now - lastBatch >= 1000) {
    lastBatch = now;
    collectBatch();
    drain();
  }
  if (welcomed && now - lastSync >= 30000) {
    lastSync = now;
    sendSync();
  }
  // LED: solid when triggered, slow blink when streaming, fast otherwise.
  bool streaming = welcomed && clockRtt >= 0;
  if (state == "triggered")
    digitalWrite(LED, HIGH);
  else
    digitalWrite(LED, ((now / (streaming ? 1000 : 150)) & 1) ? HIGH : LOW);
  if (now - lastLog >= 10000) {
    lastLog = now;
    Serial.printf("state=%s sent=%lu acked=%lu queued=%u offset=%.0f ms rtt=%.0f ms i2c_errors=%lu overruns=%lu\n", state.c_str(),
                  (unsigned long)sent, (unsigned long)acked, (unsigned)queue.size(), clockOff, clockRtt, (unsigned long)readErrors,
                  (unsigned long)overruns);
  }
  delay(2);
}
