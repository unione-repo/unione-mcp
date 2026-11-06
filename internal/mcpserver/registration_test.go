package mcpserver

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unione-repo/unione-mcp/internal/brand"
	"github.com/unione-repo/unione-mcp/internal/config"
)

// The task's tool table: name → class. Renaming, dropping or reclassifying a tool
// breaks saved client configs and the confirmation prompts clients show.
var toolClasses = map[string]string{
	"account.get":            "read",
	"domain.list":            "read",
	"domain.dns_records":     "write",
	"domain.validate":        "read",
	"domain.delete":          "dangerous",
	"domain.sandbox":         "write",
	"confirmed_email.list":   "read",
	"confirmed_email.add":    "write",
	"confirmed_email.delete": "dangerous",
	"email.send":             "dangerous",
	"template.list":          "read",
	"template.get":           "read",
	"template.set":           "dangerous",
	"template.delete":        "dangerous",
	"webhook.list":           "read",
	"webhook.get":            "read",
	"webhook.set":            "dangerous",
	"webhook.delete":         "dangerous",
	"suppression.get":        "read",
	"suppression.list":       "read",
	"suppression.set":        "dangerous",
	"suppression.delete":     "dangerous",
	"email.validate":         "write",
	"event_dump.create":      "write",
	"event_dump.get":         "read",
	"event_dump.delete":      "dangerous",
	"tag.list":               "read",
	"tag.delete":             "dangerous",
	"project.list":           "read",
	"project.create":         "write",
	"project.update":         "dangerous",
	"project.delete":         "dangerous",
}

// The tools whose call reaches beyond the account (openWorldHint=true): a letter to an
// address, a request to a handler URL, a probe of a mailbox. The rest stay inside it.
var openWorldTools = []string{"email.send", "confirmed_email.add", "webhook.set", "email.validate"}

// toolID is the code's id of the tool the brand lists under name.
func toolID(name string) string {
	for id := range toolClasses {
		if brand.Current().Tool(id) == name {
			return id
		}
	}
	return ""
}

// namesOfClass lists the brand's names of the tools of the classes.
func namesOfClass(classes ...string) []string {
	var names []string
	for id, class := range toolClasses {
		if slices.Contains(classes, class) {
			names = append(names, brand.Current().Tool(id))
		}
	}
	slices.Sort(names)
	return names
}

