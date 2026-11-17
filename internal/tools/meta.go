package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"reviewboard-mcp/internal/format"
	"reviewboard-mcp/internal/reviewboard"
)

type whoamiIn struct{}

func (d *Deps) whoami(ctx context.Context, _ whoamiIn) (string, error) {
	s, err := d.C.Session(ctx)
	if err != nil {
		return "", err
	}
	names := "yes"
	if _, err := d.C.Users(ctx, "a"); err != nil {
		names = "no"
		if reviewboard.IsStatus(err, 403) {
			names += " (the token cannot list users; pass usernames or numeric ids)"
		}
	}
	mode := "read-write"
	if d.ReadOnly {
		mode = "read-only (write tools return an error)"
	}
	var b strings.Builder
	b.WriteString("# reviewboard-mcp\n")
	fmt.Fprintf(&b, "url: %s\n", d.C.Base())
	fmt.Fprintf(&b, "user: %s\n", s.UserName())
	fmt.Fprintf(&b, "authenticated: %t\n", s.Authenticated)
	fmt.Fprintf(&b, "mode: %s\n", mode)
	fmt.Fprintf(&b, "user names: %s\n", names)
	return b.String(), nil
}

type metadataIn struct {
	Kind  string `json:"kind" jsonschema:"repositories, users, or groups"`
	Q     string `json:"q,omitempty" jsonschema:"prefix or substring filter on the name"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum rows, 1-200 (default 25)"`
}

func (d *Deps) listMetadata(ctx context.Context, in metadataIn) (string, error) {
	limit := d.limit(in.Limit)
	q := strings.ToLower(strings.TrimSpace(in.Q))
	var b strings.Builder

	switch strings.ToLower(strings.TrimSpace(in.Kind)) {
	case "repositories", "repos", "repository":
		items, err := d.C.Repositories(ctx)
		if err != nil {
			return "", err
		}
		b.WriteString("# id | name | tool | path\n")
		n := 0
		for _, r := range items {
			if q != "" && !strings.Contains(strings.ToLower(r.Name), q) && !strings.Contains(strings.ToLower(r.Path), q) {
				continue
			}
			if n == limit {
				break
			}
			b.WriteString(format.Row(strconv.Itoa(r.ID), r.Name, r.Tool, r.Path) + "\n")
			n++
		}
		fmt.Fprintf(&b, "# returned %d of %d (limit %d)\n", n, len(items), limit)
	case "users", "user":
		items, err := d.C.Users(ctx, in.Q)
		if err != nil {
			return "", err
		}
		b.WriteString("# id | username | name\n")
		n := 0
		for _, u := range items {
			if n == limit {
				break
			}
			b.WriteString(format.Row(strconv.Itoa(u.ID), u.Username, u.FullName()) + "\n")
			n++
		}
		fmt.Fprintf(&b, "# returned %d of %d (limit %d)\n", n, len(items), limit)
	case "groups", "group":
		items, err := d.C.Groups(ctx, in.Q)
		if err != nil {
			return "", err
		}
		b.WriteString("# id | name | display name\n")
		n := 0
		for _, g := range items {
			if n == limit {
				break
			}
			b.WriteString(format.Row(strconv.Itoa(g.ID), g.Name, g.DisplayName) + "\n")
			n++
		}
		fmt.Fprintf(&b, "# returned %d of %d (limit %d)\n", n, len(items), limit)
	default:
		return "", fmt.Errorf("reviewboard: kind must be repositories, users or groups")
	}
	return b.String(), nil
}
