#!/usr/bin/env bash
# Run the whole test suite with a deadline that fits this repo.
#
# Why not a bare `go test ./...`: src-go/web/handlers is integration-shaped —
# it scaffolds real git repositories and probes the machine's agent launchers
# (npx/uvx cold starts, `git` under antivirus). On a Windows dev box the
# package alone runs 8–11 minutes, i.e. right at `go test`'s 10-minute default
# (cmd/go hard-kills the binary around 11m), so a bare run flakes with
# "test timed out" / "Test killed: ran too long" while every test is healthy.
# Raising the deadline here is the supported way to fix that.
#
# A TestMain that calls flag.Set("test.timeout", …) does NOT help: go test
# passes -test.timeout on the command line and testing parses flags inside
# m.Run(), after TestMain has run, so the value gets overwritten; and cmd/go's
# own kill timer ignores the binary's deadline entirely.
#
# Usage: scripts/test.sh [go test args…]
#   scripts/test.sh                          # whole suite
#   scripts/test.sh ./src/app/               # one package
#   scripts/test.sh -run TestFoo ./src/app/  # one test pattern
set -euo pipefail

cd "$(dirname "$0")/.."
if [ $# -eq 0 ]; then
	exec go test -timeout 30m -count=1 ./...
fi
exec go test -timeout 30m -count=1 "$@"
