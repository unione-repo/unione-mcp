//go:build integration

package acceptance

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/unione-repo/unione-mcp/internal/brand"
	"github.com/unione-repo/unione-mcp/internal/config"
	actMcp "github.com/unione-repo/unione-mcp/tests/support/act/mcp"
	"github.com/unione-repo/unione-mcp/tests/support/arrange"
	arrangeAPI "github.com/unione-repo/unione-mcp/tests/support/arrange/api"
	arrangeServer "github.com/unione-repo/unione-mcp/tests/support/arrange/server"
	assertAPI "github.com/unione-repo/unione-mcp/tests/support/assert/api"
	assertHttpResponse "github.com/unione-repo/unione-mcp/tests/support/assert/httpResponse"
	assertTool "github.com/unione-repo/unione-mcp/tests/support/assert/tool"
)

func TestE2E_Relay_BearerReachesTheAPIAsXAPIKeyMarkedAsMCP(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})
	key := arrange.APIKey(t)
	arrangeAPI.NewStub(key, "system/info").WithAnswer(map[string]any{"user_id": 1, "email": "owner@example.test"}).Register(t)

	res := actMcp.Connect(t, server, key).CallTool(t, server.Tool("account.get"), nil)

	assertTool.JSONIncludes(t, res, map[string]any{"status": "success", "email": "owner@example.test"})
	// The key check on every request and the tool call itself go out alike
	for _, req := range assertAPI.CallsWithKey(t, key, "system/info") {
		assertAPI.HeaderAbsent(t, req, "Authorization")
		assertAPI.HeaderIs(t, req, "User-Agent", brand.Current().ServerName()+"/dev")
		assertAPI.QueryIs(t, req, "platform", "mcp")
	}
}

func TestE2E_Relay_OAuthModeRelaysTheBearer(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{AuthMode: config.AuthOAuth})
	token := arrangeServer.TokenPrefix + arrange.UniqueID(t, "tok")
	arrangeAPI.NewBearerStub(token, "system/info").Register(t)
	arrangeAPI.NewBearerStub(token, "domain/list").Register(t)

	res := actMcp.Connect(t, server, token).CallTool(t, server.Tool("domain.list"), nil)

	assertTool.Succeeded(t, res)
	assertAPI.HeaderAbsent(t, assertAPI.CalledWithBearer(t, token, "domain/list"), "X-API-KEY")
	// The token is checked on every request, not once on connect: a revoked one is refused on the next request.
	checks := assertAPI.CallsWithBearer(t, token, "system/info")
	if len(checks) < 2 {
		t.Fatalf("system/info was called %d time(s) for the bearer, want a check on connect and on the tool call", len(checks))
	}
	for _, check := range checks {
		assertAPI.HeaderAbsent(t, check, "X-API-KEY")
	}
}

// Servers and CI keep connecting with an API key where browsers go through OAuth.
func TestE2E_Relay_OAuthModeStillRelaysAnAPIKey(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{AuthMode: config.AuthOAuth})
	key := arrange.APIKey(t)
	arrangeAPI.NewStub(key, "system/info").Register(t)

	res := actMcp.Connect(t, server, key).CallTool(t, server.Tool("account.get"), nil)

	assertTool.Succeeded(t, res)
	for _, req := range assertAPI.CallsWithKey(t, key, "system/info") {
		assertAPI.HeaderAbsent(t, req, "Authorization")
	}
}

// A token the API refuses (expired, revoked in the cabinet) is answered with the
// invalid_token challenge, so the client refreshes it instead of failing the call.
func TestE2E_Relay_OAuthModeRefusedTokenIsAChallenge(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{AuthMode: config.AuthOAuth})
	token := arrangeServer.TokenPrefix + arrange.UniqueID(t, "tok")
	arrangeAPI.NewBearerStub(token, "system/info").WithError(http.StatusUnauthorized, 102, "Access token is revoked").Register(t)

	resp := actMcp.PostRaw(t, server.MCPEndpoint, map[string]string{"Authorization": "Bearer " + token},
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+server.Tool("account.get")+`","arguments":{}}}`)

	assertHttpResponse.Status(t, resp, http.StatusUnauthorized)
	assertHttpResponse.HeaderContains(t, resp, "WWW-Authenticate", `error="invalid_token"`)
	assertHttpResponse.HeaderContains(t, resp, "WWW-Authenticate",
		`resource_metadata="`+server.BaseURL+`/.well-known/oauth-protected-resource"`)
}

// MCP authorization wants the 401 already on initialize: a client with an expired
// token signs in again on connect instead of failing on the first action.
func TestE2E_Relay_OAuthModeRefusedTokenIsAChallengeOnConnect(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{AuthMode: config.AuthOAuth})
	token := arrangeServer.TokenPrefix + arrange.UniqueID(t, "tok")
	arrangeAPI.NewBearerStub(token, "system/info").WithError(http.StatusUnauthorized, 102, "Access token is revoked").Register(t)

	resp := actMcp.PostRaw(t, server.MCPEndpoint, map[string]string{"Authorization": "Bearer " + token}, actMcp.InitializeBody())

	assertHttpResponse.Status(t, resp, http.StatusUnauthorized)
	assertHttpResponse.HeaderContains(t, resp, "WWW-Authenticate", `error="invalid_token"`)
}

