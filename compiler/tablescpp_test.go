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
// Table* family (and the kTable* constants beside it), the snake_case table_*
// helpers, the announcement vocabulary and the unit's BuildVersion — the same
// families internal/tablenames registers for this backend. The refusal
// vocabulary (ok, not_a_cook, layout_newer ...) has no common prefix and is
// read from the enum bodies and constexpr declarations instead: see
// cppEmittedNames.
var cppRuntimeIdent = regexp.MustCompile(`\b(?:k?Table[A-Za-z0-9_]*|table_[a-z0-9_]+|Announce(?:Measure|Read)?|BuildVersion)\b`)

// cppUnscopedEnum matches an UNSCOPED enum's body. Its enumerators sit at
// namespace scope, so the refusal vocabulary's lowercase spellings are claims
// no prefix scan can see; a scoped `enum class` is not matched, because its
// enumerators are reached through the type and claim nothing.
var cppUnscopedEnum = regexp.MustCompile(`(?s)\benum\s+(?:[A-Za-z_][A-Za-z0-9_]*\s*)?(?::\s*[a-z0-9_]+\s*)?\{(.*?)\}`)

var cppEnumerator = regexp.MustCompile(`(?m)^\s*([A-Za-z_][A-Za-z0-9_]*)\b`)

// cppLowercaseConstant matches a lowercase namespace-scope constant, the other
// shape the refusal vocabulary is declared in (`constexpr TableRefuseReason
// no_vocabulary = ...`). Its leading lowercase is the point: a constexpr named
// for a schema declaration (ConfigFixedHash) is derived from the author's own
// name and claims nothing here.
var cppLowercaseConstant = regexp.MustCompile(`(?m)^constexpr\s+\w+\s+([a-z_]\w*)\s*=`)

// cppEmittedNames collects the scan's answer over one map of generated C++.
// It reads the two TABLE sources and the two BLOCK sources, strips /* */ and
// // comments first — prose is not an identifier — and returns every runtime
// spelling it sees: each Table*-family identifier whatever surrounds it (the
// scan recognises no declaration syntax, so over-collection is the only error
// it can make, and that error is safe), then the enumerators of every unscoped
// enum and every lowercase namespace-scope constant.
func cppEmittedNames(files map[string][]byte) map[string]bool {
	emitted := map[string]bool{}
	for name, data := range files {
		if !strings.HasSuffix(name, "Table.h") && !strings.HasSuffix(name, "Table.cpp") &&
			!strings.HasSuffix(name, "Block.h") && !strings.HasSuffix(name, "Block.cpp") {
			continue
		}
		lines := strings.Split(stripCComments(string(data)), "\n")
		for i, line := range lines {
			if j := strings.Index(line, "//"); j >= 0 {
				line = line[:j]
			}
			// an #include names the unit's own FILE (TablesTable.h for a
			// Tables.schema), which is the schema's spelling and claims nothing
			if strings.HasPrefix(strings.TrimSpace(line), "#include") {
				line = ""
			}
			lines[i] = line
			for _, m := range cppRuntimeIdent.FindAllString(line, -1) {
				emitted[m] = true
			}
		}
		text := strings.Join(lines, "\n")
		for _, body := range cppUnscopedEnum.FindAllStringSubmatch(text, -1) {
			for _, e := range cppEnumerator.FindAllStringSubmatch(body[1], -1) {
				emitted[e[1]] = true
			}
		}
		for _, m := range cppLowercaseConstant.FindAllStringSubmatch(text, -1) {
			emitted[m[1]] = true
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
// The positive scan this control guards is TestCppTableRuntimeNamesAreClaimed
// (compiler/tablescppnames_test.go), which reads its names through the same
// collector.
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
	sabotaged["ProbeTable.h"] = append([]byte("namespace probe\n{\n    struct TableProbe {};\n    static const int kTableControlProbe = 1;\n    enum ControlVerdict { control_refusal_value };\n}\nconstexpr ControlVerdict control_constant = (ControlVerdict) 7;\n\n"), header...)

	// the four shapes the scan reads: a Table* type, a kTable* constant, an
	// unscoped enumerator, and a lowercase constexpr. A scan blind to any one of
	// them is blind to a whole family of the real runtime.
	emitted := cppEmittedNames(sabotaged)
	for _, name := range []string{"TableProbe", "kTableControlProbe", "control_refusal_value", "control_constant"} {
		if !emitted[name] {
			t.Fatalf("the scan did not see a namespace-scope %s — it is blind, and every green run proves nothing", name)
		}
		if tablenames.Registered(name) {
			t.Fatalf("%s is registered, so the control proves nothing — pick a name the registry does not hold", name)
		}
	}

	// and the PACKET half of the scan, by the same rule: a name the packet
	// emitter does not spell, planted as a macro in a copy of a packet header,
	// must be seen and must be one the vocabulary does not account for.
	packet, packages := cppCorpus(t)
	sabotagedPacket := map[string][]byte{}
	maps.Copy(sabotagedPacket, packet)
	var key string
	for k := range packet {
		if strings.HasSuffix(k, "/Tables.h") {
			key = k
			break
		}
	}
	if key == "" {
		t.Fatal("the corpus emitted no Tables.h to plant a packet probe in")
	}
	sabotagedPacket[key] = append([]byte("#define SCHEMA_CONTROL_PROBE 1\nstatic const int schema_control_probe = 1;\n"), packet[key]...)
	seen := cppPacketEmittedNames(sabotagedPacket, packages)
	for _, name := range []string{"SCHEMA_CONTROL_PROBE", "schema_control_probe"} {
		if !seen[name] {
			t.Fatalf("the packet scan did not see a planted %s — it is blind, and every green run proves nothing", name)
		}
		if _, ok := cppPacketVocabulary[name]; ok {
			t.Fatalf("%s is in the packet vocabulary, so the control proves nothing — pick a name it does not hold", name)
		}
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
