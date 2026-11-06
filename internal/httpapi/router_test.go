package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/unione-repo/unione-mcp/internal/auth"
	"github.com/unione-repo/unione-mcp/internal/brand"
	"github.com/unione-repo/unione-mcp/internal/config"
	"github.com/unione-repo/unione-mcp/internal/httpapi"
)

const (
	metadataPath      = "/.well-known/oauth-protected-resource"
	appsChallengePath = "/.well-known/openai-apps-challenge"
	// testTokenPrefix tells an access token from an API key: a brand may have no
	// default prefix of its own.
	testTokenPrefix = "tst_"
)

type mcpSpy struct {
	reply string
	mu    sync.Mutex
	seen  int
}

func (s *mcpSpy) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.seen++
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, s.reply)
}

func (s *mcpSpy) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen
}

type stand struct {
	opts httpapi.Options
	mcp  *mcpSpy
	srv  *httptest.Server
}

func startRouter(t *testing.T, mode config.AuthMode) *stand {
	t.Helper()
	return startRouterWith(t, mode, nil)
}

func startRouterWith(t *testing.T, mode config.AuthMode, tweak func(*httpapi.Options)) *stand {
	t.Helper()
	mcp := &mcpSpy{reply: "mcp-reply-" + t.Name()}
	opts := httpapi.Options{
		Brand:               brand.Current(),
		SelfURL:             "https://mcp.example.test",
		AuthMode:            mode,
		AuthorizationServer: "https://auth.example.test",
		AccessTokenPrefix:   testTokenPrefix,
		Scopes:              []string{"scope-" + t.Name()},
		MCP:                 mcp,
	}
	if tweak != nil {
		tweak(&opts)
	}
	srv := httptest.NewServer(httpapi.New(opts))
	t.Cleanup(srv.Close)
	return &stand{opts: opts, mcp: mcp, srv: srv}
}

type response struct {
	Status int
	Header http.Header
	Body   []byte
}

func (s *stand) call(t *testing.T, method, path string, headers map[string]string) response {
	t.Helper()
	req, err := http.NewRequest(method, s.srv.URL+path, nil)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", method, path, err)
	}
	return response{Status: resp.StatusCode, Header: resp.Header, Body: body}
}

func decodeJSON[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return v
}

func TestRouter_HealthReportsOK(t *testing.T) {
	t.Parallel()
	r := startRouter(t, config.AuthAPIKey)

	res := r.call(t, http.MethodGet, "/health", nil)

	if res.Status != http.StatusOK {
		t.Fatalf("status %d, want %d", res.Status, http.StatusOK)
	}
	if got, want := decodeJSON[map[string]string](t, res.Body), (map[string]string{"status": "ok"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("body %v, want %v", got, want)
	}
}

func TestRouter_APIKeyMode_MissingKeyIsRefusedWithAClearText(t *testing.T) {
	t.Parallel()
	cases := map[string]map[string]string{
		"no headers":           nil,
		"basic scheme":         {"Authorization": "Basic dXNlcjpwYXNz"},
		"bearer without token": {"Authorization": "Bearer"},
		"X-API-KEY only":       {auth.HeaderAPIKey: "key-" + t.Name()},
	}
	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := startRouter(t, config.AuthAPIKey)

			res := r.call(t, http.MethodPost, "/mcp", headers)

			if res.Status != http.StatusUnauthorized {
				t.Fatalf("status %d, want %d", res.Status, http.StatusUnauthorized)
			}
			if challenge := res.Header.Get("WWW-Authenticate"); strings.Contains(challenge, "resource_metadata") {
				t.Fatalf("WWW-Authenticate %q points at OAuth metadata in api_key mode", challenge)
			}
			if got := decodeJSON[map[string]string](t, res.Body)["error_description"]; !strings.Contains(got, "Authorization: Bearer") {
				t.Fatalf("error_description %q does not say where the key goes", got)
			}
			if n := r.mcp.calls(); n != 0 {
				t.Fatalf("MCP handler was called %d times", n)
			}
		})
	}
}

