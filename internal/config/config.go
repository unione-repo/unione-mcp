package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/unione-repo/unione-mcp/internal/brand"
)

// AuthMode is how a remote caller proves who it is to the API. api_key takes the
// bearer as the caller's API key. oauth advertises the brand's authorization
// server (RFC 9728) and relays its access token as a bearer, while any other
// bearer is still taken as an API key.
type AuthMode string

const (
	AuthAPIKey AuthMode = "api_key"
	AuthOAuth  AuthMode = "oauth"
)

// Transport is how clients reach the server: http is the remote Streamable HTTP
// endpoint, stdio the local mode a user starts on their own machine.
type Transport string

const (
	TransportHTTP  Transport = "http"
	TransportStdio Transport = "stdio"
)

type Config struct {
	// Brand is the installation the binary is built for (brand.Current); it also
	// names the API variables and supplies the defaults of APIURL,
	// AuthorizationServer and AccessTokenPrefix.
	Brand brand.Brand

	ListenHost   string
	ExplicitPort int
	PortBase     int
	PortAttempts int
	PortFile     string

	APIURL string

	Transport Transport
	// APIKey is the local (stdio) mode's only credential, from <EnvPrefix>_API_KEY.
	APIKey string

	AuthMode            AuthMode
	AuthorizationServer string
	// AccessTokenPrefix tells an OAuth access token from an API key in one header.
	AccessTokenPrefix string
	PublicURL         string
	ScopesSupported   []string

	// AppsChallenge is served at /.well-known/openai-apps-challenge (http mode) for
	// OpenAI's domain check; each brand has its own.
	AppsChallenge string

	// ReadOnly registers only the read tools; DisableSend drops email.send.
	ReadOnly    bool
	DisableSend bool

	UpstreamTimeout time.Duration
	UpstreamMaxBody int64
	MaxRequestBody  int64

	LogLevel  slog.Level
	LogFormat string
	Version   string
}

const (
	defaultPortBase        = 3000
	defaultPortAttempts    = 50
	defaultPortFile        = ".mcp-server-port"
	defaultUpstreamTimeout = 60 * time.Second
	defaultUpstreamMaxBody = 8 << 20
	defaultMaxRequestBody  = 8 << 20
	maxPort                = 65535
)

