package assertTool

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unione-repo/unione-mcp/tests/support/assert/jsondoc"
)

func Succeeded(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()

	text := Text(res)
	if res.IsError {
		t.Fatalf("tool call failed: %s", text)
	}
	return text
}

func SucceededWith(t *testing.T, res *mcp.CallToolResult, substring string) {
	t.Helper()

	if text := Succeeded(t, res); !strings.Contains(text, substring) {
		t.Errorf("tool answer lacks %q: %s", substring, text)
	}
}

func ReturnedText(t *testing.T, res *mcp.CallToolResult, want string) {
	t.Helper()

	if text := Succeeded(t, res); text != want {
		t.Errorf("tool answer = %q, want %q", text, want)
	}
}

func Failed(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()

	text := Text(res)
	if !res.IsError {
		t.Fatalf("tool call must fail, but succeeded: %s", text)
	}
	return text
}

func FailedWith(t *testing.T, res *mcp.CallToolResult, substring string) {
	t.Helper()

	if text := Failed(t, res); !strings.Contains(text, substring) {
		t.Errorf("tool error lacks %q: %s", substring, text)
	}
}

func FailedWithout(t *testing.T, res *mcp.CallToolResult, substring string) {
	t.Helper()

	if text := Failed(t, res); strings.Contains(text, substring) {
		t.Errorf("tool error must not carry %q: %s", substring, text)
	}
}

func JSON(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()

	text := Succeeded(t, res)
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("tool answer is not a JSON object (%v): %s", err, text)
	}
	return doc
}

func JSONIncludes(t *testing.T, res *mcp.CallToolResult, expected map[string]any) {
	t.Helper()

	jsondoc.Includes(t, JSON(t, res), expected)
}

func Text(res *mcp.CallToolResult) string {
	parts := make([]string, 0, len(res.Content))
	for _, content := range res.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}
