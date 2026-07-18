package ingress

import (
	"encoding/json"
	"net/http"
)

// docsSchemaField describes one field of the POST /api/ingress/hardware
// JSON body, for the public API reference served at GET /api/v1/docs.
type docsSchemaField struct {
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

// docsResponse is the full public reference payload for third-party
// hardware integrators.
type docsResponse struct {
	Endpoint      string                     `json:"endpoint"`
	Method        string                     `json:"method"`
	Auth          string                     `json:"auth"`
	Schema        map[string]docsSchemaField `json:"schema"`
	Esp32Template string                     `json:"esp32_template"`
}

// hardwareIngressSchema mirrors HardwareTelemetryPayload field-for-field —
// token is deliberately absent, since authentication is a header
// (X-API-Token), never a body field.
var hardwareIngressSchema = map[string]docsSchemaField{
	"deviceId": {
		Type:        "string",
		Required:    true,
		Description: "Unique identifier for this hardware node. Used by the coincidence detector to count distinct devices per H3 cell — reusing the same deviceId across physically different nodes undercounts real corroborating devices.",
	},
	"lat": {
		Type:        "number",
		Required:    true,
		Description: "Latitude in decimal degrees (WGS84).",
	},
	"lng": {
		Type:        "number",
		Required:    true,
		Description: "Longitude in decimal degrees (WGS84).",
	},
	"ax": {
		Type:        "number",
		Required:    true,
		Description: "Acceleration on the X axis, in m/s^2. Must fall within +/-5g (+/-49.03 m/s^2) or the reading is rejected.",
	},
	"ay": {
		Type:        "number",
		Required:    true,
		Description: "Acceleration on the Y axis, in m/s^2. Must fall within +/-5g (+/-49.03 m/s^2) or the reading is rejected.",
	},
	"az": {
		Type:        "number",
		Required:    true,
		Description: "Acceleration on the Z axis, in m/s^2. Must fall within +/-5g (+/-49.03 m/s^2) or the reading is rejected.",
	},
	"ts": {
		Type:        "integer",
		Required:    false,
		Description: "Unix millisecond timestamp. Omit this field entirely if your device has no reliable wall-clock (no RTC/NTP) — the server stamps its own receipt time instead. Sending a fabricated value (e.g. a microcontroller's boot-relative millis() counter) is worse than omitting it: it corrupts the coincidence detector's cross-device timing window.",
	},
}

// esp32Template is a complete, working Arduino/ESP32 sketch for streaming a
// physical MPU6050 rig into NE-PULSE. It is a Go raw string literal
// specifically so the C++ source needs no escaping of quotes or backslashes
// (C++ never uses backticks, so this delimiter is always safe here).
const esp32Template = `// NE-PULSE hardware node — ESP32 + MPU6050
//
// Streams live acceleration readings to a NE-PULSE backend over HTTPS.
// Wiring: MPU6050 SDA -> ESP32 GPIO21, SCL -> ESP32 GPIO22, VCC -> 3.3V, GND -> GND.

#include <WiFi.h>
#include <WiFiClientSecure.h>
#include <HTTPClient.h>
#include <Wire.h>

// --- WiFi credentials ---
const char* WIFI_SSID = "YOUR_WIFI_SSID";
const char* WIFI_PASSWORD = "YOUR_WIFI_PASSWORD";

// --- NE-PULSE ingress ---
const char* INGRESS_URL = "https://api.ne-pulse.com/api/ingress/hardware";
const char* API_TOKEN = "YOUR_API_TOKEN"; // provisioned by NE-PULSE; only required if the backend was started with -api-tokens set
const char* DEVICE_ID = "esp32-mpu6050-001"; // unique per physical node — do not reuse across devices

// --- This node's fixed install location (decimal degrees, WGS84) ---
const double NODE_LAT = 41.311081;
const double NODE_LNG = 69.240562;

// --- MPU6050 register map (bare I2C, no external accelerometer library needed) ---
const uint8_t MPU_ADDR = 0x68;
const double LSB_PER_G = 16384.0;   // +/-2g full-scale range, the MPU6050's power-on default
const double G_TO_MS2 = 9.80665;

void mpu6050Init() {
  Wire.begin();
  Wire.beginTransmission(MPU_ADDR);
  Wire.write(0x6B); // PWR_MGMT_1
  Wire.write(0x00); // wake the sensor from its power-on sleep state
  Wire.endTransmission(true);
}

// Reads the raw accelerometer registers and converts to m/s^2, the unit
// NE-PULSE's ingress schema requires.
void readAcceleration(double &ax, double &ay, double &az) {
  Wire.beginTransmission(MPU_ADDR);
  Wire.write(0x3B); // ACCEL_XOUT_H
  Wire.endTransmission(false);
  Wire.requestFrom(MPU_ADDR, (uint8_t)6, true);

  int16_t rawX = (Wire.read() << 8) | Wire.read();
  int16_t rawY = (Wire.read() << 8) | Wire.read();
  int16_t rawZ = (Wire.read() << 8) | Wire.read();

  ax = (rawX / LSB_PER_G) * G_TO_MS2;
  ay = (rawY / LSB_PER_G) * G_TO_MS2;
  az = (rawZ / LSB_PER_G) * G_TO_MS2;
}

void setup() {
  Serial.begin(115200);
  mpu6050Init();

  WiFi.begin(WIFI_SSID, WIFI_PASSWORD);
  Serial.print("Connecting to WiFi");
  while (WiFi.status() != WL_CONNECTED) {
    delay(500);
    Serial.print(".");
  }
  Serial.println();
  Serial.print("WiFi connected, IP: ");
  Serial.println(WiFi.localIP());
}

void loop() {
  if (WiFi.status() != WL_CONNECTED) {
    Serial.println("WiFi dropped -- reconnecting...");
    WiFi.reconnect();
    delay(1000);
    return;
  }

  double ax, ay, az;
  readAcceleration(ax, ay, az);

  // ts is deliberately omitted: this sketch has no RTC/NTP wall-clock, and
  // millis() is boot-relative, not Unix time -- sending it as "ts" would
  // silently corrupt the backend's coincidence-window timing. Omitting the
  // field lets the server stamp its own receipt time instead.
  String body = "{";
  body += "\"deviceId\":\"" + String(DEVICE_ID) + "\",";
  body += "\"lat\":" + String(NODE_LAT, 6) + ",";
  body += "\"lng\":" + String(NODE_LNG, 6) + ",";
  body += "\"ax\":" + String(ax, 4) + ",";
  body += "\"ay\":" + String(ay, 4) + ",";
  body += "\"az\":" + String(az, 4);
  body += "}";

  WiFiClientSecure client;
  client.setInsecure(); // Skips TLS certificate validation -- acceptable for
                        // a hobbyist/prototype rig hitting a known host, but
                        // pin the real NE-PULSE CA certificate before
                        // deploying this to production hardware.

  HTTPClient http;
  http.begin(client, INGRESS_URL);
  http.addHeader("Content-Type", "application/json");
  http.addHeader("X-API-Token", API_TOKEN);

  int statusCode = http.POST(body);
  if (statusCode > 0) {
    Serial.printf("POST /api/ingress/hardware -> %d: %s\n", statusCode, http.getString().c_str());
  } else {
    Serial.printf("POST failed: %s\n", http.errorToString(statusCode).c_str());
  }
  http.end();

  delay(250); // ~4Hz -- matches the backend's default per-IP rate limit (see RATE_LIMIT_PER_SECOND)
}
`

// NewDocsHandler serves GET /api/v1/docs: a public, unauthenticated JSON
// reference describing the hardware ingress schema and a copy-paste ESP32
// starter sketch, so third-party integrators don't need this repo's source
// to onboard a device.
func NewDocsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(docsResponse{
			Endpoint:      "/api/ingress/hardware",
			Method:        "POST",
			Auth:          "Header X-API-Token: <token> -- required only when the server is deployed with -api-tokens configured; omit the header entirely otherwise.",
			Schema:        hardwareIngressSchema,
			Esp32Template: esp32Template,
		})
	}
}
