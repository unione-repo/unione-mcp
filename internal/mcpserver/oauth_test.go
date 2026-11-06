package mcpserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unione-repo/unione-mcp/internal/auth"
	"github.com/unione-repo/unione-mcp/internal/brand"
	"github.com/unione-repo/unione-mcp/internal/config"
	"github.com/unione-repo/unione-mcp/internal/httpapi"
)

const initializeRequest = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`

var toolCallRequest = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + brand.Current().Tool("account.get") + `","arguments":{}}}`

// The whole remote stack: router, SDK handler, tool, upstream. The API refusing the
// access token must reach the client as an HTTP 401 challenge — on connect as well
// as on a call — not as a tool error.
func TestOAuth_TokenRefusedByTheAPIIsAnswered401(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		bearer     string
		request    string
		wantStatus int
	}{
		"access token on connect: the client signs in again": {testTokenPrefix + "revoked", initializeRequest, http.StatusUnauthorized},
		"access token on a call: the client refreshes it":    {testTokenPrefix + "revoked", toolCallRequest, http.StatusUnauthorized},
		"API key on connect: nothing to check":               {"key-refused", initializeRequest, http.StatusOK},
		"API key on a call: the tool reports it":             {"key-refused", toolCallRequest, http.StatusOK},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeAPI(t).answer("system/info", http.StatusUnauthorized, `{"status":"error","code":102,"message":"Access token is revoked"}`)
			api := httptest.NewServer(fake)
			t.Cleanup(api.Close)
			cfg := oauthConfig()
			up := newUpstream(cfg.Brand, api.URL)
			server := Build(cfg, up, slog.New(slog.DiscardHandler))
			front := httptest.NewServer(httpapi.New(httpapi.Options{
				Brand:               cfg.Brand,
				SelfURL:             "https://mcp.example.test",
				AuthMode:            config.AuthOAuth,
				AuthorizationServer: "https://auth.example.test",
				AccessTokenPrefix:   cfg.AccessTokenPrefix,
				Tokens:              auth.NewVerifier(up.TokenCheck("https://mcp.example.test/mcp", "https://mcp.example.test")),
				MCP: mcp.NewStreamableHTTPHandler(
					func(*http.Request) *mcp.Server { return server },
					&mcp.StreamableHTTPOptions{Stateless: true},
				),
			}))
			t.Cleanup(front.Close)

			req, _ := http.NewRequest(http.MethodPost, front.URL+"/mcp", strings.NewReader(tc.request))
			req.Header.Set("Authorization", "Bearer "+tc.bearer)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("Mcp-Protocol-Version", "2025-06-18")
			resp, err := front.Client().Do(req)
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)

			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status %d, want %d; body %s", resp.StatusCode, tc.wantStatus, body)
			}
			if tc.wantStatus == http.StatusUnauthorized &&
				!strings.Contains(resp.Header.Get("WWW-Authenticate"), `error="invalid_token"`) {
				t.Fatalf("WWW-Authenticate %q, want the invalid_token challenge", resp.Header.Get("WWW-Authenticate"))
			}
			if tc.request == toolCallRequest && tc.wantStatus == http.StatusOK && !strings.Contains(string(body), "rejected the API key") {
				t.Fatalf("body %s does not report the refused key", body)
			}
		})
	}
}

// A token issued for another resource (the API host, say) is answered 401 like a
// revoked one; a token issued for this server goes on.
func TestOAuth_TokenOfAnotherAudienceIsAnswered401(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		audience   string
		wantStatus int
	}{
		"issued for this server": {"https://mcp.example.test/mcp", http.StatusOK},
		"issued for the API":     {"https://api.example.test", http.StatusUnauthorized},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeAPI(t).answer("system/info", http.StatusOK,
				`{"status":"success","user_id":1,"email":"a@example.test","oauth_audience":"`+tc.audience+`"}`)
			api := httptest.NewServer(fake)
			t.Cleanup(api.Close)
			cfg := oauthConfig()
			up := newUpstream(cfg.Brand, api.URL)
			server := Build(cfg, up, slog.New(slog.DiscardHandler))
			front := httptest.NewServer(httpapi.New(httpapi.Options{
				Brand:               cfg.Brand,
				SelfURL:             "https://mcp.example.test",
				AuthMode:            config.AuthOAuth,
				AuthorizationServer: "https://auth.example.test",
				AccessTokenPrefix:   cfg.AccessTokenPrefix,
				Tokens:              auth.NewVerifier(up.TokenCheck("https://mcp.example.test/mcp", "https://mcp.example.test")),
				MCP: mcp.NewStreamableHTTPHandler(
					func(*http.Request) *mcp.Server { return server },
					&mcp.StreamableHTTPOptions{Stateless: true},
				),
			}))
			t.Cleanup(front.Close)

			req, _ := http.NewRequest(http.MethodPost, front.URL+"/mcp", strings.NewReader(initializeRequest))
			req.Header.Set("Authorization", "Bearer "+testTokenPrefix+"token")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("Mcp-Protocol-Version", "2025-06-18")
			resp, err := front.Client().Do(req)
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusUnauthorized &&
				!strings.Contains(resp.Header.Get("WWW-Authenticate"), `error="invalid_token"`) {
				t.Fatalf("WWW-Authenticate %q, want the invalid_token challenge", resp.Header.Get("WWW-Authenticate"))
			}
		})
	}
}
