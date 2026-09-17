// The tables tests of ONE language, in its own file so a port adds a file and
// edits no shared one (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"maps"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
)

// cppRuntimeIdent is the C++ leg's runtime-name scan: the spellings the C++
// table emitter defines at namespace scope. It is the union of the PascalCase
// Table* family, the snake_case table_* helpers, the announcement vocabulary
// and the unit's BuildVersion — the same four families internal/tablenames
// registers for this backend.
var cppRuntimeIdent = regexp.MustCompile(`\b(?:Table[A-Za-z0-9_]*|table_[a-z0-9_]+|Announce(?:Measure|Read)?|BuildVersion)\b`)

// cppEmittedNames collects the scan's answer over one map of generated C++.
// It reads the two TABLE sources and the two BLOCK sources, strips /* */ and
// // comments first — prose is not an identifier — and returns every runtime
// spelling it sees.
func cppEmittedNames(files map[string][]byte) map[string]bool {
	emitted := map[string]bool{}
	for name, data := range files {
		if !strings.HasSuffix(name, "Table.h") && !strings.HasSuffix(name, "Table.cpp") &&
			!strings.HasSuffix(name, "Block.h") && !strings.HasSuffix(name, "Block.cpp") {
			continue
		}
		for _, line := range strings.Split(stripCComments(string(data)), "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			for _, m := range cppRuntimeIdent.FindAllString(line, -1) {
				emitted[m] = true
			}
		}
	}
	return emitted
}

// TestCppRuntimeNameScanGoesRed is the C++ scan's own NEGATIVE CONTROL, and it
// is the shape TestJavaRuntimeNameScanGoesRed uses on the Java leg: a scan
// that has gone blind passes every registry it is pointed at, so the only way
// to know it still sees is to hand it a name nobody registered and require it
// to say so.
//
// The probe is injected into a COPY of the emitted text, in a shape the
// emitter does not use — a bare namespace-scope struct declaration — which is
// exactly the case a shape-dependent scan would miss.
//
// NOTE ON THE C++ LEG (schema#414). Unlike C, C#, Go, Rust, Java, Dart and
// Elixir, the C++ leg carries no positive "every emitted Table* name is
// registered" scan in this tree: the C++ target's namespace-scope surface is
// broader than internal/tablenames currently registers, so this control is
// carried self-contained. It guards the collector above — the crude
// identifier scan the register's other honesty gates share — so that the day
// the positive C++ scan lands, its control is already here and already
// pointed at a name the registry does not hold.
func TestCppRuntimeNameScanGoesRed(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, runtimeSrc), "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	sabotaged := map[string][]byte{}
	maps.Copy(sabotaged, files)
	header, ok := sabotaged["ProbeTable.h"]
	if !ok {
		t.Fatal("--lang cpp emitted no ProbeTable.h for the runtime unit")
	}
	sabotaged["ProbeTable.h"] = append([]byte("namespace probe\n{\n    struct TableProbe {};\n}\n\n"), header...)

	emitted := cppEmittedNames(sabotaged)
	if !emitted["TableProbe"] {
		t.Fatal("the scan did not see a namespace-scope TableProbe — it is blind, and every green run proves nothing")
	}
	if tablenames.Registered("TableProbe") {
		t.Fatal("TableProbe is registered, so the control proves nothing — pick a name the registry does not hold")
	}
}