func TestRouter_APIKeyMode_BearerDelegatesToMCP(t *testing.T) {
	t.Parallel()
	r := startRouter(t, config.AuthAPIKey)

	res := r.call(t, http.MethodPost, "/mcp", map[string]string{"Authorization": "Bearer key-" + t.Name()})

	if res.Status != http.StatusOK || string(res.Body) != r.mcp.reply {
		t.Fatalf("got %d %q, want %d %q from the MCP handler", res.Status, res.Body, http.StatusOK, r.mcp.reply)
	}
}

func TestRouter_APIKeyMode_ServesNoProtectedResourceMetadata(t *testing.T) {
	t.Parallel()
	r := startRouter(t, config.AuthAPIKey)

	res := r.call(t, http.MethodGet, metadataPath, nil)

	if res.Status != http.StatusNotFound {
		t.Fatalf("status %d, want %d", res.Status, http.StatusNotFound)
	}
}

type protectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	ScopesSupported        []string `json:"scopes_supported"`
}

// RFC 9728 §3.1: the resource /mcp has its metadata at the well-known path with /mcp
// appended, the origin at the bare one; each names exactly its own resource.
func TestRouter_OAuthMode_ServesProtectedResourceMetadata(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ path, resource string }{
		"of the MCP endpoint": {metadataPath + "/mcp", "https://mcp.example.test/mcp"},
		"of the origin":       {metadataPath, "https://mcp.example.test"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := startRouter(t, config.AuthOAuth)

			res := r.call(t, http.MethodGet, tc.path, nil)

			want := protectedResourceMetadata{
				Resource:               tc.resource,
				AuthorizationServers:   []string{r.opts.AuthorizationServer},
				BearerMethodsSupported: []string{"header"},
				ScopesSupported:        r.opts.Scopes,
			}
			if res.Status != http.StatusOK {
				t.Fatalf("status %d, want %d", res.Status, http.StatusOK)
			}
			if got := decodeJSON[protectedResourceMetadata](t, res.Body); !reflect.DeepEqual(got, want) {
				t.Fatalf("metadata\n got %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestRouter_OAuthMode_MissingBearerChallenges(t *testing.T) {
	t.Parallel()
	cases := map[string]map[string]string{
		"no headers":            nil,
		"api key is not enough": {auth.HeaderAPIKey: "key-" + t.Name()},
	}
	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := startRouter(t, config.AuthOAuth)

			res := r.call(t, http.MethodPost, "/mcp", headers)

			if res.Status != http.StatusUnauthorized {
				t.Fatalf("status %d, want %d", res.Status, http.StatusUnauthorized)
			}
			if got, want := res.Header.Get("WWW-Authenticate"), `Bearer resource_metadata="`+r.opts.SelfURL+metadataPath+`"`; got != want {
				t.Fatalf("WWW-Authenticate %q, want %q", got, want)
			}
			discoveryURL := strings.TrimSuffix(strings.TrimPrefix(res.Header.Get("WWW-Authenticate"), `Bearer resource_metadata="`), `"`)
			metadata := r.call(t, http.MethodGet, strings.TrimPrefix(discoveryURL, r.opts.SelfURL), nil)
			if metadata.Status != http.StatusOK {
				t.Fatalf("advertised metadata status %d, want %d", metadata.Status, http.StatusOK)
			}
			got := decodeJSON[protectedResourceMetadata](t, metadata.Body)
			if got.Resource != r.opts.SelfURL || !reflect.DeepEqual(got.AuthorizationServers, []string{r.opts.AuthorizationServer}) {
				t.Fatalf("advertised metadata %+v does not describe the origin and its authorization server", got)
			}
			if n := r.mcp.calls(); n != 0 {
				t.Fatalf("MCP handler was called %d times", n)
			}
		})
	}
}

