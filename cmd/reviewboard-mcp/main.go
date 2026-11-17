package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"reviewboard-mcp/internal/config"
	"reviewboard-mcp/internal/reviewboard"
	"reviewboard-mcp/internal/tools"
)

const version = "0.1.0"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.FromEnv()
	if err != nil {
		logger.Error("configuration", "err", err)
		os.Exit(1)
	}

	fs := flag.NewFlagSet("reviewboard-mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&cfg.Transport, "transport", cfg.Transport, "stdio or http")
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "HTTP listen address")
	fs.BoolVar(&cfg.ReadOnly, "read-only", cfg.ReadOnly, "disable write tools")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if err := cfg.Validate(); err != nil {
		logger.Error("configuration", "err", err)
		os.Exit(1)
	}
	if cfg.IsPlainHTTP() {
		logger.Warn("REVIEWBOARD_URL is not https: the API token travels in clear text")
	}
	if cfg.Insecure {
		logger.Warn("TLS verification is disabled (REVIEWBOARD_INSECURE)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client, err := reviewboard.New(cfg.URL, cfg.Token, cfg.Timeout, cfg.Insecure, cfg.MaxUpload)
	if err != nil {
		logger.Error("client", "err", err)
		os.Exit(1)
	}

	// Fail fast: proves URL, token and TLS before a client talks to us.
	checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	session, err := client.Session(checkCtx)
	cancel()
	if err != nil {
		logger.Error("cannot reach Review Board", "url", cfg.URL, "err", err)
		os.Exit(1)
	}
	if !session.Authenticated {
		logger.Error("Review Board rejected the token: the session is not authenticated")
		os.Exit(1)
	}
	logger.Info("connected", "url", cfg.URL, "user", session.UserName(), "read_only", cfg.ReadOnly)

	srv := mcp.NewServer(&mcp.Implementation{Name: "reviewboard-mcp", Version: version}, &mcp.ServerOptions{
		Instructions: instructions(cfg, session.UserName()),
	})
	tools.Register(srv, &tools.Deps{
		C:            client,
		M:            reviewboard.NewMeta(client, cfg.MetaTTL),
		ReadOnly:     cfg.ReadOnly,
		DefaultLimit: cfg.DefaultLimit,
		TextType:     cfg.TextType,
	})

	switch cfg.Transport {
	case "stdio":
		err = srv.Run(ctx, &mcp.StdioTransport{})
	case "http":
		handler := mcp.NewStreamableHTTPHandler(
			func(*http.Request) *mcp.Server { return srv }, // one server for every session
			&mcp.StreamableHTTPOptions{Stateless: true},    // SSE responses; no per-connection state
		)
		mux := http.NewServeMux()
		mux.Handle(cfg.Path, withToken(cfg.MCPToken, handler))

		// Bind every address up front so a bad one fails startup, not half the listeners.
		var listeners []net.Listener
		for _, addr := range cfg.ListenAddrs() {
			ln, listenErr := net.Listen("tcp", addr)
			if listenErr != nil {
				for _, open := range listeners {
					_ = open.Close()
				}
				logger.Error("listen", "addr", addr, "err", listenErr)
				os.Exit(1)
			}
			listeners = append(listeners, ln)
		}

		serveCtx, stopServing := context.WithCancel(ctx)
		defer stopServing()

		servers := make([]*http.Server, len(listeners))
		errCh := make(chan error, len(listeners))
		for i, ln := range listeners {
			hs := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
			servers[i] = hs
			logger.Info("listening", "addr", ln.Addr().String(), "path", cfg.Path)
			go func() { errCh <- hs.Serve(ln) }()
		}
		go func() {
			<-serveCtx.Done()
			shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
			defer done()
			for _, hs := range servers {
				_ = hs.Shutdown(shutdown)
			}
		}()

		err = <-errCh
		stopServing()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}
	if err != nil {
		logger.Error("server", "err", err)
		os.Exit(1)
	}
}

func instructions(cfg *config.Config, user string) string {
	mode := "read-write"
	if cfg.ReadOnly {
		mode = "read-only (write tools return an error)"
	}
	return fmt.Sprintf(`Review Board at %s. Signed in as %s. Mode: %s.
Text: use %s for descriptions, testing done, review bodies and comments.
- Review request ids are integers; chain calls with the ids printed in results.
- Draft/publish: fields, diffs, reviews and replies only become visible when published. The tools publish by default; do not report a change as visible before the call succeeds.
- Diff upload is multipart and needs the basedir for that repository; never guess it. A 207 error means the basedir or repository path is wrong.
- Inline comments need a filediff id from get_diff and 1-based line numbers in the patched file.
- Status values are pending, submitted and discarded. Closing needs confirm: true.
- Prefer a small limit and summarize; paginate with offset only when the user needs more.
- After a create, update or close, show the review request URL to the user.`, cfg.URL, user, mode, cfg.TextType)
}

func withToken(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		if len(got) > 7 && got[:7] == "Bearer " {
			got = got[7:]
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
