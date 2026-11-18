---
name: reviewboard-upload
description: Push long review-request text (description, testing done, review body) through mcp-gateway without sending it through the model. Use when creating or reviewing a Review Board request with a large body.
---

# Long Review Board text (through mcp-gateway)

Review Board request descriptions, "testing done" and review bodies are often long. Stage them through
the gateway and pass the returned id; only the id crosses the model context.

```bash
python3 ~/.config/opencode/skills/mcp-upload/scripts/mcp_upload.py --server reviewboard --path ./description.md
# 9f1c0b7a4e2d4c3b8a5f6e7d0c1b2a39
```

```js
// New request
await tools.gateway.call_tool({
  server: "reviewboard",
  tool: "create_review_request",
  arguments: {
    repository: "web-svn",
    summary: "Add CSV export",
    description_upload_id: "9f1c0b7a4e2d4c3b8a5f6e7d0c1b2a39",
    testing_done_upload_id: "<another id>"
  }
})

// Review body
await tools.gateway.call_tool({
  server: "reviewboard",
  tool: "create_review",
  arguments: { id: 42, body_top_upload_id: "<id>", ship_it: true }
})
```

## Rules

- Inline text and an upload id are mutually exclusive per field; passing both is an error.
- The id is single-use, expires after 15 minutes, accepts UTF-8 text only, and is bounded by the
  server's `REVIEWBOARD_MAX_UPLOAD` (25 MiB default). It is consumed only when the write succeeds, so a
  failed call can be retried with the same id.
- The gateway must allow `create_review_request` or `create_review` (`upload_requires_any`), otherwise
  the upload answers 403 before contacting Review Board.
- Diffs are **not** part of this route: `upload_diff` still takes a local path on the server host and
  uses the multipart draft API.
- Small fields (`summary`, `branch`, `bugs_closed`, reviewer lists, comments) stay inline.