func Load() (*Config, error) {
	var errs []error
	b := brand.Current()
	cfg := &Config{
		Brand:               b,
		Transport:           Transport(strings.ToLower(strings.TrimSpace(envStr("MCP_TRANSPORT", string(TransportHTTP))))),
		APIKey:              strings.TrimSpace(os.Getenv(b.APIKeyEnv())),
		ListenHost:          os.Getenv("LISTEN_HOST"),
		Version:             os.Getenv("APP_VERSION"),
		PortFile:            envStr("PORT_FILE", defaultPortFile),
		APIURL:              stripSlash(envStr(b.APIURLEnv(), b.APIURL)),
		AuthMode:            AuthMode(strings.ToLower(strings.TrimSpace(envStr("AUTH_MODE", string(AuthAPIKey))))),
		AuthorizationServer: stripSlash(envStr("AUTHORIZATION_SERVER_URL", b.AuthServer)),
		AccessTokenPrefix:   strings.TrimSpace(envStr("OAUTH_TOKEN_PREFIX", b.TokenPrefix)),
		PublicURL:           stripSlash(os.Getenv("MCP_PUBLIC_URL")),
		ScopesSupported:     envList("OAUTH_SCOPES"),
		AppsChallenge:       strings.TrimSpace(os.Getenv("OPENAI_APPS_CHALLENGE")),
		LogFormat:           strings.ToLower(envStr("LOG_FORMAT", "text")),
	}

	cfg.ExplicitPort = envInt("PORT", 0, &errs)
	cfg.PortBase = envInt("PORT_BASE", defaultPortBase, &errs)
	cfg.PortAttempts = envInt("PORT_ATTEMPTS", defaultPortAttempts, &errs)
	cfg.ReadOnly = envBool("MCP_READ_ONLY", false, &errs)
	cfg.DisableSend = envBool("MCP_DISABLE_SEND", false, &errs)
	cfg.UpstreamTimeout = envDuration("UPSTREAM_TIMEOUT", defaultUpstreamTimeout, &errs)
	cfg.UpstreamMaxBody = int64(envInt("UPSTREAM_MAX_BODY_BYTES", defaultUpstreamMaxBody, &errs))
	cfg.MaxRequestBody = int64(envInt("MCP_MAX_REQUEST_BYTES", defaultMaxRequestBody, &errs))
	cfg.LogLevel = envLogLevel("LOG_LEVEL", slog.LevelInfo, &errs)
	switch cfg.Transport {
	case TransportHTTP:
	case TransportStdio:
		if cfg.APIKey == "" {
			errs = append(errs, fmt.Errorf("%s: required in stdio mode — the %s API key the tools act with", b.APIKeyEnv(), b.Name))
		}
	default:
		errs = append(errs, fmt.Errorf("MCP_TRANSPORT: want http or stdio, got %q", cfg.Transport))
	}
	switch cfg.AuthMode {
	case AuthAPIKey:
	case AuthOAuth:
		// stdio acts with the API key alone: OAuth belongs to the HTTP transport.
		if cfg.Transport == TransportStdio {
			break
		}
		if cfg.AuthorizationServer == "" {
			errs = append(errs, fmt.Errorf("AUTHORIZATION_SERVER_URL: required when AUTH_MODE=oauth"))
		}
		if cfg.AccessTokenPrefix == "" {
			errs = append(errs, fmt.Errorf("OAUTH_TOKEN_PREFIX: required when AUTH_MODE=oauth — %s has no default", b.Name))
		}
	default:
		errs = append(errs, fmt.Errorf("AUTH_MODE: want api_key or oauth, got %q", cfg.AuthMode))
	}
	switch {
	case cfg.APIURL == "":
		errs = append(errs, fmt.Errorf("%s: required — %s has no default", b.APIURLEnv(), b.Name))
	case !strings.HasPrefix(cfg.APIURL, "http://") && !strings.HasPrefix(cfg.APIURL, "https://"):
		errs = append(errs, fmt.Errorf("%s: want an http(s) URL, got %q", b.APIURLEnv(), cfg.APIURL))
	}
	if cfg.LogFormat != "text" && cfg.LogFormat != "json" {
		errs = append(errs, fmt.Errorf("LOG_FORMAT: want text or json, got %q", cfg.LogFormat))
	}
	if cfg.PortAttempts < 1 {
		errs = append(errs, fmt.Errorf("PORT_ATTEMPTS: must be at least 1"))
	}
	if cfg.ExplicitPort < 0 || cfg.ExplicitPort > maxPort {
		errs = append(errs, fmt.Errorf("PORT: want 1..%d (or unset to auto-pick), got %d", maxPort, cfg.ExplicitPort))
	}
	if cfg.PortBase < 1 || cfg.PortBase > maxPort {
		errs = append(errs, fmt.Errorf("PORT_BASE: want 1..%d, got %d", maxPort, cfg.PortBase))
	}
	if cfg.UpstreamMaxBody < 1 {
		errs = append(errs, fmt.Errorf("UPSTREAM_MAX_BODY_BYTES: must be at least 1, got %d", cfg.UpstreamMaxBody))
	}
	if cfg.MaxRequestBody < 1 {
		errs = append(errs, fmt.Errorf("MCP_MAX_REQUEST_BYTES: must be at least 1, got %d", cfg.MaxRequestBody))
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("config: %w", joinErrors(errs))
	}
	return cfg, nil
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envList(key string) []string {
	var out []string
	for _, item := range strings.Split(os.Getenv(key), ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func envInt(key string, fallback int, errs *[]error) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s: want an integer, got %q", key, v))
		return fallback
	}
	return n
}

func envBool(key string, fallback bool, errs *[]error) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	*errs = append(*errs, fmt.Errorf("%s: want a boolean (1/0, true/false, yes/no), got %q", key, v))
	return fallback
}

func envDuration(key string, fallback time.Duration, errs *[]error) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		*errs = append(*errs, fmt.Errorf("%s: want a positive duration like 30s, got %q", key, v))
		return fallback
	}
	return d
}

func envLogLevel(key string, fallback slog.Level, errs *[]error) slog.Level {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(v)); err != nil {
		*errs = append(*errs, fmt.Errorf("%s: want debug/info/warn/error, got %q", key, v))
		return fallback
	}
	return level
}

func joinErrors(errs []error) error {
	parts := make([]string, len(errs))
	for i, err := range errs {
		parts[i] = err.Error()
	}
	return fmt.Errorf("%s", strings.Join(parts, "; "))
}

func stripSlash(s string) string { return strings.TrimRight(s, "/") }
