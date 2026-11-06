package auth

import (
	"net/http"
	"strings"

	"github.com/unione-repo/unione-mcp/internal/config"
)

// HeaderAPIKey is the header the API reads the API key from.
const HeaderAPIKey = "X-API-KEY"

type Kind int

const (
	None Kind = iota
	APIKey
	Bearer
)

// Credential is what one caller presented, already interpreted for the auth mode:
// the upstream client relays it as X-API-KEY or as an Authorization bearer.
type Credential struct {
	Kind  Kind
	Value string
}

func (c Credential) Present() bool { return c.Kind != None && c.Value != "" }

// From reads a remote caller's credential from the Authorization bearer. In oauth
// mode an access token — it starts with tokenPrefix, which is how it tells itself
// from an API key in the same header — is relayed as a bearer and anything else is
// still an API key: servers and CI keep connecting with a key while browsers go
// through OAuth. In api_key mode the bearer is always the API key.
func From(h http.Header, mode config.AuthMode, tokenPrefix string) Credential {
	token := BearerFrom(h)
	switch {
	case token == "":
		return Credential{}
	case mode == config.AuthOAuth && tokenPrefix != "" && strings.HasPrefix(token, tokenPrefix):
		return Credential{Kind: Bearer, Value: token}
	default:
		return Credential{Kind: APIKey, Value: token}
	}
}

func BearerFrom(h http.Header) string {
	scheme, token, found := strings.Cut(strings.TrimSpace(h.Get("Authorization")), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}
