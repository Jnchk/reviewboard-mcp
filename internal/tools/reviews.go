package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"reviewboard-mcp/internal/format"
	"reviewboard-mcp/internal/reviewboard"
)

type listReviewsIn struct {
	ID     int `json:"id" jsonschema:"review request id"`
	Limit  int `json:"limit,omitempty" jsonschema:"maximum reviews, 1-200 (default 50)"`
	Offset int `json:"offset,omitempty" jsonschema:"pagination offset (0-based)"`
}

func (d *Deps) listReviews(ctx context.Context, in listReviewsIn) (string, error) {
	if in.ID <= 0 {
		return "", fmt.Errorf("reviewboard: id is required")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	reviews, total, err := d.C.Reviews(ctx, in.ID, limit, in.Offset)
	if err != nil {
		return "", err
	}
	if len(reviews) == 0 {
		return fmt.Sprintf("no reviews on #%d yet.", in.ID), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# reviews for #%d\n", in.ID)
	const maxCommentsPerReview = 20
	for _, r := range reviews {
		ship := "no"
		if r.ShipIt {
			ship = "yes"
		}
		fmt.Fprintf(&b, "# review %d | %s | %s | ship_it: %s\n", r.ID, dash(r.UserName()), format.When(r.Timestamp), ship)
		if s := strings.TrimSpace(r.BodyTop); s != "" {
			b.WriteString(format.Clip(s, 600) + "\n")
		}
		shown := 0
		if href := r.Links["general_comments"].Href; href != "" {
			cs, err := d.C.GeneralComments(ctx, href)
			if err != nil {
				return "", err
			}
			for _, c := range cs {
				if shown == maxCommentsPerReview {
					fmt.Fprintf(&b, "# %d more comments not shown\n", len(cs)-shown)
					break
				}
				fmt.Fprintf(&b, "  general comment %d%s | %s\n", c.ID, issueTag(&c), commentText(&c))
				shown++
			}
		}
		if href := r.Links["diff_comments"].Href; href != "" {
			cs, err := d.C.DiffComments(ctx, href)
			if err != nil {
				return "", err
			}
			for _, c := range cs {
				if shown == maxCommentsPerReview {
					fmt.Fprintf(&b, "# %d more comments not shown\n", len(cs)-shown)
					break
				}
				fmt.Fprintf(&b, "  diff comment %d%s%s | %s\n", c.ID, issueTag(&c), diffRef(&c), commentText(&c))
				shown++
			}
		}
		if s := strings.TrimSpace(r.BodyBottom); s != "" {
			b.WriteString(format.Clip(s, 600) + "\n")
		}
	}
	fmt.Fprintf(&b, "# returned %d of %d (offset %d, limit %d)\n", len(reviews), total, in.Offset, limit)
	return strings.TrimRight(b.String(), "\n"), nil
}

func issueTag(c *reviewboard.Comment) string {
	if c.IssueStatus != "" {
		return " [" + c.IssueStatus + "]"
	}
	if c.IssueOpened {
		return " [open]"
	}
	return ""
}

func diffRef(c *reviewboard.Comment) string {
	if c.FileName() == "" && c.FirstLine == 0 {
		return ""
	}
	loc := shortPath(c.FileName())
	if c.FirstLine > 0 {
		loc += fmt.Sprintf(":%d", c.FirstLine)
		if c.NumLines > 1 {
			loc += fmt.Sprintf("-%d", c.FirstLine+c.NumLines-1)
		}
	}
	if id := filediffID(c); id != "" {
		return " (file " + id + " " + loc + ")"
	}
	return " (" + loc + ")"
}

// shortPath trims the long "(rev) -> URL (rev)" title Review Board uses for
// file diffs down to something that fits on one line.
func shortPath(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, " -> "); i >= 0 {
		s = s[i+4:]
	}
	for strings.HasSuffix(s, ")") {
		i := strings.LastIndex(s, " (")
		if i <= 0 {
			break
		}
		s = s[:i]
	}
	if u, err := url.Parse(s); err == nil && u.Path != "" {
		s = u.Path
	}
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) > 3 {
		s = strings.Join(parts[len(parts)-3:], "/")
	}
	return s
}

