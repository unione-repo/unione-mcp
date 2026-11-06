package config_test

import (
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/unione-repo/unione-mcp/internal/brand"
	"github.com/unione-repo/unione-mcp/internal/config"
)

var configEnv = []string{
"OAUTH_TOKEN_PREFIX",
	"PORT", "PORT_BASE", "PORT_ATTEMPTS", "PORT_FILE",
	brand.Current().APIURLEnv(), brand.Current().APIKeyEnv(), "MCP_TRANSPORT", "AUTH_MODE", "AUTHORIZATION_SERVER_URL", "MCP_PUBLIC_URL", "OAUTH_SCOPES", "OPENAI_APPS_CHALLENGE",
	"MCP_READ_ONLY", "MCP_DISABLE_SEND", "UPSTREAM_TIMEOUT", "UPSTREAM_MAX_BODY_BYTES", "MCP_MAX_REQUEST_BYTES", "LISTEN_HOST", "APP_VERSION", "LOG_LEVEL", "LOG_FORMAT",
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range configEnv {
		t.Setenv(key, "")
	}
}

func loadOrFail(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestLoad_DefaultsWhenEnvironmentIsEmpty(t *testing.T) {
	clearEnv(t)
	want := &config.Config{
		Brand:               brand.Current(),
		PortBase:            3000,
		PortAttempts:        50,
		PortFile:            ".mcp-server-port",
		APIURL:              brand.Current().APIURL,
		AccessTokenPrefix:   brand.Current().TokenPrefix,
		AuthorizationServer: brand.Current().AuthServer,
		Transport:           config.TransportHTTP,
		AuthMode:            config.AuthAPIKey,
		UpstreamTimeout:     60 * time.Second,
		UpstreamMaxBody:     8 << 20,
		MaxRequestBody:      8 << 20,
		LogLevel:            slog.LevelInfo,
		LogFormat:           "text",
	}

	got := loadOrFail(t)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults\n got %+v\nwant %+v", got, want)
	}
}

func TestLoad_ReadsEveryVariable(t *testing.T) {
	clearEnv(t)
	want := &config.Config{
		Brand:               brand.Current(),
		ExplicitPort:        4321,
		PortBase:            5000,
		PortAttempts:        7,
		PortFile:            ".port-" + t.Name(),
		APIURL:              "https://api.example.test/ru/transactional/api/v1",
		Transport:           config.TransportStdio,
		APIKey:              "key-" + t.Name(),
		AuthMode:            config.AuthOAuth,
		AuthorizationServer: "https://auth.test",
		AccessTokenPrefix:   "tok_",
		PublicURL:           "https://mcp.test",
		ScopesSupported:     []string{"api", "projects"},
		AppsChallenge:       "challenge-" + t.Name(),
		ReadOnly:            true,
		DisableSend:         true,
		UpstreamTimeout:     90 * time.Second,
		UpstreamMaxBody:     1 << 20,
		MaxRequestBody:      2 << 20,
		ListenHost:          "127.0.0.1",
		Version:             "1.2.3-" + t.Name(),
		LogLevel:            slog.LevelDebug,
		LogFormat:           "json",
	}
	t.Setenv("OAUTH_TOKEN_PREFIX", want.AccessTokenPrefix)
	t.Setenv("PORT", strconv.Itoa(want.ExplicitPort))
	t.Setenv("PORT_BASE", strconv.Itoa(want.PortBase))
	t.Setenv("PORT_ATTEMPTS", strconv.Itoa(want.PortAttempts))
	t.Setenv("PORT_FILE", want.PortFile)
	t.Setenv(brand.Current().APIURLEnv(), want.APIURL)
	t.Setenv("MCP_TRANSPORT", string(want.Transport))
	t.Setenv(brand.Current().APIKeyEnv(), " "+want.APIKey+" ")
	t.Setenv("AUTH_MODE", string(want.AuthMode))
	t.Setenv("AUTHORIZATION_SERVER_URL", want.AuthorizationServer)
	t.Setenv("MCP_PUBLIC_URL", want.PublicURL)
	t.Setenv("OAUTH_SCOPES", " api , projects ,")
	t.Setenv("OPENAI_APPS_CHALLENGE", " "+want.AppsChallenge+"\n")
	t.Setenv("MCP_READ_ONLY", strconv.FormatBool(want.ReadOnly))
	t.Setenv("MCP_DISABLE_SEND", strconv.FormatBool(want.DisableSend))
	t.Setenv("UPSTREAM_TIMEOUT", want.UpstreamTimeout.String())
	t.Setenv("UPSTREAM_MAX_BODY_BYTES", strconv.FormatInt(want.UpstreamMaxBody, 10))
	t.Setenv("MCP_MAX_REQUEST_BYTES", strconv.FormatInt(want.MaxRequestBody, 10))
	t.Setenv("LISTEN_HOST", want.ListenHost)
	t.Setenv("APP_VERSION", want.Version)
	t.Setenv("LOG_LEVEL", want.LogLevel.String())
	t.Setenv("LOG_FORMAT", want.LogFormat)

	got := loadOrFail(t)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config\n got %+v\nwant %+v", got, want)
	}
}

