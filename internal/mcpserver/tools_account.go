package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func init() { registerDomain(registerAccount) }

func registerAccount(r *registry) {
	add(r, toolSpec{
		name:  "account.get",
		title: "Get [[name]] account",
		description: "Who the connection acts for (system/info). Useful to check that the connection works. " +
			"An account key or an OAuth sign-in answers user_id, email and accounting (period_start/period_end, emails_included/emails_sent, " +
			"validations_included/validations_used). A project key answers the owner's user_id and email plus project_id, project_name " +
			"and project_accounting — and cannot use the account-only tools (projects, sandbox domain, confirmed emails).",
		annotations: readOnly(),
	}, func(ctx context.Context, c *caller, _ struct{}) *mcp.CallToolResult {
		return c.post(ctx, "system/info", nil)
	})
}
