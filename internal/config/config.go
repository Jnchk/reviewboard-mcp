package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the whole process configuration. One process serves one Review Board.
type Config struct {
	URL          string
	Token        string
	Insecure     bool
	ReadOnly     bool
	Timeout      time.Duration
	DefaultLimit int
	TextType     string // markdown | plain
	MaxUpload    int64
	MetaTTL      time.Duration

	Transport string // stdio | http
	Addr      string
	Path      string
	MCPToken  string
}

func FromEnv() (*Config, error) {
	c := &Config{
		URL:       strings.TrimRight(os.Getenv("REVIEWBOARD_URL"), "/"),
		Token:     os.Getenv("REVIEWBOARD_API_TOKEN"),
		Transport: env("MCP_TRANSPORT", "stdio"),
		Addr:      env("MCP_ADDR", "127.0.0.1:8080"),
		Path:      env("MCP_PATH", "/mcp"),
		MCPToken:  os.Getenv("MCP_TOKEN"),
		TextType:  env("REVIEWBOARD_TEXT_TYPE", "markdown"),
	}
	var err error
	if c.Insecure, err = envBool("REVIEWBOARD_INSECURE"); err != nil {
		return nil, err
	}
	if c.ReadOnly, err = envBool("REVIEWBOARD_READ_ONLY"); err != nil {
		return nil, err
	}
	if c.Timeout, err = envDur("REVIEWBOARD_TIMEOUT", 45*time.Second); err != nil {
		return nil, err
	}
	if c.MetaTTL, err = envDur("REVIEWBOARD_META_TTL", 5*time.Minute); err != nil {
		return nil, err
	}
	if c.DefaultLimit, err = envInt("REVIEWBOARD_DEFAULT_LIMIT", 25); err != nil {
		return nil, err
	}
	n, err := envInt("REVIEWBOARD_MAX_UPLOAD", 25<<20)
	if err != nil {
		return nil, err
	}
	c.MaxUpload = int64(n)
	return c, c.Validate()
}

func (c *Config) Validate() error {
	if c.URL == "" {
		return fmt.Errorf("REVIEWBOARD_URL is required")
	}
	u, err := url.Parse(c.URL)
	if err != nil {
		return fmt.Errorf("REVIEWBOARD_URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("REVIEWBOARD_URL must start with http:// or https://")
	}
	if u.Host == "" {
		return fmt.Errorf("REVIEWBOARD_URL has no host")
	}
	if c.Token == "" {
		return fmt.Errorf("REVIEWBOARD_API_TOKEN is required")
	}
	if c.DefaultLimit < 1 || c.DefaultLimit > 200 {
		return fmt.Errorf("REVIEWBOARD_DEFAULT_LIMIT must be 1..200, got %d", c.DefaultLimit)
	}
	if c.TextType != "markdown" && c.TextType != "plain" {
		return fmt.Errorf("REVIEWBOARD_TEXT_TYPE must be markdown or plain, got %q", c.TextType)
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("REVIEWBOARD_TIMEOUT must be positive")
	}
	if c.MaxUpload <= 0 {
		return fmt.Errorf("REVIEWBOARD_MAX_UPLOAD must be positive")
	}
	switch c.Transport {
	case "stdio":
	case "http":
		if !strings.HasPrefix(c.Path, "/") {
			return fmt.Errorf("MCP_PATH must start with /")
		}
		addrs := c.ListenAddrs()
		if len(addrs) == 0 {
			return fmt.Errorf("MCP_ADDR contains no addresses")
		}
		nonLoopback := ""
		for _, a := range addrs {
			if _, _, err := net.SplitHostPort(a); err != nil {
				return fmt.Errorf("MCP_ADDR %q: %w", a, err)
			}
			if nonLoopback == "" && !loopback(a) {
				nonLoopback = a
			}
		}
		if c.MCPToken == "" && nonLoopback != "" {
			return fmt.Errorf("MCP_ADDR includes non-loopback %q: set MCP_TOKEN to require a bearer token", nonLoopback)
		}
	default:
		return fmt.Errorf("MCP_TRANSPORT must be stdio or http, got %q", c.Transport)
	}
	return nil
}

// ListenAddrs splits MCP_ADDR on commas; empty entries are ignored.
func (c *Config) ListenAddrs() []string {
	var out []string
	for _, a := range strings.Split(c.Addr, ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// IsPlainHTTP reports whether the Review Board URL is unencrypted, for a startup warning.
func (c *Config) IsPlainHTTP() bool { return strings.HasPrefix(c.URL, "http://") }

func loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return b, nil
}

func envInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func envDur(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}
