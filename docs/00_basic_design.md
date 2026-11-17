# reviewboard-mcp — Basic Design

| | |
|---|---|
| Status | Draft, ready to implement |
| Target | Review Board **7.0.4** (REST API 2.0, single code path) |
| Language | Go **1.26.4** |
| MCP SDK | `github.com/modelcontextprotocol/go-sdk` **v1.8.0** (official) |
| Transports | stdio **and** Streamable HTTP (SSE streaming, stateless) |
| Deployment | **one process = one Review Board server** |
| Module | `reviewboard-mcp` |
| Auth | API token (`Authorization: token <token>`) |
| Layout | `cmd/reviewboard-mcp` + `internal/{config,reviewboard,tools,format}` |

---

## 1. Scope

### Goals

1. Expose the review workflow people actually do: find a review request, read it and its diff, comment inline or
   generally, reply to comments, Ship It, create a review request, upload a diff, and close it.
2. Talk to Review Board over **REST API 2.0 only**. No RBTools, no Python, no shelling out.
3. Work over stdio (local clients spawn the binary) and Streamable HTTP (shared instance).
4. Accept human names for repositories, users and groups; resolve them to ids/usernames server-side.
5. Keep model-facing output small; compact plain text, not raw JSON.
6. Safe defaults: read-only switch, explicit confirmation for destructive/irreversible actions, token only from
   the environment.

Repositories are not configured in the server. One process serves one Review Board instance and every repository
on it; the review request names its repository, and each diff upload carries its own `basedir`. Do not add a
per-repository config file or a global `basedir`.

### Non-goals (deliberately skipped)

- Multi-server in one process (ids collide; run two processes).
- Generating diffs. The caller supplies a unified diff file; this server does not run `svn diff`/`git diff`.
- Deleting review requests, even though the API allows it.
- Screenshots, file attachments, webhooks, extensions, OAuth2, admin endpoints.
- Editing a review after it has been published; Review Board does not allow it. Use a reply.
- Storing credentials in the repository or in a config file.

---

## 2. Architecture

```
cmd/reviewboard-mcp/main.go      flags, wiring, startup check, signal handling
internal/config/config.go        env + flag parsing, validation
internal/reviewboard/            Review Board REST client (no MCP types)
    client.go                    do(), token, base URL, timeouts, retries, pagination
    errors.go                    HTTP status + err.code -> typed error
    types.go                     JSON structs for the objects we use
    requests.go                  list/get/create review requests, drafts, status
    diffs.go                     diff list/get/files, multipart draft upload
    reviews.go                   reviews, comments, replies, publish
    meta.go                      repositories, users, groups
    resolve.go                   name -> id resolution + cache
internal/tools/                  MCP layer: schema in, compact text out
    tools.go                     registry, shared helpers, read-only guard
    requests.go                  list/get/create/update/close review requests
    diffs.go                     get_diff, upload_diff
    reviews.go                   list_reviews, create_review, reply_review
    meta.go                      whoami, list_metadata
internal/format/format.go        table/field rendering, truncation
docs/00_basic_design.md          this file
```

Two hard rules:

- `internal/reviewboard` imports **nothing** from `mcp`; it is testable on its own.
- `internal/tools` never builds URLs or talks HTTP. It calls the client, then formats.

### Wiring

```go
srv := mcp.NewServer(&mcp.Implementation{Name: "reviewboard-mcp", Version: version}, &mcp.ServerOptions{
    Instructions: instructions, // Markdown text, draft/publish model, read-only state
})
tools.Register(srv, &tools.Deps{C: client, M: meta, ReadOnly: cfg.ReadOnly, DefaultLimit: cfg.DefaultLimit})

switch cfg.Transport {
case "stdio":
    err = srv.Run(ctx, &mcp.StdioTransport{})
case "http":
    handler := mcp.NewStreamableHTTPHandler(
        func(*http.Request) *mcp.Server { return srv }, // one server for every session
        &mcp.StreamableHTTPOptions{Stateless: true}, // no JSONResponse: responses stream as SSE
    )
    // bind every address before serving; require MCP_TOKEN on non-loopback;
    // graceful shutdown on SIGTERM
}
```

