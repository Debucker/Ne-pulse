#!/usr/bin/env bash
set -uo pipefail

# Verifies the split-host production deployment (Next.js on Vercel at
# ne-pulse.com, Go backend on Render behind api.ne-pulse.com) end-to-end,
# from OUTSIDE the backend's own network. Run this from your laptop or CI --
# never from the Render instance itself, since curling localhost there
# "succeeds" trivially and proves nothing about DNS, the custom domain
# attachment, or an edge proxy silently mangling the WebSocket upgrade,
# which is exactly the class of bug this script exists to catch.
#
# Does NOT stop at the first failure -- runs all four checks and prints a
# summary, so a single `curl` you can paste over Slack tells you exactly
# which piece of the stack (DNS/domain, the Go server itself, CORS config,
# or the WebSocket path) is still broken.
#
# Usage:
#   ./scripts/verify-production.sh [api-base-url] [frontend-origin]
#
# Env var overrides (equivalent to the positional args above):
#   API_BASE_URL      default: https://api.ne-pulse.com
#   FRONTEND_ORIGIN   default: https://ne-pulse.com
#                      Must be in the Go server's -cors-allowed-origins /
#                      defaultAllowedOrigins list (cmd/server/main.go) or
#                      check 3 below will correctly report a failure.

API_BASE_URL="${1:-${API_BASE_URL:-https://api.ne-pulse.com}}"
FRONTEND_ORIGIN="${2:-${FRONTEND_ORIGIN:-https://ne-pulse.com}}"
TIMEOUT_SECONDS=10

PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0

pass() { echo "PASS: $1"; PASS_COUNT=$((PASS_COUNT + 1)); }
fail() { echo "FAIL: $1" >&2; FAIL_COUNT=$((FAIL_COUNT + 1)); }
skip() { echo "SKIP: $1"; SKIP_COUNT=$((SKIP_COUNT + 1)); }

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required and was not found on PATH." >&2
  exit 2
fi

echo "== NE-PULSE production verification =="
echo "API_BASE_URL    = $API_BASE_URL"
echo "FRONTEND_ORIGIN = $FRONTEND_ORIGIN"
echo

# --- 1. GET /api/health ----------------------------------------------------
echo "-- [1/4] GET $API_BASE_URL/api/health"
HEALTH_BODY_FILE="$(mktemp)"
HEALTH_CODE=$(curl -sS -o "$HEALTH_BODY_FILE" -w "%{http_code}" --max-time "$TIMEOUT_SECONDS" \
  "$API_BASE_URL/api/health" 2>/dev/null) || HEALTH_CODE="000"

if [ "$HEALTH_CODE" = "200" ]; then
  pass "/api/health returned 200 OK"
elif [ "$HEALTH_CODE" = "000" ]; then
  fail "could not reach $API_BASE_URL/api/health at all (DNS not resolving, TLS handshake failed, firewall, or the server is down)"
else
  fail "/api/health returned HTTP $HEALTH_CODE (expected 200) -- body: $(head -c 300 "$HEALTH_BODY_FILE" 2>/dev/null)"
fi
rm -f "$HEALTH_BODY_FILE"
echo

# --- 2. GET /api/v1/docs ----------------------------------------------------
echo "-- [2/4] GET $API_BASE_URL/api/v1/docs"
DOCS_BODY_FILE="$(mktemp)"
DOCS_CODE=$(curl -sS -o "$DOCS_BODY_FILE" -w "%{http_code}" --max-time "$TIMEOUT_SECONDS" \
  "$API_BASE_URL/api/v1/docs" 2>/dev/null) || DOCS_CODE="000"

if [ "$DOCS_CODE" = "200" ]; then
  pass "/api/v1/docs returned 200 OK"
  if grep -q '"schema"' "$DOCS_BODY_FILE" && grep -q '"esp32_template"' "$DOCS_BODY_FILE"; then
    pass "/api/v1/docs payload contains both \"schema\" and \"esp32_template\" keys"
  else
    fail "/api/v1/docs returned 200 but the payload is missing \"schema\" and/or \"esp32_template\" -- has the response shape changed? (internal/ingress/docs.go)"
  fi
elif [ "$DOCS_CODE" = "000" ]; then
  fail "could not reach $API_BASE_URL/api/v1/docs at all"
else
  fail "/api/v1/docs returned HTTP $DOCS_CODE (expected 200) -- body: $(head -c 300 "$DOCS_BODY_FILE" 2>/dev/null)"
fi
rm -f "$DOCS_BODY_FILE"
echo