func filediffID(c *reviewboard.Comment) string {
	if l, ok := c.Links["filediff"]; ok && l.Href != "" {
		href := strings.TrimRight(l.Href, "/")
		if i := strings.LastIndex(href, "/"); i >= 0 {
			return href[i+1:]
		}
	}
	return ""
}

func commentText(c *reviewboard.Comment) string {
	return format.Truncate(c.Text, 600)
}

type generalCommentIn struct {
	Text        string `json:"text" jsonschema:"comment text"`
	IssueOpened bool   `json:"issue_opened,omitempty" jsonschema:"open an issue on this comment"`
}

type diffCommentIn struct {
	FilediffID  int    `json:"filediff_id" jsonschema:"file diff id from get_diff"`
	FirstLine   int    `json:"first_line" jsonschema:"1-based line in the patched file"`
	NumLines    int    `json:"num_lines" jsonschema:"number of lines; 0 pins one line"`
	Text        string `json:"text" jsonschema:"comment text"`
	IssueOpened bool   `json:"issue_opened,omitempty" jsonschema:"open an issue on this comment"`
}

type createReviewIn struct {
	ID                 int                `json:"id" jsonschema:"review request id"`
	BodyTop            string             `json:"body_top,omitempty" jsonschema:"review text above the comments; required unless body_top_upload_id is set"`
	BodyBottom         string             `json:"body_bottom,omitempty" jsonschema:"review text below the comments; required unless body_bottom_upload_id is set"`
	BodyTopUploadID    string             `json:"body_top_upload_id,omitempty" jsonschema:"id from POST /upload; use instead of body_top for large text"`
	BodyBottomUploadID string             `json:"body_bottom_upload_id,omitempty" jsonschema:"id from POST /upload; use instead of body_bottom for large text"`
	ShipIt             bool               `json:"ship_it,omitempty" jsonschema:"mark Ship It!"`
	GeneralComments    []generalCommentIn `json:"general_comments,omitempty" jsonschema:"general comments"`
	DiffComments       []diffCommentIn    `json:"diff_comments,omitempty" jsonschema:"inline comments on file diffs"`
}

