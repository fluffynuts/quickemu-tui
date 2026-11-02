// Package testtool finds the external programs that integration tests drive.
package testtool

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// RequiredEnv names the tools (comma-separated) whose absence must fail the
// tests rather than skip them. CI sets it for the tools it installs, so a
// broken install can't quietly turn the integration tests into no-ops.
const RequiredEnv = "QUICKEMU_TUI_REQUIRE_TOOLS"

// Require returns the path to tool. If it isn't installed the test is skipped,
// unless tool is listed in RequiredEnv, in which case it fails.
func Require(t testing.TB, tool string) string {
	t.Helper()
	path, err := exec.LookPath(tool)
	if err == nil {
		return path
	}
	if isRequired(tool, os.Getenv(RequiredEnv)) {
		t.Fatalf("%s is not installed, but %s lists it as required", tool, RequiredEnv)
	}
	t.Skipf("%s not installed", tool)
	return ""
}

func isRequired(tool, list string) bool {
	for _, name := range strings.Split(list, ",") {
		if strings.TrimSpace(name) == tool {
			return true
		}
	}
	return false
}
