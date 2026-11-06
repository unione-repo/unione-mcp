package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unione-repo/unione-mcp/internal/auth"
	"github.com/unione-repo/unione-mcp/internal/config"
	"github.com/unione-repo/unione-mcp/internal/httpapi"
	"github.com/unione-repo/unione-mcp/internal/mcpserver"
	"github.com/unione-repo/unione-mcp/internal/upstream"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(".env: %w", err)
	}
	// `server stdio` is the local mode's one-command start; MCP_TRANSPORT=stdio does the same.
	if len(os.Args) > 1 && os.Args[1] == string(config.TransportStdio) {
		_ = os.Setenv("MCP_TRANSPORT", string(config.TransportStdio))
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Version == "" {
		cfg.Version = version
	}
	log := newLogger(cfg)
	up := upstream.New(cfg.Brand, cfg.APIURL, cfg.Version, cfg.UpstreamTimeout, cfg.UpstreamMaxBody, log)
	server := mcpserver.Build(cfg, up, log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Transport == config.TransportStdio {
		log.Info("mcp server up", "brand", cfg.Brand.Key, "transport", cfg.Transport, "version", cfg.Version, "api_url", cfg.APIURL,
			"read_only", cfg.ReadOnly, "disable_send", cfg.DisableSend)
		// stdout is the protocol channel; the logger writes to stderr only.
		return server.Run(ctx, &mcp.StdioTransport{})
	}
	return serveHTTP(ctx, cfg, server, up, log)
}

func serveHTTP(ctx context.Context, cfg *config.Config, server *mcp.Server, up *upstream.Client, log *slog.Logger) error {
	listener, boundPort, err := listen(cfg)
	if err != nil {
		return err
	}
	selfURL := cfg.PublicURL
	if selfURL == "" {
		selfURL = fmt.Sprintf("http://localhost:%d", boundPort)
	}

	var tokens *auth.Verifier
	if cfg.AuthMode == config.AuthOAuth {
		// The metadata names both the endpoint and the origin as the resource: a token is issued for one of them
		tokens = auth.NewVerifier(up.TokenCheck(selfURL+"/mcp", selfURL))
	}
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true})

	httpServer := &http.Server{
		Handler: httpapi.New(httpapi.Options{
			Brand:               cfg.Brand,
			SelfURL:             selfURL,
			AuthMode:            cfg.AuthMode,
			AuthorizationServer: cfg.AuthorizationServer,
			AccessTokenPrefix:   cfg.AccessTokenPrefix,
			Tokens:              tokens,
			Keys:                auth.NewVerifier(up.KeyCheck()),
			Scopes:              cfg.ScopesSupported,
			MCP:                 mcpHandler,
			AppsChallenge:       cfg.AppsChallenge,
			MaxRequestBody:      cfg.MaxRequestBody,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	// In-flight tool calls are bounded by the upstream timeout; give them that long to drain.
	shutdownGrace := cfg.UpstreamTimeout + 5*time.Second

	if cfg.ExplicitPort == 0 {
		if err := publishPort(cfg.PortFile, boundPort); err != nil {
			log.Warn("port file write failed", "path", cfg.PortFile, "err", err)
		} else {
			defer func() { _ = os.Remove(cfg.PortFile) }()
		}
	}

	log.Info("mcp server up",
		"brand", cfg.Brand.Key,
		"transport", cfg.Transport,
		"version", cfg.Version,
		"port", boundPort,
		"resource", selfURL,
		"auth_mode", cfg.AuthMode,
		"authorization_server", cfg.AuthorizationServer,
		"api_url", cfg.APIURL,
		"read_only", cfg.ReadOnly,
		"disable_send", cfg.DisableSend,
	)

	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.Serve(listener) }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down", "grace", shutdownGrace)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

func newLogger(cfg *config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	var handler slog.Handler = slog.NewTextHandler(os.Stderr, opts)
	if cfg.LogFormat == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}
	log := slog.New(handler)
	slog.SetDefault(log)
	return log
}

// An explicit PORT binds exactly once; otherwise the first free port from PortBase
// upward is taken, so several instances can share a developer machine.
func listen(cfg *config.Config) (net.Listener, int, error) {
	basePort, attempts := cfg.PortBase, cfg.PortAttempts
	if cfg.ExplicitPort != 0 {
		basePort, attempts = cfg.ExplicitPort, 1
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		port := basePort + i
		listener, err := net.Listen("tcp", net.JoinHostPort(cfg.ListenHost, strconv.Itoa(port)))
		if err == nil {
			return listener, port, nil
		}
		lastErr = err
		if !errors.Is(err, syscall.EADDRINUSE) {
			break
		}
	}
	return nil, 0, fmt.Errorf("listen: %w", lastErr)
}

func publishPort(path string, port int) error {
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", port)), 0o644)
}
