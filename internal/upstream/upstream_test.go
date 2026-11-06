package upstream_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/unione-repo/unione-mcp/internal/auth"
	"github.com/unione-repo/unione-mcp/internal/brand"
	"github.com/unione-repo/unione-mcp/internal/upstream"
)

const (
	testVersion   = "1.2.3"
	clientTimeout = 5 * time.Second
	clientMaxBody = 1 << 20
)

func apiKey(t *testing.T) auth.Credential {
	return auth.Credential{Kind: auth.APIKey, Value: "key-" + strings.ReplaceAll(t.Name(), "/", "-")}
}

func newClient(t *testing.T, baseURL string, logs io.Writer) *upstream.Client {
	t.Helper()
	if logs == nil {
		logs = t.Output()
	}
	return upstream.New(brand.Current(), baseURL, testVersion, clientTimeout, clientMaxBody, slog.New(slog.NewTextHandler(logs, nil)))
}

func newClientAgainst(t *testing.T, handler http.Handler) *upstream.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return newClient(t, srv.URL+"/en/transactional/api/v1", nil)
}

func respond(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
}

type recorded struct {
	Method, Path, Query, APIKey, Authorization, ContentType, UserAgent string
	Body                                                               map[string]any
}

func recordInto(t *testing.T, seen chan<- recorded) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("request body %q is not a JSON object: %v", raw, err)
		}
		seen <- recorded{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, APIKey: r.Header.Get(auth.HeaderAPIKey),
			Authorization: r.Header.Get("Authorization"), ContentType: r.Header.Get("Content-Type"),
			UserAgent: r.Header.Get("User-Agent"), Body: body,
		}
		_, _ = io.WriteString(w, `{"status":"success"}`)
	})
}

func TestDo_PostsJSONToTheMethodPathWithTheAPIKey(t *testing.T) {
	t.Parallel()
	seen := make(chan recorded, 1)
	client := newClientAgainst(t, recordInto(t, seen))
	credential := apiKey(t)

	res := client.Do(context.Background(), credential, "email/send", map[string]any{"message": map[string]any{"subject": "S"}})

	if !res.OK {
		t.Fatalf("call failed: %+v", res)
	}
	got := <-seen
	want := recorded{
		Method: http.MethodPost, Path: "/en/transactional/api/v1/email/send.json", Query: "platform=mcp", APIKey: credential.Value,
		ContentType: "application/json", UserAgent: brand.Current().ServerName() + "/" + testVersion,
		Body: map[string]any{"message": map[string]any{"subject": "S"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request\n got %+v\nwant %+v", got, want)
	}
}

func TestDo_NilBodyIsSentAsAnEmptyObject(t *testing.T) {
	t.Parallel()
	seen := make(chan recorded, 1)
	client := newClientAgainst(t, recordInto(t, seen))

	client.Do(context.Background(), apiKey(t), "system/info", nil)

	if got := (<-seen).Body; !reflect.DeepEqual(got, map[string]any{}) {
		t.Fatalf("body = %#v, want {}", got)
	}
}

func TestDo_BearerCredentialTravelsAsAuthorization(t *testing.T) {
	t.Parallel()
	seen := make(chan recorded, 1)
	client := newClientAgainst(t, recordInto(t, seen))

	client.Do(context.Background(), auth.Credential{Kind: auth.Bearer, Value: "tok-1"}, "system/info", nil)

	got := <-seen
	if got.Authorization != "Bearer tok-1" || got.APIKey != "" {
		t.Fatalf("Authorization %q, X-API-KEY %q; want the bearer only", got.Authorization, got.APIKey)
	}
}

func TestDo_SuccessBodyPassesThrough(t *testing.T) {
	t.Parallel()
	body := `{"status":"success","job_id":"j-1"}`
	client := newClientAgainst(t, respond(http.StatusOK, body))

	res := client.Do(context.Background(), apiKey(t), "email/send", nil)

	if !res.OK || res.Body != body {
		t.Fatalf("got %+v, want OK with the body verbatim", res)
	}
}

func TestDo_ErrorsAreExplainedWithTheAPIsReason(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		status   int
		body     string
		wantCode string
		want     []string
	}{
		"bad request quotes code and message": {
			http.StatusBadRequest, `{"status":"error","code":203,"message":"Error ID:AB. Subject is missing"}`, "203",
			[]string{"HTTP 400", "code 203", "Subject is missing"},
		},
		"rejected key": {
			http.StatusUnauthorized, `{"status":"error","code":102,"message":"Api key mismatch"}`, "102",
			[]string{"API key", "account settings", "code 102"},
		},
		"forbidden keeps the business reason": {
			http.StatusForbidden, `{"status":"error","code":902,"message":"Sending limit for free tariff is reached"}`, "902",
			[]string{"refused", "Sending limit for free tariff is reached"},
		},
		"rate limit": {
			http.StatusTooManyRequests, `{"status":"error","code":0,"message":"Too many requests"}`, "0",
			[]string{"rate-limiting"},
		},
		"server failure": {
			http.StatusInternalServerError, `{"status":"error","code":150,"message":"Error ID:XY. Unknown error"}`, "150",
			[]string{"failed on its side", "Error ID:XY"},
		},
		"unknown method": {http.StatusNotFound, `{}`, "", []string{brand.Current().APIURLEnv()}},
		"html page":      {http.StatusBadGateway, `<!DOCTYPE html><html>oops</html>`, "", []string{"HTML page"}},
		"rejected recipients are listed": {
			http.StatusBadRequest, `{"status":"error","code":204,"message":"No valid recipients","failed_emails":{"b@x":"invalid","a@x":"unsubscribed"}}`, "204",
			[]string{"a@x (unsubscribed), b@x (invalid)"},
		},
		"error envelope on HTTP 200": {
			http.StatusOK, `{"status":"error","code":304,"message":"Webhook not found"}`, "304",
			[]string{"Webhook not found"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := newClientAgainst(t, respond(tc.status, tc.body))

			res := client.Do(context.Background(), apiKey(t), "email/send", nil)

			if res.OK {
				t.Fatalf("want a failure, got %+v", res)
			}
			if res.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", res.Code, tc.wantCode)
			}
			for _, fragment := range tc.want {
				if !strings.Contains(res.Err, fragment) {
					t.Errorf("Err %q lacks %q", res.Err, fragment)
				}
			}
		})
	}
}

