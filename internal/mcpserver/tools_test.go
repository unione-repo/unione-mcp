package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unione-repo/unione-mcp/internal/brand"
	"github.com/unione-repo/unione-mcp/internal/config"
)

func decoded(t *testing.T, text string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("answer is not a JSON object (%v): %s", err, text)
	}
	return doc
}

func wantBody(t *testing.T, call fakeCall, want map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(want)
	var normalized map[string]any
	_ = json.Unmarshal(raw, &normalized)
	if !reflect.DeepEqual(call.body, normalized) {
		t.Errorf("%s body\n got %#v\nwant %#v", call.method, call.body, normalized)
	}
}

func TestSendEmail_BuildsTheMessageTheAPIExpects(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t).answer("email/send", http.StatusOK, `{"status":"success","job_id":"j-1","emails":["anna@example.com"]}`)

	res := callVia(t, fake, testConfig(), "email.send", map[string]any{
		"recipients": []any{map[string]any{"email": " anna@example.com ", "substitutions": map[string]any{"name": "Anna"}}},
		"subject":    "Hi {{name}}",
		"from_email": "news@example.com",
		"html":       "<html><body>Hi {{name}}</body></html>",
		"tags":       []string{"welcome"},
		"track_read": false,
		"send_at":    "2026-09-28 13:00:00",
	})

	if res.IsError {
		t.Fatalf("send failed: %s", resultText(t, res))
	}
	call := fake.last(t, "email/send")
	if call.apiKey != "key-"+t.Name() {
		t.Errorf("X-API-KEY = %q, want the caller's bearer", call.apiKey)
	}
	if call.userAgent != serverName+"/test" {
		t.Errorf("User-Agent = %q, want %s/test", call.userAgent, serverName)
	}
	if call.platform != "mcp" {
		t.Errorf("?platform = %q, want mcp", call.platform)
	}
	wantBody(t, call, map[string]any{"message": map[string]any{
		"recipients": []any{map[string]any{"email": "anna@example.com", "substitutions": map[string]any{"name": "Anna"}}},
		"subject":    "Hi {{name}}",
		"from_email": "news@example.com",
		"body":       map[string]any{"html": "<html><body>Hi {{name}}</body></html>"},
		"tags":       []string{"welcome"},
		"track_read": 0,
		"options":    map[string]any{"send_at": "2026-09-28 13:00:00"},
	}})
}

// No checks of its own: whatever the API refuses, the agent reads its reason.
func TestSendEmail_TheAPIsRefusalReachesTheAgentAsIs(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t).answer("email/send", http.StatusForbidden,
		`{"status":"error","code":1598,"message":"Error in 'from_email' field. Sandbox domain in 'from_email' used with unconfirmed email 'a@example.com'."}`)

	res := callVia(t, fake, testConfig(), "email.send", map[string]any{
		"recipients": []any{map[string]any{"email": "a@example.com"}},
		"from_email": "x@sandbox-1.example.test",
		"plaintext":  "hi",
	})

	text := resultText(t, res)
	if !res.IsError || !strings.Contains(text, "code 1598") || !strings.Contains(text, "unconfirmed email 'a@example.com'") {
		t.Fatalf("want the API's code and text, got %s", text)
	}
	if !strings.Contains(text, "click the link in the confirmation email") || !strings.Contains(text, brand.Current().Tool("confirmed_email.list")) {
		t.Fatalf("want the next step for code 1598, got %s", text)
	}
}

func TestSetTemplate_SendsExactlyWhatWasPassed(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)

	res := callVia(t, fake, testConfig(), "template.set", map[string]any{
		"id": "t-1", "name": "Welcome", "from_email": "news@example.com", "html": "<html><body>new</body></html>",
	})

	if res.IsError {
		t.Fatalf("set failed: %s", resultText(t, res))
	}
	if calls := fake.recorded(); len(calls) != 1 {
		t.Fatalf("want exactly one call (template/set), got %v", calls)
	}
	wantBody(t, fake.last(t, "template/set"), map[string]any{"template": map[string]any{
		"id": "t-1", "name": "Welcome", "from_email": "news@example.com",
		"body": map[string]any{"html": "<html><body>new</body></html>"},
	}})
}

func TestListTemplates_AnswerPassesThroughWhole(t *testing.T) {
	t.Parallel()
	list := `{"status":"success","templates":[{"id":"t-1","name":"Welcome","body":{"html":"<b>big</b>"}}]}`
	fake := newFakeAPI(t).answer("template/list", http.StatusOK, list)

	res := callVia(t, fake, testConfig(), "template.list", nil)

	if got := resultText(t, res); got != list {
		t.Fatalf("answer changed:\n got %s\nwant %s", got, list)
	}
}

func TestSetWebhook_SendsTheSettingsInOneCall(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)

	res := callVia(t, fake, testConfig(), "webhook.set", map[string]any{
		"url": "https://hook.example/x", "status": "disabled", "max_parallel": 20,
		"email_status": []string{"delivered", "hard_bounced"}, "spam_block": true,
	})

	if res.IsError {
		t.Fatalf("set failed: %s", resultText(t, res))
	}
	if calls := fake.recorded(); len(calls) != 1 {
		t.Fatalf("want exactly one call (webhook/set), got %v", calls)
	}
	wantBody(t, fake.last(t, "webhook/set"), map[string]any{
		"url": "https://hook.example/x", "status": "disabled", "max_parallel": 20,
		"events": map[string]any{"email_status": []string{"delivered", "hard_bounced"}, "spam_block": []string{"*"}},
	})
}

