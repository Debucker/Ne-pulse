#!/usr/bin/env bash
# Verifies a deployed NE-PULSE from OUTSIDE its network (run on your laptop).
#   ./scripts/verify-production.sh [API_BASE] [SITE_BASE] [ADMIN_TOKEN]
set -uo pipefail
API="${1:-https://api.ne-pulse.com}"
SITE="${2:-https://ne-pulse.com}"
TOKEN="${3:-${NEPULSE_ADMIN_TOKEN:-}}"
pass=0; fail=0; skip=0
ok()   { echo "PASS  $1"; pass=$((pass+1)); }
bad()  { echo "FAIL  $1"; fail=$((fail+1)); }
na()   { echo "SKIP  $1"; skip=$((skip+1)); }
code() { curl -s -o /dev/null -w "%{http_code}" --max-time 20 "$@"; }

[ "$(code "$API/api/health")" = 200 ] && ok "health endpoint" || bad "health endpoint"
curl -s --max-time 20 "$API/api/v1/config" | grep -q '"configHash"' && ok "public config" || bad "public config"
curl -s --max-time 20 "$API/api/v1/challenge" | grep -q '"difficulty"' && ok "enrollment challenge" || bad "enrollment challenge"
curl -s --max-time 20 "$API/api/v1/network" | grep -q '"type":"network"' && ok "network snapshot" || bad "network snapshot"

pre=$(curl -s -D - -o /dev/null --max-time 20 -X OPTIONS "$API/api/v1/devices" -H "Origin: $SITE" -H "Access-Control-Request-Method: POST")
echo "$pre" | grep -qi "access-control-allow-origin: $SITE" && ok "CORS allows $SITE" || bad "CORS allows $SITE"
evil=$(curl -s -D - -o /dev/null --max-time 20 -X OPTIONS "$API/api/v1/devices" -H "Origin: https://evil.example" -H "Access-Control-Request-Method: POST")
echo "$evil" | grep -qi "access-control-allow-origin" && bad "CORS rejects foreign origins" || ok "CORS rejects foreign origins"

for p in /api/simulate-rupture /api/v1/alert /api/ingress/hardware; do
  c=$(code -X POST "$API$p")
  { [ "$c" = 404 ] || [ "$c" = 405 ]; } && ok "legacy route $p removed" || bad "legacy route $p answers $c"
done
[ "$(code "$API/api/admin/overview")" = 401 ] && ok "admin API requires a token" || bad "admin API requires a token"

h=$(curl -s -D - -o /dev/null --max-time 20 "$API/api/health")
for hh in x-content-type-options content-security-policy x-frame-options; do
  echo "$h" | grep -qi "^$hh" && ok "API header $hh" || bad "API header $hh"
done
s=$(curl -s -D - -o /dev/null --max-time 20 "$SITE/")
for hh in content-security-policy permissions-policy strict-transport-security; do
  echo "$s" | grep -qi "^$hh" && ok "site header $hh" || bad "site header $hh"
done
echo "$s" | grep -qi "accelerometer=(self)" && ok "site allows the motion sensor" || bad "site allows the motion sensor"

if command -v node >/dev/null && node -e 'process.exit(typeof WebSocket==="function"?0:1)'; then
  WS="${API/http/ws}/ws/public"
  node -e "const w=new WebSocket('$WS',{headers:{Origin:'$SITE'}});const t=setTimeout(()=>{console.log('timeout');process.exit(1)},10000);w.onmessage=m=>{clearTimeout(t);process.exit(JSON.parse(m.data).type==='network'?0:1)};w.onerror=()=>process.exit(1)" \
    && ok "public WebSocket delivers a snapshot" || bad "public WebSocket delivers a snapshot"
else na "public WebSocket (needs Node >= 22)"; fi

if [ -n "$TOKEN" ]; then
  who=$(curl -s --max-time 20 -H "Authorization: Bearer $TOKEN" "$API/api/admin/whoami")
  echo "      server resolved your IP as: $(echo "$who" | sed -n 's/.*"resolvedIP":"\([^"]*\)".*/\1/p')"
  echo "      compare with your real public IP: $(curl -s --max-time 10 https://api.ipify.org)"
  echo "      (they must match, or rate limits and source diversity are wrong; see docs/DEPLOYMENT.md)"
else na "client IP resolution check (pass ADMIN_TOKEN)"; fi

echo; echo "$pass passed, $fail failed, $skip skipped"
[ "$fail" = 0 ]