func TestDo_ReasonEndingInPeriodIsNotDoubled(t *testing.T) {
	t.Parallel()
	client := newClientAgainst(t, respond(http.StatusForbidden, `{"status":"error","code":105,"message":"Access Denied."}`))

	res := client.Do(context.Background(), apiKey(t), "email/send", nil)

	if strings.Contains(res.Err, "..") || !strings.HasSuffix(res.Err, "Access Denied.") {
		t.Fatalf("Err %q, want it to end with a single period after the reason", res.Err)
	}
}

func TestDo_ExpiredBearerAsksToReauthorize(t *testing.T) {
	t.Parallel()
	client := newClientAgainst(t, respond(http.StatusUnauthorized, `{"status":"error","code":102,"message":"expired"}`))

	res := client.Do(context.Background(), auth.Credential{Kind: auth.Bearer, Value: "tok-1"}, "system/info", nil)

	if !strings.Contains(res.Err, "re-authorize") {
		t.Fatalf("Err %q does not ask to re-authorize", res.Err)
	}
}

func TestDo_RejectedKeyIsRedactedFromAnswerAndLog(t *testing.T) {
	t.Parallel()
	credential := apiKey(t)
	var logs strings.Builder
	srv := httptest.NewServer(respond(http.StatusUnauthorized,
		`{"status":"error","code":102,"message":"Invalid key '`+credential.Value+`'"}`))
	t.Cleanup(srv.Close)
	client := newClient(t, srv.URL, &logs)

	res := client.Do(context.Background(), credential, "system/info", nil)

	for where, text := range map[string]string{"Err": res.Err, "Body": res.Body, "log": logs.String()} {
		if strings.Contains(text, credential.Value) {
			t.Errorf("%s leaks the key: %s", where, text)
		}
	}
	if !strings.Contains(res.Err, "Invalid key '***'") {
		t.Errorf("Err %q lost the redacted reason", res.Err)
	}
}

func TestDo_UnreachableAPI(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()

	client := newClient(t, srv.URL, nil)
	res := client.Do(context.Background(), apiKey(t), "system/info", nil)

	if res.OK || res.Err != client.MsgUnreachable() {
		t.Fatalf("got %+v, want %q", res, client.MsgUnreachable())
	}
}

func TestDo_TimeoutIsReportedAsUnreachable(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(srv.Close)
	// Cleanups run last-in first-out: the handler is let go before Close waits for it.
	t.Cleanup(func() { close(release) })
	client := upstream.New(brand.Current(), srv.URL, testVersion, 100*time.Millisecond, clientMaxBody, slog.New(slog.DiscardHandler))

	res := client.Do(context.Background(), apiKey(t), "system/info", nil)

	if res.Err != client.MsgUnreachable() {
		t.Fatalf("Err = %q, want %q", res.Err, client.MsgUnreachable())
	}
}

func TestDo_OversizedAnswerIsRefused(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(respond(http.StatusOK, `{"status":"success","x":"`+strings.Repeat("a", 64)+`"}`))
	t.Cleanup(srv.Close)
	client := upstream.New(brand.Current(), srv.URL, testVersion, clientTimeout, 32, slog.New(slog.DiscardHandler))

	res := client.Do(context.Background(), apiKey(t), "template/list", nil)

	if res.Err != client.MsgTooLarge() {
		t.Fatalf("Err = %q, want %q", res.Err, client.MsgTooLarge())
	}
}

