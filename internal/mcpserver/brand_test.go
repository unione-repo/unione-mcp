package mcpserver

import (
	"slices"
	"strings"
	"testing"

	"github.com/unione-repo/unione-mcp/internal/brand"
)

// Every text the model reads is written once for every brand: the brand of the
// build gets its own name, tool names and server name, with no marker and no
// tool id left over.
func TestBuild_RendersEveryTextForTheBrand(t *testing.T) {
	t.Parallel()
	b := brand.Current()
	s := sessionWith(t, testConfig())
	tools := listTools(t, s)

	if got := s.InitializeResult().ServerInfo.Name; got != b.ServerName() {
		t.Errorf("serverInfo.name = %q, want %q", got, b.ServerName())
	}
	texts := map[string]string{"instructions": s.InitializeResult().Instructions}
	for _, tool := range tools {
		texts[tool.Name] = tool.Description
		texts[tool.Name+" title"] = tool.Title
		for path, description := range schemaProperties(tool.Name, schemaOf(t, tool)) {
			texts[path] = description
		}
	}
	registered := toolNames(tools)
	for subject, text := range texts {
		assertBrandedText(t, subject, text)
		assertRefsResolve(t, subject, text, registered)
	}
}

// The brand names every tool, each its own way: a missing name would show the
// code's id, a clash would drop a tool.
func TestBuild_ListsEveryToolUnderTheBrandsName(t *testing.T) {
	t.Parallel()
	b := brand.Current()
	for id := range b.ToolNames {
		if _, ok := toolClasses[id]; !ok {
			t.Errorf("ToolNames names unknown tool %s", id)
		}
	}
	var listed, want []string
	for _, tool := range listTools(t, sessionWith(t, testConfig())) {
		listed = append(listed, tool.Name)
	}
	for id := range toolClasses {
		want = append(want, b.Tool(id))
	}
	slices.Sort(listed)
	slices.Sort(want)
	if !slices.Equal(listed, want) {
		t.Errorf("tools = %v, want %v", listed, want)
	}
	if len(slices.Compact(slices.Clone(want))) != len(want) {
		t.Errorf("two tools share a name: %v", want)
	}
}

func assertBrandedText(t *testing.T, subject, text string) {
	t.Helper()
	if strings.Contains(text, "[[") {
		t.Errorf("%s keeps a marker: %s", subject, text)
	}
	for id := range toolClasses {
		if strings.Contains(text, id) {
			t.Errorf("%s names the tool id %s instead of the brand's name: %s", subject, id, text)
		}
	}
}