func TestListProjects_ReturnsProjectKeysWhole(t *testing.T) {
	t.Parallel()
	list := `{"status":"success","projects":[{"id":"456","api_key":"6x7mxb5ehyzt1wjd8kpf7qsyrdshsdo3dnm4snxa","name":"Shop"}]}`
	fake := newFakeAPI(t).answer("project/list", http.StatusOK, list)

	res := callVia(t, fake, testConfig(), "project.list", nil)

	if got := resultText(t, res); got != list {
		t.Fatalf("answer changed:\n got %s\nwant %s", got, list)
	}
}

func TestUpdateProject_ByProjectID(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)

	res := callVia(t, fake, testConfig(), "project.update", map[string]any{
		"project_id": "456", "name": "Shop", "send_enabled": false, "reset_email_counter": true,
	})

	if res.IsError {
		t.Fatalf("update failed: %s", resultText(t, res))
	}
	wantBody(t, fake.last(t, "project/update"), map[string]any{
		"project_id": "456",
		"project":    map[string]any{"name": "Shop", "send_enabled": false, "email_counter": 0},
	})
}

func TestProjects_DisabledProjectsPointToSupport(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t).answer("project/list", http.StatusForbidden, `{"status":"error","code":105,"message":"No rights"}`)

	res := callVia(t, fake, testConfig(), "project.list", nil)

	text := resultText(t, res)
	if !res.IsError || !strings.Contains(text, "No rights") || !strings.Contains(text, "support") {
		t.Fatalf("want the API's text and a hint to ask support, got %s", text)
	}
}

func TestProjects_DisabledProjectsHintNamesTheBrand(t *testing.T) {
	t.Parallel()
	b := brand.Current()
	fake := newFakeAPI(t).answer("project/list", http.StatusForbidden, `{"status":"error","code":105,"message":"No rights"}`)

	text := resultText(t, callVia(t, fake, testConfig(), "project.list", nil))

	assertBrandedText(t, "projects hint", text)
	if !strings.Contains(text, "ask "+b.Name+" support") {
		t.Errorf("want the hint to name %s support, got %s", b.Name, text)
	}
}

func TestValidateDomain_AnswersBothChecks(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t).
		answer("domain/validate-verification-record", http.StatusOK, `{"status":"success","message":"Record updated"}`).
		answer("domain/validate-dkim", http.StatusBadRequest, `{"status":"error","code":122,"message":"DKIM record not found"}`)

	res := callVia(t, fake, testConfig(), "domain.validate", map[string]any{"domain": " example.com "})

	if res.IsError {
		t.Fatalf("one passing check must not fail the answer: %s", resultText(t, res))
	}
	doc := decoded(t, resultText(t, res))
	if doc["verification_record"].(map[string]any)["message"] != "Record updated" {
		t.Errorf("verification_record = %v", doc["verification_record"])
	}
	if dkim := doc["dkim"].(map[string]any); !strings.Contains(dkim["error"].(string), "DKIM record not found") {
		t.Errorf("dkim = %v", dkim)
	}
	wantBody(t, fake.last(t, "domain/validate-dkim"), map[string]any{"domain": "example.com"})
}

func TestRequestEventDump_SendsTheFilter(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t).answer("event-dump/create", http.StatusOK, `{"status":"success","dump_id":"0b8f8c5e-6a44-4b5e-9d7e-1e2f3a4b5c6d"}`)

	res := callVia(t, fake, testConfig(), "event_dump.create", map[string]any{
		"start_time": "2026-09-27 00:00:00", "filter": map[string]any{"job_id": "j-1", "status": "hard_bounced"},
	})

	if res.IsError {
		t.Fatalf("dump failed: %s", resultText(t, res))
	}
	wantBody(t, fake.last(t, "event-dump/create"), map[string]any{
		"start_time": "2026-09-27 00:00:00",
		"filter":     map[string]any{"job_id": "j-1", "status": "hard_bounced"},
	})
}

func TestCallTool_OAuthModeRelaysAnAccessTokenAsTheBearer(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)
	cfg := oauthConfig()
	token := testTokenPrefix + t.Name()

	res := callViaBearer(t, fake, cfg, token, "account.get", nil)

	if res.IsError {
		t.Fatalf("call failed: %s", resultText(t, res))
	}
	call := fake.last(t, "system/info")
	if call.authorization != "Bearer "+token || call.apiKey != "" {
		t.Fatalf("relayed Authorization %q X-API-KEY %q, want the token as the bearer only", call.authorization, call.apiKey)
	}
}

// Servers and CI connect with a key even where browsers go through OAuth.
func TestCallTool_OAuthModeStillTakesAnAPIKey(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)
	cfg := oauthConfig()

	res := callViaBearer(t, fake, cfg, "key-"+t.Name(), "account.get", nil)

	if res.IsError {
		t.Fatalf("call failed: %s", resultText(t, res))
	}
	call := fake.last(t, "system/info")
	if call.apiKey != "key-"+t.Name() || call.authorization != "" {
		t.Fatalf("relayed Authorization %q X-API-KEY %q, want the key as X-API-KEY only", call.authorization, call.apiKey)
	}
}

// The local mode has no HTTP headers: every call acts with the API key of the environment.
func TestCallTool_StdioModeActsWithTheConfiguredKey(t *testing.T) {
	t.Parallel()
	fake := newFakeAPI(t)
	api := httptest.NewServer(fake)
	t.Cleanup(api.Close)
	cfg := testConfig()
	cfg.Transport, cfg.APIKey = config.TransportStdio, "local-key-"+t.Name()
	s := connect(t, cfg, newUpstream(cfg.Brand, api.URL))

	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: brand.Current().Tool("account.get")})

	if err != nil || res.IsError {
		t.Fatalf("call failed: %v %s", err, resultText(t, res))
	}
	if got := fake.last(t, "system/info").apiKey; got != cfg.APIKey {
		t.Fatalf("X-API-KEY = %q, want the API key %q", got, cfg.APIKey)
	}
}
