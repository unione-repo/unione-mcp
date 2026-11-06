//go:build integration

package acceptance

import (
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/unione-repo/unione-mcp/internal/brand"
	actMcp "github.com/unione-repo/unione-mcp/tests/support/act/mcp"
	"github.com/unione-repo/unione-mcp/tests/support/arrange"
	arrangeAPI "github.com/unione-repo/unione-mcp/tests/support/arrange/api"
	arrangeServer "github.com/unione-repo/unione-mcp/tests/support/arrange/server"
	assertAPI "github.com/unione-repo/unione-mcp/tests/support/assert/api"
	assertTool "github.com/unione-repo/unione-mcp/tests/support/assert/tool"
)

func names(tools []*mcp.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name)
	}
	slices.Sort(out)
	return out
}

func TestE2E_Modes_LocalServerOffersTheRemoteToolsAndActsWithItsKey(t *testing.T) {
	t.Parallel()
	remote := arrangeServer.Start(t, arrangeServer.Options{})
	key := arrange.APIKey(t)
	arrangeAPI.NewStub(key, "system/info").WithAnswer(map[string]any{"user_id": 7}).Register(t)
	local := actMcp.ConnectStdio(t, key)

	remoteTools := names(actMcp.Connect(t, remote, arrange.APIKey(t)).ListTools(t))
	localTools := names(local.ListTools(t))
	res := local.CallTool(t, remote.Tool("account.get"), nil)

	if !slices.Equal(localTools, remoteTools) {
		t.Errorf("stdio tools differ from remote\n local %v\nremote %v", localTools, remoteTools)
	}
	assertTool.JSONIncludes(t, res, map[string]any{"user_id": 7})
	assertAPI.HeaderIs(t, assertAPI.Called(t, key, "system/info"), "User-Agent", brand.Current().ServerName()+"/dev")
}

func TestE2E_Modes_ReadOnlyHidesEveryToolThatChangesTheAccount(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{ReadOnly: true})

	tools := actMcp.Connect(t, server, arrange.APIKey(t)).ListTools(t)

	if len(tools) == 0 {
		t.Fatal("read-only server lists no tools")
	}
	for _, tool := range tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("read-only server lists %s", tool.Name)
		}
	}
}

func TestE2E_Modes_SendSwitchRemovesSendEmail(t *testing.T) {
	t.Parallel()
	server := arrangeServer.Start(t, arrangeServer.Options{DisableSend: true})
	session := actMcp.Connect(t, server, arrange.APIKey(t))

	listed := names(session.ListTools(t))

	if slices.Contains(listed, server.Tool("email.send")) {
		t.Errorf("email.send is listed with the send switch off")
	}
	if !slices.Contains(listed, server.Tool("account.get")) {
		t.Errorf("the send switch took other tools away: %v", listed)
	}
}
