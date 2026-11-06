package actMcp

import (
	"context"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	arrangeServer "github.com/unione-repo/unione-mcp/tests/support/arrange/server"
)

const (
	connectTimeout = 10 * time.Second
	callTimeout    = 30 * time.Second
)

type Session struct {
	cs *mcp.ClientSession
}

// Connect opens a session that presents the key the way an MCP client config
// does for the remote server: Authorization: Bearer <key>.
func Connect(t *testing.T, server *arrangeServer.Server, apiKey string) *Session {
	t.Helper()

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	return ConnectWithHeaders(t, server, headers)
}

// ConnectStdio starts the local server as a client would — a child process
// speaking MCP over stdin/stdout — acting with apiKey from the API key variable.
func ConnectStdio(t *testing.T, apiKey string) *Session {
	t.Helper()

	arrangeServer.RequireWiremock(t)
	cmd := exec.Command(arrangeServer.Binary(t), "stdio")
	cmd.Dir = t.TempDir()
	cmd.Env = arrangeServer.StdioEnv(apiKey)
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "acceptance-tests", Version: "0"}, nil)
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("start the stdio server: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return &Session{cs: cs}
}

func ConnectWithHeaders(t *testing.T, server *arrangeServer.Server, headers http.Header) *Session {
	t.Helper()

	transport := &mcp.StreamableClientTransport{
		Endpoint:             server.MCPEndpoint,
		HTTPClient:           &http.Client{Transport: &headerTransport{headers: headers, next: http.DefaultTransport}},
		DisableStandaloneSSE: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "acceptance-tests", Version: "0"}, nil)
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect to %s: %v", server.MCPEndpoint, err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return &Session{cs: cs}
}

type headerTransport struct {
	headers http.Header
	next    http.RoundTripper
}

func (h *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for name, values := range h.headers {
		req.Header[name] = values
	}
	return h.next.RoundTrip(req)
}

func (s *Session) CallTool(t *testing.T, name string, args any) *mcp.CallToolResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	res, err := s.cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return res
}

func (s *Session) ListTools(t *testing.T) []*mcp.Tool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	res, err := s.cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	return res.Tools
}

func (s *Session) ServerInfo() *mcp.Implementation {
	return s.cs.InitializeResult().ServerInfo
}

func (s *Session) Instructions() string {
	return s.cs.InitializeResult().Instructions
}

// Response is a raw HTTP exchange with the body already read: tests assert on
// values and never own a stream.
type Response struct {
	Method string
	URL    string
	Status int
	Header http.Header
	Body   []byte
}

func PostRaw(t *testing.T, endpoint string, headers map[string]string, body string) *Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build POST %s: %v", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	return send(t, req)
}

func GetRaw(t *testing.T, endpoint string) *Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatalf("build GET %s: %v", endpoint, err)
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	return send(t, req)
}

func InitializeBody() string {
	return `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"acceptance-tests","version":"0"}}}`
}

func send(t *testing.T, req *http.Request) *Response {
	t.Helper()

	resp, err := (&http.Client{Timeout: callTimeout}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s: read body: %v", req.Method, req.URL, err)
	}
	return &Response{Method: req.Method, URL: req.URL.String(), Status: resp.StatusCode, Header: resp.Header, Body: body}
}
