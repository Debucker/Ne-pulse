#!/usr/bin/env bash
# Emulator end-to-end test: install, start listening without taps, and wait
# until the live server has sent this phone a station status. Then delete the
# test phone from the network again.
set -u
APK="$1"
adb install -r "$APK"
adb logcat -c
# Tashkent, 1 km cell 14601:20819 (computed by the server's own grid code).
adb shell am start -n com.nepulse.sensor/.MainActivity \
  --ez test_autostart true --ei row 14601 --ei col 20819 --es cohort PUBLIC
ok=0
for i in $(seq 1 60); do
  sleep 5
  if adb logcat -d -s NEPULSE:I | grep -qE "state=(warmup|quiet|triggered|disturbed)"; then ok=1; break; fi
done
echo "---- app log"
adb logcat -d -s NEPULSE:I | tail -40
# Remove the test phone from the network.
prefs=$(adb shell run-as com.nepulse.sensor cat shared_prefs/nepulse.xml 2>/dev/null || true)
id=$(echo "$prefs" | sed -n 's/.*name="deviceId">\([^<]*\)<.*/\1/p')
secret=$(echo "$prefs" | sed -n 's/.*name="secret">\([^<]*\)<.*/\1/p')
backend=$(echo "$prefs" | sed -n 's/.*name="backend">\([^<]*\)<.*/\1/p')
if [ -n "$id" ] && [ -n "$backend" ]; then
  curl -s -o /dev/null -w "deleted test phone: HTTP %{http_code}\n" -X DELETE "$backend/api/v1/devices/me" \
    -H "X-Device-Id: $id" -H "Authorization: Bearer $secret"
fi
if [ "$ok" != 1 ]; then
  echo "::error::the app did not get a status from the server within 5 minutes"
  exit 1
fi
echo "E2E OK: the app registered, connected and is being processed by the server"
