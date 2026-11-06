package mcpserver

import (
	"context"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func init() { registerDomain(registerTemplates) }

type listTemplatesArgs struct {
	Limit  int64 `json:"limit,omitempty" jsonschema:"page size, 1..100"`
	Offset int64 `json:"offset,omitempty" jsonschema:"number of templates to skip (0-based)"`
}

type templateIDArg struct {
	ID string `json:"id" jsonschema:"template id from [[tool:template.list]]"`
}

type setTemplateArgs struct {
	ID                  string            `json:"id,omitempty" jsonschema:"id of the template to overwrite; omit to create a new one"`
	Name                string            `json:"name,omitempty" jsonschema:"template name, unique in the account; required for a new template"`
	Subject             string            `json:"subject,omitempty" jsonschema:"subject line; may use {{placeholders}}"`
	FromEmail           string            `json:"from_email" jsonschema:"sender address on a verified domain"`
	FromName            string            `json:"from_name,omitempty" jsonschema:"sender display name"`
	ReplyTo             string            `json:"reply_to,omitempty" jsonschema:"address replies go to"`
	ReplyToName         string            `json:"reply_to_name,omitempty" jsonschema:"display name for reply_to"`
	HTML                string            `json:"html,omitempty" jsonschema:"html body, a full document with a <body> tag"`
	Plaintext           string            `json:"plaintext,omitempty" jsonschema:"plain-text body"`
	AMP                 string            `json:"amp,omitempty" jsonschema:"AMP for Email body"`
	TemplateEngine      string            `json:"template_engine,omitempty" jsonschema:"placeholder syntax: simple ({{name}}), liquid, velocity or none"`
	GlobalSubstitutions map[string]any    `json:"global_substitutions,omitempty" jsonschema:"default placeholder values stored with the template"`
	GlobalMetadata      map[string]string `json:"global_metadata,omitempty" jsonschema:"key/value pairs attached to every email sent from the template"`
	Headers             map[string]string `json:"headers,omitempty" jsonschema:"extra MIME headers stored with the template"`
	UnsubscribeURL      string            `json:"unsubscribe_url,omitempty" jsonschema:"own unsubscribe link instead of [[name]]'s (only if the account is allowed to)"`
}

type deleteTemplateArgs struct {
	ID string `json:"id" jsonschema:"template id from [[tool:template.list]]"`
}

func registerTemplates(r *registry) {
	add(r, toolSpec{
		name:  "template.list",
		title: "List templates",
		description: "Stored email templates, newest first (template/list): templates[] with id, name, subject, from_email, body and more. " +
			"Paged by limit/offset; a page shorter than limit is the last one.",
		annotations: readOnly(),
		schema: func(s *jsonschema.Schema) {
			pageLimit(s, 100)
			atLeast(s, "offset", 0)
		},
	}, func(ctx context.Context, c *caller, in listTemplatesArgs) *mcp.CallToolResult {
		body := map[string]any{}
		positive(body, "limit", in.Limit)
		positive(body, "offset", in.Offset)
		return c.post(ctx, "template/list", body)
	})

	add(r, toolSpec{
		name:        "template.get",
		title:       "Get template",
		description: "One stored template in full (template/get): {template: {id, name, subject, from_email, from_name, body {html, plaintext, amp}, headers, template_engine, global_substitutions, …}}.",
		annotations: readOnly(),
		schema:      func(s *jsonschema.Schema) { minLength(s, "id", 1) },
	}, func(ctx context.Context, c *caller, in templateIDArg) *mcp.CallToolResult {
		return c.post(ctx, "template/get", map[string]any{"id": in.ID})
	})

	add(r, toolSpec{
		name:  "template.set",
		title: "Create or overwrite template",
		description: "Creates a template (no id; name is required) or OVERWRITES an existing one (template/set with id). " +
			"An overwrite replaces the template with exactly what is passed, and every future email sent from the template changes. " +
			"Fields left out are cleared (subject, from_name, headers, body parts); the current values are returned by " +
			"[[tool:template.get]]. Answers the saved {template}.",
		annotations: dangerous(),
		schema: func(s *jsonschema.Schema) {
			minLength(s, "from_email", 3)
			enum(s, "template_engine", templateEngines...)
		},
	}, func(ctx context.Context, c *caller, in setTemplateArgs) *mcp.CallToolResult {
		return c.post(ctx, "template/set", map[string]any{"template": templateOf(in)})
	})

	add(r, toolSpec{
		name:        "template.delete",
		title:       "Delete template",
		description: "Deletes a stored template by id (template/delete). Sends that still pass its template_id start failing; there is no undo.",
		annotations: dangerous(),
		schema:      func(s *jsonschema.Schema) { minLength(s, "id", 1) },
	}, func(ctx context.Context, c *caller, in deleteTemplateArgs) *mcp.CallToolResult {
		return c.post(ctx, "template/delete", map[string]any{"id": in.ID})
	})
}

func templateOf(in setTemplateArgs) map[string]any {
	t := map[string]any{}
	nonEmpty(t, "id", in.ID)
	nonEmpty(t, "name", in.Name)
	nonEmpty(t, "subject", in.Subject)
	nonEmpty(t, "from_email", strings.TrimSpace(in.FromEmail))
	nonEmpty(t, "from_name", in.FromName)
	nonEmpty(t, "reply_to", strings.TrimSpace(in.ReplyTo))
	nonEmpty(t, "reply_to_name", in.ReplyToName)
	nonEmpty(t, "template_engine", in.TemplateEngine)
	if body := bodyOf(in.HTML, in.Plaintext, in.AMP); body != nil {
		t["body"] = body
	}
	if len(in.GlobalSubstitutions) > 0 {
		t["global_substitutions"] = in.GlobalSubstitutions
	}
	if len(in.GlobalMetadata) > 0 {
		t["global_metadata"] = in.GlobalMetadata
	}
	if len(in.Headers) > 0 {
		t["headers"] = in.Headers
	}
	if in.UnsubscribeURL != "" {
		t["options"] = map[string]any{"unsubscribe_url": in.UnsubscribeURL}
	}
	return t
}
