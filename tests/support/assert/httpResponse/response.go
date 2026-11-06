package assertHttpResponse

import (
	"encoding/json"
	"strings"
	"testing"

	actMcp "github.com/unione-repo/unione-mcp/tests/support/act/mcp"
	"github.com/unione-repo/unione-mcp/tests/support/assert/jsondoc"
)

func Status(t *testing.T, resp *actMcp.Response, want int) {
	t.Helper()

	if resp.Status != want {
		t.Errorf("%s %s: status %d, want %d", resp.Method, resp.URL, resp.Status, want)
	}
}

func HeaderIs(t *testing.T, resp *actMcp.Response, name, value string) {
	t.Helper()

	if got := resp.Header.Get(name); got != value {
		t.Errorf("%s %s: header %s = %q, want %q", resp.Method, resp.URL, name, got, value)
	}
}

func HeaderContains(t *testing.T, resp *actMcp.Response, name, substring string) {
	t.Helper()

	if got := resp.Header.Get(name); !strings.Contains(got, substring) {
		t.Errorf("%s %s: header %s = %q lacks %q", resp.Method, resp.URL, name, got, substring)
	}
}

func BodyIs(t *testing.T, resp *actMcp.Response, want string) {
	t.Helper()

	if got := string(resp.Body); got != want {
		t.Errorf("%s %s: body %q, want %q", resp.Method, resp.URL, got, want)
	}
}

func BodyLacks(t *testing.T, resp *actMcp.Response, substring string) {
	t.Helper()

	if strings.Contains(string(resp.Body), substring) {
		t.Errorf("%s %s: body must not carry %q, got %s", resp.Method, resp.URL, substring, resp.Body)
	}
}

func JSONIncludes(t *testing.T, resp *actMcp.Response, expected map[string]any) {
	t.Helper()

	var doc map[string]any
	if err := json.Unmarshal(resp.Body, &doc); err != nil {
		t.Fatalf("%s %s: body is not a JSON object (%v): %s", resp.Method, resp.URL, err, resp.Body)
	}
	jsondoc.Includes(t, doc, expected)
}
