package mcpserver

import (
	"context"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unione-repo/unione-mcp/internal/upstream"
)

func init() { registerDomain(registerSend) }

type recipient struct {
	Email         string            `json:"email" jsonschema:"recipient address"`
	Substitutions map[string]any    `json:"substitutions,omitempty" jsonschema:"per-recipient values for the {{name}} placeholders in subject and body, e.g. {\"name\": \"Anna\"}"`
	Metadata      map[string]string `json:"metadata,omitempty" jsonschema:"per-recipient key/value pairs echoed back in webhooks and event dumps (at most 10 keys)"`
}

type attachment struct {
	Type    string `json:"type" jsonschema:"MIME type, e.g. application/pdf or image/png"`
	Name    string `json:"name" jsonschema:"file name without slashes; for inline images the name the html refers to as cid:<name>"`
	Content string `json:"content" jsonschema:"file content, base64-encoded"`
}

type sendEmailArgs struct {
	Recipients          []recipient       `json:"recipients" jsonschema:"who gets the email, 1..500 recipients; each gets a separate copy"`
	Subject             string            `json:"subject,omitempty" jsonschema:"subject line; may use {{placeholders}}; required unless the template supplies it"`
	FromEmail           string            `json:"from_email,omitempty" jsonschema:"sender address on a verified domain (see [[tool:domain.list]]) or on the sandbox domain; required unless the template supplies it"`
	FromName            string            `json:"from_name,omitempty" jsonschema:"sender display name"`
	ReplyTo             string            `json:"reply_to,omitempty" jsonschema:"address replies go to"`
	ReplyToName         string            `json:"reply_to_name,omitempty" jsonschema:"display name for reply_to"`
	HTML                string            `json:"html,omitempty" jsonschema:"html body, a full document with a <body> tag"`
	Plaintext           string            `json:"plaintext,omitempty" jsonschema:"plain-text body"`
	AMP                 string            `json:"amp,omitempty" jsonschema:"AMP for Email body; clients without AMP show html"`
	TemplateID          string            `json:"template_id,omitempty" jsonschema:"id of a stored template ([[tool:template.list]]) that fills in subject, sender and body; fields passed here override it"`
	TemplateEngine      string            `json:"template_engine,omitempty" jsonschema:"placeholder syntax: simple ({{name}}), liquid, velocity or none"`
	GlobalSubstitutions map[string]any    `json:"global_substitutions,omitempty" jsonschema:"placeholder values shared by every recipient; per-recipient substitutions win"`
	GlobalMetadata      map[string]string `json:"global_metadata,omitempty" jsonschema:"key/value pairs attached to every recipient's events (at most 10 keys)"`
	Headers             map[string]string `json:"headers,omitempty" jsonschema:"extra MIME headers, e.g. {\"List-Unsubscribe\": \"<mailto:…>\"}; Reply-To is set through reply_to"`
	Tags                []string          `json:"tags,omitempty" jsonschema:"up to 4 tags to group the send in statistics, event dumps and webhooks"`
	TrackLinks          *bool             `json:"track_links,omitempty" jsonschema:"rewrite links to count clicks ([[name]] default: true)"`
	TrackRead           *bool             `json:"track_read,omitempty" jsonschema:"add a pixel to count opens ([[name]] default: true)"`
	GlobalLanguage      string            `json:"global_language,omitempty" jsonschema:"language of the unsubscribe footer and page"`
	SendAt              string            `json:"send_at,omitempty" jsonschema:"delayed send time, UTC 'YYYY-MM-DD hh:mm:ss', at most 24 hours ahead; omit to send now"`
	UnsubscribeURL      string            `json:"unsubscribe_url,omitempty" jsonschema:"own unsubscribe link instead of [[name]]'s (only if the account is allowed to)"`
	IdempotenceKey      string            `json:"idempotence_key,omitempty" jsonschema:"up to 64 characters; a repeat with the same key is refused instead of sent twice — set it whenever a call may be retried"`
	Attachments         []attachment      `json:"attachments,omitempty" jsonschema:"files attached to the email"`
	InlineAttachments   []attachment      `json:"inline_attachments,omitempty" jsonschema:"images embedded into the html via cid:<name>"`
}

var (
	templateEngines = []string{"simple", "liquid", "velocity", "none"}
	languages       = []string{"en", "ru", "ua", "es", "fr", "pl", "de", "pt", "it", "be", "kz"}
)

// sendEmailTool is the one tool MCP_DISABLE_SEND takes away.
const sendEmailTool = "email.send"

