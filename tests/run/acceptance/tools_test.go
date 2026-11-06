//go:build integration

package acceptance

import (
	"net/http"
	"testing"

	actMcp "github.com/unione-repo/unione-mcp/tests/support/act/mcp"
	"github.com/unione-repo/unione-mcp/tests/support/arrange"
	arrangeAPI "github.com/unione-repo/unione-mcp/tests/support/arrange/api"
	arrangeServer "github.com/unione-repo/unione-mcp/tests/support/arrange/server"
	assertAPI "github.com/unione-repo/unione-mcp/tests/support/assert/api"
	assertTool "github.com/unione-repo/unione-mcp/tests/support/assert/tool"
)

// The path without the cabinet: sandbox domain, confirmed address, send to it.
func TestE2E_Tools_SandboxPath(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})
	key := arrange.APIKey(t)
	inbox := arrange.UniqueEmail(t)
	arrangeAPI.NewStub(key, "domain/get-sandbox-domain").WithAnswer(map[string]any{"domain": "sandbox-1-a.example.test"}).Register(t)
	arrangeAPI.NewStub(key, "confirmed-email/create").WithAnswer(map[string]any{
		"confirmed_email": map[string]any{"email": inbox, "status": "unconfirmed"},
	}).Register(t)
	arrangeAPI.NewStub(key, "email/send").WithAnswer(map[string]any{"job_id": "job-1", "emails": []string{inbox}}).Register(t)
	session := actMcp.Connect(t, server, key)

	sandbox := session.CallTool(t, server.Tool("domain.sandbox"), nil)
	added := session.CallTool(t, server.Tool("confirmed_email.add"), map[string]any{"email": inbox})
	sent := session.CallTool(t, server.Tool("email.send"), map[string]any{
		"recipients": []any{map[string]any{"email": inbox}},
		"from_email": "hello@sandbox-1-a.example.test",
		"subject":    "First letter",
		"plaintext":  "It works",
	})

	assertTool.JSONIncludes(t, sandbox, map[string]any{"domain": "sandbox-1-a.example.test"})
	assertTool.SucceededWith(t, added, "unconfirmed")
	assertTool.JSONIncludes(t, sent, map[string]any{"job_id": "job-1"})
	assertAPI.BodyAt(t, assertAPI.Called(t, key, "confirmed-email/create"), "email", inbox)
	req := assertAPI.Called(t, key, "email/send")
	assertAPI.BodyAt(t, req, "message.recipients.0.email", inbox)
	assertAPI.BodyAt(t, req, "message.from_email", "hello@sandbox-1-a.example.test")
	assertAPI.BodyAt(t, req, "message.body.plaintext", "It works")
}

func TestE2E_Tools_SendRefusalIsTheAPIs(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})
	key := arrange.APIKey(t)
	to := arrange.UniqueEmail(t)
	arrangeAPI.NewStub(key, "email/send").
		WithBody(`{"status":"error","code":204,"message":"No valid recipients","failed_emails":{"` + to + `":"unsubscribed"}}`).
		WithStatus(http.StatusBadRequest).
		Register(t)

	res := actMcp.Connect(t, server, key).CallTool(t, server.Tool("email.send"), map[string]any{
		"recipients": []any{map[string]any{"email": to}}, "from_email": "a@example.test", "subject": "S", "plaintext": "x",
	})

	assertTool.FailedWith(t, res, "code 204")
	assertTool.FailedWith(t, res, to+" (unsubscribed)")
}

func TestE2E_Tools_ValidateDomainRunsBothChecks(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})
	key := arrange.APIKey(t)
	arrangeAPI.NewStub(key, "domain/validate-verification-record").WithAnswer(map[string]any{"message": "Record updated"}).Register(t)
	arrangeAPI.NewStub(key, "domain/validate-dkim").WithError(http.StatusBadRequest, 122, "DKIM record not found").Register(t)

	res := actMcp.Connect(t, server, key).CallTool(t, server.Tool("domain.validate"), map[string]any{"domain": "example.test"})

	assertTool.SucceededWith(t, res, "Record updated")
	assertTool.SucceededWith(t, res, "DKIM record not found")
	assertAPI.Called(t, key, "domain/validate-verification-record")
	assertAPI.Called(t, key, "domain/validate-dkim")
}

