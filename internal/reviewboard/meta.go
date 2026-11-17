package reviewboard

import (
	"context"
	"fmt"
	"net/url"
)

func (c *Client) Repositories(ctx context.Context) ([]Repository, error) {
	q := url.Values{"max-results": {"200"}}
	return getAll[Repository](ctx, c, "api/repositories/", "repositories", q)
}

func (c *Client) Users(ctx context.Context, query string) ([]User, error) {
	q := url.Values{"max-results": {"200"}}
	if query != "" {
		q.Set("q", query)
		q.Set("fullname", "1")
	}
	return getAll[User](ctx, c, "api/users/", "users", q)
}

func (c *Client) Groups(ctx context.Context, query string) ([]Group, error) {
	q := url.Values{"max-results": {"200"}}
	if query != "" {
		q.Set("q", query)
		q.Set("displayname", "1")
	}
	return getAll[Group](ctx, c, "api/groups/", "groups", q)
}

func (c *Client) UserByID(ctx context.Context, id int) (*User, error) {
	var r struct {
		User *User `json:"user"`
	}
	if err := c.get(ctx, fmt.Sprintf("api/users/%d/", id), nil, &r); err != nil {
		return nil, err
	}
	if r.User == nil {
		return nil, fmt.Errorf("reviewboard: empty response for user %d", id)
	}
	return r.User, nil
}

func (c *Client) GroupByID(ctx context.Context, id int) (*Group, error) {
	var r struct {
		Group *Group `json:"group"`
	}
	if err := c.get(ctx, fmt.Sprintf("api/groups/%d/", id), nil, &r); err != nil {
		return nil, err
	}
	if r.Group == nil {
		return nil, fmt.Errorf("reviewboard: empty response for group %d", id)
	}
	return r.Group, nil
}
