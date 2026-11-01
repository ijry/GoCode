#!/usr/bin/env bash
# Boot a throwaway gocode-server, run scripts/smoke.js against it, stop it.
#
# The smoke test is the wire-contract gate (401/404 shapes, AppCommandError
# envelope, health, WS handshake) — it talks to a REAL server process, not the
# in-process test harness. Starting the server and running the checks in one
# shot matters: the sandbox reaps background children when a command ends, so
# "start here, probe there" does not work.
#
# The WS checks need the `ws` npm package resolvable from smoke.js's directory;
# without it they print SKIP and the HTTP checks still run.
#
# Usage: scripts/smoke.sh [port] [token]
set -euo pipefail
cd "$(dirname "$0")/.."

PORT="${1:-3999}"
TOKEN="${2:-smoke-token-$$}"
# Normalize to a C:/… path: mktemp hands back a mixed "C:\…\Temp/tmp.xxx" that
# the safe-delete shim rejects ("embedded drive prefix"), and the native server
# binary cannot read MSYS /tmp paths either.
WORK="$(cygpath -m "$(mktemp -d)" 2>/dev/null || mktemp -d)"
DATA="$WORK/data"
BIN="$WORK/gocode-server.exe"
mkdir -p "$DATA"

cleanup() {
	if [ -n "${SRV:-}" ]; then kill "$SRV" 2>/dev/null || true; fi
	rm -rf "$WORK"
}
trap cleanup EXIT

echo "smoke: building server…"
go build -o "$BIN" ./cmd/gocode-server

GOCODE_PORT="$PORT" GOCODE_HOST=127.0.0.1 \
	"$BIN" --data-dir "$DATA" --token "$TOKEN" >"$WORK/server.log" 2>&1 &
SRV=$!

for _ in $(seq 1 30); do
	if curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$PORT/api/health"; then break; fi
	sleep 1
done

set +e
NO_PROXY='*' no_proxy='*' node scripts/smoke.js "http://127.0.0.1:$PORT" "$TOKEN"
status=$?
set -e

if [ "$status" -ne 0 ]; then
	echo "--- server log:"
	tail -20 "$WORK/server.log"
fi
exit "$status"