func TestRouter_OAuthMode_BearerDelegates(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"access token": testTokenPrefix + "tok",
		"API key":      "key",
	}
	for name, bearer := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := startRouter(t, config.AuthOAuth)

			res := r.call(t, http.MethodPost, "/mcp", map[string]string{"Authorization": "Bearer " + bearer})

			if res.Status != http.StatusOK || string(res.Body) != r.mcp.reply {
				t.Fatalf("got %d %q, want %d %q from the MCP handler", res.Status, res.Body, http.StatusOK, r.mcp.reply)
			}
		})
	}
}

// A token the API refuses during the call turns the answer into the RFC 6750
// invalid_token challenge, so the client refreshes the token instead of failing.
func TestRouter_OAuthMode_RejectedTokenIsAnswered401(t *testing.T) {
	t.Parallel()
	mcp := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.MarkTokenRejected(r.Context())
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "tool result that must not leak")
	})
	opts := httpapi.Options{Brand: brand.Current(), SelfURL: "https://mcp.example.test", AuthMode: config.AuthOAuth, AuthorizationServer: "https://auth.example.test", AccessTokenPrefix: testTokenPrefix, MCP: mcp}
	srv := httptest.NewServer(httpapi.New(opts))
	t.Cleanup(srv.Close)
	r := &stand{opts: opts, srv: srv}

	res := r.call(t, http.MethodPost, "/mcp", map[string]string{"Authorization": "Bearer " + testTokenPrefix + "expired"})

	if res.Status != http.StatusUnauthorized {
		t.Fatalf("status %d, want %d", res.Status, http.StatusUnauthorized)
	}
	challenge := res.Header.Get("WWW-Authenticate")
	for _, part := range []string{`error="invalid_token"`, `resource_metadata="https://mcp.example.test` + metadataPath + `"`} {
		if !strings.Contains(challenge, part) {
			t.Fatalf("WWW-Authenticate %q lacks %s", challenge, part)
		}
	}
	if strings.Contains(string(res.Body), "must not leak") {
		t.Fatalf("body %q carries the dropped tool result", res.Body)
	}
}

// MCP authorization wants the 401 on any request with a bad token, initialize and
// tools/list included: the client then signs in again on connect.
func TestRouter_OAuthMode_TokenTheAPIRefusesIsAnswered401BeforeMCP(t *testing.T) {
	t.Parallel()
	asked := map[string]int{}
	var mu sync.Mutex
	tokens := auth.NewVerifier(func(_ context.Context, token string) auth.Verdict {
		mu.Lock()
		asked[token]++
		mu.Unlock()
		if strings.HasSuffix(token, "revoked") {
			return auth.Rejected
		}
		return auth.Accepted
	})
	mcp := &mcpSpy{reply: "mcp-reply"}
	opts := httpapi.Options{Brand: brand.Current(), SelfURL: "https://mcp.example.test", AuthMode: config.AuthOAuth, AuthorizationServer: "https://auth.example.test", AccessTokenPrefix: testTokenPrefix, MCP: mcp, Tokens: tokens}
	srv := httptest.NewServer(httpapi.New(opts))
	t.Cleanup(srv.Close)
	r := &stand{opts: opts, mcp: mcp, srv: srv}

	refused := r.call(t, http.MethodPost, "/mcp", map[string]string{"Authorization": "Bearer " + testTokenPrefix + "revoked"})
	accepted := r.call(t, http.MethodPost, "/mcp", map[string]string{"Authorization": "Bearer " + testTokenPrefix + "valid"})
	key := r.call(t, http.MethodPost, "/mcp", map[string]string{"Authorization": "Bearer key"})

	if refused.Status != http.StatusUnauthorized || !strings.Contains(refused.Header.Get("WWW-Authenticate"), `error="invalid_token"`) {
		t.Fatalf("refused token: got %d %q, want the invalid_token challenge", refused.Status, refused.Header.Get("WWW-Authenticate"))
	}
	if got := refused.Header.Get("WWW-Authenticate"); !strings.Contains(got, `resource_metadata="`+opts.SelfURL+metadataPath+`"`) {
		t.Fatalf("refused token challenge %q does not advertise origin metadata", got)
	}
	if accepted.Status != http.StatusOK || key.Status != http.StatusOK {
		t.Fatalf("accepted token %d, API key %d, want both served", accepted.Status, key.Status)
	}
	if n := mcp.calls(); n != 2 {
		t.Fatalf("MCP handler was called %d times, want 2: the refused token must not reach it", n)
	}
	if asked["key"] != 0 {
		t.Fatal("an API key went to the token check")
	}
}

