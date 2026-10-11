// Copy to config.h and fill in. config.h is git-ignored: it holds your
// Wi-Fi password and your station's exact position.
#pragma once

#define WIFI_SSID "your-wifi"
#define WIFI_PASS "your-wifi-password"

// NE-PULSE API (the backend, not the website).
#define API_HOST "api.ne-pulse.com"
#define API_PORT 443
#define API_TLS 1  // 1 = https/wss (production), 0 = http/ws (local testing)

// Where the board is. Stays on the board: only the 1 km grid cell that
// contains it is sent to the server.
#define STATION_LAT 41.3111
#define STATION_LNG 69.2797

// Optional research class code (registered by the teacher), e.g. "TASH-9B".
// With a code the server records this station's raw data for that class.
#define CLASS_CODE ""

// I2C pins for the MPU-6050 (ESP32 DevKit defaults).
#define PIN_SDA 21
#define PIN_SCL 22
