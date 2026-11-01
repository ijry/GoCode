# GoCode

A native Go desktop workspace for multi-agent AI coding — one window for the
agents you already run. All fifteen agents it launches are wired end to end:
each one's own session store is read back (JSONL, zstd-compressed JSONL, SQLite
or protobuf, depending on the agent), so a conversation survives a restart and
its history stays readable without the CLI that wrote it.

## Shape

| Path | What |
|---|---|
| `src-go/` | Server: HTTP/WebSocket layer, ACP engine, SQLite, command handlers |
| `src/` | mygo native UI (mirrors the upstream page tree) |
| `cmd/gocode/` | Desktop app — in-process backend + native window |
| `cmd/gocode-server/` | Standalone server binary |
| `cmd/gocode-mcp/` | MCP companion binary |
| `scripts/` | `gen_inventory.js` (route checklist), `smoke.js` (protocol smoke test) |
| `_upstream/` | Local reference clones — **not** part of the repo |

The UI never reaches into the database: it speaks the same loopback
`/api/*` + `/ws/events` contract the mobile client uses, so the two clients
exercise one wire.

## Build

```bash
go build ./...                                                # compile check
go vet ./... && scripts/test.sh                               # verify
scripts/smoke.sh                                              # wire contract, real server
go run github.com/egoist/mygo/cmd/mygo dev ./cmd/gocode     # live-reload dev
go run github.com/egoist/mygo/cmd/mygo build ./cmd/gocode   # packaged app
```

`scripts/test.sh` is `go test -timeout 30m ./...`: the `src-go/web/handlers`
suite scaffolds real git repositories and probes the machine's agent launchers,
so it runs 8–11 minutes on a Windows dev box — past `go test`'s 10-minute
default. A bare `go test ./...` therefore flakes with "test timed out"; the
script is the supported entry point.

`go run github.com/egoist/mygo/cmd/mygo build ./cmd/gocode` is the release
path: it compiles the app and packages an installer into `dist/` (both `dist/`
and `build/` are gitignored). Verified at 25 MB app + 7.9 MB installer.

`scripts/smoke.sh` builds the server, boots it on a throwaway data directory,
and runs `scripts/smoke.js` against it: 401/404 shapes, the `AppCommandError`
envelope, authenticated `health`, and the `/ws/events` handshake (`__ready__`
+ attach → `detached`). The WS checks need the `ws` npm package resolvable —
point `NODE_PATH` at any `node_modules` that contains it (another JS project on
the box will do), otherwise they print SKIP. With it, all eleven checks pass.

Requires Go 1.27+. The UI toolkit (mygo) is a normal module dependency fetched
over git; if the module proxy is unreachable, `GOPROXY=direct go mod download`
works.

## Docs

- `docs/REPLICATION_PLAN.md` — the contract: goals, directory mapping, wire
  compatibility rules.
- `docs/ROUTE_INVENTORY.md` — every route, ticked off; regenerate with
  `node scripts/gen_inventory.js`.
- `docs/UI_GAPS.md` — what the native UI replicates from upstream, item by item,
  including the gaps left on purpose and why.
- `docs/UI_OPTIMIZATION.md` — the three places this port deliberately improves on
  upstream's interaction model.
- `docs/PROGRESS.md` — milestones and the rolling work log.
