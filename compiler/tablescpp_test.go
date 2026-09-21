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
		for line := range strings.SplitSeq(stripCComments(string(data)), "\n") {
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

// keyedValueInitSrc carries BOTH halves #335 turns on: a keyed array whose
// element is a SELF-INITIALISING table (Cell) and one whose element is a
// SCALAR (int32). The two take opposite answers, so one probe cannot pass by
// agreeing with whichever the emitter happened to write.
const keyedValueInitSrc = `package probe

enum Kind { Alpha, Beta }

fixed table Cell
{
    score int32 = 7
    ratio float32 = 1.5
}

fixed table Board
{
    cells [Kind]Cell
}

fixed table Numbers
{
    cells [Kind]int32
}
`

// TestCppKeyedSelfInitializingSlotsCarryNoValueInit is the last #320 residual
// (#335): TableKeyed's slot array must NOT value-initialise itself for a
// self-initialising element type — cl expands a whole-array value-init element
// by element in its front end, at O(bytes) — while a scalar element, which has
// no member initializer of its own, keeps the ` = {}` that supplies its zero.
// `<Name>Reset` already fills both from one element (schema#322, docs/SPEC-TABLES.md §8.1).
func TestCppKeyedSelfInitializingSlotsCarryNoValueInit(t *testing.T) {
	header := tableHeader(t, keyedValueInitSrc)
	if strings.Contains(header, "T slots[kSlots] = {};") {
		t.Errorf("TableKeyed's slot array still carries the redundant `= {}` (#335)")
	}
	if !strings.Contains(header, "    T slots[kSlots];") {
		t.Errorf("TableKeyed's slot array is not the bare form (#335):\n%s", tableKeyedLines(header))
	}
	// the self-initialising element drops the member's `= {}`
	if !strings.Contains(header, "TableKeyed<Cell, Kind> cells; //") {
		t.Errorf("a keyed array of a self-initialising table lost its member or grew an initializer (#335):\n%s", tableKeyedLines(header))
	}
	// the NEGATIVE CONTROL: a scalar element cannot state its own zero, so the
	// member keeps the braces.
	if !strings.Contains(header, "TableKeyed<int32_t, Kind> cells = {}; //") {
		t.Errorf("a keyed array of a scalar dropped the zero its element cannot state:\n%s", tableKeyedLines(header))
	}
}

// tableKeyedLines returns the TableKeyed declarations a header carries, for a
// failure message that shows the shape rather than only the needle.
func tableKeyedLines(header string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(header, "\n") {
		if strings.Contains(line, "TableKeyed") || strings.Contains(line, "slots[kSlots]") {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String()
}
