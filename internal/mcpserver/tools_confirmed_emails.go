package mcpserver

import (
	"context"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func init() { registerDomain(registerConfirmedEmails) }

type confirmedEmailArg struct {
	Email string `json:"email" jsonschema:"recipient address the user owns, e.g. dev@example.com"`
}

func registerConfirmedEmails(r *registry) {
	add(r, toolSpec{
		name:  "confirmed_email.list",
		title: "List confirmed emails",
		description: "Addresses that may receive mail sent from the sandbox domain (confirmed-email/list): " +
			"confirmed_emails[] with email and status confirmed|unconfirmed. Only confirmed ones receive sandbox mail. " +
			"At most 10 addresses per account. Account keys only.",
		annotations: readOnly(),
	}, func(ctx context.Context, c *caller, _ struct{}) *mcp.CallToolResult {
		return c.post(ctx, "confirmed-email/list", nil)
	})

	add(r, toolSpec{
		name:  "confirmed_email.add",
		title: "Add confirmed email",
		description: "Adds a recipient address for sandbox sending (confirmed-email/create) and SENDS a confirmation email to it; " +
			"the address stays unconfirmed until its owner clicks the link. The link lives 48 hours and works once, and there is " +
			"no resend: if it expired, remove the address with [[tool:confirmed_email.delete]] and add it again. " +
			"At most 10 addresses per account. Add only the user's own addresses. Check progress with [[tool:confirmed_email.list]]. " +
			"Account keys only.",
		annotations: write(),
		openWorld:   true,
		schema:      func(s *jsonschema.Schema) { minLength(s, "email", 3) },
	}, func(ctx context.Context, c *caller, in confirmedEmailArg) *mcp.CallToolResult {
		return c.post(ctx, "confirmed-email/create", map[string]any{"email": strings.TrimSpace(in.Email)})
	})

	add(r, toolSpec{
		name:  "confirmed_email.delete",
		title: "Delete confirmed email",
		description: "Removes an address from the confirmed list (confirmed-email/delete): mail from the sandbox domain can no longer go to it. " +
			"Also the way to get a fresh confirmation link: delete, then [[tool:confirmed_email.add]] again. Account keys only.",
		annotations: dangerous(),
		schema:      func(s *jsonschema.Schema) { minLength(s, "email", 3) },
	}, func(ctx context.Context, c *caller, in confirmedEmailArg) *mcp.CallToolResult {
		return c.post(ctx, "confirmed-email/delete", map[string]any{"email": strings.TrimSpace(in.Email)})
	})
}
