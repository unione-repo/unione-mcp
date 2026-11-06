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
			"Each call is billed against the account's validations (validations_used in [[tool:account.get]]).",
		annotations: write(),
		openWorld:   true,
		schema:      func(s *jsonschema.Schema) { minLength(s, "email", 3) },
	}, func(ctx context.Context, c *caller, in validateEmailArgs) *mcp.CallToolResult {
		return c.post(ctx, "email-validation/single", map[string]any{"email": strings.TrimSpace(in.Email)})
	})
}
