package arrange

import (
	"math/rand/v2"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const (
	defaultWiremockURL = "http://localhost:8081"
	// APIPrefix is where the server under test looks for the API on wiremock.
	APIPrefix = "/en/transactional/api/v1"
)

func EnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func WiremockURL() string {
	return strings.TrimRight(EnvOrDefault("E2E_WIREMOCK_URL", defaultWiremockURL), "/")
}

func APIURL() string {
	return WiremockURL() + APIPrefix
}

// MethodPath is the URL path of an API method as it reaches wiremock.
func MethodPath(method string) string {
	return APIPrefix + "/" + method + ".json"
}

func UniqueID(t *testing.T, prefix string) string {
	t.Helper()
	return prefix + "-" + uuid.NewString()
}

func UniqueNumber(t *testing.T) int64 {
	t.Helper()
	return rand.Int64N(1_000_000_000) + 1
}

// APIKey is each test's unique credential: stubs match it and assertions filter
// the wiremock journal by it, so tests run in parallel without cleaners.
func APIKey(t *testing.T) string {
	t.Helper()
	return UniqueID(t, "key")
}

func UniqueEmail(t *testing.T) string {
	t.Helper()
	return strings.ReplaceAll(uuid.NewString(), "-", "") + "@example.test"
}
