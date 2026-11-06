package arrangeServer

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/unione-repo/unione-mcp/internal/brand"
	"github.com/unione-repo/unione-mcp/internal/config"
	"github.com/unione-repo/unione-mcp/tests/support/arrange"
)

type Options struct {
	ReadOnly        bool
	DisableSend     bool
	AuthMode        config.AuthMode
	UpstreamTimeout time.Duration
	// AppsChallenge sets OPENAI_APPS_CHALLENGE; empty leaves it unset.
	AppsChallenge string
}

type Server struct {
	BaseURL     string
	MCPEndpoint string
}

const (
	defaultUpstreamTimeout = 2 * time.Second
	readyDeadline          = 10 * time.Second
	stopDeadline           = 5 * time.Second
	probeInterval          = 20 * time.Millisecond
	maxProbeInterval       = 250 * time.Millisecond
	// AuthorizationServer is what the server advertises in oauth mode.
	AuthorizationServer = "https://auth.example.test"
	// TokenPrefix tells an access token from an API key in oauth mode: a brand may
	// have no default prefix of its own.
	TokenPrefix = "tst_"
)

// startAttempts covers the gap between picking a free port and the server binding
// it: a parallel test can take the port in between.
const startAttempts = 3

func Start(t *testing.T, opts Options) *Server {
	t.Helper()

	requireWiremock(t)
	opts = withDefaults(opts)
	for attempt := 1; ; attempt++ {
		server, stderr, err := launch(t, opts)
		if err == nil {
			return server
		}
		if attempt == startAttempts || !strings.Contains(stderr, "address already in use") {
			t.Fatalf("%v\n%s", err, stderr)
		}
	}
}

func launch(t *testing.T, opts Options) (*Server, string, error) {
	t.Helper()

	port := freePort(t)
	server := &Server{BaseURL: fmt.Sprintf("http://localhost:%d", port)}
	server.MCPEndpoint = server.BaseURL + "/mcp"

	var stderr bytes.Buffer
	cmd := exec.Command(binary(t))
	cmd.Dir = t.TempDir()
	cmd.Env = environment(port, opts)
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mcp server: %v", err)
	}
	proc := &process{cmd: cmd, exited: make(chan struct{})}
	go proc.wait()
	t.Cleanup(func() {
		proc.stop()
		if t.Failed() {
			t.Logf("mcp server stderr:\n%s", stderr.String())
		}
	})

	if err := awaitReady(server, proc); err != nil {
		proc.stop()
		return nil, stderr.String(), err
	}
	return server, "", nil
}

// Tool is the name the tool with this id is listed under: the acceptance server
// runs as the brand of the build.
func (s *Server) Tool(id string) string {
	return brand.Current().Tool(id)
}

func withDefaults(opts Options) Options {
	if opts.AuthMode == "" {
		opts.AuthMode = config.AuthAPIKey
	}
	if opts.UpstreamTimeout == 0 {
		opts.UpstreamTimeout = defaultUpstreamTimeout
	}
	return opts
}

func environment(port int, opts Options) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"PORT=" + strconv.Itoa(port),
		brand.Current().APIURLEnv() + "=" + arrange.APIURL(),
		"AUTH_MODE=" + string(opts.AuthMode),
		"AUTHORIZATION_SERVER_URL=" + AuthorizationServer,
		"OAUTH_TOKEN_PREFIX=" + TokenPrefix,
		"MCP_READ_ONLY=" + strconv.FormatBool(opts.ReadOnly),
		"MCP_DISABLE_SEND=" + strconv.FormatBool(opts.DisableSend),
		"UPSTREAM_TIMEOUT=" + opts.UpstreamTimeout.String(),
		"LOG_FORMAT=json",
		"LOG_LEVEL=debug",
	}
	if opts.AppsChallenge != "" {
		env = append(env, "OPENAI_APPS_CHALLENGE="+opts.AppsChallenge)
	}
	return env
}

type process struct {
	cmd     *exec.Cmd
	exited  chan struct{}
	exitErr error
}

func (p *process) wait() {
	p.exitErr = p.cmd.Wait()
	close(p.exited)
}

func (p *process) stop() {
	select {
	case <-p.exited:
		return
	default:
	}
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.exited:
	case <-time.After(stopDeadline):
		_ = p.cmd.Process.Kill()
		<-p.exited
	}
}

func awaitReady(server *Server, proc *process) error {
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(readyDeadline)
	interval := probeInterval
	for {
		if healthy(client, server.BaseURL+"/health") {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("mcp server at %s did not become healthy within %s", server.BaseURL, readyDeadline)
		}
		select {
		case <-proc.exited:
			return fmt.Errorf("mcp server exited before becoming healthy: %w", proc.exitErr)
		case <-time.After(interval):
		}
		interval = min(2*interval, maxProbeInterval)
	}
}

func healthy(client *http.Client, url string) bool {
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("pick a free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

var build struct {
	once sync.Once
	dir  string
	path string
	err  error
}

func binary(t *testing.T) string {
	t.Helper()

	build.once.Do(compile)
	if build.err != nil {
		t.Fatalf("build ./cmd/unione-mcp: %v", build.err)
	}
	return build.path
}

func compile() {
	root, err := moduleRoot()
	if err != nil {
		build.err = err
		return
	}
	build.dir, err = os.MkdirTemp("", "mcp-acceptance-")
	if err != nil {
		build.err = err
		return
	}
	build.path = filepath.Join(build.dir, "mcp-server")
	cmd := exec.Command("go", "build", "-o", build.path, "./cmd/unione-mcp")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		build.err = fmt.Errorf("%w\n%s", err, out)
	}
}

// Binary is the built server, for tests that start it themselves (stdio mode).
func Binary(t *testing.T) string {
	t.Helper()
	return binary(t)
}

// StdioEnv is the environment of a local-mode server acting with apiKey.
func StdioEnv(apiKey string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		brand.Current().APIURLEnv() + "=" + arrange.APIURL(),
		brand.Current().APIKeyEnv() + "=" + apiKey,
		"LOG_FORMAT=json",
	}
}

// RequireWiremock fails the test early when the API stand-in is not running.
func RequireWiremock(t *testing.T) {
	t.Helper()
	requireWiremock(t)
}

func RemoveBuild() {
	if build.dir != "" {
		_ = os.RemoveAll(build.dir)
	}
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found above " + dir)
		}
		dir = parent
	}
}

var wiremock struct {
	once sync.Once
	err  error
}

func requireWiremock(t *testing.T) {
	t.Helper()

	wiremock.once.Do(func() {
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(arrange.WiremockURL() + "/__admin/health")
		if err != nil {
			wiremock.err = err
			return
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			wiremock.err = fmt.Errorf("health answered HTTP %d", resp.StatusCode)
		}
	})
	if wiremock.err != nil {
		t.Fatalf("wiremock is not reachable at %s (%v): start WireMock there or point E2E_WIREMOCK_URL at a running one",
			arrange.WiremockURL(), wiremock.err)
	}
}
