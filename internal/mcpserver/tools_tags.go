package mcpserver

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func init() { registerDomain(registerTags) }

type deleteTagArgs struct {
	TagID int64 `json:"tag_id" jsonschema:"tag_id from [[tool:tag.list]]"`
}

func registerTags(r *registry) {
	add(r, toolSpec{
		name:  "tag.list",
		title: "List tags",
		description: "Tags the account has used in sends (tag/list): tags[] with tag_id and tag. Tags are created by passing them " +
			"to [[tool:email.send]]; an account holds at most 10000.",
		annotations: readOnly(),
	}, func(ctx context.Context, c *caller, _ struct{}) *mcp.CallToolResult {
		return c.post(ctx, "tag/list", nil)
	})

	add(r, toolSpec{
		name:        "tag.delete",
		title:       "Delete tag",
		description: "Deletes a tag by tag_id from [[tool:tag.list]] (tag/delete), freeing a slot of the 10000-tag limit.",
		annotations: dangerous(),
		schema:      func(s *jsonschema.Schema) { atLeast(s, "tag_id", 1) },
	}, func(ctx context.Context, c *caller, in deleteTagArgs) *mcp.CallToolResult {
		return c.post(ctx, "tag/delete", map[string]any{"tag_id": in.TagID})
	})
}