func TestLoad_StripsTrailingSlashFromURLs(t *testing.T) {
	cases := map[string]func(*config.Config) string{
		brand.Current().APIURLEnv(): func(c *config.Config) string { return c.APIURL },
		"AUTHORIZATION_SERVER_URL":  func(c *config.Config) string { return c.AuthorizationServer },
		"MCP_PUBLIC_URL":            func(c *config.Config) string { return c.PublicURL },
	}
	for env, read := range cases {
		t.Run(env, func(t *testing.T) {
			clearEnv(t)
			const origin = "https://origin.test"
			t.Setenv(env, origin+"//")

			got := read(loadOrFail(t))

			if got != origin {
				t.Fatalf("%s = %q, want %q", env, got, origin)
			}
		})
	}
}

func TestLoad_AuthModeIsCaseInsensitive(t *testing.T) {
	clearEnv(t)
	t.Setenv("AUTH_MODE", " OAuth ")
	t.Setenv("AUTHORIZATION_SERVER_URL", "https://auth.test")
	t.Setenv("OAUTH_TOKEN_PREFIX", "tst_")

	got := loadOrFail(t)

	if got.AuthMode != config.AuthOAuth {
		t.Fatalf("AuthMode = %q, want %q", got.AuthMode, config.AuthOAuth)
	}
}

func TestLoad_RejectsMalformedValue(t *testing.T) {
	cases := map[string]struct{ env, value string }{
		"port is not a number":          {"PORT", "eighty"},
		"port base is not a number":     {"PORT_BASE", "3k"},
		"port attempts is not a number": {"PORT_ATTEMPTS", "many"},
		"port attempts is zero":         {"PORT_ATTEMPTS", "0"},
		"port attempts is negative":     {"PORT_ATTEMPTS", "-1"},
		"read only is not a boolean":    {"MCP_READ_ONLY", "maybe"},
		"disable send is not a boolean": {"MCP_DISABLE_SEND", "sometimes"},
		"transport is unknown":          {"MCP_TRANSPORT", "sse"},
		"timeout is not a duration":     {"UPSTREAM_TIMEOUT", "soon"},
		"timeout is zero":               {"UPSTREAM_TIMEOUT", "0s"},
		"timeout is negative":           {"UPSTREAM_TIMEOUT", "-1s"},
		"max body is not a number":      {"UPSTREAM_MAX_BODY_BYTES", "8MB"},
		"max body is zero":              {"UPSTREAM_MAX_BODY_BYTES", "0"},
		"max body is negative":          {"UPSTREAM_MAX_BODY_BYTES", "-1"},
		"request body cap is zero":      {"MCP_MAX_REQUEST_BYTES", "0"},
		"port is negative":              {"PORT", "-1"},
		"port is above the range":       {"PORT", "65536"},
		"port base is zero":             {"PORT_BASE", "0"},
		"port base is above the range":  {"PORT_BASE", "65536"},
		"log level is unknown":          {"LOG_LEVEL", "verbose"},
		"log format is unknown":         {"LOG_FORMAT", "xml"},
		"auth mode is unknown":          {"AUTH_MODE", "basic"},
		"api url has no scheme":         {brand.Current().APIURLEnv(), "api.example.test/en/transactional/api/v1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(tc.env, tc.value)

			cfg, err := config.Load()

			if err == nil {
				t.Fatalf("Load accepted %s=%q: %+v", tc.env, tc.value, cfg)
			}
			if cfg != nil {
				t.Fatalf("Load returned a config alongside the error: %+v", cfg)
			}
			if !strings.Contains(err.Error(), tc.env) {
				t.Fatalf("error %q does not name %s", err, tc.env)
			}
		})
	}
}

