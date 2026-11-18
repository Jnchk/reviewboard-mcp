---
name: reviewboard
description: Read and write Review Board review requests, diffs and reviews, including long descriptions and review bodies staged from a local file. Use when the user asks about review requests, code review, diffs, Ship It, or replying to review comments.
compatibility: Requires reviewboard-mcp, connected directly or as an upstream named "reviewboard" on mcp-gateway.
---

# Review Board

Call `whoami` first: it reports the server, the signed-in user, read-only mode and whether user names
resolve. Resolve repositories, users and groups through `list_metadata`.

## Mode

- Tools appear directly: call them as tools.
- Only `list_servers` / `list_tools` / `call_tool` exist: `tools.gateway.call_tool({ server:
  "reviewboard", tool: "<name>", arguments: {...} })`. `list_tools({ server: "reviewboard", full: true })`
  returns the schemas.
- Read-only mode rejects every write tool.

## Task → tool

| Goal | Tool | Required / easy to miss |
|---|---|---|
| who am I | `whoami` | — |
| repositories, users, groups | `list_metadata` | names or ids, both accepted |
| list requests | `list_review_requests` | `status` defaults to `pending`; pass `all` to widen |
| one request | `get_review_request` | `id`; includes reviewer state, diff revisions, URL |
| inspect a diff | `get_diff` | `id`; `revision` omitted = latest published, `0` = current draft; `files_only` for the file table with `filediff_id` |
| existing reviews and comments | `list_reviews` | `id`; gives comment ids for replies |
| create a request | `create_review_request` | `repository` + `summary`; `publish` defaults to true |
| edit a request | `update_review_request` | `id`; reviewer lists **replace**, not append |
| attach a diff | `upload_diff` | `id` + `path` **on the server host**; this is a multipart draft upload, not the staging route |
| write a review | `create_review` | `id`; `ship_it`, general comments, inline comments by `filediff_id` |
| reply to comments | `reply_review` | `id` + `review_id` + `reply_to_id` from `list_reviews` |
| close or reopen | `close_review_request` | `id`; `submitted`, `discarded` or back to `pending` |

## Long text from a local file

Descriptions, testing done and review bodies are often long. Stage the file and pass the id; inline
text and an upload id are mutually exclusive per field. Never base64 text into an argument.

```bash
# through mcp-gateway
python3 <mcp-gateway clone>/skills/mcp-upload/scripts/mcp_upload.py --server reviewboard --path ./description.md
# direct HTTP to reviewboard-mcp (token = that server's MCP_TOKEN)
curl -sS -X POST "http://<server>:8080/upload?filename=description.md" \
  -H "Authorization: Bearer <MCP_TOKEN>" --data-binary @description.md
```

```js
await tools.gateway.call_tool({ server: "reviewboard", tool: "create_review_request",
  arguments: { repository: "web-svn", summary: "Add CSV export",
               description_upload_id: "<id>", testing_done_upload_id: "<id>" } })
```

- Fields taking an id: `description_upload_id`, `testing_done_upload_id`,
  `body_top_upload_id`, `body_bottom_upload_id`.
- The id is single use, expires after 15 minutes, UTF-8 only, bounded by `REVIEWBOARD_MAX_UPLOAD`
  (25 MiB default). A failed write keeps it for a retry.
- Direct stdio has no `/upload` route; pass the text inline there.
- Diffs are not part of this route: `upload_diff` still takes a path on the server host.
- Small fields (`summary`, `branch`, `bugs_closed`, reviewer lists, comments) stay inline.
