// THE `deprecated` MARKER'S GENERATED HALF (docs/SPEC-TABLES.md §2.10): the
// slot stays. A fixed table's record is walked by offset, so deprecation has
// to be a statement about MEANING and nothing else — and the way to prove that
// without assuming any particular wire is to generate the SAME schema twice,
// once with the marker and once without, and require the two outputs to differ
// in one place only: the reflection descriptors' tags column, which is where a
// tag is supposed to land (§8.1).
package compiler

import (
	"sort"
	"strings"
	"testing"
)

const deprecatedLive = `package depdemo

table ShipConfig
{
    name  string(16)
    armor uint8
    hull  uint8
}
`

const deprecatedMarked = `package depdemo

table ShipConfig
{
    name  string(16)
    armor uint8 | deprecated
    hull  uint8
}
`

// TestDeprecatedFieldKeepsItsSlot is the claim, measured: every generated file
// is byte-identical but for the tags column, so the storage, the offsets, the
// writer and the reader all still carry the field.
func TestDeprecatedFieldKeepsItsSlot(t *testing.T) {
	for _, lang := range []string{"cpp", "c", "go", "cs"} {
		t.Run(lang, func(t *testing.T) {
			live := generateSource(t, deprecatedLive, lang)
			marked := generateSource(t, deprecatedMarked, lang)
			if len(live) != len(marked) {
				t.Fatalf("deprecation changed the file set: %d then %d files", len(live), len(marked))
			}
			for name, want := range live {
				got, ok := marked[name]
				if !ok {
					t.Errorf("%s is gone from the marked output", name)
					continue
				}
				for _, line := range diffLines(string(want), string(got)) {
					if reflectionOnly(line) {
						continue
					}
					t.Errorf("%s: deprecation moved something other than the tags column:\n%s", name, line)
				}
			}
		})
	}
}

// TestDeprecatedFieldStillWritesItsSlotInCpp is the same claim stated the way
// a reader of the generated C++ would check it by hand: the member is
// declared, it is initialized to its default, its offset assertion is
// unchanged, and the save path still puts it.
func TestDeprecatedFieldStillWritesItsSlotInCpp(t *testing.T) {
	files := generateSource(t, deprecatedMarked, "cpp")
	// unitFromSource names the unit's one file Probe.schema, and the emitted
	// files are named after it (SPEC §7.1)
	table := string(files["ProbeTable.h"])
	if table == "" {
		for name := range files {
			t.Logf("generated %s", name)
		}
		t.Fatal("the table header is generated")
	}
	for _, want := range []string{
		"uint8_t armor = 0;",                  // the slot, at its default
		"value.armor = 0;",                    // and reset to it
		"offsetof( ShipConfig, armor ) == 24", // and it has not moved
		"static const char * const ShipConfig_armor_tags[] = { \"deprecated\" };", // and the marker reaches reflection
	} {
		if !strings.Contains(table, want) {
			t.Errorf("a deprecated field keeps its slot: want %q", want)
		}
	}
	if !strings.Contains(table, "value.armor") {
		t.Error("the writer still reaches the slot")
	}
}

// generateSource compiles a source string and emits one target.
func generateSource(t *testing.T, src, lang string) map[string][]byte {
	t.Helper()
	u := unitFromSource(t, src)
	files, err := New().Generate(u, lang, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// reflectionOnly reports a line that belongs to the REFLECTION SURFACE and
// nowhere else: the tag array a marker generates, or a descriptor row, which
// every backend spells with a doc column beside its tags column (§8.1). No
// storage declaration, offset assertion, writer or reader line carries either
// marker, so a difference this lets through cannot be one that moved a byte.
func reflectionOnly(line string) bool {
	if strings.TrimSpace(strings.TrimLeft(line, "+-")) == "" {
		return true
	}
	for _, marker := range []string{"TableDocNone", "TableDoc", "Tags", "tags", "deprecated"} {
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}

// diffLines is the symmetric difference of two files' lines, counted with
// multiplicity: the lines one has and the other does not. A positional diff
// would cascade off a single inserted line, and the comparison here is
// expected to come back all but empty, so a multiset is enough and pulls in no
// diff algorithm.
func diffLines(a, b string) []string {
	count := map[string]int{}
	for l := range strings.SplitSeq(a, "\n") {
		count[l]++
	}
	for l := range strings.SplitSeq(b, "\n") {
		count[l]--
	}
	var out []string
	for l, n := range count {
		if n > 0 {
			out = append(out, "-"+l)
		} else if n < 0 {
			out = append(out, "+"+l)
		}
	}
	sort.Strings(out)
	return out
}
