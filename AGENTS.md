# AGENTS.md

Go MCP server exposing one locally deployed Review Board 7.0.4 instance over REST API 2.0.
Design: `docs/00_basic_design.md` (full workflow, API-token auth, 12 tools).

## State

- Scaffolded and building: `go.mod`, `cmd/`, `internal/{config,reviewboard,tools,format}`, README and CI exist.
- Layout:

```
cmd/reviewboard-mcp/main.go   flags, startup check, transport switch
internal/config/              env + flag parsing, validation
internal/reviewboard/         REST client for /api; must not import mcp
internal/tools/               MCP tools; must not do HTTP
internal/format/              compact plain-text rendering
docs/00_basic_design.md       design doc, kept in sync with tool schemas
```

Module `reviewboard-mcp`, Go 1.26.4, sole direct dependency `github.com/modelcontextprotocol/go-sdk`
(pin v1.8.0). No Makefile, no golangci config.

## Commands

```bash
go build ./...
go test ./...
go vet ./...
gofmt -l .                        # any output fails CI; use gofmt -w .
go test -race ./...               # what CI runs
go test ./internal/tools -run TestX -v
```

CI (`.github/workflows/ci.yml`): gofmt check, `go vet ./...`, `go test -race ./...`.

## MCP rules that are easy to break

- **stdout is the JSON-RPC wire under stdio.** Any `fmt.Println` corrupts the session. Log via `slog` to stderr.
- Tool schema comes from the `In` struct: `jsonschema` tags are descriptions, **no `omitempty` = required**,
  pointers optional. No hand-written schema.
- Handlers return one `TextContent` block and an `error`; the SDK sets `IsError`. Never a result plus an error.
- HTTP transport is MCP Streamable HTTP: `Stateless: true`, SSE (`text/event-stream`) responses, one shared
  `*mcp.Server`. Leave `JSONResponse` false — enabling it turns streaming off. Stateless means no
  `Mcp-Session-Id` and GET/DELETE return 405.
- Read tools set `readOnly`; write tools set the destructive hint and call the read-only guard.
- Verify both transports. For stdio, pipe `initialize` / `notifications/initialized` / `tools/list` /
  `tools/call` and keep stdin open; closing stdin cancels in-flight calls.

## Review Board specifics that are easy to get wrong

Talk to the server over REST API 2.0 only: https://www.reviewboard.org/docs/manual/latest/webapi/2.0/.
No RBTools, no skill scripts, no Python helpers — the binary is self-contained.

- Base URL has **no** `/api` suffix; API paths are `{base}/api/...`. Keep a trailing slash or join via `url.JoinPath`.
- Auth header is `Authorization: token <API_TOKEN>` — **not** `Bearer`. Token only in v1; Basic auth is not implemented.
  Token comes from the environment only; never argv, logs, or tool output.
- Connectivity check at startup: `GET /api/session/` (returns `session.authenticated` + `links.user`).
  Fail fast and exit; a broken client should never reach MCP.
- List envelopes are `{"stat":"ok","<plural>":[...],"total_results":N,"links":{...}}`; pagination is
  `links.next.href`, default 25 per page, **hard cap `max-results=200`**. Always print a returned-of-total footer.
- Fetch related resources via `links.<name>.href` (preferred over building URLs).
- Review requests: create is blank/private/pending; all fields go through the **draft**, then publishing makes it
  public. Filtering uses hyphenated params (`status=pending|submitted|discarded|all`, `from-user`, `to-users`,
  `repository`, `time-added-from`, `last-updated-from`, `start`, `max-results`).
- Reviews/comments/replies are drafts too: `POST /api/review-requests/{id}/reviews/`, then
  `PUT {links.self}` with `public=true` to publish. Replies live under `reviews/{rid}/replies/` and mirror the
  comment APIs. Do not report a comment as posted before the publish PUT succeeds.
- Diff upload is multipart `path` + `basedir` (+ optional `parent_diff_path`). `basedir` depends on the
  repository and the diff, so the caller supplies it on every upload; never infer it or store one global value.
  A missing/wrong value fails with 207 "repository file not found"; fix it with a new call, don't retry.
- Status values on 7.0.4 are `pending|submitted|discarded`, not `completed`.
- Comment semantics: `body_top` / `body_bottom`, `ship_it`; opening an issue is `issue_opened: true`, its state is
  `issue_status` (`open|resolved|dropped`).
- `rbt`/RBTools is not installed here and must not be a dependency: no shelling out, no `.reviewboardrc`.

## Config convention

Use these names: `REVIEWBOARD_URL`, `REVIEWBOARD_API_TOKEN`, `REVIEWBOARD_INSECURE`,
`REVIEWBOARD_READ_ONLY`, `REVIEWBOARD_TIMEOUT`, `REVIEWBOARD_DEFAULT_LIMIT`, `REVIEWBOARD_TEXT_TYPE`,
`REVIEWBOARD_MAX_UPLOAD`, `REVIEWBOARD_META_TTL`;
transport from `MCP_TRANSPORT`, `MCP_ADDR`, `MCP_PATH`, `MCP_TOKEN`, with `--transport`, `--addr`,
`--read-only` flags. No URL, token, or username is configured anywhere on this machine yet — ask, don't invent one.

## Tests

- Each test builds its own fake Review Board with `httptest`; no shared fixtures or `testdata/`.
- `internal/tools/tools_test.go` is the end-to-end test: register real tools on an `mcp.Server`, call them
  through an in-memory MCP client. A new tool gets assertions there.
- Pin the request shape (token header, `max-results`, hyphenated params, draft→publish ordering), not just parsing.
- Nothing has run against a live Review Board yet; do not claim compatibility without doing so.

## Conventions

- Compact, line-oriented text output: `#` header, `-` for empty, UTC `YYYY-MM-DD HH:MMZ`, footer
  `# returned N of TOTAL`. Truncation must be labelled.
- Errors are one line starting `reviewboard:` and never contain the token.
- When a tool schema or behavior changes, update `docs/00_basic_design.md` in the same change.