func listedNames(t *testing.T, s *mcp.ClientSession) []string {
	t.Helper()
	var names []string
	for _, tool := range listTools(t, s) {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

func TestBuild_ListsExactlyTheToolsOfTheTable(t *testing.T) {
	t.Parallel()

	got, want := listedNames(t, session(t)), namesOfClass("read", "write", "dangerous")

	if !slices.Equal(got, want) {
		t.Fatalf("tools\n got %v\nwant %v", got, want)
	}
}

func TestBuild_AnnotationsFollowTheClassOfEachTool(t *testing.T) {
	t.Parallel()
	for _, tool := range listTools(t, session(t)) {
		class := toolClasses[toolID(tool.Name)]
		a := tool.Annotations
		if a == nil || a.DestructiveHint == nil {
			t.Errorf("%s: DestructiveHint unset, the protocol default is true", tool.Name)
			continue
		}
		switch class {
		case "read":
			if !a.ReadOnlyHint {
				t.Errorf("%s: read tool without readOnlyHint", tool.Name)
			}
		case "write":
			if a.ReadOnlyHint || *a.DestructiveHint {
				t.Errorf("%s: write tool wants readOnlyHint=false destructiveHint=false, got %v/%v", tool.Name, a.ReadOnlyHint, *a.DestructiveHint)
			}
		case "dangerous":
			if a.ReadOnlyHint || !*a.DestructiveHint {
				t.Errorf("%s: dangerous tool wants readOnlyHint=false destructiveHint=true, got %v/%v", tool.Name, a.ReadOnlyHint, *a.DestructiveHint)
			}
		default:
			t.Errorf("%s: not in the tool table", tool.Name)
		}
	}
}

// MCP takes an unset openWorldHint as true, so every tool states it.
func TestBuild_EveryToolStatesOpenWorldHint(t *testing.T) {
	t.Parallel()
	for _, tool := range listTools(t, session(t)) {
		a := tool.Annotations
		if a == nil || a.OpenWorldHint == nil {
			t.Errorf("%s: OpenWorldHint unset, the protocol default is true", tool.Name)
			continue
		}
		if want := slices.Contains(openWorldTools, toolID(tool.Name)); *a.OpenWorldHint != want {
			t.Errorf("%s: openWorldHint=%v, want %v", tool.Name, *a.OpenWorldHint, want)
		}
	}
}

// Confirmation is the client's job, driven by the annotations; the server has no
// argument of its own for it.
func TestBuild_NoToolTakesAConfirmArgument(t *testing.T) {
	t.Parallel()
	for _, tool := range listTools(t, session(t)) {
		if _, ok := schemaOf(t, tool).Properties["confirm"]; ok {
			t.Errorf("%s: takes confirm", tool.Name)
		}
	}
}

func TestBuild_ReadOnlyModeListsOnlyReadTools(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.ReadOnly = true

	got, want := listedNames(t, sessionWith(t, cfg)), namesOfClass("read")

	if !slices.Equal(got, want) {
		t.Fatalf("read-only tools\n got %v\nwant %v", got, want)
	}
}

func TestBuild_SendSwitchDropsOnlySendEmail(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.DisableSend = true

	got := listedNames(t, sessionWith(t, cfg))

	want := slices.DeleteFunc(namesOfClass("read", "write", "dangerous"), func(n string) bool { return n == brand.Current().Tool(sendEmailTool) })
	if !slices.Equal(got, want) {
		t.Fatalf("tools\n got %v\nwant %v", got, want)
	}
}

func TestBuild_StdioModeListsTheSameTools(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.Transport, cfg.APIKey = config.TransportStdio, "key"

	if got, want := listedNames(t, sessionWith(t, cfg)), listedNames(t, session(t)); !slices.Equal(got, want) {
		t.Fatalf("stdio tools\n got %v\nwant %v", got, want)
	}
}

func TestBuild_ToolTextsAreFilledAndCrossReferencesResolve(t *testing.T) {
	t.Parallel()
	tools := listTools(t, session(t))
	registered := toolNames(tools)

	refs := 0
	for _, tool := range tools {
		if tool.Title == "" || tool.Description == "" {
			t.Errorf("%s: empty title or description", tool.Name)
		}
		refs += assertRefsResolve(t, tool.Name, tool.Description, registered)
	}
	if refs == 0 {
		t.Fatal("no cross-references found: the check matched nothing")
	}
}

// The facts the task requires in tool descriptions, or the agent gets them wrong.
func TestBuild_DescriptionsCarryTheRequiredFacts(t *testing.T) {
	t.Parallel()
	required := map[string][]string{
		"domain.list":            {"sandbox domain is never listed"},
		"domain.dns_records":     {"sandbox domain", "error"},
		"domain.validate":        {"sandbox domain", "error"},
		"domain.sandbox":         {"confirmed", "100 emails a day"},
		"email.send":             {"sandbox", "100 emails a day", "24 hours", "billed", "emails_sent"},
		"confirmed_email.add":    {"48 hours", "works once", "no resend", "confirmed_email.delete", "At most 10"},
		"confirmed_email.list":   {"At most 10"},
		"suppression.delete":     {"is_deletable=true", "3004", "906"},
		"domain.delete":          {"project's API key"},
		"project.create":         {"only once, in this response", "limits every call"},
		"project.update":         {"project_id (preferred)"},
		"project.list":           {"until they are enabled"},
		"webhook.delete":         {"status=disabled"},
		"email.validate":         {"billed", "validations_used"},
		"event_dump.create":      {"event_dump.get"},
		"confirmed_email.delete": {"confirmed_email.add"},
	}
	byName := map[string]string{}
	for _, tool := range listTools(t, session(t)) {
		byName[tool.Name] = tool.Description
	}
	b := brand.Current()
	for id, facts := range required {
		for _, fact := range facts {
			if _, isTool := toolClasses[fact]; isTool {
				fact = b.Tool(fact)
			}
			if !strings.Contains(byName[b.Tool(id)], fact) {
				t.Errorf("%s description lacks %q", id, fact)
			}
		}
	}
}

func TestBuild_EverySchemaPropertyIsDescribed(t *testing.T) {
	t.Parallel()
	properties := 0
	for _, tool := range listTools(t, session(t)) {
		for path, description := range schemaProperties(tool.Name, schemaOf(t, tool)) {
			properties++
			if description == "" {
				t.Errorf("%s: no description", path)
			}
		}
	}
	if properties == 0 {
		t.Fatal("no schema properties found: the check matched nothing")
	}
}

func TestBuild_InstructionsDescribeTheSandboxPath(t *testing.T) {
	t.Parallel()
	s := session(t)

	instructions := s.InitializeResult().Instructions

	if assertRefsResolve(t, "instructions", instructions, toolNames(listTools(t, s))) == 0 {
		t.Error("instructions reference no tools: the check matched nothing")
	}
	path := []string{"domain.sandbox", "confirmed_email.add", "email.send"}
	last := -1
	for _, step := range path {
		at := strings.Index(instructions, brand.Current().Tool(step))
		if at <= last {
			t.Fatalf("instructions do not walk %v in order", path)
		}
		last = at
	}
}

func TestBuild_ServerInfoNamesTheServer(t *testing.T) {
	t.Parallel()

	if got := session(t).InitializeResult().ServerInfo.Name; got != serverName {
		t.Errorf("serverInfo.name = %q, want %q", got, serverName)
	}
}

func TestCallTool_WithoutCredentialNeverReachesTheAPI(t *testing.T) {
	t.Parallel()
	s := session(t)

	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: brand.Current().Tool("account.get")})

	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError || !strings.Contains(resultText(t, res), "Authorization: Bearer") {
		t.Fatalf("want an error saying where the key goes, got %s", resultText(t, res))
	}
}

func TestCallTool_RejectsInvalidArgumentsBeforeTheAPI(t *testing.T) {
	t.Parallel()
	cases := map[string]map[string]any{
		"unknown argument":  {"id": "t-1", "bogus": true},
		"missing required":  {},
		"wrong type":        {"id": 42},
		"empty required id": {"id": ""},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeAPI(t)

			res := callVia(t, fake, testConfig(), "template.get", args)

			if !res.IsError {
				t.Fatalf("want IsError, got %s", resultText(t, res))
			}
			if n := len(fake.recorded()); n != 0 {
				t.Errorf("the API was called %d times", n)
			}
		})
	}
}