# --- 3. CORS preflight allows X-API-Token cross-origin ----------------------
echo "-- [3/4] OPTIONS $API_BASE_URL/api/ingress/hardware (simulated preflight from Origin: $FRONTEND_ORIGIN)"
PREFLIGHT_HEADERS_FILE="$(mktemp)"
PREFLIGHT_CODE=$(curl -sS -o /dev/null -D "$PREFLIGHT_HEADERS_FILE" -w "%{http_code}" --max-time "$TIMEOUT_SECONDS" \
  -X OPTIONS "$API_BASE_URL/api/ingress/hardware" \
  -H "Origin: $FRONTEND_ORIGIN" \
  -H "Access-Control-Request-Method: POST" \
  -H "Access-Control-Request-Headers: content-type,x-api-token" 2>/dev/null) || PREFLIGHT_CODE="000"

ALLOW_HEADERS_LINE=$(grep -i '^access-control-allow-headers:' "$PREFLIGHT_HEADERS_FILE" 2>/dev/null || true)
ALLOW_ORIGIN_LINE=$(grep -i '^access-control-allow-origin:' "$PREFLIGHT_HEADERS_FILE" 2>/dev/null || true)

if [ "$PREFLIGHT_CODE" = "000" ]; then
  fail "could not reach $API_BASE_URL/api/ingress/hardware for the preflight check"
elif [ "$PREFLIGHT_CODE" != "204" ] && [ "$PREFLIGHT_CODE" != "200" ]; then
  fail "preflight OPTIONS returned HTTP $PREFLIGHT_CODE (expected 204 No Content)"
elif ! echo "$ALLOW_ORIGIN_LINE" | grep -qi "$FRONTEND_ORIGIN"; then
  fail "preflight succeeded but Access-Control-Allow-Origin didn't reflect $FRONTEND_ORIGIN -- is it in the server's -cors-allowed-origins list? (got: ${ALLOW_ORIGIN_LINE:-<no header present>})"
elif echo "$ALLOW_HEADERS_LINE" | grep -qi "x-api-token"; then
  pass "preflight allows X-API-Token cross-origin (${ALLOW_HEADERS_LINE:-<missing>})"
else
  fail "preflight succeeded but Access-Control-Allow-Headers is missing X-API-Token -- browsers will silently drop the real request even though the server would have accepted the token (got: ${ALLOW_HEADERS_LINE:-<no header present>})"
fi
rm -f "$PREFLIGHT_HEADERS_FILE"
echo

# --- 4. WebSocket upgrade handshake -----------------------------------------
WS_URL="$(printf '%s' "$API_BASE_URL" | sed -E 's#^https#wss#; s#^http#ws#')/ws/telemetry"
echo "-- [4/4] WebSocket handshake to $WS_URL"

if ! command -v node >/dev/null 2>&1; then
  skip "node not found on PATH -- install Node 22+ (for the built-in WebSocket client) to run this check"
else
  WS_CHECK_SCRIPT="$(mktemp)"
  # Plain CommonJS (no import/export), so this runs with `node <file>`
  # regardless of extension or any nearby package.json's "type" field.
  cat > "$WS_CHECK_SCRIPT" <<'NODE_EOF'
const url = process.argv[2];
const TIMEOUT_MS = 8000;

if (typeof WebSocket === "undefined") {
  console.error("SKIP: this Node version has no built-in WebSocket global (needs Node >=22). Re-run with a newer Node, or add the \"ws\" package and adapt this snippet.");
  process.exit(3);
}

const timer = setTimeout(() => {
  console.error(
    `FAIL: no response within ${TIMEOUT_MS}ms connecting to ${url} -- the WebSocket upgrade is likely being ` +
      "dropped, buffered, or held open by an edge proxy in front of the backend rather than passed through.",
  );
  process.exit(1);
}, TIMEOUT_MS);

let socket;
try {
  socket = new WebSocket(url);
} catch (err) {
  clearTimeout(timer);
  console.error(`FAIL: could not construct a WebSocket for ${url}: ${err && err.message ? err.message : err}`);
  process.exit(1);
}

socket.addEventListener("open", () => {
  clearTimeout(timer);
  console.log(`PASS: WebSocket handshake to ${url} completed (server responded 101 Switching Protocols).`);
  socket.close();
  process.exit(0);
});

socket.addEventListener("error", () => {
  clearTimeout(timer);
  console.error(`FAIL: WebSocket handshake to ${url} errored -- the upgrade never completed.`);
  process.exit(1);
});
NODE_EOF

  node "$WS_CHECK_SCRIPT" "$WS_URL"
  WS_EXIT_CODE=$?
  rm -f "$WS_CHECK_SCRIPT"

  case "$WS_EXIT_CODE" in
    0) pass "WebSocket upgrade to $WS_URL succeeded" ;;
    3) skip "WebSocket check skipped (Node too old -- see message above)" ;;
    *) fail "WebSocket upgrade to $WS_URL failed (see message above)" ;;
  esac
fi
echo

# --- Summary -----------------------------------------------------------
echo "== Summary: $PASS_COUNT passed, $FAIL_COUNT failed, $SKIP_COUNT skipped =="
if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi
exit 0
