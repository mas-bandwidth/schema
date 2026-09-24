package slowtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryToolchainTestIsBehindTheGate is the red test for issue #1025: a
// package whose tests shell out to a foreign compiler costs seconds per test,
// every run, and a package full of them costs minutes. `go test ./compiler/`
// was 261 s and `go test ./internal/codegen/gotable/` 41 s on the card's own
// machine, against the owner's one-to-two-minute rule. The cause is not Go; it
// is cc, c++, dotnet and the Go toolchain, invoked by a test that never says
// it is about to spend somebody else's compiler.
//
// The gate the fix adds is internal/slowtest.Gate. This test holds the two
// lists together: every test source that reaches a foreign toolchain must
// carry a Gate call, so a new expensive test cannot land ungated and quietly
// push the per-commit run back over the rule.
func TestEveryToolchainTestIsBehindTheGate(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	scopes := []string{"compiler", filepath.Join("internal", "codegen", "gotable")}
	toolchain := []string{
		`exec.LookPath("cc")`,
		`exec.LookPath("c++")`,
		`exec.LookPath("clang")`,
		`exec.LookPath("g++")`,
		`exec.LookPath("dotnet")`,
		`exec.Command("go"`,
		"findDotnet",
	}

	scanned := 0
	for _, scope := range scopes {
		dir := filepath.Join(root, scope)
		paths, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(paths) == 0 {
			t.Fatalf("no test sources under %s — this gate would pass over an empty set", scope)
		}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			reaches := false
			for _, marker := range toolchain {
				if strings.Contains(text, marker) {
					reaches = true
					break
				}
			}
			if !reaches {
				continue
			}
			scanned++
			if !strings.Contains(text, "slowtest.Gate(") {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s reaches a foreign toolchain but never calls slowtest.Gate: a default `go test` pays for that compiler on every run (issue #1025)", rel)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("no toolchain test sources found — this gate is looking at the wrong text")
	}
	t.Logf("toolchain test sources held behind the gate: %d", scanned)
}