func TestDo_ResponseBodiesNeverReachTheLog(t *testing.T) {
	t.Parallel()
	const secret = "project-key-that-must-not-be-logged"
	for name, status := range map[string]int{"success": http.StatusOK, "failure": http.StatusBadRequest} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var logs strings.Builder
			body := `{"status":"success","project_api_key":"` + secret + `"}`
			if status != http.StatusOK {
				body = `{"status":"error","code":1,"message":"` + secret + `"}`
			}
			srv := httptest.NewServer(respond(status, body))
			t.Cleanup(srv.Close)

			newClient(t, srv.URL, &logs).Do(context.Background(), apiKey(t), "project/create", nil)

			if strings.Contains(logs.String(), secret) {
				t.Fatalf("the log carries the response body: %s", logs.String())
			}
		})
	}
}

// Every message the model reads names the brand the server fronts.
func TestDo_MessagesNameTheBrand(t *testing.T) {
	t.Parallel()
	statuses := []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusRequestEntityTooLarge, http.StatusTooManyRequests, http.StatusInternalServerError}
	b := brand.Current()
	for _, status := range statuses {
		srv := httptest.NewServer(respond(status, `{"status":"error","code":1,"message":"nope"}`))
		t.Cleanup(srv.Close)
		client := upstream.New(b, srv.URL, testVersion, clientTimeout, clientMaxBody, slog.New(slog.DiscardHandler))

		res := client.Do(context.Background(), apiKey(t), "system/info", nil)

		if !strings.Contains(res.Err, b.Name) || strings.Contains(res.Err, "[[") {
			t.Errorf("%s, HTTP %d: %q", b.Key, status, res.Err)
		}
	}
	client := upstream.New(b, "http://127.0.0.1:1", testVersion, clientTimeout, clientMaxBody, slog.New(slog.DiscardHandler))
	for _, msg := range []string{client.MsgUnreachable(), client.MsgUnauthorized(), client.MsgTooLarge(), client.MsgRateLimited()} {
		if !strings.Contains(msg, b.Name) || strings.Contains(msg, "[[") {
			t.Errorf("%s: %q", b.Key, msg)
		}
	}
}

// The server takes only tokens issued for it: system/info names the token's audience.
func TestCheckToken_ComparesTheAudienceWithTheServersResources(t *testing.T) {
	t.Parallel()
	resources := []string{"https://mcp.example.test/mcp", "https://mcp.example.test"}
	cases := map[string]struct {
		status int
		body   string
		want   auth.Verdict
	}{
		"issued for the endpoint":       {http.StatusOK, `{"status":"success","user_id":1,"oauth_audience":"https://mcp.example.test/mcp"}`, auth.Accepted},
		"issued for the origin":         {http.StatusOK, `{"status":"success","user_id":1,"oauth_audience":"https://mcp.example.test/"}`, auth.Accepted},
		"issued for the API host":       {http.StatusOK, `{"status":"success","user_id":1,"oauth_audience":"https://api.example.test"}`, auth.Rejected},
		"issued for another MCP path":   {http.StatusOK, `{"status":"success","user_id":1,"oauth_audience":"https://mcp.example.test/other"}`, auth.Rejected},
		"API without the audience yet":  {http.StatusOK, `{"status":"success","user_id":1}`, auth.Accepted},
		"expired or revoked":            {http.StatusUnauthorized, `{"status":"error","code":102,"message":"Access token is revoked"}`, auth.Rejected},
		"API unavailable: request goes": {http.StatusBadGateway, `bad gateway`, auth.Unknown},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := newClientAgainst(t, respond(tc.status, tc.body))

			got := c.TokenCheck(resources...)(context.Background(), "access-token")

			if got != tc.want {
				t.Fatalf("verdict %v, want %v", got, tc.want)
			}
		})
	}
}

func TestKeyCheck_TellsAnUnknownKeyFromAnUnavailableAPI(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		status int
		body   string
		want   auth.Verdict
	}{
		"account key":                   {http.StatusOK, `{"status":"success","user_id":1}`, auth.Accepted},
		"project key":                   {http.StatusOK, `{"status":"success","user_id":1,"project_id":"p"}`, auth.Accepted},
		"unknown key":                   {http.StatusUnauthorized, `{"status":"error","code":114,"message":"user not found"}`, auth.Rejected},
		"API unavailable: request goes": {http.StatusBadGateway, `bad gateway`, auth.Unknown},
		"rate limited: request goes":    {http.StatusTooManyRequests, `{"status":"error","code":429,"message":"slow down"}`, auth.Unknown},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := newClientAgainst(t, respond(tc.status, tc.body))

			got := c.KeyCheck()(context.Background(), "api-key")

			if got != tc.want {
				t.Fatalf("verdict %v, want %v", got, tc.want)
			}
		})
	}
}
