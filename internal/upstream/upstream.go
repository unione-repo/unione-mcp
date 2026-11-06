package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/unione-repo/unione-mcp/internal/auth"
	"github.com/unione-repo/unione-mcp/internal/brand"
)

// Client calls the transactional API: every method is POST
// <BaseURL>/<method>.json with a JSON body, and answers {"status":"success",…}
// or {"status":"error","code":…,"message":…}.
type Client struct {
	// Brand names the product in the messages the model reads.
	Brand     brand.Brand
	BaseURL   string
	UserAgent string
	MaxBody   int64
	HTTP      *http.Client
	Log       *slog.Logger
}

// platformQuery marks every request as MCP traffic: the API already takes the
// calling platform from ?platform in its URLs.
const platformQuery = "?platform=mcp"

// New builds the client; every request carries ?platform=mcp, which is how the API
// tells MCP traffic apart, and User-Agent: <key>-mcp/<version>.
func New(b brand.Brand, baseURL, version string, timeout time.Duration, maxBody int64, log *slog.Logger) *Client {
	return &Client{
		Brand:     b,
		BaseURL:   strings.TrimRight(baseURL, "/"),
		UserAgent: b.ServerName() + "/" + version,
		MaxBody:   maxBody,
		HTTP:      &http.Client{Timeout: timeout},
		Log:       log,
	}
}

type Result struct {
	OK     bool
	Status int
	// Code is the API's own error code from a failed call ("304"), empty otherwise.
	Code string
	Body string
	Err  string
}

func (c *Client) MsgUnreachable() string {
	return c.Brand.Render("The [[name]] API did not answer (network error or timeout). Try again; if it keeps failing, tell the user the service is unavailable right now.")
}

func (c *Client) MsgUnauthorized() string {
	return c.Brand.Render("No [[name]] API key reached this server. Ask the user to put their [[name]] API key into the MCP client config " +
		"(header Authorization: Bearer <key>, or [[api_key_env]] for the local server) and reconnect.")
}

func (c *Client) MsgTooLarge() string {
	return c.Brand.Render("The [[name]] API response was too large to handle. Narrow the request (smaller limit, fewer fields).")
}

func (c *Client) MsgRateLimited() string {
	return c.Brand.Render("[[name]] is rate-limiting this key (HTTP 429). Wait a little before the next call and do not retry in a loop.")
}

func (c *Client) Do(ctx context.Context, credential auth.Credential, method string, body map[string]any) Result {
	if body == nil {
		body = map[string]any{}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		c.Log.Error("upstream body encode failed", "method", method, "err", err)
		return Result{Err: c.MsgUnreachable()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/"+method+".json"+platformQuery, bytes.NewReader(encoded))
	if err != nil {
		c.Log.Error("upstream request build failed", "method", method, "err", err)
		return Result{Err: c.MsgUnreachable()}
	}
	switch credential.Kind {
	case auth.Bearer:
		req.Header.Set("Authorization", "Bearer "+credential.Value)
	default:
		req.Header.Set(auth.HeaderAPIKey, credential.Value)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		c.Log.Warn("upstream call failed", "method", method, "err", transportError(err))
		return Result{Err: c.MsgUnreachable()}
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, c.MaxBody+1))
	if err != nil {
		c.Log.Warn("upstream body read failed", "method", method, "status", resp.StatusCode, "err", err)
		return Result{Status: resp.StatusCode, Err: c.MsgUnreachable()}
	}
	if int64(len(raw)) > c.MaxBody {
		c.Log.Warn("upstream body over limit", "method", method, "status", resp.StatusCode, "limit", c.MaxBody)
		return Result{Status: resp.StatusCode, Err: c.MsgTooLarge()}
	}
	// The API quotes a rejected key back in its error message; the model does not
	// get to see it. Bodies never reach the log at all — answers carry project keys.
	text := redact(string(raw), credential.Value)
	failed := resp.StatusCode < 200 || resp.StatusCode > 299
	if !failed && statusOf(raw) == "error" {
		failed = true
	}
	if failed {
		e, _ := parseAPIError(text)
		c.Log.Warn("upstream rejected the call", "method", method, "status", resp.StatusCode, "code", e.Code.String())
		return Result{Status: resp.StatusCode, Code: e.Code.String(), Body: text, Err: c.explain(resp.StatusCode, text, credential.Kind)}
	}
	return Result{OK: true, Status: resp.StatusCode, Body: text}
}

// TokenCheck is the auth.Verifier check of this server: CheckToken against its own
// resources (RFC 8707) — the MCP endpoint and the origin its metadata names.
func (c *Client) TokenCheck(resources ...string) func(ctx context.Context, token string) auth.Verdict {
	return func(ctx context.Context, token string) auth.Verdict {
		return c.CheckToken(ctx, token, resources)
	}
}

// KeyCheck is the auth.Verifier check of an API key: system/info made with the key,
// 401 means the API does not know it. A project key answers system/info too.
func (c *Client) KeyCheck() func(ctx context.Context, key string) auth.Verdict {
	return func(ctx context.Context, key string) auth.Verdict {
		res := c.Do(ctx, auth.Credential{Kind: auth.APIKey, Value: key}, "system/info", nil)
		switch {
		case res.OK:
			return auth.Accepted
		case res.Status == http.StatusUnauthorized:
			return auth.Rejected
		}
		return auth.Unknown
	}
}

// CheckToken asks the API about an access token with its cheapest method
// (system/info, what account.get calls): 401 means expired or revoked. The answer
// names the token's audience (oauth_audience): a token issued for another resource
// is refused too — MCP authorization wants a server to take only its own tokens.
func (c *Client) CheckToken(ctx context.Context, token string, resources []string) auth.Verdict {
	res := c.Do(ctx, auth.Credential{Kind: auth.Bearer, Value: token}, "system/info", nil)
	switch {
	case res.OK:
		return c.audienceVerdict(res.Body, resources)
	case res.Status == http.StatusUnauthorized:
		return auth.Rejected
	}
	return auth.Unknown
}

func (c *Client) audienceVerdict(body string, resources []string) auth.Verdict {
	var info struct {
		Audience *string `json:"oauth_audience"`
	}
	if err := json.Unmarshal([]byte(body), &info); err != nil || info.Audience == nil {
		// An API that does not name the audience yet: the token is taken as before
		c.Log.Warn("system/info did not name the access token's audience; the audience is not checked")
		return auth.Accepted
	}
	audience := strings.TrimRight(*info.Audience, "/")
	for _, resource := range resources {
		if audience == strings.TrimRight(resource, "/") {
			return auth.Accepted
		}
	}
	c.Log.Info("access token issued for another resource refused", "audience", audience)
	return auth.Rejected
}

// url.Error repeats the full URL; the log keeps the method and the underlying cause.
func transportError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

func redact(text, secret string) string {
	if len(secret) < 8 {
		return text
	}
	return strings.ReplaceAll(text, secret, "***")
}

func statusOf(body []byte) string {
	var envelope struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return ""
	}
	return envelope.Status
}

