package reviewboard

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Meta resolves human names to Review Board ids/usernames and caches the lookups.
type Meta struct {
	c   *Client
	ttl time.Duration

	mu      sync.Mutex
	repos   []Repository
	reposAt time.Time
	users   map[string]userCache
	groups  map[string]groupCache
}

type userCache struct {
	items []User
	at    time.Time
}

type groupCache struct {
	items []Group
	at    time.Time
}

func NewMeta(c *Client, ttl time.Duration) *Meta {
	return &Meta{c: c, ttl: ttl, users: map[string]userCache{}, groups: map[string]groupCache{}}
}

// Repository resolves a repository name, path or numeric id.
func (m *Meta) Repository(ctx context.Context, value string) (Repository, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Repository{}, fmt.Errorf("reviewboard: repository is required")
	}
	if n, err := strconv.Atoi(value); err == nil {
		return Repository{ID: n}, nil
	}
	repos, err := m.allRepos(ctx)
	if err != nil {
		return Repository{}, err
	}
	var exact, prefix, sub []Repository
	for _, r := range repos {
		switch {
		case strings.EqualFold(r.Name, value), strings.EqualFold(r.Path, value):
			exact = append(exact, r)
		case strings.HasPrefix(strings.ToLower(r.Name), strings.ToLower(value)):
			prefix = append(prefix, r)
		case strings.Contains(strings.ToLower(r.Name), strings.ToLower(value)),
			strings.Contains(strings.ToLower(r.Path), strings.ToLower(value)):
			sub = append(sub, r)
		}
	}
	for _, set := range [][]Repository{exact, prefix, sub} {
		if len(set) == 1 {
			return set[0], nil
		}
		if len(set) > 1 {
			return Repository{}, ambiguous("repository", value, repoLabels(set))
		}
	}
	return Repository{}, unknown("repository", value, repoLabels(repos))
}

func (m *Meta) allRepos(ctx context.Context) ([]Repository, error) {
	m.mu.Lock()
	if m.repos != nil && time.Since(m.reposAt) < m.ttl {
		repos := append([]Repository(nil), m.repos...)
		m.mu.Unlock()
		return repos, nil
	}
	m.mu.Unlock()

	repos, err := m.c.Repositories(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.repos, m.reposAt = repos, time.Now()
	m.mu.Unlock()
	return repos, nil
}

// User resolves a username, full name, numeric id or "me".
func (m *Meta) User(ctx context.Context, value string) (User, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return User{}, fmt.Errorf("reviewboard: user is required")
	}
	if strings.EqualFold(value, "me") {
		name, err := m.c.CurrentUser(ctx)
		if err != nil {
			return User{}, err
		}
		value = name
	}
	if n, err := strconv.Atoi(value); err == nil {
		u, err := m.c.UserByID(ctx, n)
		if err != nil {
			return User{}, err
		}
		return *u, nil
	}
	users, err := m.usersFor(ctx, userQuery(value))
	if err != nil {
		return User{}, err
	}
	var exact []User
	for _, u := range users {
		if strings.EqualFold(u.Username, value) || strings.EqualFold(u.FullName(), value) {
			exact = append(exact, u)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return User{}, ambiguous("user", value, userLabels(exact))
	}
	if len(users) == 1 {
		return users[0], nil
	}
	if len(users) > 1 {
		return User{}, ambiguous("user", value, userLabels(users))
	}
	return User{}, fmt.Errorf("reviewboard: unknown user %q: the username starts with this value on no account; use list_metadata kind=users", value)
}

// Group resolves a review group name, display name or numeric id.
func (m *Meta) Group(ctx context.Context, value string) (Group, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Group{}, fmt.Errorf("reviewboard: group is required")
	}
	if n, err := strconv.Atoi(value); err == nil {
		g, err := m.c.GroupByID(ctx, n)
		if err != nil {
			return Group{}, err
		}
		return *g, nil
	}
	groups, err := m.groupsFor(ctx, value)
	if err != nil {
		return Group{}, err
	}
	var exact []Group
	for _, g := range groups {
		if strings.EqualFold(g.Name, value) || strings.EqualFold(g.DisplayName, value) {
			exact = append(exact, g)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return Group{}, ambiguous("group", value, groupLabels(exact))
	}
	if len(groups) == 1 {
		return groups[0], nil
	}
	if len(groups) > 1 {
		return Group{}, ambiguous("group", value, groupLabels(groups))
	}
	return Group{}, fmt.Errorf("reviewboard: unknown group %q: use list_metadata kind=groups", value)
}

func (m *Meta) usersFor(ctx context.Context, query string) ([]User, error) {
	key := strings.ToLower(query)
	m.mu.Lock()
	if c, ok := m.users[key]; ok && time.Since(c.at) < m.ttl {
		items := append([]User(nil), c.items...)
		m.mu.Unlock()
		return items, nil
	}
	m.mu.Unlock()

	items, err := m.c.Users(ctx, query)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.users[key] = userCache{items: items, at: time.Now()}
	m.mu.Unlock()
	return items, nil
}

func (m *Meta) groupsFor(ctx context.Context, query string) ([]Group, error) {
	key := strings.ToLower(query)
	m.mu.Lock()
	if c, ok := m.groups[key]; ok && time.Since(c.at) < m.ttl {
		items := append([]Group(nil), c.items...)
		m.mu.Unlock()
		return items, nil
	}
	m.mu.Unlock()

	items, err := m.c.Groups(ctx, query)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.groups[key] = groupCache{items: items, at: time.Now()}
	m.mu.Unlock()
	return items, nil
}

func repoLabels(repos []Repository) []string {
	out := make([]string, 0, len(repos))
	for i, r := range repos {
		if i == 10 {
			out = append(out, fmt.Sprintf("… %d more", len(repos)-i))
			break
		}
		out = append(out, fmt.Sprintf("%s (%d)", r.Name, r.ID))
	}
	return out
}

func userLabels(users []User) []string {
	out := make([]string, 0, len(users))
	for i, u := range users {
		if i == 10 {
			out = append(out, fmt.Sprintf("… %d more", len(users)-i))
			break
		}
		out = append(out, fmt.Sprintf("%s (%d)", u.Username, u.ID))
	}
	return out
}

func groupLabels(groups []Group) []string {
	out := make([]string, 0, len(groups))
	for i, g := range groups {
		if i == 10 {
			out = append(out, fmt.Sprintf("… %d more", len(groups)-i))
			break
		}
		out = append(out, fmt.Sprintf("%s (%d)", g.Label(), g.ID))
	}
	return out
}

func ambiguous(kind, value string, labels []string) error {
	return fmt.Errorf("reviewboard: ambiguous %s %q: %d matches — %s. Pass an id or a longer name.",
		kind, value, len(labels), strings.Join(labels, ", "))
}

func unknown(kind, value string, labels []string) error {
	return fmt.Errorf("reviewboard: unknown %s %q. Available: %s. Use list_metadata for the full list.",
		kind, value, strings.Join(labels, ", "))
}

// userQuery gives Review Board's prefix search a usable term for a full name.
func userQuery(value string) string {
	if fields := strings.Fields(value); len(fields) > 1 {
		return fields[0]
	}
	return value
}
