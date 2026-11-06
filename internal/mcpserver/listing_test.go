package mcpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unione-repo/unione-mcp/internal/brand"
)

// wireTool is a tool of tools/list as it crosses the wire, before the SDK decodes it.
type wireTool struct {
	Name        string                     `json:"name"`
	Title       string                     `json:"title"`
	Annotations map[string]json.RawMessage `json:"annotations"`
}

// recordingTransport keeps the raw result of every response the client reads.
type recordingTransport struct {
	mcp.Transport
	mu      sync.Mutex
	results []json.RawMessage
}

func (t *recordingTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	conn, err := t.Transport.Connect(ctx)
	return recordingConn{conn, t}, err
}

type recordingConn struct {
	mcp.Connection
	t *recordingTransport
}

func (c recordingConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := c.Connection.Read(ctx)
	if res, ok := msg.(*jsonrpc.Response); ok && err == nil {
		c.t.mu.Lock()
		c.t.results = append(c.t.results, res.Result)
		c.t.mu.Unlock()
	}
	return msg, err
}

func wireTools(t *testing.T) []wireTool {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server := Build(testConfig(), newUpstream(brand.Current(), "http://127.0.0.1:1"), slog.New(slog.DiscardHandler))
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	recording := &recordingTransport{Transport: clientTransport}
	session, err := mcp.NewClient(&mcp.Implementation{Name: t.Name(), Version: "test"}, nil).Connect(ctx, recording, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		_ = serverSession.Close()
	})
	if _, err := session.ListTools(ctx, nil); err != nil {
		t.Fatalf("list tools: %v", err)
	}
	recording.mu.Lock()
	defer recording.mu.Unlock()
	var listed struct {
		Tools []wireTool `json:"tools"`
	}
	if err := json.Unmarshal(recording.results[len(recording.results)-1], &listed); err != nil {
		t.Fatalf("tools/list result: %v", err)
	}
	if len(listed.Tools) != len(toolClasses) {
		t.Fatalf("tools/list carries %d tools, want %d", len(listed.Tools), len(toolClasses))
	}
	return listed.Tools
}

// The directory review reads the hints from the wire: every write tool states
// readOnlyHint=false although the SDK's omitempty drops a false.
func TestListTools_EveryToolStatesReadOnlyHintOnTheWire(t *testing.T) {
	t.Parallel()
	for _, tool := range wireTools(t) {
		want := "false"
		if toolClasses[toolID(tool.Name)] == "read" {
			want = "true"
		}
		if got := string(tool.Annotations["readOnlyHint"]); got != want {
			t.Errorf("%s: readOnlyHint=%s on the wire, want %s", tool.Name, got, want)
		}
	}
}

func TestListTools_AnnotationsRepeatTheTitle(t *testing.T) {
	t.Parallel()
	for _, tool := range wireTools(t) {
		var title string
		if err := json.Unmarshal(tool.Annotations["title"], &title); err != nil || tool.Title == "" || title != tool.Title {
			t.Errorf("%s: annotations.title %q, title %q", tool.Name, title, tool.Title)
		}
	}
}

// The webhook tools link to the Webhooks section of the API reference next to the method.
func TestListTools_WebhookToolsLinkTheReference(t *testing.T) {
	t.Parallel()
	b := brand.Current()
	if b.APIRefURL == "" {
		t.Skip("the brand has no API reference page")
	}
	byName := map[string]string{}
	for _, tool := range listTools(t, session(t)) {
		byName[tool.Name] = tool.Description
	}
	for id, method := range map[string]string{"webhook.list": "list", "webhook.get": "get", "webhook.set": "set", "webhook.delete": "delete"} {
		want := "(webhook/" + method + ", " + b.APIRefURL + "#webhook)"
		if desc := byName[b.Tool(id)]; !strings.Contains(desc, want) {
			t.Errorf("%s: description lacks %q", id, want)
		}
	}
}