// A token revoked after a served request is refused on the next one, before MCP:
// tools/list right after the revoke and initialize of a new connection alike.
func TestRouter_OAuthMode_TokenRevokedAfterAServedRequestIsAnswered401(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	revoked := false
	tokens := auth.NewVerifier(func(context.Context, string) auth.Verdict {
		mu.Lock()
		defer mu.Unlock()
		if revoked {
			return auth.Rejected
		}
		return auth.Accepted
	})
	r := startRouterWith(t, config.AuthOAuth, func(o *httpapi.Options) { o.Tokens = tokens })
	bearer := map[string]string{"Authorization": "Bearer " + testTokenPrefix + "token"}

	if res := r.call(t, http.MethodPost, "/mcp", bearer); res.Status != http.StatusOK {
		t.Fatalf("before the revoke: got %d, want 200", res.Status)
	}
	mu.Lock()
	revoked = true
	mu.Unlock()
	res := r.call(t, http.MethodPost, "/mcp", bearer)

	if res.Status != http.StatusUnauthorized || !strings.Contains(res.Header.Get("WWW-Authenticate"), `error="invalid_token"`) {
		t.Fatalf("after the revoke: got %d %q, want the invalid_token challenge", res.Status, res.Header.Get("WWW-Authenticate"))
	}
	if n := r.mcp.calls(); n != 1 {
		t.Fatalf("MCP handler was called %d times, want 1: the revoked token must not reach it", n)
	}
}

// An API key is never answered with the token challenge: there is nothing to refresh.
func TestRouter_OAuthMode_RejectionOfAKeyStaysAToolResult(t *testing.T) {
	t.Parallel()
	mcp := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.MarkTokenRejected(r.Context())
		_, _ = io.WriteString(w, "tool error")
	})
	opts := httpapi.Options{Brand: brand.Current(), SelfURL: "https://mcp.example.test", AuthMode: config.AuthOAuth, AuthorizationServer: "https://auth.example.test", AccessTokenPrefix: testTokenPrefix, MCP: mcp}
	srv := httptest.NewServer(httpapi.New(opts))
	t.Cleanup(srv.Close)
	r := &stand{opts: opts, srv: srv}

	res := r.call(t, http.MethodPost, "/mcp", map[string]string{"Authorization": "Bearer key"})

	if res.Status != http.StatusOK || string(res.Body) != "tool error" {
		t.Fatalf("got %d %q, want the tool result", res.Status, res.Body)
	}
}