type apiError struct {
	Code         json.Number       `json:"code"`
	Message      string            `json:"message"`
	FailedEmails map[string]string `json:"failed_emails"`
}

func (e apiError) reason() string {
	parts := make([]string, 0, 2)
	if e.Code != "" {
		parts = append(parts, "code "+e.Code.String())
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	reason := strings.Join(parts, ": ")
	if len(e.FailedEmails) > 0 {
		emails := make([]string, 0, len(e.FailedEmails))
		for _, email := range slices.Sorted(maps.Keys(e.FailedEmails)) {
			emails = append(emails, email+" ("+e.FailedEmails[email]+")")
		}
		reason += "; rejected recipients: " + strings.Join(emails, ", ")
	}
	return truncate(reason, 600)
}

func parseAPIError(body string) (apiError, bool) {
	var e apiError
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&e) != nil || (e.Message == "" && e.Code == "") {
		return apiError{}, false
	}
	return e, true
}

func (c *Client) explain(status int, body string, kind auth.Kind) string {
	reason := ""
	if e, ok := parseAPIError(body); ok {
		reason = e.reason()
	} else if looksLikeHTML(body) {
		return fmt.Sprintf(c.Brand.Render("The [[name]] API answered HTTP %d with an HTML page instead of JSON — [[api_url_env]] is probably wrong; tell the user the server looks misconfigured."), status)
	} else {
		reason = truncate(strings.TrimSpace(body), 300)
	}
	quoted := ""
	if reason != "" {
		quoted = " " + c.Brand.Name + " says: " + reason
		if !strings.HasSuffix(reason, ".") && !strings.HasSuffix(reason, "!") && !strings.HasSuffix(reason, "?") && !strings.HasSuffix(reason, "…") {
			quoted += "."
		}
	}

	switch {
	case status == http.StatusUnauthorized && kind == auth.Bearer:
		return c.Brand.Render("[[name]] rejected the access token (expired or revoked). Ask the user to re-authorize the MCP connection.") + quoted
	case status == http.StatusUnauthorized:
		return c.Brand.Render("[[name]] rejected the API key. Ask the user to check the key in the MCP client config: it must be an account or project "+
			"API key from the [[name]] account settings.") + quoted
	case status == http.StatusForbidden:
		return c.Brand.Render("[[name]] refused this action for the current key (account or tariff limits, permissions, or a project key where an account key is needed).") + quoted
	case status == http.StatusNotFound:
		return c.Brand.Render("[[name]] does not know this API method (HTTP 404): either [[api_url_env]] is wrong or this [[name]] installation does not have the method yet. Tell the user the tool is unavailable here.") + quoted
	case status == http.StatusRequestEntityTooLarge:
		return c.Brand.Render("[[name]] refused the request as too large (the API accepts up to 10 MB). Send fewer or smaller attachments.") + quoted
	case status == http.StatusTooManyRequests:
		return c.MsgRateLimited() + quoted
	case status >= 500:
		return c.Brand.Render("The [[name]] API failed on its side. Try again a bit later; if it repeats, tell the user to contact [[name]] support with the Error ID.") + quoted
	}
	if reason == "" {
		return fmt.Sprintf("%s rejected the request (HTTP %d) without a reason.", c.Brand.Name, status)
	}
	return fmt.Sprintf("%s rejected the request (HTTP %d): %s", c.Brand.Name, status, reason)
}

func looksLikeHTML(body string) bool {
	head := strings.ToLower(strings.TrimSpace(truncate(body, 256)))
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html")
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}
