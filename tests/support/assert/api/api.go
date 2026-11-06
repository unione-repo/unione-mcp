package assertAPI

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wiremock/go-wiremock"
	"github.com/wiremock/go-wiremock/journal"

	"github.com/unione-repo/unione-mcp/tests/support/arrange"
)

const (
	journalSettle   = 2 * time.Second
	pollInterval    = 25 * time.Millisecond
	maxPollInterval = 250 * time.Millisecond
)

// Request is one call that reached the API stand-in.
type Request struct {
	Method   string
	Query    url.Values
	Headers  http.Header
	Body     map[string]any
	RawBody  string
	LoggedAt time.Time
}

// Called returns the single `method` call made with X-API-KEY: key.
func Called(t *testing.T, key, method string) Request {
	t.Helper()
	return Calls(t, key, method, 1)[0]
}

// Calls returns exactly `want` calls in the order the API received them.
func Calls(t *testing.T, key, method string, want int) []Request {
	t.Helper()

	got := await(t, byHeader("X-API-KEY", key, method), want)
	if len(got) != want {
		t.Fatalf("API: want exactly %d %s call(s) for key %s, got %d", want, method, key, len(got))
	}
	return got
}

// CallsWithKey returns every `method` call made with X-API-KEY: key, at least one.
// The router checks a key with system/info on every request, so that method is
// called once per MCP request, not once per tool call.
func CallsWithKey(t *testing.T, key, method string) []Request {
	t.Helper()

	got := await(t, byHeader("X-API-KEY", key, method), 1)
	if len(got) == 0 {
		t.Fatalf("API: want %s calls for key %s, got none", method, key)
	}
	return got
}

// CalledWithBearer is Called for a caller identified by an Authorization bearer.
func CalledWithBearer(t *testing.T, token, method string) Request {
	t.Helper()

	got := await(t, byHeader("Authorization", "Bearer "+token, method), 1)
	if len(got) != 1 {
		t.Fatalf("API: want exactly 1 %s call for bearer %s, got %d", method, token, len(got))
	}
	return got[0]
}

// CallsWithBearer returns every `method` call made with the bearer, at least one.
func CallsWithBearer(t *testing.T, token, method string) []Request {
	t.Helper()

	got := await(t, byHeader("Authorization", "Bearer "+token, method), 1)
	if len(got) == 0 {
		t.Fatalf("API: want %s calls for bearer %s, got none", method, token)
	}
	return got
}

func NotCalled(t *testing.T, key, method string) {
	t.Helper()

	if got := find(t, byHeader("X-API-KEY", key, method)); len(got) != 0 {
		t.Errorf("API: want no %s calls for key %s, got %d", method, key, len(got))
	}
}

// BodyAt checks the value at a dot path of the JSON body ("message.recipients.0.email").
func BodyAt(t *testing.T, req Request, path string, want any) {
	t.Helper()

	got, ok := lookup(req.Body, path)
	if !ok {
		t.Errorf("API %s: body lacks %q; body: %s", req.Method, path, req.RawBody)
		return
	}
	if !reflect.DeepEqual(got, normalize(t, want)) {
		t.Errorf("API %s: body %q = %#v, want %#v", req.Method, path, got, want)
	}
}

func BodyLacks(t *testing.T, req Request, path string) {
	t.Helper()

	if got, ok := lookup(req.Body, path); ok {
		t.Errorf("API %s: body must not carry %q, got %#v", req.Method, path, got)
	}
}

func HeaderIs(t *testing.T, req Request, name, value string) {
	t.Helper()

	if got := req.Headers.Get(name); got != value {
		t.Errorf("API %s: header %s = %q, want %q", req.Method, name, got, value)
	}
}

func QueryIs(t *testing.T, req Request, name, value string) {
	t.Helper()

	if got := req.Query.Get(name); got != value {
		t.Errorf("API %s: query %s = %q, want %q", req.Method, name, got, value)
	}
}

func HeaderAbsent(t *testing.T, req Request, name string) {
	t.Helper()

	if got, present := req.Headers[http.CanonicalHeaderKey(name)]; present {
		t.Errorf("API %s: header %s must be absent, got %q", req.Method, name, got)
	}
}

func lookup(doc any, path string) (any, bool) {
	current := doc
	for _, part := range strings.Split(path, ".") {
		switch node := current.(type) {
		case map[string]any:
			next, ok := node[part]
			if !ok {
				return nil, false
			}
			current = next
		case []any:
			var index int
			if _, err := fmt.Sscanf(part, "%d", &index); err != nil || index < 0 || index >= len(node) {
				return nil, false
			}
			current = node[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func normalize(t *testing.T, value any) any {
	t.Helper()

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("expected value %v is not JSON-encodable: %v", value, err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("expected value %s does not decode back: %v", raw, err)
	}
	return out
}

func byHeader(header, value, method string) *wiremock.Request {
	return wiremock.NewRequest(http.MethodPost, wiremock.URLPathEqualTo(arrange.MethodPath(method))).
		WithHeader(header, wiremock.EqualTo(value))
}

func await(t *testing.T, criteria *wiremock.Request, want int) []Request {
	t.Helper()

	deadline := time.Now().Add(journalSettle)
	interval := pollInterval
	for {
		got := find(t, criteria)
		if len(got) >= want || time.Now().After(deadline) {
			return got
		}
		<-time.After(interval)
		interval = min(2*interval, maxPollInterval)
	}
}

func find(t *testing.T, criteria *wiremock.Request) []Request {
	t.Helper()

	result, err := wiremock.NewClient(arrange.WiremockURL()).FindRequestsByCriteria(criteria)
	if err != nil {
		t.Fatalf("wiremock journal: %v", err)
	}
	requests := make([]Request, 0, len(result.Requests))
	for _, entry := range result.Requests {
		requests = append(requests, convert(entry))
	}
	slices.SortStableFunc(requests, func(a, b Request) int { return a.LoggedAt.Compare(b.LoggedAt) })
	return requests
}

func convert(entry journal.Request) Request {
	headers := http.Header{}
	for name, value := range entry.Headers {
		headers.Set(name, value)
	}
	path, query, _ := strings.Cut(entry.URL, "?")
	params, _ := url.ParseQuery(query)
	req := Request{
		Method:   strings.TrimSuffix(strings.TrimPrefix(path, arrange.APIPrefix+"/"), ".json"),
		Query:    params,
		Headers:  headers,
		RawBody:  entry.Body,
		LoggedAt: time.UnixMilli(entry.LoggedDate),
	}
	_ = json.Unmarshal([]byte(entry.Body), &req.Body)
	return req
}