func TestE2E_Relay_ParallelCallersStayIsolated(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})

	for i := range 5 {
		t.Run(fmt.Sprintf("caller-%d", i), func(t *testing.T) {
			t.Parallel()
			key := arrange.APIKey(t)
			email := arrange.UniqueEmail(t)
			arrangeAPI.NewStub(key, "system/info").WithAnswer(map[string]any{"email": email}).Register(t)

			res := actMcp.Connect(t, server, key).CallTool(t, server.Tool("account.get"), nil)

			assertTool.JSONIncludes(t, res, map[string]any{"email": email})
			assertAPI.CallsWithKey(t, key, "system/info")
		})
	}
}

func TestE2E_Relay_RejectionsCarryTheAPIsCodeAndText(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		status  int
		code    int
		message string
		want    []string
	}{
		"tariff limit":   {http.StatusForbidden, 902, "Sending limit for free tariff is reached", []string{"code 902", "Sending limit for free tariff is reached"}},
		"rate limit":     {http.StatusTooManyRequests, 0, "slow down", []string{"rate-limiting"}},
		"server failure": {http.StatusServiceUnavailable, 150, "Unknown error", []string{"code 150", "Unknown error"}},
	}
	server := arrangeServer.Start(t, arrangeServer.Options{})

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			key := arrange.APIKey(t)
			arrangeAPI.NewStub(key, "system/info").WithError(tc.status, tc.code, tc.message).Register(t)

			res := actMcp.Connect(t, server, key).CallTool(t, server.Tool("account.get"), nil)

			for _, fragment := range tc.want {
				assertTool.FailedWith(t, res, fragment)
			}
		})
	}
}

// A key the API does not know is refused on initialize already, before any tool
// call: the client shows the error on connect instead of on the first action.
func TestE2E_Relay_RejectedKeyIsAChallengeOnConnect(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})
	key := arrange.APIKey(t)
	arrangeAPI.NewStub(key, "system/info").WithError(http.StatusUnauthorized, 102, "Invalid key '"+key+"'").Register(t)

	resp := actMcp.PostRaw(t, server.MCPEndpoint, map[string]string{"Authorization": "Bearer " + key}, actMcp.InitializeBody())

	assertHttpResponse.Status(t, resp, http.StatusUnauthorized)
	assertHttpResponse.HeaderContains(t, resp, "WWW-Authenticate", `error="invalid_token"`)
	assertHttpResponse.JSONIncludes(t, resp, map[string]any{"error": "invalid_token"})
	assertHttpResponse.BodyLacks(t, resp, key)
}

// The API quotes the key back in some errors; the model never gets to see it.
func TestE2E_Relay_KeyQuotedByTheAPIIsNotEchoedBack(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})
	key := arrange.APIKey(t)
	arrangeAPI.NewStub(key, "system/info").Register(t)
	arrangeAPI.NewStub(key, "domain/list").WithError(http.StatusForbidden, 902, "No access for key '"+key+"'").Register(t)

	res := actMcp.Connect(t, server, key).CallTool(t, server.Tool("domain.list"), nil)

	assertTool.FailedWith(t, res, "No access for key '***'")
	assertTool.FailedWithout(t, res, key)
}

func TestE2E_Relay_TimeoutIsReportedAsUnreachable(t *testing.T) {
	t.Parallel()
	upstreamTimeout := 500 * time.Millisecond
	server := arrangeServer.Start(t, arrangeServer.Options{UpstreamTimeout: upstreamTimeout})
	key := arrange.APIKey(t)
	arrangeAPI.NewStub(key, "system/info").WithDelay(3 * upstreamTimeout).Register(t)

	res := actMcp.Connect(t, server, key).CallTool(t, server.Tool("account.get"), nil)

	assertTool.FailedWith(t, res, "did not answer")
}

func TestE2E_Relay_InvalidArgumentsNeverReachTheAPI(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})
	key := arrange.APIKey(t)
	session := actMcp.Connect(t, server, key)

	missing := session.CallTool(t, server.Tool("template.get"), map[string]any{})
	unknown := session.CallTool(t, server.Tool("template.get"), map[string]any{"id": "t-1", "bogus": 1})
	badEnum := session.CallTool(t, server.Tool("suppression.set"), map[string]any{"email": "a@example.test", "cause": "bored"})

	assertTool.FailedWith(t, missing, "validating")
	assertTool.FailedWith(t, unknown, "validating")
	assertTool.FailedWith(t, badEnum, "validating")
	// Only the key check on each request reaches the API, never the tools' methods
	assertAPI.NotCalled(t, key, "template/get")
	assertAPI.NotCalled(t, key, "suppression/set")
}
