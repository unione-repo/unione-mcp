package mcpserver

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unione-repo/unione-mcp/internal/upstream"
)

func text(t string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: t}}}
}

func errText(t string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: t}}}
}

func jsonText(v any) *mcp.CallToolResult {
	payload := encodePlain(v, "  ")
	if payload == "" {
		return errText("Could not encode the result.")
	}
	return text(payload)
}

func toResult(res upstream.Result) *mcp.CallToolResult {
	if res.OK {
		return text(res.Body)
	}
	return errText(res.Err)
}

// embed keeps a composite tool's JSON as JSON instead of a quoted string, and
// keeps one failed part from failing the whole answer.
func embed(res upstream.Result) any {
	if !res.OK {
		return map[string]any{"status": "error", "error": res.Err}
	}
	if json.Valid([]byte(res.Body)) {
		return json.RawMessage(res.Body)
	}
	return res.Body
}

func encodePlain(v any, indent string) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", indent)
	if err := encoder.Encode(v); err != nil {
		return ""
	}
	return strings.TrimSuffix(buf.String(), "\n")
}
