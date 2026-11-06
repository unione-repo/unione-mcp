package mcpserver

import (
	"context"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func init() { registerDomain(registerWebhooks) }

type listWebhooksArgs struct {
	Limit  int64 `json:"limit,omitempty" jsonschema:"page size, 1..100"`
	Offset int64 `json:"offset,omitempty" jsonschema:"number of webhooks to skip (0-based)"`
}

type webhookURLArg struct {
	URL string `json:"url" jsonschema:"the webhook's handler URL — webhooks are identified by it"`
}

type setWebhookArgs struct {
	URL          string   `json:"url" jsonschema:"handler URL that receives the events; its host must resolve"`
	Status       string   `json:"status,omitempty" jsonschema:"active delivers events, disabled pauses the webhook without losing it ([[name]] default: active)"`
	EventFormat  string   `json:"event_format,omitempty" jsonschema:"json_post, or json_post_gzip for a gzip-compressed body ([[name]] default: json_post)"`
	DeliveryInfo *bool    `json:"delivery_info,omitempty" jsonschema:"true adds delivery details (destination response, user agent, ip) to each event"`
	SingleEvent  *bool    `json:"single_event,omitempty" jsonschema:"true posts one event per request instead of batches"`
	MaxParallel  int64    `json:"max_parallel,omitempty" jsonschema:"how many requests [[name]] sends to the handler at once, 5..100 ([[name]] default: 10)"`
	EmailStatus  []string `json:"email_status,omitempty" jsonschema:"email statuses to report; * is every status"`
	SpamBlock    bool     `json:"spam_block,omitempty" jsonschema:"true also reports spam blocks of [[name]]'s sending servers"`
}

var emailStatuses = []string{"*", "accepted", "sent", "delivered", "opened", "clicked", "unsubscribed", "subscribed", "soft_bounced", "hard_bounced", "spam"}

func registerWebhooks(r *registry) {
	add(r, toolSpec{
		name:  "webhook.list",
		title: "List webhooks",
		description: "Webhooks of the account, newest first (webhook/list): objects[] with id, url, status, event_format, " +
			"delivery_info, single_event, max_parallel, updated_at and events {email_status[], spam_block[]}.",
		annotations: readOnly(),
		schema: func(s *jsonschema.Schema) {
			pageLimit(s, 100)
			atLeast(s, "offset", 0)
		},
	}, func(ctx context.Context, c *caller, in listWebhooksArgs) *mcp.CallToolResult {
		body := map[string]any{}
		positive(body, "limit", in.Limit)
		positive(body, "offset", in.Offset)
		return c.post(ctx, "webhook/list", body)
	})

	add(r, toolSpec{
		name:        "webhook.get",
		title:       "Get webhook",
		description: "One webhook with its settings, found by its handler url (webhook/get): {object: {id, url, status, events, …}}.",
		annotations: readOnly(),
		schema:      func(s *jsonschema.Schema) { minLength(s, "url", 1) },
	}, func(ctx context.Context, c *caller, in webhookURLArg) *mcp.CallToolResult {
		return c.post(ctx, "webhook/get", map[string]any{"url": strings.TrimSpace(in.URL)})
	})

	add(r, toolSpec{
		name:  "webhook.set",
		title: "Create or overwrite webhook",
		description: "Creates a webhook for a handler url, or OVERWRITES the one that already has this url (webhook/set). " +
			"An overwrite replaces all its settings with what is passed, [[name]] defaults for the rest — read the current ones with " +
			"[[tool:webhook.get]] and pass them back with the change. Pass email_status and/or spam_block: a webhook needs events. " +
			"To pause a webhook without losing it, set status=disabled instead of deleting it. Answers the saved {object}.",
		annotations: dangerous(),
		openWorld:   true,
		schema: func(s *jsonschema.Schema) {
			minLength(s, "url", 1)
			enum(s, "status", "active", "disabled")
			enum(s, "event_format", "json_post", "json_post_gzip")
			between(s, "max_parallel", 5, 100)
			itemsEnum(s, "email_status", emailStatuses...)
		},
	}, func(ctx context.Context, c *caller, in setWebhookArgs) *mcp.CallToolResult {
		return c.post(ctx, "webhook/set", webhookOf(in))
	})

	add(r, toolSpec{
		name:  "webhook.delete",
		title: "Delete webhook",
		description: "Deletes a webhook by its handler url (webhook/delete): its events stop coming at once and the settings are gone. " +
			"To pause it instead, use [[tool:webhook.set]] with status=disabled.",
		annotations: dangerous(),
		schema:      func(s *jsonschema.Schema) { minLength(s, "url", 1) },
	}, func(ctx context.Context, c *caller, in webhookURLArg) *mcp.CallToolResult {
		return c.post(ctx, "webhook/delete", map[string]any{"url": strings.TrimSpace(in.URL)})
	})
}

func webhookOf(in setWebhookArgs) map[string]any {
	w := map[string]any{"url": strings.TrimSpace(in.URL)}
	nonEmpty(w, "status", in.Status)
	nonEmpty(w, "event_format", in.EventFormat)
	present(w, "delivery_info", flag(in.DeliveryInfo))
	present(w, "single_event", flag(in.SingleEvent))
	positive(w, "max_parallel", in.MaxParallel)
	events := map[string]any{}
	if len(in.EmailStatus) > 0 {
		events["email_status"] = in.EmailStatus
	}
	if in.SpamBlock {
		events["spam_block"] = []string{"*"}
	}
	w["events"] = events
	return w
}
