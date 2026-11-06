package arrangeAPI

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wiremock/go-wiremock"

	"github.com/unione-repo/unione-mcp/tests/support/arrange"
)

// Stub is one API method answer, matched by the caller's credential.
type Stub struct {
	header   string
	value    string
	method   string
	status   int64
	body     string
	matchers []string
	delay    time.Duration
	priority int64
}

// NewStub answers `method` (e.g. "email/send") for requests carrying X-API-KEY: key.
func NewStub(key, method string) *Stub {
	return &Stub{
		header: "X-API-KEY",
		value:  key,
		method: method,
		status: http.StatusOK,
		body:   Success(nil),
	}
}

// NewBearerStub matches Authorization: Bearer <token> instead of the API key.
func NewBearerStub(token, method string) *Stub {
	s := NewStub("", method)
	s.header, s.value = "Authorization", "Bearer "+token
	return s
}

// WithAnswer answers {"status":"success", …fields}.
func (s *Stub) WithAnswer(fields map[string]any) *Stub {
	s.body = Success(fields)
	return s
}

// WithError answers {"status":"error","code":…,"message":…} with the HTTP status.
func (s *Stub) WithError(status int, code int, message string) *Stub {
	s.status = int64(status)
	s.body = encode(map[string]any{"status": "error", "code": code, "message": message})
	return s
}

func (s *Stub) WithStatus(status int) *Stub {
	s.status = int64(status)
	return s
}

func (s *Stub) WithBody(body string) *Stub {
	s.body = body
	return s
}

// WithBodyField matches only requests whose JSON body has path == value, the path
// in JSONPath dot form without the leading $ (e.g. "template.id").
func (s *Stub) WithBodyField(path string, value any) *Stub {
	literal, _ := json.Marshal(value)
	s.matchers = append(s.matchers, fmt.Sprintf("$[?(@.%s == %s)]", path, strings.ReplaceAll(string(literal), `"`, "'")))
	return s
}

func (s *Stub) WithDelay(delay time.Duration) *Stub {
	s.delay = delay
	return s
}

func (s *Stub) AtPriority(priority int) *Stub {
	s.priority = int64(priority)
	return s
}

func (s *Stub) Register(t *testing.T) *Stub {
	t.Helper()

	rule := wiremock.NewStubRule(http.MethodPost, wiremock.URLPathEqualTo(arrange.MethodPath(s.method))).
		WithHeader(s.header, wiremock.EqualTo(s.value))
	for _, expression := range s.matchers {
		rule = rule.WithBodyPattern(wiremock.MatchingJsonPath(expression))
	}
	response := wiremock.NewResponse().
		WithStatus(s.status).
		WithHeader("Content-Type", "application/json").
		WithBody(s.body)
	if s.delay > 0 {
		response = response.WithFixedDelay(s.delay)
	}
	rule = rule.WillReturnResponse(response)
	if s.priority > 0 {
		rule = rule.AtPriority(s.priority)
	}

	client := wiremock.NewClient(arrange.WiremockURL())
	if err := client.StubFor(rule); err != nil {
		t.Fatalf("register API stub %s: %v", s.method, err)
	}
	t.Cleanup(func() { _ = client.DeleteStub(rule) })
	return s
}

func Success(fields map[string]any) string {
	doc := map[string]any{"status": "success"}
	maps.Copy(doc, fields)
	return encode(doc)
}

func encode(doc map[string]any) string {
	body, err := json.Marshal(doc)
	if err != nil {
		panic(fmt.Sprintf("API stub body: %v", err))
	}
	return string(body)
}
