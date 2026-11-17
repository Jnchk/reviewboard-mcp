package reviewboard

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// Reviews lists public reviews on a review request.
func (c *Client) Reviews(ctx context.Context, id, limit, offset int) ([]Review, int, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("max-results", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("start", strconv.Itoa(offset))
	}
	return getPage[Review](ctx, c, fmt.Sprintf("api/review-requests/%d/reviews/", id), "reviews", q)
}

// DiffComments lists inline comments of one review via its links.diff_comments href.
func (c *Client) DiffComments(ctx context.Context, href string) ([]Comment, error) {
	return getAllAt[Comment](ctx, c, href, "diff_comments")
}

// GeneralComments lists general comments of one review.
func (c *Client) GeneralComments(ctx context.Context, href string) ([]Comment, error) {
	return getAllAt[Comment](ctx, c, href, "general_comments")
}

type DiffCommentInput struct {
	FilediffID  int
	FirstLine   int
	NumLines    int
	Text        string
	IssueOpened bool
}

type GeneralCommentInput struct {
	Text        string
	IssueOpened bool
}

type ReviewInput struct {
	BodyTop         string
	BodyBottom      string
	ShipIt          bool
	DiffComments    []DiffCommentInput
	GeneralComments []GeneralCommentInput
}

// CreateReview creates a review and publishes it. With comments it must first
// create the private draft, attach the comments, then publish.
func (c *Client) CreateReview(ctx context.Context, id int, in ReviewInput, textType string) (int, error) {
	body := map[string]any{"ship_it": in.ShipIt}
	if in.BodyTop != "" {
		body["body_top"] = in.BodyTop
		body["body_top_text_type"] = textType
	}
	if in.BodyBottom != "" {
		body["body_bottom"] = in.BodyBottom
		body["body_bottom_text_type"] = textType
	}
	hasComments := len(in.DiffComments) > 0 || len(in.GeneralComments) > 0
	if !hasComments {
		body["public"] = true
	}
	h, err := c.post(ctx, fmt.Sprintf("api/review-requests/%d/reviews/", id), body, nil)
	if err != nil {
		return 0, err
	}
	rid := idFromLocation(h, 0)
	if !hasComments {
		return rid, nil
	}
	if rid == 0 {
		return 0, fmt.Errorf("reviewboard: review created but no Location header; cannot attach comments")
	}
	for _, dc := range in.DiffComments {
		if err := c.AddDiffComment(ctx, id, rid, dc, textType); err != nil {
			return rid, err
		}
	}
	for _, gc := range in.GeneralComments {
		if err := c.AddGeneralComment(ctx, id, rid, gc, textType); err != nil {
			return rid, err
		}
	}
	if err := c.PublishReview(ctx, id, rid); err != nil {
		return rid, err
	}
	return rid, nil
}

func (c *Client) AddDiffComment(ctx context.Context, rrID, reviewID int, in DiffCommentInput, textType string) error {
	body := map[string]any{
		"filediff_id": in.FilediffID,
		"first_line":  in.FirstLine,
		"num_lines":   in.NumLines,
		"text":        in.Text,
		"text_type":   textType,
	}
	if in.IssueOpened {
		body["issue_opened"] = true
	}
	_, err := c.post(ctx, fmt.Sprintf("api/review-requests/%d/reviews/%d/diff-comments/", rrID, reviewID), body, nil)
	return err
}

func (c *Client) AddGeneralComment(ctx context.Context, rrID, reviewID int, in GeneralCommentInput, textType string) error {
	body := map[string]any{"text": in.Text, "text_type": textType}
	if in.IssueOpened {
		body["issue_opened"] = true
	}
	_, err := c.post(ctx, fmt.Sprintf("api/review-requests/%d/reviews/%d/general-comments/", rrID, reviewID), body, nil)
	return err
}

func (c *Client) PublishReview(ctx context.Context, rrID, reviewID int) error {
	_, err := c.put(ctx, fmt.Sprintf("api/review-requests/%d/reviews/%d/", rrID, reviewID),
		map[string]any{"public": true}, nil)
	return err
}

// CreateReply starts a draft reply and returns its id.
func (c *Client) CreateReply(ctx context.Context, rrID, reviewID int, bodyTop, bodyBottom, textType string) (int, error) {
	body := map[string]any{}
	if bodyTop != "" {
		body["body_top"] = bodyTop
		body["body_top_text_type"] = textType
	}
	if bodyBottom != "" {
		body["body_bottom"] = bodyBottom
		body["body_bottom_text_type"] = textType
	}
	h, err := c.post(ctx, fmt.Sprintf("api/review-requests/%d/reviews/%d/replies/", rrID, reviewID), body, nil)
	if err != nil {
		return 0, err
	}
	rid := idFromLocation(h, 0)
	if rid == 0 {
		return 0, fmt.Errorf("reviewboard: reply created but no Location header")
	}
	return rid, nil
}

// AddReplyComment replies to a general or diff comment. kind is "general" or "diff".
func (c *Client) AddReplyComment(ctx context.Context, rrID, reviewID, replyID int, kind string, replyToID int, text, textType string) error {
	segment := "general-comments"
	if kind == "diff" {
		segment = "diff-comments"
	}
	body := map[string]any{"reply_to_id": replyToID, "text": text, "text_type": textType}
	_, err := c.post(ctx, fmt.Sprintf("api/review-requests/%d/reviews/%d/replies/%d/%s/", rrID, reviewID, replyID, segment), body, nil)
	return err
}

func (c *Client) PublishReply(ctx context.Context, rrID, reviewID, replyID int) error {
	_, err := c.put(ctx, fmt.Sprintf("api/review-requests/%d/reviews/%d/replies/%d/", rrID, reviewID, replyID),
		map[string]any{"public": true}, nil)
	return err
}