func TestE2E_Tools_DangerousToolsRunWithoutAServerSideConfirmation(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})
	key := arrange.APIKey(t)
	arrangeAPI.NewStub(key, "domain/delete").WithAnswer(map[string]any{"message": "Domain deleted"}).Register(t)
	arrangeAPI.NewStub(key, "tag/delete").Register(t)
	session := actMcp.Connect(t, server, key)

	domain := session.CallTool(t, server.Tool("domain.delete"), map[string]any{"domain": "example.test"})
	tag := session.CallTool(t, server.Tool("tag.delete"), map[string]any{"tag_id": 54})

	assertTool.SucceededWith(t, domain, "Domain deleted")
	assertTool.Succeeded(t, tag)
	assertAPI.BodyAt(t, assertAPI.Called(t, key, "tag/delete"), "tag_id", 54)
}

func TestE2E_Tools_ProjectKeysComeBackWhole(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})
	key := arrange.APIKey(t)
	projectKey := arrange.UniqueID(t, "project-key")
	arrangeAPI.NewStub(key, "project/create").WithAnswer(map[string]any{"project_id": "457", "project_api_key": projectKey}).Register(t)
	arrangeAPI.NewStub(key, "project/list").WithAnswer(map[string]any{
		"projects": []any{map[string]any{"id": "457", "name": "Shop", "api_key": projectKey}},
	}).Register(t)
	session := actMcp.Connect(t, server, key)

	created := session.CallTool(t, server.Tool("project.create"), map[string]any{"name": "Shop", "country": "es"})
	listed := session.CallTool(t, server.Tool("project.list"), nil)

	assertTool.JSONIncludes(t, created, map[string]any{"project_api_key": projectKey})
	assertTool.SucceededWith(t, listed, projectKey)
	req := assertAPI.Called(t, key, "project/create")
	assertAPI.BodyAt(t, req, "project.name", "Shop")
	assertAPI.BodyAt(t, req, "project.country", "ES")
}

func TestE2E_Tools_ProjectsOffPointToSupport(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})
	key := arrange.APIKey(t)
	arrangeAPI.NewStub(key, "project/list").WithError(http.StatusForbidden, 105, "No rights to use projects").Register(t)

	res := actMcp.Connect(t, server, key).CallTool(t, server.Tool("project.list"), nil)

	assertTool.FailedWith(t, res, "No rights to use projects")
	assertTool.FailedWith(t, res, "support")
}

func TestE2E_Tools_EventDumpRequestAndPoll(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})
	key := arrange.APIKey(t)
	const dumpID = "0b8f8c5e-6a44-4b5e-9d7e-1e2f3a4b5c6d"
	arrangeAPI.NewStub(key, "event-dump/create").WithAnswer(map[string]any{"dump_id": dumpID}).Register(t)
	arrangeAPI.NewStub(key, "event-dump/get").WithAnswer(map[string]any{"event_dump": map[string]any{
		"dump_id": dumpID, "dump_status": "ready", "files": []any{map[string]any{"url": "https://files.example.test/1.csv", "size": 10}},
	}}).Register(t)
	session := actMcp.Connect(t, server, key)

	created := session.CallTool(t, server.Tool("event_dump.create"), map[string]any{
		"start_time": "2026-09-27 00:00:00", "filter": map[string]any{"job_id": "job-1"}, "format": "csv_gzip",
	})
	polled := session.CallTool(t, server.Tool("event_dump.get"), map[string]any{"dump_id": dumpID})

	assertTool.JSONIncludes(t, created, map[string]any{"dump_id": dumpID})
	assertTool.SucceededWith(t, polled, "https://files.example.test/1.csv")
	req := assertAPI.Called(t, key, "event-dump/create")
	assertAPI.BodyAt(t, req, "filter.job_id", "job-1")
	assertAPI.BodyAt(t, req, "format", "csv_gzip")
}

func TestE2E_Tools_ListSuppressionsPassesFiltersAndCursor(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{})
	key := arrange.APIKey(t)
	arrangeAPI.NewStub(key, "suppression/list").WithAnswer(map[string]any{"suppressions": []any{}, "cursor": nil}).Register(t)

	res := actMcp.Connect(t, server, key).CallTool(t, server.Tool("suppression.list"), map[string]any{
		"cause": "complained", "cursor": "Ajfb6", "limit": 100,
	})

	assertTool.Succeeded(t, res)
	req := assertAPI.Called(t, key, "suppression/list")
	assertAPI.BodyAt(t, req, "cause", "complained")
	assertAPI.BodyAt(t, req, "cursor", "Ajfb6")
	assertAPI.BodyAt(t, req, "limit", 100)
}
