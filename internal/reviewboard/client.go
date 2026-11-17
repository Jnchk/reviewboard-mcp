package reviewboard

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client is a thin Review Board REST client. It knows nothing about MCP.
type Client struct {
	base      string // no trailing slash, may include a sub-path
	token     string
	hc        *http.Client
	maxUpload int64

	mu       sync.Mutex
	username string
}

func New(baseURL, token string, timeout time.Duration, insecure bool, maxUpload int64) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("reviewboard: invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("reviewboard: URL must start with http:// or https://")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Client{
		base:      strings.TrimRight(u.String(), "/"),
		token:     token,
		hc:        &http.Client{Timeout: timeout, Transport: tr},
		maxUpload: maxUpload,
	}, nil
}

// Base is the configured Review Board URL, used to build user-facing links.
func (c *Client) Base() string { return c.base }

// Session proves the token and returns the current user's session.
func (c *Client) Session(ctx context.Context) (*Session, error) {
	var r struct {
		Session *Session `json:"session"`
	}
	if err := c.get(ctx, "api/session/", nil, &r); err != nil {
		return nil, err
	}
	if r.Session == nil {
		return nil, fmt.Errorf("reviewboard: empty response from api/session/")
	}
	c.mu.Lock()
	c.username = r.Session.UserName()
	c.mu.Unlock()
	return r.Session, nil
}

// CurrentUser returns the username from /api/session/, cached for the process lifetime.
func (c *Client) CurrentUser(ctx context.Context) (string, error) {
	c.mu.Lock()
	u := c.username
	c.mu.Unlock()
	if u != "" {
		return u, nil
	}
	s, err := c.Session(ctx)
	if err != nil {
		return "", err
	}
	if u := s.UserName(); u != "" {
		return u, nil
	}
	return "", fmt.Errorf("reviewboard: the session has no authenticated user")
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	_, err := c.do(ctx, http.MethodGet, c.url(path, q), nil, "", out)
	return err
}

func (c *Client) post(ctx context.Context, path string, body, out any) (http.Header, error) {
	return c.send(ctx, http.MethodPost, c.url(path, nil), body, out)
}

func (c *Client) put(ctx context.Context, path string, body, out any) (http.Header, error) {
	return c.send(ctx, http.MethodPut, c.url(path, nil), body, out)
}

func (c *Client) send(ctx context.Context, method, target string, body, out any) (http.Header, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, fmt.Errorf("reviewboard: encoding request: %w", err)
		}
	}
	return c.do(ctx, method, target, payload, "application/json", out)
}

// url joins a relative API path with the base URL. The path must end in "/"
// because Review Board redirects a slashless POST and drops the body.
func (c *Client) url(path string, q url.Values) string {
	target := c.base + "/" + strings.TrimPrefix(path, "/")
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	return target
}

// do sends one request and decodes a JSON 2xx body into out.
func (c *Client) do(ctx context.Context, method, target string, payload []byte, contentType string, out any) (http.Header, error) {
	data, h, err := c.fetch(ctx, method, target, payload, contentType)
	if err != nil {
		return h, err
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return h, fmt.Errorf("reviewboard: decoding %s: %w", target, err)
		}
	}
	return h, nil
}

// fetch sends one request. GET is retried once on transport errors and 502/503/504;
// every other method is never retried, because a duplicated review or diff is
// worse than a clear failure.
func (c *Client) fetch(ctx context.Context, method, target string, payload []byte, contentType string) ([]byte, http.Header, error) {
	attempts := 1
	if method == http.MethodGet {
		attempts = 2
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(payload))
		if err != nil {
			return nil, nil, fmt.Errorf("reviewboard: building request: %w", err)
		}
		req.Header.Set("Authorization", "token "+c.token)
		req.Header.Set("Accept", "application/json")
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}

		resp, err := c.hc.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("reviewboard: %s: %w", target, err)
			if ctx.Err() != nil {
				return nil, nil, lastErr
			}
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("reviewboard: reading response: %w", readErr)
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return data, resp.Header, nil
		}
		lastErr = parseError(resp.StatusCode, data)
		if i+1 < attempts && retryable(resp.StatusCode) {
			continue
		}
		return nil, resp.Header, lastErr
	}
	return nil, nil, lastErr
}

// doRaw fetches a non-JSON representation, such as a unified diff.
func (c *Client) doRaw(ctx context.Context, target, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("reviewboard: building request: %w", err)
	}
	req.Header.Set("Authorization", "token "+c.token)
	req.Header.Set("Accept", accept)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reviewboard: %s: %w", target, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("reviewboard: reading response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseError(resp.StatusCode, data)
	}
	return data, nil
}

func retryable(status int) bool {
	return status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

// listEnvelope is the common shape of every Review Board collection.
type listEnvelope struct {
	Total int             `json:"total_results"`
	Links map[string]Link `json:"links"`
}

func decodeList[T any](data []byte, key string) (items []T, total int, next string, err error) {
	var env listEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, 0, "", fmt.Errorf("reviewboard: decoding list: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, 0, "", fmt.Errorf("reviewboard: decoding list: %w", err)
	}
	if v, ok := raw[key]; ok {
		if err := json.Unmarshal(v, &items); err != nil {
			return nil, 0, "", fmt.Errorf("reviewboard: decoding %s: %w", key, err)
		}
	}
	if l, ok := env.Links["next"]; ok {
		next = l.Href
	}
	return items, env.Total, next, nil
}

func getPage[T any](ctx context.Context, c *Client, path, key string, q url.Values) ([]T, int, error) {
	data, _, err := c.fetch(ctx, http.MethodGet, c.url(path, q), nil, "")
	if err != nil {
		return nil, 0, err
	}
	items, total, _, err := decodeList[T](data, key)
	return items, total, err
}

// getAll walks every page via links.next.href and stops after 50 pages.
func getAll[T any](ctx context.Context, c *Client, path, key string, q url.Values) ([]T, error) {
	return getAllAt[T](ctx, c, c.url(path, q), key)
}

func getAllAt[T any](ctx context.Context, c *Client, target, key string) ([]T, error) {
	var out []T
	for page := 0; page < 50; page++ {
		data, _, err := c.fetch(ctx, http.MethodGet, target, nil, "")
		if err != nil {
			return nil, err
		}
		items, _, next, err := decodeList[T](data, key)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if next == "" || len(items) == 0 {
			return out, nil
		}
		target = next
	}
	return out, nil
}

// idFromLocation returns the last numeric path segment of a Location header.
func idFromLocation(h http.Header, fallback int) int {
	loc := h.Get("Location")
	loc = strings.TrimRight(loc, "/")
	if loc == "" {
		return fallback
	}
	if i := strings.LastIndex(loc, "/"); i >= 0 {
		if n, err := strconv.Atoi(loc[i+1:]); err == nil {
			return n
		}
	}
	return fallback
}