Both transports serve the same `*mcp.Server`; the transport is a startup switch, not a second code path.

HTTP is MCP **Streamable HTTP**: a `POST` is answered with a `text/event-stream` carrying the JSON-RPC
responses instead of a single JSON body. `Stateless: true` means no `Mcp-Session-Id`, no standalone
GET stream (GET/DELETE return 405), and no per-connection state. Clients must send
`Accept: application/json, text/event-stream`; a request without both is rejected with 400 by the SDK.

Startup sequence:

1. Parse and validate config (fail fast on missing URL/token; warn on `http://`).
2. `GET /api/session/` — proves token, TLS, base URL and login. Log
   `connected to <url> as <username>` to stderr; exit on failure.
3. Register tools. In read-only mode write tools stay listed but return a clear error.

---

## 3. Configuration

| Var / flag | Default | Meaning |
|---|---|---|
| `REVIEWBOARD_URL` | required | `https://reviews.example.com` or a sub-path install `https://host/reviewboard`. **No `/api` suffix.** |
| `REVIEWBOARD_API_TOKEN` | required | Sent as `Authorization: token <token>`. Never logged or echoed. |
| `REVIEWBOARD_INSECURE` | `false` | Skip TLS verification (self-signed internal certs). |
| `REVIEWBOARD_TIMEOUT` | `45s` | Per-request timeout. |
| `REVIEWBOARD_READ_ONLY` | `false` | Reject every write tool. |
| `REVIEWBOARD_DEFAULT_LIMIT` | `25` | Page size when `limit` is omitted; clamped to 1..200. |
| `REVIEWBOARD_TEXT_TYPE` | `markdown` | `markdown` or `plain`; sent as `*_text_type` on every text field. |
| `REVIEWBOARD_MAX_UPLOAD` | `25 MiB` | Reject larger diff files before opening them. |
| `REVIEWBOARD_META_TTL` | `5m` | Repository/user/group cache lifetime. |
| `MCP_TRANSPORT` / `--transport` | `stdio` | `stdio`, or `http` for Streamable HTTP with SSE responses. |
| `MCP_ADDR` / `--addr` | `127.0.0.1:8080` | HTTP listen address(es), comma-separated. |
| `MCP_PATH` | `/mcp` | HTTP endpoint path. |
| `MCP_TOKEN` | — | Bearer token; mandatory when any `MCP_ADDR` entry is non-loopback. |
| `--read-only` | — | Flag equivalent of `REVIEWBOARD_READ_ONLY`. |

URL handling: strip the trailing slash once, then join with `url.JoinPath(base, "api", "review-requests", "42")`
and append the trailing `/`. Do **not** use `url.ResolveReference` with a relative path: it drops a sub-path base.

---

## 4. Tool catalogue

Twelve tools. Names are snake_case, no prefix. Every tool returns one plain-text `TextContent` block.

| Tool | Annotations | Purpose |
|---|---|---|
| `whoami` | read-only | Connected user, server URL, read-only mode, name-resolution availability. First call on a deployment. |
| `list_review_requests` | read-only | Filters + pagination; the main discovery tool. |
| `get_review_request` | read-only | Fields, reviewers, issue counts, diff summary, URL. |
| `get_diff` | read-only | Diff revisions, file table with `filediff_id`, and the raw patch on request. |
| `list_reviews` | read-only | Reviews with their general and diff comments, including comment ids for replies. |
| `create_review_request` | write | Create and publish a review request (draft + publish). |
| `update_review_request` | write, destructive | Replace fields via the draft, then publish. |
| `upload_diff` | write | Upload a unified diff from a local path (multipart), then publish. |
| `create_review` | write | Post a review: body, Ship It, general comments, inline diff comments. |
| `reply_review` | write | Reply to review comments, then publish the reply. |
| `close_review_request` | write, destructive | `submitted`, `discarded`, or reopen as `pending`. `confirm: true` required. |
| `list_metadata` | read-only | `repositories`, `users`, or `groups`. |

