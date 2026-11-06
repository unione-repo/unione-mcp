package mcpserver

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func init() { registerDomain(registerEventDumps) }

type eventDumpFilter struct {
	JobID          string `json:"job_id,omitempty" jsonschema:"only this send, job_id from [[tool:email.send]]"`
	Status         string `json:"status,omitempty" jsonschema:"only events with this status"`
	DeliveryStatus string `json:"delivery_status,omitempty" jsonschema:"comma-separated delivery statuses, e.g. err_mailbox_full,err_user_unknown"`
	Email          string `json:"email,omitempty" jsonschema:"only this recipient"`
	EmailFrom      string `json:"email_from,omitempty" jsonschema:"only this sender address"`
	Domain         string `json:"domain,omitempty" jsonschema:"only recipients on this domain"`
	CampaignID     string `json:"campaign_id,omitempty" jsonschema:"only this campaign_id from the emails' metadata"`
}

type requestEventDumpArgs struct {
	StartTime   string          `json:"start_time" jsonschema:"first moment of the period, UTC 'YYYY-MM-DD hh:mm:ss'; the tariff limits how far back"`
	EndTime     string          `json:"end_time,omitempty" jsonschema:"end of the period (not included), UTC 'YYYY-MM-DD hh:mm:ss'; defaults to now"`
	Limit       int64           `json:"limit,omitempty" jsonschema:"maximum number of events in the dump; [[name]]'s default is 50"`
	AllProjects *bool           `json:"all_projects,omitempty" jsonschema:"true also takes the events of the account's projects (account keys with projects only)"`
	Filter      eventDumpFilter `json:"filter,omitempty" jsonschema:"narrows the events; cannot be combined with aggregate"`
	DumpFields  []string        `json:"dump_fields,omitempty" jsonschema:"columns of the dump; omit for [[name]]'s default set"`
	Aggregate   string          `json:"aggregate,omitempty" jsonschema:"day_status counts events per day and status instead of listing them"`
	Delimiter   string          `json:"delimiter,omitempty" jsonschema:"CSV column delimiter"`
	Format      string          `json:"format,omitempty" jsonschema:"csv, or csv_gzip for compressed files"`
}

type dumpIDArg struct {
	DumpID string `json:"dump_id" jsonschema:"dump_id from [[tool:event_dump.create]]"`
}

var (
	eventStatuses = []string{"accepted", "sent", "delivered", "opened", "clicked", "unsubscribed", "subscribed", "soft_bounced", "hard_bounced", "spam"}
	dumpFields    = []string{"project_id", "event_time", "job_id", "from_email", "email", "subject", "status", "delivery_status", "metadata", "destination_response", "user_agent", "url", "ip", "tags"}
)

func registerEventDumps(r *registry) {
	add(r, toolSpec{
		name:  "event_dump.create",
		title: "Request event dump",
		description: "Queues a CSV export of sending events for a period (event-dump/create) and answers {dump_id} at once; the file is built " +
			"in the background. Poll [[tool:event_dump.get]] with the dump_id until dump_status is ready and hand the user the file URLs. " +
			"The account can hold only a few dumps at a time — delete finished ones with [[tool:event_dump.delete]]. " +
			"Use it for questions like \"what happened to the emails sent yesterday\" or \"who bounced\".",
		annotations: write(),
		schema: func(s *jsonschema.Schema) {
			pattern(s, "start_time", timePattern)
			pattern(s, "end_time", timePattern)
			atLeast(s, "limit", 1)
			enum(property(s, "filter"), "status", eventStatuses...)
			itemsEnum(s, "dump_fields", dumpFields...)
			enum(s, "aggregate", "day_status")
			enum(s, "delimiter", ",", ";")
			enum(s, "format", "csv", "csv_gzip")
		},
	}, func(ctx context.Context, c *caller, in requestEventDumpArgs) *mcp.CallToolResult {
		body := map[string]any{"start_time": in.StartTime}
		nonEmpty(body, "end_time", in.EndTime)
		positive(body, "limit", in.Limit)
		present(body, "all_projects", in.AllProjects)
		filter := map[string]any{}
		nonEmpty(filter, "job_id", in.Filter.JobID)
		nonEmpty(filter, "status", in.Filter.Status)
		nonEmpty(filter, "delivery_status", in.Filter.DeliveryStatus)
		nonEmpty(filter, "email", in.Filter.Email)
		nonEmpty(filter, "email_from", in.Filter.EmailFrom)
		nonEmpty(filter, "domain", in.Filter.Domain)
		nonEmpty(filter, "campaign_id", in.Filter.CampaignID)
		if len(filter) > 0 {
			body["filter"] = filter
		}
		listedStrings(body, "dump_fields", in.DumpFields)
		nonEmpty(body, "aggregate", in.Aggregate)
		nonEmpty(body, "delimiter", in.Delimiter)
		nonEmpty(body, "format", in.Format)
		return c.post(ctx, "event-dump/create", body)
	})

	add(r, toolSpec{
		name:  "event_dump.get",
		title: "Get event dump",
		description: "State of a dump from [[tool:event_dump.create]] (event-dump/get): event_dump {dump_id, dump_status queued|in_process|ready|failed, " +
			"files[] {url, size}}. Files are complete only once dump_status is ready; a big period is split into several files. " +
			"While it is queued or in_process, wait before asking again instead of polling in a tight loop.",
		annotations: readOnly(),
		schema:      func(s *jsonschema.Schema) { pattern(s, "dump_id", uuidPattern) },
	}, func(ctx context.Context, c *caller, in dumpIDArg) *mcp.CallToolResult {
		return c.post(ctx, "event-dump/get", map[string]any{"dump_id": in.DumpID})
	})

	add(r, toolSpec{
		name:  "event_dump.delete",
		title: "Delete event dump",
		description: "Deletes a dump and its files, or takes it off the queue if it is not built yet (event-dump/delete). " +
			"The file URLs stop working.",
		annotations: dangerous(),
		schema:      func(s *jsonschema.Schema) { pattern(s, "dump_id", uuidPattern) },
	}, func(ctx context.Context, c *caller, in dumpIDArg) *mcp.CallToolResult {
		return c.post(ctx, "event-dump/delete", map[string]any{"dump_id": in.DumpID})
	})
}
