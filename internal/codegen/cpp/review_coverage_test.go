// Branches of the C++ packet emitter that no test reached at base f28b1a58
// (docs/CPP-REFERENCE-REVIEW.md, part 2: the merged coverage over
// internal/codegen/cpp, cpptable, ci, goldens, fuzz and the compiler's C++
// tests left each at count 0). Each test emits the smallest unit that reaches
// the branch and pins what the branch writes.
package cpp

import (
	"strings"
	"testing"
)

// TestGenerateRefusesHeaderCycleAndWireCollision reaches Generate's two
// refusals (cpp.go:35, cpp.go:44): a file graph whose headers would include
// each other — types compose order-free across files, headers cannot — and a
// file named <Base>Wire beside <Base>, whose wire header it would overwrite.
func TestGenerateRefusesHeaderCycleAndWireCollision(t *testing.T) {
	t.Parallel()
	cycle := unitFromSources(t, map[string]string{
		"Alpha.schema": `package t
type AlphaOuter {
    inner BetaInner
}
type AlphaInner {
    x int32
}
`,
		"Beta.schema": `package t
type BetaOuter {
    inner AlphaInner
}
type BetaInner {
    y int32
}
`,
	})
	if _, err := Generate(cycle); err == nil || !strings.Contains(err.Error(), "include each other in a cycle") {
		t.Errorf("a two-file header cycle must be refused by name; got %v", err)
	} else if !strings.Contains(err.Error(), "Alpha -> Beta -> Alpha") {
		t.Errorf("the cycle refusal must print the cycle; got %v", err)
	}

	collide := unitFromSources(t, map[string]string{
		"Probe.schema": `package t
type Probe {
    x int32
}
`,
		"ProbeWire.schema": `package t
type Other {
    y int32
}
`,
	})
	if _, err := Generate(collide); err == nil || !strings.Contains(err.Error(), "schema files Probe and ProbeWire collide") {
		t.Errorf("Probe.schema beside ProbeWire.schema must be refused by name; got %v", err)
	}
}
