package slowtest

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// An unavailable Node alone is not a slow-test gate: an installed Node would
// still run the probe. Keep the disabled path observable even on such hosts.
func TestJavaScriptBOMProbeHonorsSlowGate(t *testing.T) {
	Gate(t, "go (it verifies the JavaScript BOM probe's disabled path)")
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	// jsNode accepts an existing NODE path. Without the explicit gate, this
	// non-executable sentinel makes the child fail before it can run any Node.
	sentinel := filepath.Join(t.TempDir(), "node-must-not-run")
	if err := os.WriteFile(sentinel, []byte("must not execute\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-json", "-timeout=0", "-count=1",
		"./internal/codegen/jstable", "-run", "^TestJavaScriptTextBOMMatchesGo$")
	cmd.Dir = root
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "SCHEMA_SLOW" && key != "SCHEMA_REQUIRE_CORPUS" && key != "NODE" {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "SCHEMA_SLOW=0", "SCHEMA_REQUIRE_CORPUS=", "NODE="+sentinel)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("disabled BOM probe ran or failed: %v\n%s", err, output)
	}
	const pkg = "github.com/mas-bandwidth/schema/v2/internal/codegen/jstable"
	const name = "TestJavaScriptTextBOMMatchesGo"
	var skipped int
	var reason bool
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	for {
		var event struct {
			Action, Package, Test, Output string
		}
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode go test event: %v\n%s", err, output)
		}
		if event.Package != pkg || event.Test != name {
			continue
		}
		switch event.Action {
		case "skip":
			skipped++
		case "pass", "fail":
			t.Fatalf("disabled BOM probe ended with %s, want skip\n%s", event.Action, output)
		}
		reason = reason || strings.Contains(event.Output, "SCHEMA_SLOW:")
	}
	if skipped != 1 || !reason {
		t.Fatalf("missing exact named slow-gate skip: skips=%d reason=%t\n%s", skipped, reason, output)
	}
}
