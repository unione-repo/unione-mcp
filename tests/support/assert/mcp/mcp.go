package assertMcp

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func HasTool(t *testing.T, tools []*mcp.Tool, name string) {
	t.Helper()

	for _, tool := range tools {
		if tool.Name == name {
			return
		}
	}
	t.Errorf("tool %q is not among the %d listed tools", name, len(tools))
}

func ServerNamed(t *testing.T, info *mcp.Implementation, name string) {
	t.Helper()

	if info == nil {
		t.Fatal("initialize result carries no serverInfo")
	}
	if info.Name != name {
		t.Errorf("serverInfo.name = %q, want %q", info.Name, name)
	}
}

func InstructionsMention(t *testing.T, instructions, fragment string) {
	t.Helper()

	if !strings.Contains(instructions, fragment) {
		t.Errorf("instructions lack %q:\n%s", fragment, instructions)
	}
}
