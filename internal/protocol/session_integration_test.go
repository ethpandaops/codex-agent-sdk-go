//go:build integration

package protocol

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethpandaops/codex-agent-sdk-go/internal/config"
	"github.com/stretchr/testify/require"
)

func generateSchemaDir(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("Codex CLI not installed")
	}

	dir := t.TempDir()
	cmd := exec.Command("codex", "app-server", "generate-json-schema", "--out", dir)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "generate schema: %s", string(output))

	return dir
}

func readSchemaFile(t *testing.T, dir string, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)

	return string(data)
}

func requestMethodToSubtype(method string) string {
	parts := strings.SplitN(method, "/", 2)
	if len(parts) == 2 {
		return parts[0] + "_" + parts[1]
	}

	return method
}

func TestSessionRegisterHandlers_CoversCurrentCodexServerRequests(t *testing.T) {
	schemaDir := generateSchemaDir(t)
	serverRequestJSON := readSchemaFile(t, schemaDir, "ServerRequest.json")

	controller := NewController(slog.Default(), newMockTransport())
	session := NewSession(slog.Default(), controller, &config.Options{})
	session.RegisterHandlers()

	liveMethods := []string{
		"item/tool/call",
		"item/commandExecution/requestApproval",
		"item/fileChange/requestApproval",
		"item/tool/requestUserInput",
		"account/chatgptAuthTokens/refresh",
		"applyPatchApproval",
		"execCommandApproval",
	}

	for _, method := range liveMethods {
		require.Contains(t, serverRequestJSON, `"`+method+`"`,
			"installed codex schema no longer contains %s; update this proof test", method)

		subtype := requestMethodToSubtype(method)
		_, ok := controller.handlers[subtype]
		require.True(t, ok, "no session handler registered for current codex server request %q (subtype %q)", method, subtype)
	}
}

func TestSessionRegisterHandlers_DoesNotRegisterRequestTypesMissingFromCurrentCodexSchema(t *testing.T) {
	schemaDir := generateSchemaDir(t)
	serverRequestJSON := readSchemaFile(t, schemaDir, "ServerRequest.json")

	controller := NewController(slog.Default(), newMockTransport())
	session := NewSession(slog.Default(), controller, &config.Options{})
	session.RegisterHandlers()

	staleMethods := []string{
		"item/permissions/requestApproval",
		"mcpServer/elicitation/request",
	}

	for _, method := range staleMethods {
		require.NotContains(t, serverRequestJSON, `"`+method+`"`,
			"current codex schema unexpectedly contains %s; update this proof test", method)

		subtype := requestMethodToSubtype(method)
		found := false
		for registered := range controller.handlers {
			if registered == subtype {
				found = true
				break
			}
		}

		require.False(t, found,
			"stale handler %q should not remain registered when the current codex schema no longer exposes that request", subtype)
	}

	// Guard against accidental whitespace-only schema reads.
	require.NotEmpty(t, strings.TrimSpace(serverRequestJSON))
}
