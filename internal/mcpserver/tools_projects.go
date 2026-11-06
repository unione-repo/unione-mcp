package mcpserver

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unione-repo/unione-mcp/internal/upstream"
)

func init() { registerDomain(registerProjects) }

type listProjectsArgs struct {
	ProjectID     string `json:"project_id,omitempty" jsonschema:"only this project"`
	ProjectAPIKey string `json:"project_api_key,omitempty" jsonschema:"only the project with this API key"`
}

type projectFields struct {
	Country                     string `json:"country,omitempty" jsonschema:"ISO 3166-1 alpha-2 country code of the project's business, e.g. ES"`
	SendEnabled                 *bool  `json:"send_enabled,omitempty" jsonschema:"false switches the project's sending off ([[name]] default: true)"`
	CustomUnsubscribeURLEnabled *bool  `json:"custom_unsubscribe_url_enabled,omitempty" jsonschema:"true lets the project's sends use their own unsubscribe_url (only if the account is allowed to)"`
	BackendDomainID             int64  `json:"backend_domain_id,omitempty" jsonschema:"backend domain for the project's tracking links"`
	UnsubscribePageID           int64  `json:"unsubscribe_page_id,omitempty" jsonschema:"unsubscribe page template for the project"`
}

type createProjectArgs struct {
	Name string `json:"name" jsonschema:"project name, unique in the account, up to 255 characters"`
	projectFields
}

type updateProjectArgs struct {
	ProjectID     string `json:"project_id,omitempty" jsonschema:"project id from [[tool:project.list]] — preferred way to pick the project"`
	ProjectAPIKey string `json:"project_api_key,omitempty" jsonschema:"the project's API key, only when the id is unknown"`
	Name          string `json:"name" jsonschema:"project name; required on every update — pass the current one to keep it"`
	projectFields
	EmailCounterLimit *int64 `json:"email_counter_limit,omitempty" jsonschema:"most emails the project may send per counter period; 0 removes the limit"`
	EmailCounterMode  string `json:"email_counter_mode,omitempty" jsonschema:"default resets the counter every billing period, permanent never resets it"`
	ResetEmailCounter bool   `json:"reset_email_counter,omitempty" jsonschema:"true sets the project's sent-emails counter back to 0"`
}

type deleteProjectArgs struct {
	ProjectID     string `json:"project_id,omitempty" jsonschema:"project id from [[tool:project.list]] — preferred way to pick the project"`
	ProjectAPIKey string `json:"project_api_key,omitempty" jsonschema:"the project's API key, only when the id is unknown"`
}

const (
	projectIDPattern = `^[0-9]+$`
	projectsOnly     = "Projects need an account key with projects enabled; until they are enabled for the account these tools answer an error."
	projectsHint     = " If the account does not have projects enabled yet, tell the user to ask [[name]] support to enable them."
)