// initialize and tools/list make no API call of their own: an API key the API does
// not know is refused before MCP, or any bearer would get the tool list.
func TestRouter_KeyTheAPIRefusesIsAnswered401BeforeMCP(t *testing.T) {
	t.Parallel()
	for _, mode := range []config.AuthMode{config.AuthAPIKey, config.AuthOAuth} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			asked := map[string]int{}
			var mu sync.Mutex
			keys := auth.NewVerifier(func(_ context.Context, key string) auth.Verdict {
				mu.Lock()
				asked[key]++
				mu.Unlock()
				switch key {
				case "garbage":
					return auth.Rejected
				case "api-down":
					return auth.Unknown
				}
				return auth.Accepted
			})
			r := startRouterWith(t, mode, func(o *httpapi.Options) { o.Keys = keys })

			refused := r.call(t, http.MethodPost, "/mcp", map[string]string{"Authorization": "Bearer garbage"})
			accepted := r.call(t, http.MethodPost, "/mcp", map[string]string{"Authorization": "Bearer valid"})
			unknown := r.call(t, http.MethodPost, "/mcp", map[string]string{"Authorization": "Bearer api-down"})

			if refused.Status != http.StatusUnauthorized || !strings.Contains(refused.Header.Get("WWW-Authenticate"), `error="invalid_token"`) {
				t.Fatalf("refused key: got %d %q, want the invalid_token challenge", refused.Status, refused.Header.Get("WWW-Authenticate"))
			}
			if strings.Contains(string(refused.Body), r.mcp.reply) {
				t.Fatalf("refused key reached MCP: %q", refused.Body)
			}
			if accepted.Status != http.StatusOK || unknown.Status != http.StatusOK {
				t.Fatalf("accepted key %d, unanswered check %d, want both served", accepted.Status, unknown.Status)
			}
			if n := r.mcp.calls(); n != 2 {
				t.Fatalf("MCP handler was called %d times, want 2: the refused key must not reach it", n)
			}
			wantMetadata := mode == config.AuthOAuth
			if got := strings.Contains(refused.Header.Get("WWW-Authenticate"), "resource_metadata="); got != wantMetadata {
				t.Fatalf("WWW-Authenticate %q: resource_metadata present %v, want %v", refused.Header.Get("WWW-Authenticate"), got, wantMetadata)
			}
			if wantMetadata && !strings.Contains(refused.Header.Get("WWW-Authenticate"), `resource_metadata="`+r.opts.SelfURL+metadataPath+`"`) {
				t.Fatalf("refused key challenge %q does not advertise origin metadata", refused.Header.Get("WWW-Authenticate"))
			}
		})
	}
}

// An access token goes to the token check only, never to the key check.
func TestRouter_OAuthMode_AccessTokenSkipsTheKeyCheck(t *testing.T) {
	t.Parallel()
	keys := auth.NewVerifier(func(context.Context, string) auth.Verdict {
		t.Error("an access token went to the key check")
		return auth.Rejected
	})
	r := startRouterWith(t, config.AuthOAuth, func(o *httpapi.Options) { o.Keys = keys })

	res := r.call(t, http.MethodPost, "/mcp", map[string]string{"Authorization": "Bearer " + testTokenPrefix + "token"})

	if res.Status != http.StatusOK {
		t.Fatalf("status %d, want 200", res.Status)
	}
}

func TestRouter_MCPRejectsNonPostMethods(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			r := startRouter(t, config.AuthAPIKey)

			res := r.call(t, method, "/mcp", map[string]string{"Authorization": "Bearer key-" + t.Name()})

			if res.Status != http.StatusMethodNotAllowed {
				t.Fatalf("status %d, want %d", res.Status, http.StatusMethodNotAllowed)
			}
			if allow := res.Header.Get("Allow"); allow != http.MethodPost {
				t.Fatalf("Allow %q, want %q", allow, http.MethodPost)
			}
			if n := r.mcp.calls(); n != 0 {
				t.Fatalf("MCP handler was called %d times", n)
			}
		})
	}
}

func TestRouter_UnknownPathIsNotFound(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/", "/mcp/extra", "/healthz"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			r := startRouter(t, config.AuthAPIKey)

			res := r.call(t, http.MethodGet, path, nil)

			if res.Status != http.StatusNotFound {
				t.Fatalf("status %d, want %d", res.Status, http.StatusNotFound)
			}
		})
	}
}

// OpenAI compares the body with the token it issued: plain text, nothing around it,
// no credential asked, in either auth mode.
func TestRouter_ServesTheAppsChallengeAsPlainText(t *testing.T) {
	t.Parallel()
	for _, mode := range []config.AuthMode{config.AuthAPIKey, config.AuthOAuth} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			token := "challenge-" + t.Name()
			r := startRouterWith(t, mode, func(o *httpapi.Options) { o.AppsChallenge = token })

			res := r.call(t, http.MethodGet, appsChallengePath, nil)

			if res.Status != http.StatusOK {
				t.Fatalf("status %d, want %d", res.Status, http.StatusOK)
			}
			if got := res.Header.Get("Content-Type"); got != "text/plain" {
				t.Fatalf("Content-Type %q, want text/plain", got)
			}
			if string(res.Body) != token {
				t.Fatalf("body %q, want %q", res.Body, token)
			}
		})
	}
}

