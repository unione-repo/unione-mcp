package mcpserver

import (
	"context"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func init() { registerDomain(registerDomains) }

type listDomainsArgs struct {
	Domain string `json:"domain,omitempty" jsonschema:"only this domain, e.g. example.com"`
	Limit  int64  `json:"limit,omitempty" jsonschema:"page size, 1..100"`
	Offset int64  `json:"offset,omitempty" jsonschema:"number of domains to skip (0-based)"`
}

type domainArg struct {
	Domain string `json:"domain" jsonschema:"sender domain, e.g. example.com — the part after @ in from_email"`
}

// Every domain tool but domain.sandbox works on own domains only.
const sandboxNotHere = "The sandbox domain is not handled here: it never appears in [[tool:domain.list]], and " +
	"[[tool:domain.dns_records]] / [[tool:domain.validate]] answer an error for it — it needs no DNS setup."

func registerDomains(r *registry) {
	add(r, toolSpec{
		name:  "domain.list",
		title: "List sender domains",
		description: "Sender domains of the account with their verification state (domain/list): domains[] carries domain, " +
			"verification-record {value, status confirmed|unconfirmed} and dkim {key, status active|inactive}. " +
			"A domain can send once its verification record is confirmed and DKIM is active. " +
			"The sandbox domain is never listed here; [[tool:domain.sandbox]] returns it.",
		annotations: readOnly(),
		schema: func(s *jsonschema.Schema) {
			pageLimit(s, 100)
			atLeast(s, "offset", 0)
		},
	}, func(ctx context.Context, c *caller, in listDomainsArgs) *mcp.CallToolResult {
		body := map[string]any{}
		nonEmpty(body, "domain", strings.TrimSpace(in.Domain))
		positive(body, "limit", in.Limit)
		positive(body, "offset", in.Offset)
		return c.post(ctx, "domain/list", body)
	})

	add(r, toolSpec{
		name:  "domain.dns_records",
		title: "Add domain / get its DNS records",
		description: "DNS records the user must publish for a sender domain (domain/get-dns-records). Adds the domain to the account " +
			"if it is not there yet; repeating the call returns the same records. Answers verification-record " +
			"(a TXT record for the domain itself), dkim (the public key for the TXT record at us._domainkey.<domain>) and dmarc. " +
			"Published records are checked by [[tool:domain.validate]]. " +
			"Free-mail domains (gmail.com and the like) are refused. " + sandboxNotHere,
		annotations: write(),
		schema:      func(s *jsonschema.Schema) { minLength(s, "domain", 1) },
	}, func(ctx context.Context, c *caller, in domainArg) *mcp.CallToolResult {
		return c.post(ctx, "domain/get-dns-records", map[string]any{"domain": strings.TrimSpace(in.Domain)})
	})

	add(r, toolSpec{
		name:  "domain.validate",
		title: "Check domain DNS records",
		description: "Checks that the user published the records from [[tool:domain.dns_records]]: calls " +
			"domain/validate-verification-record and domain/validate-dkim and answers both results " +
			"({domain, verification_record, dkim}), each [[name]]'s own answer or its error. " +
			"A failed check right after publishing is normal; DNS changes can take up to a day. " +
			"A domain that is not added yet is refused; [[tool:domain.dns_records]] adds it. " + sandboxNotHere,
		annotations: readOnly(),
		schema:      func(s *jsonschema.Schema) { minLength(s, "domain", 1) },
	}, func(ctx context.Context, c *caller, in domainArg) *mcp.CallToolResult {
		body := map[string]any{"domain": strings.TrimSpace(in.Domain)}
		record := c.call(ctx, "domain/validate-verification-record", body)
		dkim := c.call(ctx, "domain/validate-dkim", body)
		res := jsonText(map[string]any{
			"domain":              body["domain"],
			"verification_record": embed(record),
			"dkim":                embed(dkim),
		})
		res.IsError = !record.OK && !dkim.OK
		return res
	})

	add(r, toolSpec{
		name:  "domain.delete",
		title: "Delete sender domain",
		description: "Deletes a sender domain (domain/delete). Sending from any address on it stops working at once; " +
			"adding it back means publishing and checking DNS records again. A project's domain can be deleted only with " +
			"that project's API key; the sandbox domain cannot be deleted.",
		annotations: dangerous(),
		schema:      func(s *jsonschema.Schema) { minLength(s, "domain", 1) },
	}, func(ctx context.Context, c *caller, in domainArg) *mcp.CallToolResult {
		return c.post(ctx, "domain/delete", map[string]any{"domain": strings.TrimSpace(in.Domain)})
	})

	add(r, toolSpec{
		name:  "domain.sandbox",
		title: "Get sandbox domain",
		description: "The account's sandbox domain (domain/get-sandbox-domain), created on the first call; repeated calls return the same {domain}. " +
			"It lets the user send before verifying a domain of their own, from any address on it — but only to addresses " +
			"confirmed with [[tool:confirmed_email.add]], and at most 100 emails a day (code 1599 once the day's limit is used up). " +
			"It needs no DNS setup and is not shown by [[tool:domain.list]]. Account keys only, not project keys.",
		annotations: write(),
	}, func(ctx context.Context, c *caller, _ struct{}) *mcp.CallToolResult {
		return c.post(ctx, "domain/get-sandbox-domain", nil)
	})
}
