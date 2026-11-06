//go:build integration

package acceptance

import (
	"os"
	"testing"

	arrangeServer "github.com/unione-repo/unione-mcp/tests/support/arrange/server"
)

func TestMain(m *testing.M) {
	code := m.Run()
	arrangeServer.RemoveBuild()
	os.Exit(code)
}