func TestRouter_WithoutAppsChallengeThePathIsNotFound(t *testing.T) {
	t.Parallel()
	r := startRouter(t, config.AuthOAuth)

	res := r.call(t, http.MethodGet, appsChallengePath, nil)

	if res.Status != http.StatusNotFound {
		t.Fatalf("status %d, want %d", res.Status, http.StatusNotFound)
	}
}

func TestRouter_ForeignOriginIsRefusedBeforeAuth(t *testing.T) {
	t.Parallel()
	for _, mode := range []config.AuthMode{config.AuthAPIKey, config.AuthOAuth} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			r := startRouter(t, mode)

			res := r.call(t, http.MethodPost, "/mcp", map[string]string{
				"Authorization": "Bearer key-" + t.Name(),
				"Origin":        "https://evil.example",
			})

			if res.Status != http.StatusForbidden {
				t.Fatalf("status %d, want %d", res.Status, http.StatusForbidden)
			}
			if n := r.mcp.calls(); n != 0 {
				t.Fatalf("MCP handler was called %d times", n)
			}
		})
	}
}

// Near misses of the own origin https://mcp.example.test are foreign too.
func TestRouter_NearMissOriginIsRefused(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"opaque origin":   "null",
		"other scheme":    "http://mcp.example.test",
		"other port":      "https://mcp.example.test:8443",
		"default of http": "https://mcp.example.test:80",
		"subdomain":       "https://evil.mcp.example.test",
		"suffix":          "https://mcp.example.test.evil.example",
		"credentials":     "https://user@mcp.example.test",
	}
	for name, origin := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := startRouter(t, config.AuthAPIKey)

			res := r.call(t, http.MethodPost, "/mcp", map[string]string{
				"Authorization": "Bearer key-" + t.Name(),
				"Origin":        origin,
			})

			if res.Status != http.StatusForbidden {
				t.Fatalf("Origin %q: status %d, want %d", origin, res.Status, http.StatusForbidden)
			}
			if n := r.mcp.calls(); n != 0 {
				t.Fatalf("MCP handler was called %d times", n)
			}
		})
	}
}

// Discovery is public: a browser page (an MCP inspector, a web client) reads the
// metadata cross-origin, so the Origin check guards /mcp only.
func TestRouter_DiscoveryIgnoresOrigin(t *testing.T) {
	t.Parallel()
	for _, path := range []string{metadataPath, metadataPath + "/mcp"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			r := startRouter(t, config.AuthOAuth)

			res := r.call(t, http.MethodGet, path, map[string]string{"Origin": "https://evil.example"})

			if res.Status != http.StatusOK {
				t.Fatalf("status %d, want %d", res.Status, http.StatusOK)
			}
		})
	}
}

func TestRouter_OwnOriginOrNoOriginReachesMCP(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ self, origin string }{
		"no Origin":                         {"https://mcp.example.test", ""},
		"own origin":                        {"https://mcp.example.test", "https://mcp.example.test"},
		"own origin, other case":            {"https://mcp.example.test", "HTTPS://MCP.example.test"},
		"own origin, default port spelled":  {"https://mcp.example.test", "https://mcp.example.test:443"},
		"public URL names the default port": {"https://mcp.example.test:443", "https://mcp.example.test"},
		"public URL with a custom port":     {"http://mcp.example.test:8080", "http://mcp.example.test:8080"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := startRouterWith(t, config.AuthAPIKey, func(o *httpapi.Options) { o.SelfURL = c.self })
			origin := c.origin
			headers := map[string]string{"Authorization": "Bearer key-" + t.Name()}
			if origin != "" {
				headers["Origin"] = origin
			}

			res := r.call(t, http.MethodPost, "/mcp", headers)

			if res.Status != http.StatusOK || string(res.Body) != r.mcp.reply {
				t.Fatalf("got %d %q, want the MCP handler's reply", res.Status, res.Body)
			}
		})
	}
}
