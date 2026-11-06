package mcpserver

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unione-repo/unione-mcp/internal/auth"
	"github.com/unione-repo/unione-mcp/internal/config"
	"github.com/unione-repo/unione-mcp/internal/upstream"
)

type deps struct {
	cfg *config.Config
	up  *upstream.Client
	log *slog.Logger
}

// caller is one tool call's identity: the key the remote client presented, or the
// local server's API key, relayed to the API verbatim — the API validates it,
// this server never does.
type caller struct {
	cfg        *config.Config
	up         *upstream.Client
	log        *slog.Logger
	credential auth.Credential
}

func (d *deps) callerFor(req *mcp.CallToolRequest) *caller {
	headers := http.Header{}
	if req != nil && req.Extra != nil && req.Extra.Header != nil {
		headers = req.Extra.Header
	}
	credential := auth.From(headers, d.cfg.AuthMode, d.cfg.AccessTokenPrefix)
	if d.cfg.Transport == config.TransportStdio {
		credential = auth.Credential{Kind: auth.APIKey, Value: d.cfg.APIKey}
	}
	return &caller{
		cfg:        d.cfg,
		up:         d.up,
		log:        d.log,
		credential: credential,
	}
}

// call runs one API method: POST <base>/<method>.json with a JSON body.
func (c *caller) call(ctx context.Context, method string, body map[string]any) upstream.Result {
	if !c.credential.Present() {
		return upstream.Result{Status: http.StatusUnauthorized, Err: c.up.MsgUnauthorized()}
	}
	result := c.up.Do(ctx, c.credential, method, body)
	if result.Status == http.StatusUnauthorized && c.credential.Kind == auth.Bearer {
		auth.MarkTokenRejected(ctx)
	}
	return result
}

func (c *caller) post(ctx context.Context, method string, body map[string]any) *mcp.CallToolResult {
	return toResult(c.call(ctx, method, body))
}