### Inputs that matter

`list_review_requests`:

```
status (default "pending": pending|submitted|discarded|all),
repository (name, path or id), from_user, to_user ("me" allowed),
time_added_from, time_added_to, updated_from, updated_to,
limit (default 25, max 200), offset, raw
```

Review Board has **no full-text search** on this endpoint. Do not invent a `q` filter; searching summaries is a
client-side job over a small page, and the tool description says so.

`get_review_request`: `id`, `raw`. Always include the review request URL.

`get_diff`: `id`, `revision` (0 = current draft; omitted = latest published), `files_only`, `max_chars`
(default 40000). Returns the file table even when `files_only` is false, because `filediff_id` is the input for
inline comments.

`list_reviews`: `id`, `limit` (default 50), `offset`. Formats each review as a header plus its comments:

```
# review 12 | alice | 2026-11-16 09:40Z | ship_it | issues open 2 / resolved 0
body_top truncated to 600 chars
  general comment 31 [open] | use a constant here
  diff comment 32 [resolved] (file 41 src/app.go:120-124) | why not reuse parse()
```

The literal `(file <filediff_id> path:first-last)` form tells the model exactly what to pass to `reply_review`.

`create_review_request`: `repository` (required), `summary` (required), `description`, `testing_done`, `branch`,
`bugs_closed` (comma list), `target_people`, `target_groups`, `commit_id`, `publish` (default true).

`update_review_request`: `id`, `summary`, `description`, `testing_done`, `branch`, `bugs_closed`,
`target_people`, `target_groups`, `publish`, `confirm`. Reviewer lists are **replaced**, not appended. Replacing
`description` or `testing_done` loses the old text and needs `confirm: true`; an empty update is rejected.

`upload_diff`: `id`, `path` (required), `basedir`, `parent_diff_path`, `base_commit_id`, `publish` (default true).
`path` is a local path on the process host (client machine for stdio, server machine for HTTP) and must be a
regular file within `REVIEWBOARD_MAX_UPLOAD`. `basedir` is **supplied per upload**, never cached or inferred: this
server serves one Review Board but many repositories, each with its own diff root. If a diff is rejected with
207, the fix is a new call with the right `basedir`, not a retry.

`create_review`: `id`, `body_top`, `body_bottom`, `ship_it`, `general_comments[]` (`text`, `issue_opened`),
`diff_comments[]` (`filediff_id`, `first_line`, `num_lines`, `text`, `issue_opened`). Always published; a review
that stays a draft is invisible and useless to the model.

`reply_review`: `id`, `review_id`, `comments[]` (`reply_to_id`, `kind` = `general|diff`, `text`), `body_top`,
`body_bottom`. Always published.

`close_review_request`: `id`, `status` (`submitted|discarded|pending`), `description`, `confirm`.

---

## 5. Draft/publish model (normative)

This is the part of Review Board that is easy to get wrong. Nothing except a status change takes effect until a
draft is published.

| Action | Calls |
|---|---|
| Create review request | `POST /api/review-requests/` → `PUT /api/review-requests/{id}/draft/` with fields → same PUT with `public=true` |
| Update review request | `PUT .../draft/` with fields → `PUT .../draft/` with `public=true` |
| Upload diff | `POST .../draft/diffs/` (multipart) → `PUT .../draft/` with `public=true` |
| Post a review with comments | `POST .../reviews/` (no `public`) → read `Location` → `POST` each comment to the review's `links.*_comments.href` → `PUT .../reviews/{rid}/` with `public=true` |
| Post a review without comments | `POST .../reviews/` with `public=true` in one call |
| Reply | `POST .../reviews/{rid}/replies/` → `POST` each reply comment with `reply_to_id` → `PUT .../replies/{reply_id}/` with `public=true` |
| Close / reopen | `PUT /api/review-requests/{id}/` with `status=submitted|discarded|pending`; no draft involved |

