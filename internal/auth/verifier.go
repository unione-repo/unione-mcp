package auth

import (
	"context"
)

// Verdict is what the API said about an access token or an API key.
type Verdict int

const (
	// Unknown: the API did not answer clearly (network, 5xx, 429). The request goes
	// on — the tool call itself will tell, and an outage must not log everybody out.
	Unknown Verdict = iota
	Accepted
	Rejected
)

// Verifier asks the API whether it accepts an access token before the MCP handler
// sees the request. MCP authorization wants the 401 on any request with a bad token,
// initialize included, so a client with an expired token signs in again on connect
// instead of failing on the user's first action. The server has no introspection
// endpoint to ask, so the check is a cheap API call made with the token.
//
// The answer is not remembered: a token revoked in the cabinet is refused on the very
// next request, initialize and tools/list included, which make no API call of their own.
type Verifier struct {
	check func(ctx context.Context, token string) Verdict
}

func NewVerifier(check func(ctx context.Context, token string) Verdict) *Verifier {
	return &Verifier{check: check}
}

// Rejected reports whether the API refused the token.
func (v *Verifier) Rejected(ctx context.Context, token string) bool {
	return v.check(ctx, token) == Rejected
}
