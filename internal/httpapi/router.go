package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/unione-repo/unione-mcp/internal/auth"
	"github.com/unione-repo/unione-mcp/internal/brand"
	"github.com/unione-repo/unione-mcp/internal/config"
)

type Options struct {
	// Brand names the product in the challenges.
	Brand brand.Brand
	// SelfURL is this server's public origin; the MCP endpoint SelfURL+"/mcp" is its RFC 9728 resource.
	SelfURL             string
	AuthMode            config.AuthMode
	AuthorizationServer string
	// AccessTokenPrefix tells an OAuth access token from an API key (oauth mode).
	AccessTokenPrefix string
	Scopes            []string
	MCP               http.Handler
	// Tokens checks an access token before the MCP handler sees the request (oauth
	// mode); nil leaves the check to the tool calls.
	Tokens *auth.Verifier
	// Keys checks an API key before the MCP handler sees the request: initialize and
	// tools/list make no API call of their own, so without it any bearer would get
	// the tool list. nil leaves the check to the tool calls.
	Keys *auth.Verifier
	// AppsChallenge is the token OpenAI checks at /.well-known/openai-apps-challenge
	// before listing the server in the ChatGPT and Codex plugin catalog; empty
	// leaves that path unserved (404).
	AppsChallenge string
	// MaxRequestBody caps what a caller may POST to /mcp; a tool call is a small JSON-RPC envelope even with a full letter inside.
	MaxRequestBody int64
}

const (
	mcpPath           = "/mcp"
	metadataPath      = "/.well-known/oauth-protected-resource"
	appsChallengePath = "/.well-known/openai-apps-challenge"
)

func New(o Options) http.Handler {
	mcpHandler := o.MCP
	if o.MaxRequestBody > 0 {
		mcpHandler = http.MaxBytesHandler(o.MCP, o.MaxRequestBody)
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	if o.AppsChallenge != "" {
		// OpenAI's domain check compares the body with the token byte for byte: no JSON, no newline.
		mux.HandleFunc("GET "+appsChallengePath, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, o.AppsChallenge)
		})
	}

	if o.AuthMode == config.AuthOAuth {
		// Each metadata document identifies its own resource; challenges advertise the origin.
		// The /mcp document remains available for path-aware discovery.
		mux.HandleFunc("GET "+metadataPath+mcpPath, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, protectedResourceMetadata(o, o.SelfURL+mcpPath))
		})
		mux.HandleFunc("GET "+metadataPath, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, protectedResourceMetadata(o, o.SelfURL))
		})
	}

	mux.HandleFunc("POST "+mcpPath, func(w http.ResponseWriter, r *http.Request) {
		if !originAllowed(r, o.SelfURL) {
			originRejected(w)
			return
		}
		credential := auth.From(r.Header, o.AuthMode, o.AccessTokenPrefix)
		if !credential.Present() {
			challenge(w, o)
			return
		}
		if credential.Kind == auth.Bearer {
			if o.Tokens != nil && o.Tokens.Rejected(r.Context(), credential.Value) {
				tokenRejected(w, o)
				return
			}
			serveWatchingToken(w, r, mcpHandler, o)
			return
		}
		if o.Keys != nil && o.Keys.Rejected(r.Context(), credential.Value) {
			keyRejected(w, o)
			return
		}
		mcpHandler.ServeHTTP(w, r)
	})
	mux.HandleFunc(mcpPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"jsonrpc": "2.0",
			"error":   map[string]any{"code": -32000, "message": "Only POST is served in stateless mode."},
			"id":      nil,
		})
	})

	return mux
}

// MCP Streamable HTTP asks the server to validate Origin against DNS rebinding.
// A browser page sets Origin; MCP clients run outside a browser and send none, so
// a request without it passes, and one from a page is accepted only from this
// server's own origin.
func originAllowed(r *http.Request, selfURL string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	self, ok := originOf(selfURL)
	if !ok {
		return false
	}
	got, ok := originOf(origin)
	return ok && got == self
}

// A serialized origin: lower-case scheme and host, the port always spelled out.
// MCP_PUBLIC_URL may name the default port (https://host:443) while a browser
// omits it from Origin; both mean the same origin. "null" (an opaque origin) and
// a value with credentials, a query or a fragment never match.
func originOf(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	if port == "" {
		switch scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			return "", false
		}
	}
	return scheme + "://" + strings.ToLower(u.Hostname()) + ":" + port, true
}