func registerProjects(r *registry) {
	add(r, toolSpec{
		name:  "project.list",
		title: "List projects",
		description: "Active projects of the account (project/list): projects[] with id, api_key, name, country, send_enabled, reg_time, " +
			"backend_domain_id, unsubscribe_page_id, email_counter, email_counter_limit and email_counter_mode. " +
			projectsOnly,
		annotations: readOnly(),
		schema:      func(s *jsonschema.Schema) { pattern(s, "project_id", projectIDPattern) },
	}, func(ctx context.Context, c *caller, in listProjectsArgs) *mcp.CallToolResult {
		body := map[string]any{}
		nonEmpty(body, "project_id", in.ProjectID)
		nonEmpty(body, "project_api_key", in.ProjectAPIKey)
		return projectResult(c, c.call(ctx, "project/list", body))
	})

	add(r, toolSpec{
		name:  "project.create",
		title: "Create project",
		description: "Creates a project — a separate sender inside the account with its own API key, templates, webhooks and statistics " +
			"(project/create). Answers project_id and project_api_key: the key comes back only in this answer, show it to the user " +
			"right away so they can put it into their integration. A project key limits every call made with it to that project's data. " +
			projectsOnly,
		annotations: write(),
		schema: func(s *jsonschema.Schema) {
			minLength(s, "name", 1)
			property(s, "name").MaxLength = ptr(255)
			pattern(s, "country", `^[A-Za-z]{2}$`)
			atLeast(s, "backend_domain_id", 1)
			atLeast(s, "unsubscribe_page_id", 1)
		},
	}, func(ctx context.Context, c *caller, in createProjectArgs) *mcp.CallToolResult {
		project := map[string]any{"name": strings.TrimSpace(in.Name)}
		fillProject(project, in.projectFields)
		return projectResult(c, c.call(ctx, "project/create", map[string]any{"project": project}))
	})

	add(r, toolSpec{
		name:  "project.update",
		title: "Update project",
		description: "OVERWRITES a project's settings (project/update). Pick the project by project_id (preferred) or project_api_key, " +
			"one of them. name is required every time, and backend_domain_id and unsubscribe_page_id are reset when left out — " +
			"read the project with [[tool:project.list]] and pass them back. send_enabled=false stops the project's sending at once; " +
			"reset_email_counter sets its counter back to 0. " + projectsOnly,
		annotations: dangerous(),
		schema: func(s *jsonschema.Schema) {
			pattern(s, "project_id", projectIDPattern)
			minLength(s, "name", 1)
			property(s, "name").MaxLength = ptr(255)
			pattern(s, "country", `^([A-Za-z]{2})?$`)
			atLeast(s, "backend_domain_id", 1)
			atLeast(s, "unsubscribe_page_id", 1)
			atLeast(s, "email_counter_limit", 0)
			enum(s, "email_counter_mode", "default", "permanent")
		},
	}, func(ctx context.Context, c *caller, in updateProjectArgs) *mcp.CallToolResult {
		project := map[string]any{"name": strings.TrimSpace(in.Name)}
		fillProject(project, in.projectFields)
		present(project, "email_counter_limit", in.EmailCounterLimit)
		nonEmpty(project, "email_counter_mode", in.EmailCounterMode)
		if in.ResetEmailCounter {
			project["email_counter"] = 0
		}
		body := map[string]any{"project": project}
		nonEmpty(body, "project_id", in.ProjectID)
		nonEmpty(body, "project_api_key", in.ProjectAPIKey)
		return projectResult(c, c.call(ctx, "project/update", body))
	})

	add(r, toolSpec{
		name:  "project.delete",
		title: "Delete project",
		description: "Deletes a project (project/delete): its API key stops working at once, so every integration using it stops sending. " +
			"The project goes to the trash and only [[name]] support can restore it. Pick it by project_id (preferred) or project_api_key. " +
			projectsOnly,
		annotations: dangerous(),
		schema:      func(s *jsonschema.Schema) { pattern(s, "project_id", projectIDPattern) },
	}, func(ctx context.Context, c *caller, in deleteProjectArgs) *mcp.CallToolResult {
		body := map[string]any{}
		nonEmpty(body, "project_id", in.ProjectID)
		nonEmpty(body, "project_api_key", in.ProjectAPIKey)
		return projectResult(c, c.call(ctx, "project/delete", body))
	})
}

func fillProject(project map[string]any, f projectFields) {
	nonEmpty(project, "country", strings.ToUpper(f.Country))
	present(project, "send_enabled", f.SendEnabled)
	present(project, "custom_unsubscribe_url_enabled", f.CustomUnsubscribeURLEnabled)
	positive(project, "backend_domain_id", f.BackendDomainID)
	positive(project, "unsubscribe_page_id", f.UnsubscribePageID)
}

// The API refuses project methods with 403 while projects are off for the account.
func projectResult(c *caller, res upstream.Result) *mcp.CallToolResult {
	if !res.OK && res.Status == http.StatusForbidden {
		res.Err += c.cfg.Brand.Render(projectsHint)
	}
	return toResult(res)
}
