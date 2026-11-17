package reviewboard

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

type RequestQuery struct {
	Status        string
	Repository    string // repository name, path or id
	FromUser      string
	ToUser        string
	TimeAddedFrom string
	TimeAddedTo   string
	UpdatedFrom   string
	UpdatedTo     string
	Limit         int
	Offset        int
}

func (q RequestQuery) values() url.Values {
	vals := url.Values{}
	if q.Status != "" {
		vals.Set("status", q.Status)
	}
	if q.Repository != "" {
		vals.Set("repository", q.Repository)
	}
	if q.FromUser != "" {
		vals.Set("from-user", q.FromUser)
	}
	if q.ToUser != "" {
		vals.Set("to-users", q.ToUser)
	}
	if q.TimeAddedFrom != "" {
		vals.Set("time-added-from", q.TimeAddedFrom)
	}
	if q.TimeAddedTo != "" {
		vals.Set("time-added-to", q.TimeAddedTo)
	}
	if q.UpdatedFrom != "" {
		vals.Set("last-updated-from", q.UpdatedFrom)
	}
	if q.UpdatedTo != "" {
		vals.Set("last-updated-to", q.UpdatedTo)
	}
	if q.Limit > 0 {
		vals.Set("max-results", strconv.Itoa(q.Limit))
	}
	if q.Offset > 0 {
		vals.Set("start", strconv.Itoa(q.Offset))
	}
	return vals
}

func (c *Client) ReviewRequests(ctx context.Context, q RequestQuery) ([]ReviewRequest, int, error) {
	return getPage[ReviewRequest](ctx, c, "api/review-requests/", "review_requests", q.values())
}

func (c *Client) ReviewRequestsRaw(ctx context.Context, q RequestQuery) ([]byte, error) {
	return c.RawGet(ctx, "api/review-requests/", q.values())
}

func (c *Client) ReviewRequest(ctx context.Context, id int) (*ReviewRequest, error) {
	var r struct {
		ReviewRequest *ReviewRequest `json:"review_request"`
	}
	if err := c.get(ctx, fmt.Sprintf("api/review-requests/%d/", id), nil, &r); err != nil {
		return nil, err
	}
	if r.ReviewRequest == nil {
		return nil, fmt.Errorf("reviewboard: empty response for review request %d", id)
	}
	return r.ReviewRequest, nil
}

// CreateReviewRequest creates the blank, private, pending request that a draft
// then fills in. repository is a name, path or numeric id.
func (c *Client) CreateReviewRequest(ctx context.Context, repository string) (*ReviewRequest, error) {
	if repository == "" {
		return nil, fmt.Errorf("reviewboard: repository is required")
	}
	var r struct {
		ReviewRequest *ReviewRequest `json:"review_request"`
	}
	if _, err := c.post(ctx, "api/review-requests/", map[string]any{"repository": repository}, &r); err != nil {
		return nil, err
	}
	if r.ReviewRequest == nil {
		return nil, fmt.Errorf("reviewboard: empty response creating a review request")
	}
	return r.ReviewRequest, nil
}

// UpdateDraft applies fields to the draft and optionally publishes it.
func (c *Client) UpdateDraft(ctx context.Context, id int, fields map[string]any, publish bool) error {
	if len(fields) == 0 && !publish {
		return fmt.Errorf("reviewboard: empty update")
	}
	path := fmt.Sprintf("api/review-requests/%d/draft/", id)
	if len(fields) > 0 {
		if _, err := c.put(ctx, path, fields, nil); err != nil {
			return err
		}
	}
	if publish {
		if _, err := c.put(ctx, path, map[string]any{"public": true}, nil); err != nil {
			return err
		}
	}
	return nil
}

// SetStatus closes (submitted/discarded) or reopens (pending) a review request.
func (c *Client) SetStatus(ctx context.Context, id int, status, description, textType string) error {
	body := map[string]any{"status": status}
	if description != "" {
		body["close_description"] = description
		body["close_description_text_type"] = textType
	}
	_, err := c.put(ctx, fmt.Sprintf("api/review-requests/%d/", id), body, nil)
	return err
}

// RawGet returns the untouched JSON for a GET, used by the raw output modes.
func (c *Client) RawGet(ctx context.Context, path string, q url.Values) ([]byte, error) {
	data, _, err := c.fetch(ctx, "GET", c.url(path, q), nil, "")
	return data, err
}

// URL exposes the user-facing review request page for a path-built id.
func (c *Client) ReviewRequestURL(id int) string {
	return fmt.Sprintf("%s/r/%d/", c.base, id)
}
