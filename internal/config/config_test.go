package config

import (
	"testing"
)

func base() *Config {
	return &Config{
		URL:          "https://rb.example.com",
		Token:        "t",
		Transport:    "stdio",
		DefaultLimit: 25,
		TextType:     "markdown",
		Timeout:      1,
		MaxUpload:    1,
		Path:         "/mcp",
		Addr:         "127.0.0.1:8080",
	}
}

func TestValidate(t *testing.T) {
	if err := base().Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	c := base()
	c.Token = ""
	if err := c.Validate(); err == nil {
		t.Fatal("expected missing token error")
	}
	c = base()
	c.DefaultLimit = 500
	if err := c.Validate(); err == nil {
		t.Fatal("expected limit error")
	}
	c = base()
	c.TextType = "html"
	if err := c.Validate(); err == nil {
		t.Fatal("expected text type error")
	}
}

func TestNonLoopbackRequiresMCPToken(t *testing.T) {
	c := base()
	c.Transport = "http"
	c.Addr = "0.0.0.0:8080"
	if err := c.Validate(); err == nil {
		t.Fatal("expected MCP_TOKEN requirement")
	}
	c.MCPToken = "secret"
	if err := c.Validate(); err != nil {
		t.Fatalf("token set: %v", err)
	}
}

func TestListenAddrs(t *testing.T) {
	c := base()
	c.Addr = "127.0.0.1:8080, 10.0.0.5:8080,"
	got := c.ListenAddrs()
	if len(got) != 2 || got[0] != "127.0.0.1:8080" || got[1] != "10.0.0.5:8080" {
		t.Fatalf("addrs = %#v", got)
	}
}
