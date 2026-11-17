package tools

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"reviewboard-mcp/internal/format"
	"reviewboard-mcp/internal/reviewboard"
)

type listRequestsIn struct {
	Status        string `json:"status,omitempty" jsonschema:"pending (default), submitted, discarded, all"`
	Repository    string `json:"repository,omitempty" jsonschema:"repository name, path or id"`
	FromUser      string `json:"from_user,omitempty" jsonschema:"submitter username, full name, id, or me"`
	ToUser        string `json:"to_user,omitempty" jsonschema:"reviewer username, full name, id, or me"`
	TimeAddedFrom string `json:"time_added_from,omitempty" jsonschema:"added on/after, YYYY-MM-DD"`
	TimeAddedTo   string `json:"time_added_to,omitempty" jsonschema:"added on/before, YYYY-MM-DD"`
	UpdatedFrom   string `json:"updated_from,omitempty" jsonschema:"last updated on/after, YYYY-MM-DD"`
	UpdatedTo     string `json:"updated_to,omitempty" jsonschema:"last updated on/before, YYYY-MM-DD"`
	Limit         int    `json:"limit,omitempty" jsonschema:"page size, 1-200 (default 25)"`
	Offset        int    `json:"offset,omitempty" jsonschema:"pagination offset (0-based)"`
	Raw           bool   `json:"raw,omitempty" jsonschema:"return the untouched Review Board JSON"`
}

func (d *Deps) listReviewRequests(ctx context.Context, in listRequestsIn) (string, error) {
	status, err := statusParam(in.Status)
	if err != nil {
		return "", err
	}
	q := reviewboard.RequestQuery{
		Status:        status,
		TimeAddedFrom: isoDay(in.TimeAddedFrom, false),
		TimeAddedTo:   isoDay(in.TimeAddedTo, true),
		UpdatedFrom:   isoDay(in.UpdatedFrom, false),
		UpdatedTo:     isoDay(in.UpdatedTo, true),
		Limit:         d.limit(in.Limit),
		Offset:        in.Offset,
	}
	if in.Repository != "" {
		r, err := d.M.Repository(ctx, in.Repository)
		if err != nil {
			return "", err
		}
		q.Repository = strconv.Itoa(r.ID)
	}
	if in.FromUser != "" {
		if q.FromUser, err = d.userName(ctx, in.FromUser); err != nil {
			return "", err
		}
	}
	if in.ToUser != "" {
		if q.ToUser, err = d.userName(ctx, in.ToUser); err != nil {
			return "", err
		}
	}

	if in.Raw {
		data, err := d.C.ReviewRequestsRaw(ctx, q)
		if err != nil {
			return "", err
		}
		return rawClip(data), nil
	}

	items, total, err := d.C.ReviewRequests(ctx, q)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return fmt.Sprintf("no review requests matched (status=%s). Widen the filters or use status=all.", status), nil
	}

	var b strings.Builder
	b.WriteString("# id | status | submitter | repository | updated | ship-its | issues o/r/d | summary\n")
	for _, r := range items {
		b.WriteString(format.Row(
			strconv.Itoa(r.ID),
			r.Status,
			r.SubmitterName(),
			r.RepositoryLink().Name(),
			format.When(r.LastUpdated),
			strconv.Itoa(r.ShipItCount),
			fmt.Sprintf("%d/%d/%d", r.IssueOpenCount, r.IssueResolvedCount, r.IssueDroppedCount),
			format.Truncate(r.Summary, 120),
		) + "\n")
	}
	fmt.Fprintf(&b, "# returned %d of %d (offset %d, limit %d)\n", len(items), total, q.Offset, q.Limit)
	return b.String(), nil
}

func statusParam(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "open", "pending":
		return "pending", nil
	case "submitted", "closed", "completed":
		return "submitted", nil
	case "discarded":
		return "discarded", nil
	case "all":
		return "all", nil
	}
	return "", fmt.Errorf("reviewboard: status must be pending, submitted, discarded or all, got %q", s)
}

