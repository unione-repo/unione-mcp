//go:build integration

package acceptance

import (
	"net/http"
	"testing"

	"github.com/unione-repo/unione-mcp/internal/brand"
	"github.com/unione-repo/unione-mcp/internal/config"
	actMcp "github.com/unione-repo/unione-mcp/tests/support/act/mcp"
	"github.com/unione-repo/unione-mcp/tests/support/arrange"
	arrangeServer "github.com/unione-repo/unione-mcp/tests/support/arrange/server"
	assertHttpResponse "github.com/unione-repo/unione-mcp/tests/support/assert/httpResponse"
	assertMcp "github.com/unione-repo/unione-mcp/tests/support/assert/mcp"
)

func TestE2E_Protocol_MissingKeyIsRefusedWithAClearText(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})

	resp := actMcp.PostRaw(t, server.MCPEndpoint, nil, actMcp.InitializeBody())

	assertHttpResponse.Status(t, resp, http.StatusUnauthorized)
	assertHttpResponse.HeaderIs(t, resp, "WWW-Authenticate", `Bearer realm="`+brand.Current().Name+`"`)
	assertHttpResponse.JSONIncludes(t, resp, map[string]any{"error": "invalid_token"})
}

func TestE2E_Protocol_APIKeyModeServesNoOAuthMetadata(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})

	resp := actMcp.GetRaw(t, server.BaseURL+"/.well-known/oauth-protected-resource")

	assertHttpResponse.Status(t, resp, http.StatusNotFound)
}

func TestE2E_Protocol_OAuthModeChallengesAndAdvertisesTheAuthorizationServer(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{AuthMode: config.AuthOAuth})

	challenge := actMcp.PostRaw(t, server.MCPEndpoint, nil, actMcp.InitializeBody())
	metadata := actMcp.GetRaw(t, server.BaseURL+"/.well-known/oauth-protected-resource/mcp")
	originMetadata := actMcp.GetRaw(t, server.BaseURL+"/.well-known/oauth-protected-resource")

	assertHttpResponse.Status(t, challenge, http.StatusUnauthorized)
	assertHttpResponse.HeaderContains(t, challenge, "WWW-Authenticate",
		`resource_metadata="`+server.BaseURL+`/.well-known/oauth-protected-resource"`)
	assertHttpResponse.Status(t, metadata, http.StatusOK)
	assertHttpResponse.JSONIncludes(t, metadata, map[string]any{
		"resource":              server.MCPEndpoint,
		"authorization_servers": []string{arrangeServer.AuthorizationServer},
	})
	assertHttpResponse.Status(t, originMetadata, http.StatusOK)
	assertHttpResponse.JSONIncludes(t, originMetadata, map[string]any{
		"resource":              server.BaseURL,
		"authorization_servers": []string{arrangeServer.AuthorizationServer},
	})
}

func TestE2E_Protocol_ServesTheOpenAIAppsChallenge(t *testing.T) {
	t.Parallel()
	token := "challenge-" + t.Name()
	server := arrangeServer.Start(t, arrangeServer.Options{AuthMode: config.AuthOAuth, AppsChallenge: token})

	challenge := actMcp.GetRaw(t, server.BaseURL+"/.well-known/openai-apps-challenge")
	metadata := actMcp.GetRaw(t, server.BaseURL+"/.well-known/oauth-protected-resource/mcp")
	health := actMcp.GetRaw(t, server.BaseURL+"/health")

	assertHttpResponse.Status(t, challenge, http.StatusOK)
	assertHttpResponse.HeaderIs(t, challenge, "Content-Type", "text/plain")
	assertHttpResponse.BodyIs(t, challenge, token)
	assertHttpResponse.Status(t, metadata, http.StatusOK)
	assertHttpResponse.Status(t, health, http.StatusOK)
}

func TestE2E_Protocol_WithoutTheVariableTheAppsChallengeIs404(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{AuthMode: config.AuthOAuth})

	resp := actMcp.GetRaw(t, server.BaseURL+"/.well-known/openai-apps-challenge")

	assertHttpResponse.Status(t, resp, http.StatusNotFound)
}

func TestE2E_Protocol_GetOnMcpIs405WithAllow(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})

	resp := actMcp.GetRaw(t, server.MCPEndpoint)

	assertHttpResponse.Status(t, resp, http.StatusMethodNotAllowed)
	assertHttpResponse.HeaderIs(t, resp, "Allow", http.MethodPost)
}

func TestE2E_Protocol_HealthIsOk(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})

	resp := actMcp.GetRaw(t, server.BaseURL+"/health")

	assertHttpResponse.Status(t, resp, http.StatusOK)
	assertHttpResponse.JSONIncludes(t, resp, map[string]any{"status": "ok"})
}

func TestE2E_Protocol_ServerIntroducesItselfAndItsTools(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})
	session := actMcp.Connect(t, server, arrange.APIKey(t))

	tools := session.ListTools(t)

	assertMcp.ServerNamed(t, session.ServerInfo(), brand.Current().ServerName())
	assertMcp.HasTool(t, tools, server.Tool("email.send"))
	assertMcp.InstructionsMention(t, session.Instructions(), server.Tool("domain.dns_records"))
}