func (d *Deps) createReview(ctx context.Context, in createReviewIn) (string, error) {
	if err := d.guardWrite(); err != nil {
		return "", err
	}
	if in.ID <= 0 {
		return "", fmt.Errorf("reviewboard: id is required")
	}
	bodyTop, err := d.resolveUpload("body_top", in.BodyTop, in.BodyTopUploadID)
	if err != nil {
		return "", err
	}
	bodyBottom, err := d.resolveUpload("body_bottom", in.BodyBottom, in.BodyBottomUploadID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(bodyTop) == "" && strings.TrimSpace(bodyBottom) == "" && !in.ShipIt &&
		len(in.GeneralComments) == 0 && len(in.DiffComments) == 0 {
		return "", fmt.Errorf("reviewboard: empty review: add body text, ship_it, or at least one comment")
	}
	input := reviewboard.ReviewInput{BodyTop: bodyTop, BodyBottom: bodyBottom, ShipIt: in.ShipIt}
	for i, c := range in.DiffComments {
		if c.FilediffID <= 0 {
			return "", fmt.Errorf("reviewboard: diff_comments[%d].filediff_id is required; get it from get_diff", i)
		}
		if c.FirstLine < 1 {
			return "", fmt.Errorf("reviewboard: diff_comments[%d].first_line must be 1-based", i)
		}
		if c.NumLines < 0 {
			return "", fmt.Errorf("reviewboard: diff_comments[%d].num_lines must be >= 0", i)
		}
		if strings.TrimSpace(c.Text) == "" {
			return "", fmt.Errorf("reviewboard: diff_comments[%d].text is required", i)
		}
		input.DiffComments = append(input.DiffComments, reviewboard.DiffCommentInput{
			FilediffID: c.FilediffID, FirstLine: c.FirstLine, NumLines: c.NumLines,
			Text: c.Text, IssueOpened: c.IssueOpened,
		})
	}
	for i, c := range in.GeneralComments {
		if strings.TrimSpace(c.Text) == "" {
			return "", fmt.Errorf("reviewboard: general_comments[%d].text is required", i)
		}
		input.GeneralComments = append(input.GeneralComments, reviewboard.GeneralCommentInput{
			Text: c.Text, IssueOpened: c.IssueOpened,
		})
	}
	rid, err := d.C.CreateReview(ctx, in.ID, input, d.TextType)
	if err != nil {
		return "", err
	}
	d.consumeUploads(in.BodyTopUploadID, in.BodyBottomUploadID)
	return fmt.Sprintf("#%d review %d posted (ship_it: %t, %d general, %d diff comments). %s",
		in.ID, rid, in.ShipIt, len(in.GeneralComments), len(in.DiffComments), d.reviewURL(in.ID, rid)), nil
}

type replyCommentIn struct {
	ReplyToID int    `json:"reply_to_id" jsonschema:"comment id from list_reviews"`
	Kind      string `json:"kind,omitempty" jsonschema:"general or diff (default general)"`
	Text      string `json:"text" jsonschema:"reply text"`
}

type replyReviewIn struct {
	ID         int              `json:"id" jsonschema:"review request id"`
	ReviewID   int              `json:"review_id" jsonschema:"review id from list_reviews"`
	BodyTop    string           `json:"body_top,omitempty" jsonschema:"reply text above the comments"`
	BodyBottom string           `json:"body_bottom,omitempty" jsonschema:"reply text below the comments"`
	Comments   []replyCommentIn `json:"comments,omitempty" jsonschema:"replies to existing comments"`
}

func (d *Deps) replyReview(ctx context.Context, in replyReviewIn) (string, error) {
	if err := d.guardWrite(); err != nil {
		return "", err
	}
	if in.ID <= 0 || in.ReviewID <= 0 {
		return "", fmt.Errorf("reviewboard: id and review_id are required")
	}
	if len(in.Comments) == 0 && strings.TrimSpace(in.BodyTop) == "" && strings.TrimSpace(in.BodyBottom) == "" {
		return "", fmt.Errorf("reviewboard: empty reply: add comments or body text")
	}
	replyID, err := d.C.CreateReply(ctx, in.ID, in.ReviewID, in.BodyTop, in.BodyBottom, d.TextType)
	if err != nil {
		return "", err
	}
	for i, c := range in.Comments {
		if c.ReplyToID <= 0 {
			return "", fmt.Errorf("reviewboard: comments[%d].reply_to_id is required", i)
		}
		if strings.TrimSpace(c.Text) == "" {
			return "", fmt.Errorf("reviewboard: comments[%d].text is required", i)
		}
		kind := strings.ToLower(strings.TrimSpace(c.Kind))
		if kind == "" {
			kind = "general"
		}
		if kind != "general" && kind != "diff" {
			return "", fmt.Errorf("reviewboard: comments[%d].kind must be general or diff", i)
		}
		if err := d.C.AddReplyComment(ctx, in.ID, in.ReviewID, replyID, kind, c.ReplyToID, c.Text, d.TextType); err != nil {
			return "", err
		}
	}
	if err := d.C.PublishReply(ctx, in.ID, in.ReviewID, replyID); err != nil {
		return "", fmt.Errorf("reviewboard: reply %d saved as a draft but publishing failed: %w", replyID, err)
	}
	return fmt.Sprintf("#%d reply %d to review %d posted (%d comments). %s",
		in.ID, replyID, in.ReviewID, len(in.Comments), d.reviewURL(in.ID, in.ReviewID)), nil
}

func (d *Deps) reviewURL(rrID, reviewID int) string {
	return fmt.Sprintf("%s/r/%d/#review%d", d.C.Base(), rrID, reviewID)
}
