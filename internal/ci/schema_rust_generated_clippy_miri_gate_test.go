package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaRustGeneratedClippyMiriGateHasBlades pins that the Rust clippy and
// Miri gate (lens: schema-per-language-lenses) opens every job with a TOOL
// PROBE that fails by name when the checker is absent. A gate that passes
// while its toolchain is missing is a gate with no blade (the toolchain gate,
// issue #599): the probe is what keeps the clippy and Miri legs from going
// green on a machine that never installed them.
func TestSchemaRustGeneratedClippyMiriGateHasBlades(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	path := filepath.Join(root, ".github", "workflows", "schema-rust-generated-clippy-miri-gate.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(data)
	for _, probe := range []struct {
		name string
		want string
	}{
		{"clippy probe", "cargo clippy --version"},
		{"Miri probe", "cargo +nightly miri --version"},
	} {
		if !strings.Contains(text, probe.want) {
			t.Errorf("the gate's %s is missing — expected the workflow to name %q", probe.name, probe.want)
		}
	}
}
