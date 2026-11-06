package mcpserver

import (
	"context"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func init() { registerDomain(registerSuppressions) }

type getSuppressionArgs struct {
	Email       string `json:"email" jsonschema:"address to look up"`
	AllProjects *bool  `json:"all_projects,omitempty" jsonschema:"true also looks through the account's projects (account keys with projects only)"`
}

type listSuppressionsArgs struct {
	Cause     string `json:"cause,omitempty" jsonschema:"only this cause"`
	Source    string `json:"source,omitempty" jsonschema:"only blocks from this source: user (added via API or cabinet), system (bounces, complaints), subscriber (unsubscribed themselves)"`
	StartTime string `json:"start_time,omitempty" jsonschema:"only blocks created since this UTC time, 'YYYY-MM-DD hh:mm:ss'"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"cursor from the previous page's answer; omit for the first page"`
	Limit     int64  `json:"limit,omitempty" jsonschema:"page size, 1..1000"`
}

type setSuppressionArgs struct {
	Email   string `json:"email" jsonschema:"address to block"`
	Cause   string `json:"cause" jsonschema:"why the address is blocked"`
	Created string `json:"created,omitempty" jsonschema:"when the block started, UTC 'YYYY-MM-DD hh:mm:ss'; defaults to now"`
}

type deleteSuppressionArgs struct {
	Email string `json:"email" jsonschema:"address to unblock"`
}

var (
	suppressionCauses    = []string{"unsubscribed", "complained", "temporary_unavailable", "permanent_unavailable", "blocked"}
	settableCauses       = suppressionCauses[:4]
	suppressionSourceSet = []string{"user", "system", "subscriber"}
)

func registerSuppressions(r *registry) {
	add(r, toolSpec{
		name:  "suppression.get",
		title: "Get suppression of an address",
		description: "Why and since when [[name]] does not send to an address (suppression/get): {email, suppressions[]} with cause, " +
			"source, created and is_deletable per block; an empty suppressions[] means the address is not blocked. " +
			"Explains why a recipient lands in failed_emails of [[tool:email.send]] or never gets mail.",
		annotations: readOnly(),
		schema:      func(s *jsonschema.Schema) { minLength(s, "email", 3) },
	}, func(ctx context.Context, c *caller, in getSuppressionArgs) *mcp.CallToolResult {
		body := map[string]any{"email": strings.TrimSpace(in.Email)}
		present(body, "all_projects", in.AllProjects)
		return c.post(ctx, "suppression/get", body)
	})

	add(r, toolSpec{
		name:  "suppression.list",
		title: "List suppressions",
		description: "Blocked addresses of the account (suppression/list): suppressions[] with email, cause, source, created and " +
			"is_deletable, plus cursor, which leads to the next page; a null cursor means this was the last page. " +
			"cause, source and start_time narrow the list.",
		annotations: readOnly(),
		schema: func(s *jsonschema.Schema) {
			enum(s, "cause", suppressionCauses...)
			enum(s, "source", suppressionSourceSet...)
			pattern(s, "start_time", timePattern)
			pageLimit(s, 1000)
		},
	}, func(ctx context.Context, c *caller, in listSuppressionsArgs) *mcp.CallToolResult {
		body := map[string]any{}
		nonEmpty(body, "cause", in.Cause)
		nonEmpty(body, "source", in.Source)
		nonEmpty(body, "start_time", in.StartTime)
		nonEmpty(body, "cursor", in.Cursor)
		positive(body, "limit", in.Limit)
		return c.post(ctx, "suppression/list", body)
	})

	add(r, toolSpec{
		name:  "suppression.set",
		title: "Block an address",
		description: "Stops every send to an address (suppression/set) until the block is removed with [[tool:suppression.delete]]. " +
			"cause says why: unsubscribed, complained, temporary_unavailable or permanent_unavailable.",
		annotations: dangerous(),
		schema: func(s *jsonschema.Schema) {
			minLength(s, "email", 3)
			enum(s, "cause", settableCauses...)
			pattern(s, "created", timePattern)
		},
	}, func(ctx context.Context, c *caller, in setSuppressionArgs) *mcp.CallToolResult {
		body := map[string]any{"email": strings.TrimSpace(in.Email), "cause": in.Cause}
		nonEmpty(body, "created", in.Created)
		return c.post(ctx, "suppression/set", body)
	})

	add(r, toolSpec{
		name:  "suppression.delete",
		title: "Unblock an address",
		description: "Removes the blocks on an address so sends reach it again (suppression/delete). Only blocks with is_deletable=true " +
			"(see [[tool:suppression.get]]) can go — any other answers error 3004. Removals have a daily limit per account; " +
			"over it the answer is error 906 until the next day. Unblocking an address that unsubscribed or complained means mailing " +
			"a person who asked not to be mailed.",
		annotations: dangerous(),
		schema:      func(s *jsonschema.Schema) { minLength(s, "email", 3) },
	}, func(ctx context.Context, c *caller, in deleteSuppressionArgs) *mcp.CallToolResult {
		return c.post(ctx, "suppression/delete", map[string]any{"email": strings.TrimSpace(in.Email)})
	})
}
