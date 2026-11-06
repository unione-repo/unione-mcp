package mcpserver

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unione-repo/unione-mcp/internal/brand"
)

type registry struct {
	server *mcp.Server
	deps   *deps
}

type toolSpec struct {
	name        string
	title       string
	description string
	annotations mcp.ToolAnnotations
	schema      func(*jsonschema.Schema)
	// openWorld marks a tool whose call reaches beyond the account: it mails an
	// address, calls a handler URL or probes a mailbox. Every tool states it, since
	// MCP takes an unset openWorldHint as true.
	openWorld bool
}

type handler[In any] func(context.Context, *caller, In) *mcp.CallToolResult

// add registers one prefixed tool whose arguments the SDK validates against the
// schema inferred from In (required = fields without omitempty, description = the
// jsonschema tag) before the handler runs. Its texts are rendered for the brand.
func add[In any](r *registry, spec toolSpec, h handler[In]) {
	if !r.offers(spec) {
		return
	}
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("tool %s: input schema: %v", spec.name, err))
	}
	if spec.schema != nil {
		spec.schema(schema)
	}
	b := r.deps.cfg.Brand
	renderDescriptions(schema, b)
	title := b.Render(spec.title)
	annotations := spec.annotations
	annotations.Title = title
	annotations.OpenWorldHint = ptr(spec.openWorld)
	mcp.AddTool(r.server, &mcp.Tool{
		Name:        b.Tool(spec.name),
		Title:       title,
		Description: b.Render(spec.description),
		Annotations: &annotations,
		InputSchema: schema,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		return h(ctx, r.deps.callerFor(req), in), nil, nil
	})
}

func renderDescriptions(s *jsonschema.Schema, b brand.Brand) {
	if s == nil {
		return
	}
	s.Description = b.Render(s.Description)
	for _, p := range s.Properties {
		renderDescriptions(p, b)
	}
	renderDescriptions(s.Items, b)
	renderDescriptions(s.AdditionalProperties, b)
}

// offers applies the server's switches: read-only mode keeps only the read tools,
// and the send switch drops email.send. A tool left out is not listed at all.
func (r *registry) offers(spec toolSpec) bool {
	cfg := r.deps.cfg
	if cfg.ReadOnly && !spec.annotations.ReadOnlyHint {
		return false
	}
	return !cfg.DisableSend || spec.name != sendEmailTool
}

// The three classes of the task map onto MCP annotations; clients ask the user
// before a destructive call, the server itself never does. readOnlyHint=false of
// the write classes reaches the wire through explicitReadOnlyHint.
func readOnly() mcp.ToolAnnotations {
	return mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: ptr(false)}
}

func write() mcp.ToolAnnotations {
	return mcp.ToolAnnotations{DestructiveHint: ptr(false)}
}

func dangerous() mcp.ToolAnnotations {
	return mcp.ToolAnnotations{DestructiveHint: ptr(true)}
}

func ptr[T any](v T) *T { return &v }