func originRejected(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, map[string]any{
		"jsonrpc": "2.0",
		"error":   map[string]any{"code": -32000, "message": "Forbidden: the Origin header is not allowed."},
		"id":      nil,
	})
}

func protectedResourceMetadata(o Options, resource string) map[string]any {
	metadata := map[string]any{
		"resource":                 resource,
		"authorization_servers":    []string{o.AuthorizationServer},
		"bearer_methods_supported": []string{"header"},
	}
	if len(o.Scopes) > 0 {
		metadata["scopes_supported"] = o.Scopes
	}
	return metadata
}

// An access token expires or gets revoked in the cabinet while the client keeps it.
// Options.Tokens catches that before the request reaches MCP. When the API refuses
// the token during the call anyway (revoked between the check and the call), the tool
// result is dropped and the request is answered 401 invalid_token (RFC 6750 §3.1):
// the client then refreshes the token and repeats the call instead of showing the
// user an error.
func serveWatchingToken(w http.ResponseWriter, r *http.Request, next http.Handler, o Options) {
	ctx, rejection := auth.WithRejection(r.Context())
	buffered := &bufferedResponse{header: http.Header{}}
	next.ServeHTTP(buffered, r.WithContext(ctx))

	if rejection.Rejected() {
		tokenRejected(w, o)
		return
	}
	buffered.writeTo(w)
}

func tokenRejected(w http.ResponseWriter, o Options) {
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(
		`Bearer error="invalid_token", error_description="The access token is expired or revoked", resource_metadata=%q`,
		resourceMetadataURL(o),
	))
	writeJSON(w, http.StatusUnauthorized, map[string]string{
		"error":             "invalid_token",
		"error_description": "The access token is expired or revoked.",
	})
}

// A key the API does not know is answered like a missing one: in oauth mode the
// challenge still points at the authorization server, so the client may sign in instead.
func keyRejected(w http.ResponseWriter, o Options) {
	description := o.Brand.Render("[[name]] rejected the API key. Check the key in the MCP client config: it must be an account " +
		"or project API key from the [[name]] account settings.")
	if o.AuthMode == config.AuthOAuth {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer error="invalid_token", resource_metadata=%q`, resourceMetadataURL(o)))
	} else {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm=%q, error="invalid_token"`, o.Brand.Name))
	}
	writeJSON(w, http.StatusUnauthorized, map[string]string{
		"error":             "invalid_token",
		"error_description": description,
	})
}

// In oauth mode the 401 carries the RFC 9728 challenge — that is what lets an MCP
// client discover the authorization server and start the flow. In api_key mode
// there is nothing to discover: the key goes into the client's MCP config.
func challenge(w http.ResponseWriter, o Options) {
	if o.AuthMode == config.AuthOAuth {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf("Bearer resource_metadata=%q", resourceMetadataURL(o)))
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error":             "invalid_token",
			"error_description": o.Brand.Render("A bearer token is required: connect through OAuth or put the [[name]] API key into the header Authorization: Bearer <key>."),
		})
		return
	}
	w.Header().Set("WWW-Authenticate", fmt.Sprintf("Bearer realm=%q", o.Brand.Name))
	writeJSON(w, http.StatusUnauthorized, map[string]string{
		"error": "invalid_token",
		"error_description": o.Brand.Render("[[name]] API key is missing. Put it into the MCP client config as the header " +
			"Authorization: Bearer <[[name]] API key>; the key is in the [[name]] account settings."),
	})
}

func resourceMetadataURL(o Options) string { return o.SelfURL + metadataPath }

// bufferedResponse holds the MCP answer until it is known whether the token was
// accepted. A stateless call is one short JSON-RPC exchange, so nothing streams.
type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.header }

func (b *bufferedResponse) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}

// Flush is a no-op: the SDK flushes event streams, the buffer is written out at the end.
func (b *bufferedResponse) Flush() {}

func (b *bufferedResponse) writeTo(w http.ResponseWriter) {
	for name, values := range b.header {
		w.Header()[name] = values
	}
	if b.status == 0 {
		b.status = http.StatusOK
	}
	w.WriteHeader(b.status)
	_, _ = w.Write(b.body.Bytes())
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
