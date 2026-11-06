package auth_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/unione-repo/unione-mcp/internal/auth"
	"github.com/unione-repo/unione-mcp/internal/config"
)

const tokenPrefix = "tok_"

func TestBearerFrom_ExtractsToken(t *testing.T) {
	t.Parallel()
	token := "tok-" + t.Name()
	cases := map[string]struct{ authorization, want string }{
		"bearer scheme":              {"Bearer " + token, token},
		"lowercase scheme":           {"bearer " + token, token},
		"uppercase scheme":           {"BEARER " + token, token},
		"spaces around and inside":   {"  Bearer   " + token + "  ", token},
		"empty header":               {"", ""},
		"basic scheme":               {"Basic " + token, ""},
		"scheme without token":       {"Bearer", ""},
		"scheme with trailing space": {"Bearer ", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			header := http.Header{}
			header.Set("Authorization", tc.authorization)

			got := auth.BearerFrom(header)

			if got != tc.want {
				t.Fatalf("BearerFrom(%q) = %q, want %q", tc.authorization, got, tc.want)
			}
		})
	}
}

func TestFrom_TakesTheBearer(t *testing.T) {
	t.Parallel()
	token := "tok-" + t.Name()
	cases := map[string]struct {
		mode          config.AuthMode
		apiKey, authz string
		want          auth.Credential
	}{
		"api_key mode: bearer is the API key": {config.AuthAPIKey, "", "Bearer " + token, auth.Credential{Kind: auth.APIKey, Value: token}},
		"oauth mode: access token is a token": {config.AuthOAuth, "", "Bearer " + tokenPrefix + token, auth.Credential{Kind: auth.Bearer, Value: tokenPrefix + token}},
		"oauth mode: other bearer is a key":   {config.AuthOAuth, "", "Bearer " + token, auth.Credential{Kind: auth.APIKey, Value: token}},
		"api_key mode: token prefix is a key": {config.AuthAPIKey, "", "Bearer " + tokenPrefix + token, auth.Credential{Kind: auth.APIKey, Value: tokenPrefix + token}},
		"X-API-KEY is not accepted":           {config.AuthAPIKey, token, "", auth.Credential{}},
		"basic scheme is ignored":             {config.AuthAPIKey, "", "Basic " + token, auth.Credential{}},
		"nothing":                             {config.AuthAPIKey, "", "", auth.Credential{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			header := http.Header{}
			header.Set(auth.HeaderAPIKey, tc.apiKey)
			header.Set("Authorization", tc.authz)

			got := auth.From(header, tc.mode, tokenPrefix)

			if got != tc.want {
				t.Fatalf("From = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestCredential_Present(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		credential auth.Credential
		want       bool
	}{
		"api key":        {auth.Credential{Kind: auth.APIKey, Value: "k"}, true},
		"bearer":         {auth.Credential{Kind: auth.Bearer, Value: "t"}, true},
		"zero value":     {auth.Credential{}, false},
		"kind but empty": {auth.Credential{Kind: auth.APIKey}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := tc.credential.Present(); got != tc.want {
				t.Fatalf("Present = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRejection_IsSeenOnlyWithinTheWatchedRequest(t *testing.T) {
	t.Parallel()
	ctx, rejection := auth.WithRejection(context.Background())

	auth.MarkTokenRejected(context.Background())
	if rejection.Rejected() {
		t.Fatalf("a mark outside the request reached it")
	}
	auth.MarkTokenRejected(ctx)
	if !rejection.Rejected() {
		t.Fatalf("the mark within the request was lost")
	}
}