func TestLoad_OAuthModeRequiresAuthorizationServer(t *testing.T) {
	clearEnv(t)
	t.Setenv("AUTH_MODE", "oauth")

	_, err := config.Load()

	if err == nil || !strings.Contains(err.Error(), "AUTHORIZATION_SERVER_URL") {
		t.Fatalf("Load error = %v, want one naming AUTHORIZATION_SERVER_URL", err)
	}
}

// stdio has no OAuth: AUTH_MODE=oauth left over from the HTTP setup must not block it.
func TestLoad_StdioModeIgnoresOAuthSettings(t *testing.T) {
	clearEnv(t)
	t.Setenv("MCP_TRANSPORT", "stdio")
	t.Setenv(brand.Current().APIKeyEnv(), "key")
	t.Setenv("AUTH_MODE", "oauth")

	loadOrFail(t)
}

func TestLoad_BrandSuppliesTheAPIURL(t *testing.T) {
	clearEnv(t)
	b := brand.Current()
	if b.APIURL == "" {
		t.Skip("the brand has no default API URL")
	}

	got := loadOrFail(t)

	if got.Brand.Key != b.Key || got.APIURL != b.APIURL {
		t.Fatalf("gave brand %q and API URL %q, want %q", got.Brand.Key, got.APIURL, b.APIURL)
	}
}

// A brand without a known token prefix cannot tell a token from a key in oauth mode.
func TestLoad_OAuthModeRequiresTheTokenPrefixTheBrandLacks(t *testing.T) {
	clearEnv(t)
	if brand.Current().TokenPrefix != "" {
		t.Skip("the brand has a token prefix")
	}
	t.Setenv("AUTH_MODE", "oauth")
	t.Setenv("AUTHORIZATION_SERVER_URL", "https://auth.test")

	_, err := config.Load()

	if err == nil || !strings.Contains(err.Error(), "OAUTH_TOKEN_PREFIX") {
		t.Fatalf("Load error = %v, want one naming OAUTH_TOKEN_PREFIX", err)
	}
}

func TestLoad_StdioModeRequiresTheAPIKey(t *testing.T) {
	clearEnv(t)
	t.Setenv("MCP_TRANSPORT", "stdio")

	_, err := config.Load()

	if err == nil || !strings.Contains(err.Error(), brand.Current().APIKeyEnv()) {
		t.Fatalf("Load error = %v, want one naming %s", err, brand.Current().APIKeyEnv())
	}
}

func TestLoad_ReportsEveryMalformedVariableAtOnce(t *testing.T) {
	clearEnv(t)
	t.Setenv("PORT_BASE", "3k")
	t.Setenv("LOG_FORMAT", "xml")

	_, err := config.Load()

	if err == nil {
		t.Fatal("Load accepted two malformed variables")
	}
	for _, env := range []string{"PORT_BASE", "LOG_FORMAT"} {
		if !strings.Contains(err.Error(), env) {
			t.Fatalf("error %q does not name %s", err, env)
		}
	}
}

func TestLoad_BooleanSwitchesAcceptEverySpelling(t *testing.T) {
	cases := map[string]bool{
		"1": true, "true": true, "yes": true, "on": true, "TRUE": true, "Yes": true,
		"0": false, "false": false, "no": false, "off": false, "FALSE": false, "Off": false,
	}
	for value, want := range cases {
		t.Run(value, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("MCP_READ_ONLY", value)

			got := loadOrFail(t)

			if got.ReadOnly != want {
				t.Fatalf("MCP_READ_ONLY=%q gave %v, want %v", value, got.ReadOnly, want)
			}
		})
	}
}

func TestLoad_LogLevelAcceptsSlogNames(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"INFO":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"Error": slog.LevelError,
	}
	for value, want := range cases {
		t.Run(value, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("LOG_LEVEL", value)

			got := loadOrFail(t)

			if got.LogLevel != want {
				t.Fatalf("LOG_LEVEL=%q gave %v, want %v", value, got.LogLevel, want)
			}
		})
	}
}

func TestLoad_LogFormatIsCaseInsensitive(t *testing.T) {
	clearEnv(t)
	t.Setenv("LOG_FORMAT", "JSON")

	got := loadOrFail(t)

	if got.LogFormat != "json" {
		t.Fatalf("LogFormat = %q, want the lower-cased json", got.LogFormat)
	}
}