type getRequestIn struct {
	ID  int  `json:"id,omitempty" jsonschema:"review request id"`
	Raw bool `json:"raw,omitempty" jsonschema:"return the untouched Review Board JSON"`
}

func (d *Deps) getReviewRequest(ctx context.Context, in getRequestIn) (string, error) {
	if in.ID <= 0 {
		return "", fmt.Errorf("reviewboard: id is required")
	}
	if in.Raw {
		data, err := d.C.RawGet(ctx, fmt.Sprintf("api/review-requests/%d/", in.ID), nil)
		if err != nil {
			return "", err
		}
		return rawClip(data), nil
	}
	r, err := d.C.ReviewRequest(ctx, in.ID)
	if err != nil {
		return "", err
	}
	return d.renderRequest(ctx, r)
}

func (d *Deps) renderRequest(ctx context.Context, r *reviewboard.ReviewRequest) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s\n", r.ID, format.Truncate(r.Summary, 120))
	b.WriteString(format.Row(
		"status: "+dash(r.Status),
		"repository: "+dash(r.RepositoryLink().Name()),
		"submitter: "+dash(r.SubmitterName()),
		"updated: "+dash(format.When(r.LastUpdated)),
	) + "\n")
	approved := "no"
	if r.Approved {
		approved = "yes"
	}
	b.WriteString(format.Row(
		"approved: "+approved,
		"ship its: "+strconv.Itoa(r.ShipItCount),
		fmt.Sprintf("issues: open %d / resolved %d / dropped %d", r.IssueOpenCount, r.IssueResolvedCount, r.IssueDroppedCount),
	) + "\n")
	var reviewers []string
	for _, p := range r.TargetPeople {
		reviewers = append(reviewers, p.Name())
	}
	for _, g := range r.TargetGroups {
		reviewers = append(reviewers, g.Name())
	}
	fmt.Fprintf(&b, "reviewers: %s\n", dash(strings.Join(reviewers, ", ")))
	url := r.URL()
	if url == "" {
		url = d.C.ReviewRequestURL(r.ID)
	}
	fmt.Fprintf(&b, "url: %s\n", url)
	if s := strings.TrimSpace(r.Description); s != "" {
		fmt.Fprintf(&b, "\n[description]\n%s\n", format.Clip(s, 4000))
	}
	if s := strings.TrimSpace(r.TestingDone); s != "" {
		fmt.Fprintf(&b, "\n[testing done]\n%s\n", format.Clip(s, 4000))
	}

	diffs, err := d.C.Diffs(ctx, r.ID, false)
	if err != nil {
		return "", err
	}
	b.WriteString("\n[diffs]\n")
	if len(diffs) == 0 {
		b.WriteString("-\n")
	} else {
		for _, df := range diffs {
			fmt.Fprintf(&b, "%s\n", format.Row(
				"revision "+strconv.Itoa(df.Revision),
				dash(df.Name),
				"basedir: "+dash(df.Basedir),
				format.When(df.Timestamp),
			))
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

type createRequestIn struct {
	Repository   string `json:"repository" jsonschema:"repository name, path or id (required)"`
	Summary      string `json:"summary" jsonschema:"review request summary (required)"`
	Description  string `json:"description,omitempty" jsonschema:"description, markdown"`
	TestingDone  string `json:"testing_done,omitempty" jsonschema:"testing done, markdown"`
	Branch       string `json:"branch,omitempty" jsonschema:"branch name"`
	BugsClosed   string `json:"bugs_closed,omitempty" jsonschema:"comma-separated bug ids"`
	TargetPeople string `json:"target_people,omitempty" jsonschema:"comma-separated reviewer usernames, full names or ids"`
	TargetGroups string `json:"target_groups,omitempty" jsonschema:"comma-separated review group names or ids"`
	CommitID     string `json:"commit_id,omitempty" jsonschema:"commit id the change is based on"`
	Publish      *bool  `json:"publish,omitempty" jsonschema:"publish the draft (default true)"`
}

func (d *Deps) createReviewRequest(ctx context.Context, in createRequestIn) (string, error) {
	if err := d.guardWrite(); err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Repository) == "" {
		return "", fmt.Errorf("reviewboard: repository is required")
	}
	if strings.TrimSpace(in.Summary) == "" {
		return "", fmt.Errorf("reviewboard: summary is required")
	}
	repo, err := d.M.Repository(ctx, in.Repository)
	if err != nil {
		return "", err
	}
	rr, err := d.C.CreateReviewRequest(ctx, strconv.Itoa(repo.ID))
	if err != nil {
		return "", err
	}

	fields := map[string]any{"summary": in.Summary}
	if in.Description != "" {
		fields["description"] = in.Description
		fields["description_text_type"] = d.TextType
	}
	if in.TestingDone != "" {
		fields["testing_done"] = in.TestingDone
		fields["testing_done_text_type"] = d.TextType
	}
	if in.Branch != "" {
		fields["branch"] = in.Branch
	}
	if in.BugsClosed != "" {
		fields["bugs_closed"] = in.BugsClosed
	}
	if in.CommitID != "" {
		fields["commit_id"] = in.CommitID
	}
	if in.TargetPeople != "" {
		people, err := d.resolvePeople(ctx, in.TargetPeople)
		if err != nil {
			return "", err
		}
		fields["target_people"] = people
	}
	if in.TargetGroups != "" {
		groups, err := d.resolveGroups(ctx, in.TargetGroups)
		if err != nil {
			return "", err
		}
		fields["target_groups"] = groups
	}
	publish := boolOr(in.Publish, true)
	if err := d.C.UpdateDraft(ctx, rr.ID, fields, publish); err != nil {
		return "", fmt.Errorf("reviewboard: created #%d but updating the draft failed: %w", rr.ID, err)
	}
	state := "created and published"
	if !publish {
		state = "created as a draft (not published)"
	}
	return fmt.Sprintf("#%d %s (%s, %s). %s", rr.ID, state, repo.Name, format.Truncate(in.Summary, 120), d.C.ReviewRequestURL(rr.ID)), nil
}

type updateRequestIn struct {
	ID           int    `json:"id" jsonschema:"review request id"`
	Summary      string `json:"summary,omitempty" jsonschema:"new summary"`
	Description  string `json:"description,omitempty" jsonschema:"new description, markdown; replacing it needs confirm: true"`
	TestingDone  string `json:"testing_done,omitempty" jsonschema:"new testing done, markdown; replacing it needs confirm: true"`
	Branch       string `json:"branch,omitempty" jsonschema:"new branch"`
	BugsClosed   string `json:"bugs_closed,omitempty" jsonschema:"comma-separated bug ids"`
	TargetPeople string `json:"target_people,omitempty" jsonschema:"replace the reviewer list with these comma-separated users"`
	TargetGroups string `json:"target_groups,omitempty" jsonschema:"replace the reviewer list with these comma-separated groups"`
	Publish      *bool  `json:"publish,omitempty" jsonschema:"publish the draft (default true); false leaves changes in the draft"`
	Confirm      bool   `json:"confirm,omitempty" jsonschema:"required to replace description or testing_done"`
}

func (d *Deps) updateReviewRequest(ctx context.Context, in updateRequestIn) (string, error) {
	if err := d.guardWrite(); err != nil {
		return "", err
	}
	if in.ID <= 0 {
		return "", fmt.Errorf("reviewboard: id is required")
	}
	if (in.Description != "" || in.TestingDone != "") && !in.Confirm {
		return "", fmt.Errorf("reviewboard: replacing description or testing_done loses the old text; pass confirm: true")
	}
	fields := map[string]any{}
	if in.Summary != "" {
		fields["summary"] = in.Summary
	}
	if in.Description != "" {
		fields["description"] = in.Description
		fields["description_text_type"] = d.TextType
	}
	if in.TestingDone != "" {
		fields["testing_done"] = in.TestingDone
		fields["testing_done_text_type"] = d.TextType
	}
	if in.Branch != "" {
		fields["branch"] = in.Branch
	}
	if in.BugsClosed != "" {
		fields["bugs_closed"] = in.BugsClosed
	}
	if in.TargetPeople != "" {
		people, err := d.resolvePeople(ctx, in.TargetPeople)
		if err != nil {
			return "", err
		}
		fields["target_people"] = people
	}
	if in.TargetGroups != "" {
		groups, err := d.resolveGroups(ctx, in.TargetGroups)
		if err != nil {
			return "", err
		}
		fields["target_groups"] = groups
	}
	if len(fields) == 0 {
		return "", fmt.Errorf("reviewboard: empty update: pass at least one field")
	}
	publish := boolOr(in.Publish, true)
	if err := d.C.UpdateDraft(ctx, in.ID, fields, publish); err != nil {
		return "", err
	}
	state := "updated and published"
	if !publish {
		state = "updated as a draft (not published)"
	}
	changed := make([]string, 0, len(fields))
	for k := range fields {
		if !strings.HasSuffix(k, "_text_type") {
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	return fmt.Sprintf("#%d %s: %s. %s", in.ID, state, strings.Join(changed, ", "), d.C.ReviewRequestURL(in.ID)), nil
}

type closeRequestIn struct {
	ID          int    `json:"id" jsonschema:"review request id"`
	Status      string `json:"status" jsonschema:"submitted, discarded, or pending (reopen)"`
	Description string `json:"description,omitempty" jsonschema:"close description"`
	Confirm     bool   `json:"confirm,omitempty" jsonschema:"required to close as submitted or discarded"`
}

func (d *Deps) closeReviewRequest(ctx context.Context, in closeRequestIn) (string, error) {
	if err := d.guardWrite(); err != nil {
		return "", err
	}
	if in.ID <= 0 {
		return "", fmt.Errorf("reviewboard: id is required")
	}
	status := strings.ToLower(strings.TrimSpace(in.Status))
	switch status {
	case "submitted", "discarded":
		if !in.Confirm {
			return "", fmt.Errorf("reviewboard: closing as %s is irreversible; pass confirm: true", status)
		}
	case "pending":
	default:
		return "", fmt.Errorf("reviewboard: status must be submitted, discarded or pending")
	}
	if err := d.C.SetStatus(ctx, in.ID, status, in.Description, d.TextType); err != nil {
		return "", err
	}
	return fmt.Sprintf("#%d status -> %s. %s", in.ID, status, d.C.ReviewRequestURL(in.ID)), nil
}

func (d *Deps) resolvePeople(ctx context.Context, list string) (string, error) {
	var out []string
	for _, raw := range strings.Split(list, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		u, err := d.M.User(ctx, raw)
		if err != nil {
			return "", err
		}
		if u.Username != "" {
			out = append(out, u.Username)
		} else {
			out = append(out, strconv.Itoa(u.ID))
		}
	}
	return strings.Join(out, ","), nil
}

func (d *Deps) resolveGroups(ctx context.Context, list string) (string, error) {
	var out []string
	for _, raw := range strings.Split(list, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		g, err := d.M.Group(ctx, raw)
		if err != nil {
			return "", err
		}
		if g.Name != "" {
			out = append(out, g.Name)
		} else {
			out = append(out, strconv.Itoa(g.ID))
		}
	}
	return strings.Join(out, ","), nil
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// isoDay widens a YYYY-MM-DD filter to an ISO 8601 timestamp Review Board accepts.
func isoDay(s string, end bool) string {
	s = strings.TrimSpace(s)
	if len(s) == 10 && s[4] == '-' && s[7] == '-' {
		if end {
			return s + "T23:59:59"
		}
		return s + "T00:00:00"
	}
	return s
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