Rules:

1. A `POST` to the review list returns **HTTP 201 and no body**; the review URL is in the `Location` header. Parse
   the id from it rather than searching the public list, because a draft review is not listed.
2. Publishing is one-way: a public review, reply, or review request cannot be made private again. Never retry a
   publish call blindly.
3. The draft diff always has revision `0`; publishing moves it to the next public revision. `filediff_id`s belong
   to one revision and become invalid after a new diff is published.
4. If a publish step fails, report the failure and the draft state. Do not claim the comment or diff is visible.
5. `public=true` on the same PUT as the fields is allowed, but two calls are easier to reason about and to test.

---

## 6. Name → ID resolution (normative)

1. **Numbers win.** A digit string for `repository` is an id; no lookup.
2. **`me`** for `to_user`/`from_user` uses the username from `/api/session/`.
3. Repositories: resolve against `/api/repositories/` (visible only). Match exact case-insensitive `name`, then
   exact `path`/mirror path, then unique prefix, then unique substring. Ambiguity is an error listing the
   candidates.
4. Users: `/api/users/?q=<prefix>&fullname=1` first; accept only an exact case-insensitive username or full name
   match. Do not page the whole user table.
5. Groups: `/api/groups/?q=<prefix>&displayname=1`, same exact-match rule.
6. Unknown values list the real ones (capped at 20): `unknown repository "web". Available: web-svn (3), web-git (7).`
7. Cache metadata for `REVIEWBOARD_META_TTL`; drop an entry when a follow-up call returns 404.
8. Never invent a repository: it is required for a review request that carries a diff. A missing repository on
   create is an error, not a default.

---

## 7. Output format (normative)

Context is the budget. JSON is opt-in via `raw`.

1. Lists are one record per line with a `#` header:
   ```
   # id | status | submitter | repository | updated | ship-its | issues o/r/d | summary
   42 | pending | alice | web-svn | 2026-11-16 09:40Z | 1 | 2/0/0 | add CSV export
   ```
2. Every list ends with `# returned N of TOTAL (offset X, limit L)`.
3. Empty is explicit: `no review requests matched (status=pending, repository=web-svn). Widen filters or use status=all.`
4. Missing values render as `-`, never `null` or an empty column.
5. Timestamps are UTC `YYYY-MM-DD HH:MMZ`.
6. Ids lead the line and are bare integers so the model can chain calls.
7. Truncation is labelled: summary 120 chars, description/testing 4000 chars in detail views, each comment 600
   chars, patch at `max_chars` with `… (truncated, N chars total)`.
8. `get_review_request` uses labelled sections: `#42 summary`, key/value block, `[description]`,
   `[testing done]`, `[reviewers]`, `[diffs]`.
9. `get_diff` prints `# revision N (draft|published)` and a file table, then the patch:
   ```
   # filediff | status | +/- | file
   41 | modified | +12/-3 | src/app.go
   ```
10. Write results are one line plus the URL and say what happened:
    `#42 updated and published: summary, reviewers (alice, web-team). https://reviews.example.com/r/42/`
11. Errors are one line starting `reviewboard:` and never contain the token or the whole request body.
12. No conversational filler, no markdown tables, no emoji.

---

## 8. Review Board API mapping and traps

