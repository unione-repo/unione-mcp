package mcpserver

import (
	"context"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func init() { registerDomain(registerValidation) }

type validateEmailArgs struct {
	Email string `json:"email" jsonschema:"address to check"`
}

func registerValidation(r *registry) {
	add(r, toolSpec{
		name:  "email.validate",
		title: "Validate an email address (billed)",
		description: "Checks whether an address can receive mail (email-validation/single): result valid|invalid|suspicious|unknown, " +
			"cause (no_mx_record, syntax_error, possible_typo, mailbox_not_found, disposable, role, …), did_you_mean, mx_found. " +
			"Every call uses up the account's paid validations (validations_used in [[tool:account.get]]) — check only the addresses " +
			"the user asked about, never whole lists on your own.",
		annotations: write(),
		openWorld:   true,
		schema:      func(s *jsonschema.Schema) { minLength(s, "email", 3) },
	}, func(ctx context.Context, c *caller, in validateEmailArgs) *mcp.CallToolResult {
		return c.post(ctx, "email-validation/single", map[string]any{"email": strings.TrimSpace(in.Email)})
	})
}
