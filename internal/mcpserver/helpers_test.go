package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unione-repo/unione-mcp/internal/brand"
	"github.com/unione-repo/unione-mcp/internal/config"
	"github.com/unione-repo/unione-mcp/internal/upstream"
)

func newUpstream(b brand.Brand, baseURL string) *upstream.Client {
	up := upstream.New(b, baseURL, "test", time.Second, 1<<20, slog.New(slog.DiscardHandler))
	up.HTTP.Transport = ownTransport()
	return up
}

// ownTransport keeps a test off http.DefaultTransport: httptest.Server.Close
// calls DefaultTransport.CloseIdleConnections, which breaks the connections of
// the tests running in parallel ("http: CloseIdleConnections called").
func ownTransport() http.RoundTripper {
	return http.DefaultTransport.(*http.Transport).Clone()
}

// The unit tests run as the brand of the build (brand.Current) and call every
// tool by its id; the helpers turn it into the brand's name.
var serverName = brand.Current().ServerName()

// testTokenPrefix tells an access token from an API key in oauth mode: a brand
// may have no default prefix of its own.
const testTokenPrefix = "tst_"

func testConfig() *config.Config {
	return &config.Config{Brand: brand.Current(), Transport: config.TransportHTTP, AuthMode: config.AuthAPIKey}
}

func oauthConfig() *config.Config {
	cfg := testConfig()
	cfg.AuthMode = config.AuthOAuth
	cfg.AccessTokenPrefix = testTokenPrefix
	return cfg
}

// connect opens an in-memory session. The in-memory transport carries no HTTP
// headers, so tool calls reach the API only through a session from connectHTTP.
func connect(t *testing.T, cfg *config.Config, up *upstream.Client) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := Build(cfg, up, slog.New(slog.DiscardHandler)).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: t.Name(), Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		_ = serverSession.Close()
	})
	return session
}

func session(t *testing.T) *mcp.ClientSession {
	t.Helper()
	return connect(t, testConfig(), newUpstream(brand.Current(), "http://127.0.0.1:1"))
}

func sessionWith(t *testing.T, cfg *config.Config) *mcp.ClientSession {
	t.Helper()
	return connect(t, cfg, newUpstream(cfg.Brand, "http://127.0.0.1:1"))
}

func listTools(t *testing.T, session *mcp.ClientSession) []*mcp.Tool {
	t.Helper()
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	return listed.Tools
}

func toolNames(tools []*mcp.Tool) map[string]bool {
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}
	return names
}

// toolRefRe matches the brand's name of any tool, so a text naming a tool the
// server does not register (read-only mode, the send switch) fails to resolve.
var toolRefRe = func() *regexp.Regexp {
	var names []string
	for id := range toolClasses {
		names = append(names, regexp.QuoteMeta(brand.Current().Tool(id)))
	}
	return regexp.MustCompile(`\b(?:` + strings.Join(names, "|") + `)\b`)
}()

func assertRefsResolve(t *testing.T, subject, text string, registered map[string]bool) int {
	t.Helper()
	refs := toolRefRe.FindAllString(text, -1)
	for _, ref := range refs {
		if !registered[ref] {
			t.Errorf("%s refers to unregistered %s", subject, ref)
		}
	}
	return len(refs)
}

func schemaOf(t *testing.T, tool *mcp.Tool) *jsonschema.Schema {
	t.Helper()
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("%s: encode input schema: %v", tool.Name, err)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("%s: decode input schema: %v", tool.Name, err)
	}
	return &s
}

func schemaProperties(path string, s *jsonschema.Schema) map[string]string {
	described := map[string]string{}
	for name, p := range s.Properties {
		described[path+"."+name] = p.Description
		maps.Copy(described, schemaProperties(path+"."+name, p))
		if p.Items != nil {
			maps.Copy(described, schemaProperties(path+"."+name+"[]", p.Items))
		}
	}
	return described
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	parts := make([]string, 0, len(res.Content))
	for _, content := range res.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// fakeAPI answers API methods from a table and records what reached it.
type fakeAPI struct {
	t       *testing.T
	answers map[string]fakeAnswer
	mu      sync.Mutex
	calls   []fakeCall
}

type fakeAnswer struct {
	status int
	body   string
}

type fakeCall struct {
	method        string
	apiKey        string
	authorization string
	userAgent     string
	platform      string
	body          map[string]any
}

func newFakeAPI(t *testing.T) *fakeAPI {
	return &fakeAPI{t: t, answers: map[string]fakeAnswer{}}
}

func (f *fakeAPI) answer(method string, status int, body string) *fakeAPI {
	f.answers[method] = fakeAnswer{status: status, body: body}
	return f
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	method := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".json")
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{
		method:        method,
		apiKey:        r.Header.Get("X-API-KEY"),
		authorization: r.Header.Get("Authorization"),
		userAgent:     r.Header.Get("User-Agent"),
		platform:      r.URL.Query().Get("platform"),
		body:          body,
	})
	f.mu.Unlock()
	answer, ok := f.answers[method]
	if !ok {
		answer = fakeAnswer{status: http.StatusOK, body: `{"status":"success"}`}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(answer.status)
	_, _ = io.WriteString(w, answer.body)
}

func (f *fakeAPI) recorded() []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeCall(nil), f.calls...)
}

func (f *fakeAPI) last(t *testing.T, method string) fakeCall {
	t.Helper()
	calls := f.recorded()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].method == method {
			return calls[i]
		}
	}
	t.Fatalf("%s was never called; calls: %v", method, calls)
	return fakeCall{}
}

// callVia runs one tool against the fake over the real HTTP transport, so the
// caller's API key travels as Authorization: Bearer as it does in production.
func callVia(t *testing.T, fake *fakeAPI, cfg *config.Config, name string, args any) *mcp.CallToolResult {
	t.Helper()
	return callViaBearer(t, fake, cfg, "key-"+t.Name(), name, args)
}

func callViaBearer(t *testing.T, fake *fakeAPI, cfg *config.Config, bearer, name string, args any) *mcp.CallToolResult {
	t.Helper()
	api := httptest.NewServer(fake)
	t.Cleanup(api.Close)
	server := Build(cfg, newUpstream(cfg.Brand, api.URL), slog.New(slog.DiscardHandler))
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	front := httptest.NewServer(handler)
	t.Cleanup(front.Close)

	transport := &mcp.StreamableClientTransport{
		Endpoint:             front.URL,
		HTTPClient:           &http.Client{Transport: keyTransport{key: bearer, base: ownTransport()}},
		DisableStandaloneSSE: true,
	}
	ctx := context.Background()
	session, err := mcp.NewClient(&mcp.Implementation{Name: t.Name(), Version: "test"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: cfg.Brand.Tool(name), Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return res
}

type keyTransport struct {
	key  string
	base http.RoundTripper
}

func (k keyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+k.key)
	return k.base.RoundTrip(req)
}
