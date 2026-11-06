package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"runtime/debug"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unione-repo/unione-mcp/internal/config"
	"github.com/unione-repo/unione-mcp/internal/upstream"
)

// One server for the whole process: tool handlers read the caller's credential from
// the request headers the transport attaches, so nothing per-caller is captured
// at registration time.
func Build(cfg *config.Config, up *upstream.Client, log *slog.Logger) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: cfg.Brand.ServerName(), Version: cfg.Version},
		&mcp.ServerOptions{Instructions: cfg.Brand.Render(instructions), Logger: slog.New(warnAndAbove{log.Handler()})},
	)
	server.AddReceivingMiddleware(nullArgumentsAsEmpty, recoverToolPanics(log))
	r := &registry{server: server, deps: &deps{cfg: cfg, up: up, log: log}}
	for _, register := range domains {
		register(r)
	}
	return server
}

// go-sdk v1.6.1 unmarshals `"arguments": null` into a nil map and then writes the
// schema defaults into it — a panic that takes every session down. A null argument
// object means "no arguments", so it is read as one.
func nullArgumentsAsEmpty(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if call, ok := req.(*mcp.CallToolRequest); ok && call.Params != nil && isJSONNull(call.Params.Arguments) {
			call.Params.Arguments = json.RawMessage("{}")
		}
		return next(ctx, method, req)
	}
}

func isJSONNull(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

// A panic inside one tool call must cost that call, not the process.
func recoverToolPanics(log *slog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (result mcp.Result, err error) {
			defer func() {
				if r := recover(); r != nil {
					log.Error("tool call panicked", "method", method, "panic", r, "stack", string(debug.Stack()))
					result, err = errText("The server hit an internal error on this call. Try again; if it repeats, report it."), nil
				}
			}()
			return next(ctx, method, req)
		}
	}
}

// The SDK narrates every stateless request at Info; only its warnings are worth a log line.
type warnAndAbove struct{ slog.Handler }

func (h warnAndAbove) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn && h.Handler.Enabled(ctx, level)
}

var domains []func(*registry)

func registerDomain(f func(*registry)) { domains = append(domains, f) }