func registerSend(r *registry) {
	add(r, toolSpec{
		name:  sendEmailTool,
		title: "Send email",
		description: "REALLY sends an email to real recipients (email/send); it cannot be recalled. Each recipient gets a separate copy; " +
			"put the addresses in recipients, never in headers. The body is html (a full document with a <body> tag) and/or plaintext, " +
			"or template_id of a stored template. from_email must be on a domain with a confirmed verification record and active DKIM " +
			"([[tool:domain.list]]), or on the sandbox domain — which delivers only to addresses confirmed with [[tool:confirmed_email.add]] " +
			"(code 1598 otherwise) and at most 100 emails a day (code 1599). send_at delays the send by up to 24 hours. " +
			"Answers job_id, emails (accepted) and failed_emails {email: reason} for the recipients [[name]] dropped " +
			"(invalid, duplicate, unsubscribed, …); a suppressed address is explained by [[tool:suppression.get]].",
		annotations: dangerous(),
		openWorld:   true,
		schema: func(s *jsonschema.Schema) {
			p := property(s, "recipients")
			p.MinItems, p.MaxItems = ptr(1), ptr(500)
			minLength(p.Items, "email", 3)
			property(s, "tags").MaxItems = ptr(4)
			enum(s, "template_engine", templateEngines...)
			enum(s, "global_language", languages...)
			pattern(s, "send_at", timePattern)
			property(s, "idempotence_key").MaxLength = ptr(64)
		},
	}, func(ctx context.Context, c *caller, in sendEmailArgs) *mcp.CallToolResult {
		return sendResult(c, c.call(ctx, "email/send", map[string]any{"message": messageOf(in)}))
	})
}

// unconfirmedHint is the next step after code 1598: the API names the address but not what the user has to do.
const unconfirmedHint = " The recipient is not confirmed for the sandbox domain yet: the user must click the link in the confirmation " +
	"email within 48 hours; [[tool:confirmed_email.list]] shows the status, and [[tool:confirmed_email.add]] adds an address that is not on the list."

func sendResult(c *caller, res upstream.Result) *mcp.CallToolResult {
	if !res.OK && res.Code == "1598" {
		res.Err += c.cfg.Brand.Render(unconfirmedHint)
	}
	return toResult(res)
}

func messageOf(in sendEmailArgs) map[string]any {
	recipients := make([]any, 0, len(in.Recipients))
	for _, r := range in.Recipients {
		item := map[string]any{"email": strings.TrimSpace(r.Email)}
		if len(r.Substitutions) > 0 {
			item["substitutions"] = r.Substitutions
		}
		if len(r.Metadata) > 0 {
			item["metadata"] = r.Metadata
		}
		recipients = append(recipients, item)
	}
	message := map[string]any{"recipients": recipients}
	nonEmpty(message, "subject", in.Subject)
	nonEmpty(message, "from_email", strings.TrimSpace(in.FromEmail))
	nonEmpty(message, "from_name", in.FromName)
	nonEmpty(message, "reply_to", strings.TrimSpace(in.ReplyTo))
	nonEmpty(message, "reply_to_name", in.ReplyToName)
	if body := bodyOf(in.HTML, in.Plaintext, in.AMP); body != nil {
		message["body"] = body
	}
	nonEmpty(message, "template_id", in.TemplateID)
	nonEmpty(message, "template_engine", in.TemplateEngine)
	if len(in.GlobalSubstitutions) > 0 {
		message["global_substitutions"] = in.GlobalSubstitutions
	}
	if len(in.GlobalMetadata) > 0 {
		message["global_metadata"] = in.GlobalMetadata
	}
	if len(in.Headers) > 0 {
		message["headers"] = in.Headers
	}
	listedStrings(message, "tags", in.Tags)
	present(message, "track_links", flag(in.TrackLinks))
	present(message, "track_read", flag(in.TrackRead))
	nonEmpty(message, "global_language", in.GlobalLanguage)
	nonEmpty(message, "idempotence_key", in.IdempotenceKey)
	if files := attachmentsOf(in.Attachments); files != nil {
		message["attachments"] = files
	}
	if files := attachmentsOf(in.InlineAttachments); files != nil {
		message["inline_attachments"] = files
	}
	options := map[string]any{}
	nonEmpty(options, "send_at", in.SendAt)
	nonEmpty(options, "unsubscribe_url", in.UnsubscribeURL)
	if len(options) > 0 {
		message["options"] = options
	}
	return message
}

func bodyOf(html, plaintext, amp string) map[string]any {
	body := map[string]any{}
	nonEmpty(body, "html", html)
	nonEmpty(body, "plaintext", plaintext)
	nonEmpty(body, "amp", amp)
	if len(body) == 0 {
		return nil
	}
	return body
}

func attachmentsOf(files []attachment) []any {
	if len(files) == 0 {
		return nil
	}
	out := make([]any, 0, len(files))
	for _, f := range files {
		out = append(out, map[string]any{"type": f.Type, "name": f.Name, "content": f.Content})
	}
	return out
}

// The API reads its boolean flags as 0/1.
func flag(v *bool) *int {
	if v == nil {
		return nil
	}
	if *v {
		return ptr(1)
	}
	return ptr(0)
}