| Operation | Endpoint |
|---|---|
| Session | `GET /api/session/` |
| Review requests | `GET /api/review-requests/` (`status`, `from-user`, `to-users`, `repository`, `time-added-from/to`, `last-updated-from/to`, `start`, `max-results`) |
| Review request | `GET /api/review-requests/{id}/` |
| Create | `POST /api/review-requests/` (`repository`, `commit_id`, `submit_as`) |
| Status / close | `PUT /api/review-requests/{id}/` (`status`, `close_description`) |
| Draft | `GET|PUT|DELETE /api/review-requests/{id}/draft/` (`public=true` publishes) |
| Upload diff | `POST /api/review-requests/{id}/draft/diffs/` (multipart: `path`, `parent_diff_path`, `basedir`, `base_commit_id`) |
| Diffs | `GET /api/review-requests/{id}/diffs/`, `GET .../diffs/{rev}/` |
| Diff files | `GET /api/review-requests/{id}/diffs/{rev}/files/` |
| Raw patch | `GET .../diffs/{rev}/` with `Accept: text/x-patch` |
| Reviews | `GET|POST /api/review-requests/{id}/reviews/` |
| Publish review | `PUT /api/review-requests/{id}/reviews/{rid}/` (`public=true`) |
| Diff comment | `POST {review}.links.diff_comments.href` (`filediff_id`, `first_line`, `num_lines`, `text`, `issue_opened`) |
| General comment | `POST {review}.links.general_comments.href` (`text`, `issue_opened`) |
| Replies | `POST .../reviews/{rid}/replies/`; reply comments at `.../replies/{reply_id}/diff-comments/` or `/general-comments/` (`reply_to_id`, `text`) |
| Repositories | `GET /api/repositories/` (`q`, `name-or-path`) |
| Users | `GET /api/users/` (`q`, `fullname=1`, `include-inactive=1`) |
| Groups | `GET /api/groups/` (`q`, `displayname=1`) |

Traps encoded in code, not comments:

- **Trailing slashes.** Every Review Board API path ends in `/`. A `POST` without it can 301 and lose the body.
- **Token scheme.** `Authorization: token <token>`, not `Bearer`. Basic auth is not implemented in v1.
- **Response envelope.** Lists are `{"stat":"ok","<plural>":[...],"total_results":N,"links":{...}}`; errors are
  `{"stat":"fail","err":{"code":N,"msg":"..."}}`. Read `err.msg` verbatim.
- **Pagination.** Follow `links.next.href` as an absolute URL; do not rebuild it. Default 25, hard cap 200.
- **`Location` header.** Creating a review or reply returns 201/303 with no body; the id comes from `Location`.
- **Draft reviews are invisible.** `GET .../reviews/` lists public reviews only; do not look for a draft there.
- **`filediff_id` is revision-scoped.** Fetch it from `get_diff` for the revision being reviewed.
- **Comment line numbers.** `first_line` is 1-based in the **patched/destination file**, not the hunk offset or
  the diff line. `num_lines` must be ≥ 0 (`0` pins a single line). A comment outside the file's lines is rejected.
- **Issue flags.** `issue_opened: true` is how an open issue is created; resolution happens on the comment later
  (`issue_status=resolved|dropped`), not through this server in v1.
- **Reviewer lists replace.** `target_people`/`target_groups` overwrite the previous reviewer set.
- **Status is `submitted`, not `completed`.** Verified in the 7.x API: `pending|submitted|discarded`.
- **`close_description`** replaces the deprecated `description` field on the close call.
- **Text types.** Send the matching `*_text_type` (`description_text_type`, `testing_done_text_type`,
  `body_top_text_type`, `text_type` for comments, `close_description_text_type`) so mixed server defaults cannot
  mangle Markdown.
- **Diffs are unified text only.** Binary patches and huge files are rejected (code 219/220/224). Do not retry.
- **SVN `basedir`.** For SVN diffs whose paths are relative to a subdirectory of the registered repository, pass
  `basedir`. It depends on the repository and on the diff, so the caller supplies it on every upload; store no
  global value and infer nothing. A wrong or missing value fails with "repository file not found" (207).
- **No delete.** Do not expose `DELETE /api/review-requests/{id}/`.
- **HTTP status is not the whole story.** Review Board returns 400 with `err.code` 105 for validation; map on both.

---

## 9. Error model

One typed error from the client, mapped once in the tool layer:

