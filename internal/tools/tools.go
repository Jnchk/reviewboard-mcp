package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"reviewboard-mcp/internal/format"
	"reviewboard-mcp/internal/reviewboard"
)

// Deps is everything the tools need. One Deps belongs to one Review Board server.
type Deps struct {
	C            *reviewboard.Client
	M            *reviewboard.Meta
	ReadOnly     bool
	DefaultLimit int
	TextType     string // markdown | plain, sent as *_text_type
}

func Register(srv *mcp.Server, d *Deps) {
	add(srv, &mcp.Tool{
		Name:        "whoami",
		Description: "Show the connected Review Board user, server URL, read-only mode and whether user names can be resolved. Call this first.",
		Annotations: readOnly,
	}, d.whoami)

	add(srv, &mcp.Tool{
		Name:        "list_review_requests",
		Description: "Find review requests. Filters: status, repository, from_user, to_user, date ranges. Defaults to pending, 25 per page; status=all widens it.",
		Annotations: readOnly,
	}, d.listReviewRequests)

	add(srv, &mcp.Tool{
		Name:        "get_review_request",
		Description: "Read one review request: fields, reviewers, issue counts, diff revisions and its URL.",
		Annotations: readOnly,
	}, d.getReviewRequest)

	add(srv, &mcp.Tool{
		Name:        "get_diff",
		Description: "Read a diff revision: file table with filediff ids and the unified patch. Omit revision for the latest published diff; revision 0 means the current draft.",
		Annotations: readOnly,
	}, d.getDiff)

	add(srv, &mcp.Tool{
		Name:        "list_reviews",
		Description: "List reviews on a review request with their general and inline comments, including the comment ids needed for reply_review.",
		Annotations: readOnly,
	}, d.listReviews)

	add(srv, &mcp.Tool{
		Name:        "create_review_request",
		Description: "Create and publish a review request. repository and summary are required; reviewers accept names or ids. Diffs are uploaded separately with upload_diff.",
		Annotations: write(false),
	}, d.createReviewRequest)

	add(srv, &mcp.Tool{
		Name: "update_review_request",
		Description: "Update a review request through its draft, then publish. Reviewer lists are replaced, not appended. " +
			"Replacing description or testing_done needs confirm: true. publish: false leaves the changes in the draft.",
		Annotations: write(true),
	}, d.updateReviewRequest)

	add(srv, &mcp.Tool{
		Name: "upload_diff",
		Description: "Upload a unified diff file from the machine running this server to a review request, then publish. " +
			"basedir depends on the repository and the diff and must be supplied; a wrong value fails with 207.",
		Annotations: write(false),
	}, d.uploadDiff)

	add(srv, &mcp.Tool{
		Name: "create_review",
		Description: "Post a published review: body, Ship It, general comments and inline diff comments. " +
			"diff_comments need a filediff id from get_diff and 1-based line numbers in the patched file.",
		Annotations: write(false),
	}, d.createReview)

	add(srv, &mcp.Tool{
		Name:        "reply_review",
		Description: "Reply to comments on a review and publish the reply. Use comment ids from list_reviews; kind is general or diff.",
		Annotations: write(false),
	}, d.replyReview)

	add(srv, &mcp.Tool{
		Name:        "close_review_request",
		Description: "Close a review request as submitted or discarded, or reopen it as pending. Closing needs confirm: true.",
		Annotations: write(true),
	}, d.closeReviewRequest)

	add(srv, &mcp.Tool{
		Name:        "list_metadata",
		Description: "List repositories, users or review groups. Use it to discover valid names before creating a review request or adding reviewers.",
		Annotations: readOnly,
	}, d.listMetadata)
}

// add adapts a string-returning handler to the SDK's typed tool API.
func add[In any](srv *mcp.Server, t *mcp.Tool, h func(context.Context, In) (string, error)) {
	mcp.AddTool(srv, t, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		s, err := h(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		return text(s)
	})
}

func text(s string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}, nil, nil
}

var readOnly = &mcp.ToolAnnotations{ReadOnlyHint: true}

func write(destructive bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: &destructive}
}

func (d *Deps) guardWrite() error {
	if d.ReadOnly {
		return fmt.Errorf("reviewboard: this server runs in read-only mode (REVIEWBOARD_READ_ONLY); write tools are disabled")
	}
	return nil
}

func (d *Deps) limit(n int) int {
	switch {
	case n <= 0:
		return d.DefaultLimit
	case n > 200:
		return 200
	default:
		return n
	}
}

// rawClip returns untouched JSON, truncated with an explicit note.
func rawClip(data []byte) string {
	var v any
	if json.Unmarshal(data, &v) == nil {
		if pretty, err := json.MarshalIndent(v, "", "  "); err == nil {
			data = pretty
		}
	}
	return format.Clip(string(data), 128<<10)
}

// userName resolves a user argument to the username Review Board's query params expect.
func (d *Deps) userName(ctx context.Context, value string) (string, error) {
	u, err := d.M.User(ctx, value)
	if err != nil {
		return "", err
	}
	if u.Username != "" {
		return u.Username, nil
	}
	return fmt.Sprintf("%d", u.ID), nil
}
