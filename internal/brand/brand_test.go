package brand_test

import (
	"testing"

	"github.com/unione-repo/unione-mcp/internal/brand"
)

func TestRender_FillsEveryMarker(t *testing.T) {
	t.Parallel()
	b := brand.Brand{
		Key: "acme", Name: "Acme", DocsURL: "https://docs.acme.test", EnvPrefix: "ACME",
		APIRefURL: "https://docs.acme.test/api",
		ToolNames: map[string]string{"email.send": "acme_send"},
	}
	cases := map[string]string{
		"[[name]] API":                     "Acme API",
		"call [[tool:email.send]] first":   "call acme_send first",
		"a method[[docs_ref]] with":        "a method (https://docs.acme.test) with",
		"(webhook/get[[api_ref:webhook]])": "(webhook/get, https://docs.acme.test/api#webhook)",
		"set [[api_key_env]]":              "set ACME_API_KEY",
		"check [[api_url_env]]":            "check ACME_API_URL",
		"the API's {{name}} placeholder":   "the API's {{name}} placeholder",
	}
	for text, want := range cases {
		if got := b.Render(text); got != want {
			t.Errorf("Render(%q) = %q, want %q", text, got, want)
		}
	}
}

// The code's ids are nobody's names: a tool the brand does not name must never
// reach the model under its id.
func TestTool_RequiresTheBrandsName(t *testing.T) {
	t.Parallel()
	b := brand.Brand{Key: "acme", ToolNames: map[string]string{"account.get": "acme_account"}}
	if got := b.Tool("account.get"); got != "acme_account" {
		t.Errorf("named tool = %q", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("Tool returned a name for a tool the brand does not name")
		}
	}()
	b.Tool("email.send")
}

func TestRender_DocsRefVanishesWithoutDocs(t *testing.T) {
	t.Parallel()
	if got := (brand.Brand{Name: "Acme"}).Render("a method[[docs_ref]] with"); got != "a method with" {
		t.Fatalf("got %q", got)
	}
}

func TestRender_APIRefVanishesWithoutTheReference(t *testing.T) {
	t.Parallel()
	if got := (brand.Brand{Name: "Acme"}).Render("(webhook/get[[api_ref:webhook]])"); got != "(webhook/get)" {
		t.Fatalf("got %q", got)
	}
}

func TestRender_UnknownMarkerPanics(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"[[nam]]", "[[tool]]", "[[name:x]]"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Render(%q) did not panic", text)
				}
			}()
			brand.Current().Render(text)
		}()
	}
}

// The brand of the build is complete: what the model reads and the variables the
// user sets come from it.
func TestCurrent_IsComplete(t *testing.T) {
	t.Parallel()
	b := brand.Current()
	if b.Key == "" || b.Name == "" || b.EnvPrefix == "" || len(b.ToolNames) == 0 {
		t.Fatalf("incomplete brand: %+v", b)
	}
}