| Condition | Message |
|---|---|
| DNS/TLS/timeout | `reviewboard: cannot reach <host>: <cause>. Check REVIEWBOARD_URL / REVIEWBOARD_INSECURE / network.` |
| 401 / 103 | `reviewboard: authentication failed (token invalid, expired, or revoked). Regenerate it under My Account -> API Tokens.` |
| 403 / 101 | `reviewboard: permission denied: <err.msg>. This is an account permission, not a retryable error.` |
| 404 / 100 | `reviewboard: not found: review request 42 does not exist or is not visible to you.` + cache invalidation |
| 400 / 105 | `reviewboard: validation failed: <err.msg>.` — verbatim, never retried |
| 207 | `reviewboard: the diff does not match the repository path. Check the diff's base directory and the repository configuration.` |
| 224 | `reviewboard: diff parse error: <err.msg>. The file must be a unified diff.` |
| 5xx | `reviewboard: server returned <code> after 1 retry. Check the Review Board logs.` |
| Read-only | `reviewboard: this server runs in read-only mode (REVIEWBOARD_READ_ONLY); write tools are disabled.` |

Retries: **GET only**, once, on 502/503/504 and transport errors. Never retry `POST`/`PUT`/`PATCH` — a retried
review or diff creates a duplicate.

---

## 10. Security

- Token only from the environment; never in argv, logs, tool output, or errors. `whoami` shows the username.
- HTTP transport binds loopback by default; refuse to start on a non-loopback address without `MCP_TOKEN`.
  Compare tokens with `crypto/subtle.ConstantTimeCompare`.
- `REVIEWBOARD_INSECURE` logs a warning on every start.
- `upload_diff.path` is a trust boundary: it is model-supplied and, over HTTP, reads the **server's** filesystem.
  Read only the given path, require a regular file, enforce `REVIEWBOARD_MAX_UPLOAD`, never follow it into a
  directory. The tool description says where the path is resolved.
- Read-only is checked once in `internal/tools` before dispatch.
- Do not echo the raw error body when it may contain credentials; surface `err.msg` only.

---

## 11. Testing

- Unit tests use `httptest`; each test builds its own fake Review Board inline. No shared fixture files.
- Pin the request shape: trailing slash, `Authorization: token …`, `max-results`, hyphenated query params,
  multipart field names (`path`, `parent_diff_path`, `basedir`), and the draft→publish call order.
- Tool-level end-to-end: register the real tools on an `mcp.Server`, connect an in-memory MCP client, call every
  read tool plus create/update/upload/review/reply/close. This is the test that catches rendering and
  draft-state mistakes.
- Resolution tests are table-driven: digits, `me`, exact, prefix, substring, ambiguous, unknown, 403 on users.
- Formatting tests are golden strings: empty results, truncation, counts, missing fields.
- Integration test is opt-in (`//go:build integration`) and **read-only by default**: it needs
  `REVIEWBOARD_URL`, `REVIEWBOARD_API_TOKEN` and `REVIEWBOARD_TEST_RR_ID`, and only reads. A write test needs a
  separate flag and a disposable review request; never create or close review requests on a shared server by
  default.
- Manual smoke: `whoami` over stdio; over HTTP use `curl -N` with
  `Accept: application/json, text/event-stream` and the same `initialize`/`tools/list` bodies.

Definition of done for the first release: `gofmt -l .` clean, `go vet ./...`, `go test -race ./...`, and
`whoami / list_review_requests / get_review_request / get_diff / list_reviews` verified against the local server.
Write flows are verified only after an explicit test on a disposable review request.

---

## 12. First live verification

Decisions are closed: target is Review Board **7.0.4**, Markdown is the text type, and the draft→publish flow is
used exactly as specified. What remains is a short smoke test against the real instance once the binary exists:

1. `GET /api/session/` returns `authenticated: true` and the expected username.
2. A disposable review request accepts `status=submitted` on close and `status=pending` on reopen.
3. A diff upload with the caller-supplied `basedir` succeeds (no error 207). Repeat once per repository used.
4. A reply to a diff comment is accepted with `reply_to_id` + `text`; if the server rejects it, add
   `base_filediff_id` from the original comment and record the requirement in §8.

No other behavior is unverified; everything else is the documented 7.x API.
