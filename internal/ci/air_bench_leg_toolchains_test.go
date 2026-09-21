package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAirBenchLegToolchainsRecord holds the darwin/arm64 air bench leg
// toolchain inventory to its claims (issue #1114). The record is an
// inventory only from the air bench (Apple M2 MacBook Air, darwin/arm64,
// 8 cores) taken twice — inside the wall and on the bare machine — and the
// finding is that three toolchains the machine has (go, cs, java) are
// nonetheless absent for a card. Without this gate the markdown file can be
// dropped or hollowed out and no `go test` goes red — the fleet's only
// darwin/arm64 record of which bench legs a card can actually build
// silently vanishes.
func TestAirBenchLegToolchainsRecord(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "docs", "measurements", "2026-09-18-air-bench-leg-toolchains.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("air bench leg toolchain inventory missing at %s (issue #1114): %v", path, err)
	}
	doc := string(data)

	// One program per leg: the red line names the leg's program, the green
	// line carries it.
	for _, program := range []string{
		"`c++`",
		"`cc`",
		"`go`",
		"`rustc`",
		"`dotnet`",
		"`node`",
		"`java`",
		"`dart`",
		"`elixir`",
	} {
		if !strings.Contains(doc, program) {
			t.Errorf("record does not carry the %s leg program (issue #1114)", program)
		}
	}

	// The bench identity: the air, darwin/arm64, eight cores.
	for _, marker := range []string{
		"Apple M2 MacBook Air",
		"darwin/arm64",
		"8 cores",
	} {
		if !strings.Contains(doc, marker) {
			t.Errorf("record does not carry the bench marker %q (issue #1114)", marker)
		}
	}

	// The finding: four legs build, the three refusals are one shape, and the
	// go remedy is the single extra read root.
	for _, marker := range []string{
		"Four of the nine legs build",
		"refused",
		"go1.27.1 darwin/arm64",
		"/opt/homebrew/Cellar/go/1.27.1",
	} {
		if !strings.Contains(doc, marker) {
			t.Errorf("record does not carry the finding marker %q (issue #1114)", marker)
		}
	}

	// It is an inventory only: no timing, no benchmark, not bench evidence.
	for _, marker := range []string{
		"no timing",
		"no benchmark",
		"not bench evidence",
	} {
		if !strings.Contains(doc, marker) {
			t.Errorf("record does not state it carries %q (issue #1114)", marker)
		}
	}
}
