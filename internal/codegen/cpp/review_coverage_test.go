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

// TestFlagAppendHelperOncePerFile reaches emitFlagAppendHelper's once-per-file
// guard (cpp.go:460): with two flags declarations in one file the bounded
// append pair is defined exactly once, inside its per-package guard.
func TestFlagAppendHelperOncePerFile(t *testing.T) {
	t.Parallel()
	u := unitFromSources(t, map[string]string{
		"Bits.schema": `package t
flags Caps { Jump, Crouch }
flags Moods { Calm, Angry, Sleepy }
`,
	})
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	h := string(files["Bits.h"])
	for _, def := range []string{
		"inline int SchemaFlagAppend_( char * buffer, int bufferSize, int position, const char * name )",
		"inline int SchemaFlagAppendHex_( char * buffer, int bufferSize, int position, uint64_t bits )",
		"#ifndef SCHEMA_T_FLAG_APPEND_DEFINED",
	} {
		if got := strings.Count(h, def); got != 1 {
			t.Errorf("Bits.h defines %q %d times, want once per file", def, got)
		}
	}
	for _, use := range []string{"FlagNamesCaps(", "FlagNamesMoods("} {
		if !strings.Contains(h, use) {
			t.Errorf("Bits.h lost %s", use)
		}
	}
}
