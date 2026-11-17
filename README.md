# reviewboard-mcp

MCP server for Review Board 7.0.4, written in Go. One process serves one Review Board instance over REST API 2.0.
Reads review requests, diffs and comments; creates and updates review requests, uploads diffs, posts reviews and
replies, and closes requests. Returns compact plain text instead of raw JSON.

Design: [docs/00_basic_design.md](docs/00_basic_design.md).

## Build

```bash
go build -o reviewboard-mcp ./cmd/reviewboard-mcp
go test -race ./...
```

Go 1.26.4. Only direct dependency: `github.com/modelcontextprotocol/go-sdk`.

## Configure

| Variable | Default | Meaning |
|---|---|---|
| `REVIEWBOARD_URL` | required | `https://reviews.example.com` (no `/api` suffix; a sub-path is fine) |
| `REVIEWBOARD_API_TOKEN` | required | `Authorization: token <token>`, from **My Account → API Tokens** |
| `REVIEWBOARD_INSECURE` | `false` | skip TLS verification (self-signed internal certificate) |
| `REVIEWBOARD_READ_ONLY` | `false` | disable every write tool |
| `REVIEWBOARD_TIMEOUT` | `45s` | per-request timeout |
| `REVIEWBOARD_DEFAULT_LIMIT` | `25` | page size when a tool omits `limit` (max 200) |
| `REVIEWBOARD_TEXT_TYPE` | `markdown` | `markdown` or `plain`; sent as `*_text_type` |
| `REVIEWBOARD_MAX_UPLOAD` | `25 MiB` | diff file size cap |
| `REVIEWBOARD_META_TTL` | `5m` | repository/user/group cache lifetime |
| `MCP_TRANSPORT` | `stdio` | `stdio` or `http` |
| `MCP_ADDR` | `127.0.0.1:8080` | HTTP listen address(es), comma-separated |
| `MCP_PATH` | `/mcp` | HTTP endpoint path |
| `MCP_TOKEN` | — | bearer token; required when any address is non-loopback |

Flags: `--transport`, `--addr`, `--read-only` override the env values.

Startup calls `GET /api/session/`; if the token is rejected the process exits instead of serving a broken client.

## Run

stdio:

```bash
REVIEWBOARD_URL=https://reviews.example.com REVIEWBOARD_API_TOKEN=xxx ./reviewboard-mcp
```

Streamable HTTP (SSE, stateless):

```bash
REVIEWBOARD_URL=https://reviews.example.com REVIEWBOARD_API_TOKEN=xxx \
MCP_TRANSPORT=http MCP_ADDR=127.0.0.1:8080 MCP_TOKEN=change-me ./reviewboard-mcp
```

```bash
curl -N http://127.0.0.1:8080/mcp \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'
```

Client config (opencode, stdio):

```json
{
  "mcp": {
    "servers": {
      "reviewboard": {
        "type": "local",
        "command": ["/path/to/reviewboard-mcp"],
        "environment": {
          "REVIEWBOARD_URL": "https://reviews.example.com",
          "REVIEWBOARD_API_TOKEN": "{env:REVIEWBOARD_API_TOKEN}"
        }
      }
    }
  }
}
```

## Tools

| Tool | Writes | Notes |
|---|---|---|
| `whoami` | no | connectivity, user, read-only mode, whether user names resolve |
| `list_review_requests` | no | status/repository/user/date filters, pagination |
| `get_review_request` | no | fields, reviewers, issue counts, diff revisions, URL |
| `get_diff` | no | file table with `filediff_id`, raw patch, draft or a revision |
| `list_reviews` | no | reviews with general and inline comments, including comment ids |
| `create_review_request` | yes | repository + summary, reviewers by name, publish |
| `update_review_request` | yes | draft fields then publish; reviewer lists replace |
| `upload_diff` | yes | multipart upload of a local diff file, then publish |
| `create_review` | yes | body, Ship It, general and inline comments |
| `reply_review` | yes | reply to comment ids, then publish |
| `close_review_request` | yes | submitted, discarded, or reopen as pending |
| `list_metadata` | no | repositories, users, groups |

## Behavior worth knowing

- **Draft/publish.** A review request, diff, review or reply is invisible until published. The tools publish by
  default; `publish: false` leaves a draft. A published review or reply cannot be edited again.
- **`basedir` is per upload.** It depends on the repository and on the diff's root. A wrong value fails with error
  207; fix it with a new upload, do not retry the same call.
- **Inline comments** need a `filediff_id` from `get_diff` and 1-based line numbers in the patched file.
- **Status values** are `pending`, `submitted`, `discarded`.
- **Paths are on the process host.** Over HTTP, `upload_diff.path` reads the server machine's filesystem; only
  regular files within `REVIEWBOARD_MAX_UPLOAD` are read.

## Known limits

- Diffs are not generated; the caller supplies a unified diff.
- Review requests are never deleted (the API allows it; this server does not).
- Screenshots, file attachments, webhooks, extensions and OAuth2 are out of scope.
- The code has not yet been exercised against a live Review Board; the first `whoami` and a disposable review
  request are the verification step (`docs/00_basic_design.md` §12).
