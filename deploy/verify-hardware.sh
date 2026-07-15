#!/usr/bin/env bash
set -euo pipefail

# Verifies POST /api/ingress/hardware is reachable from OUTSIDE the server's
# own network — run this from your laptop or any machine that isn't the VPS
# itself. Curling localhost on the server always "succeeds" and proves
# nothing about the public path through DNS, the firewall, and Caddy.
#
# Usage:
#   ./verify-hardware.sh [https://ne-pulse.com]

URL="${1:-https://ne-pulse.com}/api/ingress/hardware"
TMP_BODY="$(mktemp)"
trap 'rm -f "$TMP_BODY"' EXIT

echo "POSTing a synthetic hardware frame to $URL ..."

HTTP_CODE=$(curl -sS -o "$TMP_BODY" -w "%{http_code}" \
  --max-time 10 \
  -X POST "$URL" \
  -H "Content-Type: application/json" \
  -d '{"deviceId":"verify-script","lat":41.2995,"lng":69.2401,"accX":0.1,"accY":0.1,"accZ":9.8}') || {
    echo "FAILED: could not reach $URL at all (DNS, firewall, or the server itself may be down)." >&2
    exit 1
  }

echo "HTTP $HTTP_CODE"
echo "Body: $(cat "$TMP_BODY")"

if [ "$HTTP_CODE" = "202" ]; then
  echo "OK: hardware ingress is reachable from outside and accepted the frame."
  exit 0
fi

echo "WARNING: reached the server but got HTTP $HTTP_CODE (expected 202 Accepted)." >&2
echo "  400 -> check the payload shape or the new accX/Y/Z (+/-5g) validation." >&2
echo "  429 -> the per-IP rate limiter is engaged; wait a second and retry." >&2
echo "  502/503/504 -> Caddy is up but the Go backend behind it isn't responding." >&2
exit 2
