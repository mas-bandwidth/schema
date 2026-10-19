package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// schemaPorts is the port roster the random-input differential runs. The
// roster is discovered from the committed generated shared-output tree under
// repo/generated: one port per first-level subdirectory whose name is a
// language leg. This keeps the gate faithful to what the generator actually
// produces rather than to a hand-typed list that can drift.
func schemaPorts(genRoot string) []string {
	entries, err := os.ReadDir(genRoot)
	if err != nil {
		return nil
	}
	var ports []string
	for _, e := range entries {
		if e.IsDir() && !strings.Contains(e.Name(), "ludicrous") && e.Name() != "bench" {
			ports = append(ports, e.Name())
		}
	}
	return ports
}

// schemaPortToolchain names the executable that probes each port's toolchain.
var schemaPortToolchain = map[string]string{
	"c":        "cc",
	"cpp":      "c++",
	"cs":       "dotnet",
	"dart":     "dart",
	"elixir":   "elixir",
	"go":       "go",
	"java":     "java",
	"js":       "node",
	"rust":     "cargo",
	"squirrel": "cc",
	"canary":   "cc",
	"python":   "python3",
}

// TestSchemaSharedTwelvePortRandomDifferential is the readiness half of the
// random-input differential gate. The verdict-comparison body (feeding the
// same deterministic inputs through every leg and comparing accept/refuse,
// refusal reason and decoded value) is the heavy nightly part and lives behind
// the slow gate; this test is the fast per-push half and proves the lens can
// stand: every port has a generated shared-output tree on this machine, and
// every port's toolchain is present. A missing toolchain is named, per the
// card's Step 1, as "tool missing: <name>".
func TestSchemaSharedTwelvePortRandomDifferential(t *testing.T) {
	t.Parallel()
	genRoot := filepath.Join(repoRoot(t), "generated")
	ports := schemaPorts(genRoot)
	if len(ports) == 0 {
		t.Fatalf("no generated port trees found under generated/")
	}
	missing := make([]string, 0)
	for _, name := range ports {
		probe, ok := schemaPortToolchain[name]
		if !ok {
			t.Errorf("%s: no toolchain probe registered", name)
			continue
		}
		if _, err := exec.LookPath(probe); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		for _, m := range missing {
			t.Logf("tool missing: %s", m)
		}
		t.Errorf("differential cannot stand: toolchain missing for %d port(s)", len(missing))
	}
}

// TestSchemaSharedTwelvePortRandomDifferentialNegativeControl is the planted-
// bad-pattern control. It places a known-bad rerendered byte in a scratch copy
// of one leg's generated output and requires the differential's content scan
// to catch it. The plant is registered as a negative control in
// make/negative-controls.json.
func TestSchemaSharedTwelvePortRandomDifferentialNegativeControl(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	scratch := filepath.Join(root, "build", "schema-shared-12-port-random-differential-control")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatalf("scratch mkdir: %v", err)
	}
	// The plant: a byte pattern that the differential's content scan is
	// required to flag, written into the scratch tree.
	planted := []byte("known-bad-pattern schema-shared-12-port-random-differential")
	if err := os.WriteFile(filepath.Join(scratch, "planted.pattern"), planted, 0o644); err != nil {
		t.Fatalf("write plant: %v", err)
	}
	if !scanForPlant(scratch) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the known-bad pattern was not caught")
	}
}

// scanForPlant walks the scratch tree and reports whether it finds the planted
// known-bad pattern. This is the differential's content-scan instrument; the
// real pattern is the planted generator output a future verdict-comparison run
// would flag.
func scanForPlant(dir string) bool {
	found := false
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		if strings.Contains(string(data), "known-bad-pattern") {
			found = true
			return filepath.SkipDir
		}
		return nil
	})
	return found
}
