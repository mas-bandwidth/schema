//go:build schema_leg_moo

// The moo table surface's compiler claims (internal/codegen/mootable;
// docs/PORT-MOO.md, THE LAYERS, layer 3). The target is behind the
// schema_leg_moo tag (compiler/target_moo.go), and so is this file: run it
// with `go test -tags schema_leg_moo ./compiler/` (make test-moo-tables).
package compiler

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
)

// TestMooTargetCarriesTheTableCorpus: --lang moo generates every unit of the
// tables conformance manifest, and every unit gets its table modules and the
// table runtime beside the packet modules.
func TestMooTargetCarriesTheTableCorpus(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	units := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 || fields[0] != "unit" {
			continue
		}
		var paths []string
		for _, p := range fields[2:] {
			paths = append(paths, filepath.Join("..", p))
		}
		paths, err := GatherPaths(paths)
		if err != nil {
			t.Fatal(err)
		}
		u, err := New().Load(paths)
		if err != nil {
			t.Fatalf("unit %s: %v", fields[1], err)
		}
		files, err := New().Generate(u, "moo", Options{})
		if err != nil {
			t.Errorf("unit %s: --lang moo refuses it: %v", fields[1], err)
			continue
		}
		if files["schema_tables.moo"] == nil || files["table_load.moo"] == nil || files["table_save.moo"] == nil {
			t.Errorf("unit %s: the table runtime is not emitted: %v", fields[1], sortedNames(files))
		}
		units++
	}
	if units == 0 {
		t.Fatal("the manifest named no unit")
	}
}

func sortedNames(files map[string][]byte) []string {
	var out []string
	for n := range files {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// TestMooRegistersItsTableCarriers holds the target's registrations to what
// internal/codegen/mootable's named tests prove (compiler/target_moo.go).
func TestMooRegistersItsTableCarriers(t *testing.T) {
	for name, list := range map[string][]string{
		"tableTargets": tableTargets, "tableArmTargets": tableArmTargets, "unionArrayTargets": unionArrayTargets,
		"blobTargets": blobTargets, "stringTargets": stringTargets, "mapTargets": mapTargets, "listTargets": listTargets,
		"optionalArrayTargets": optionalArrayTargets, "valueDefaultTargets": valueDefaultTargets, "wasRowTargets": wasRowTargets,
	} {
		if !slices.Contains(list, "moo") {
			t.Errorf("%s does not name moo", name)
		}
	}
	for _, c := range []string{carryTimestamp, carryIntegrity, carryTextList, carryTextListTotal, carryOneSidedRange, carryOptionalVariableClosure, carryOptionalMap} {
		if !carries("moo", c) {
			t.Errorf("moo does not register %s", c)
		}
	}
	for _, c := range []string{carryRequired, carryForms, carryUnboundedText, carryArrayRoot} {
		if carries("moo", c) {
			t.Errorf("moo registers %s, which no moo test proves yet", c)
		}
	}
}

// TestMooRefusesTheConstructsItDoesNotCarryByName: each construct the moo
// table surface does not carry yet is refused by name, naming the target.
func TestMooRefusesTheConstructsItDoesNotCarryByName(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"required", "package probe\ntable T\n{\n    a int32 | required\n}\n", "required"},
		{"forms", "package probe\ntable T | forms = \"wire\"\n{\n    a int32\n}\n", "forms"},
		{"unbounded text", "package probe\ntable T\n{\n    words []string\n}\n", "unbounded"},
		{"array root", "package probe\ntable G\n{\n    x int32\n}\ntable L | json = \"array\"\n{\n    goals []G\n}\n", "array"},
	} {
		_, err := New().Generate(unitFromSource(t, c.src), "moo", Options{})
		if err == nil || !strings.Contains(err.Error(), "moo") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want a refusal naming moo and %q, got %v", c.name, c.want, err)
		}
	}
	// the control: the same table without the construct generates
	if _, err := New().Generate(unitFromSource(t, "package probe\ntable T\n{\n    a int32\n}\n"), "moo", Options{}); err != nil {
		t.Errorf("the control, a plain table: %v", err)
	}
}

// TestTableRuntimeNamesAreClaimedMoo scans the emitted moo for every Table*
// identifier (comments stripped) and requires the set to equal what
// internal/tablenames registers for the moo backend, in both directions.
func TestTableRuntimeNamesAreClaimedMoo(t *testing.T) {
	src := "package probe\nenum E { a, b }\nunion U\n{\n    n uint32\n    s string(8)\n}\ntable Node\n{\n    next *Node\n    name *string\n    data *bytes\n    kids []*Node\n    tags map[uint32]int32\n    e E\n    u U\n    w wstring(4)\n    big int128 | min = 0, max = 9\n}\n"
	files, err := New().Generate(unitFromSource(t, src), "moo", Options{})
	if err != nil {
		t.Fatal(err)
	}
	ident := regexp.MustCompile(`\bTable[A-Za-z0-9_]*\b`)
	emitted := map[string]bool{}
	for name, data := range files {
		if !strings.HasSuffix(name, ".moo") {
			continue
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			if i := strings.Index(line, "--"); i >= 0 {
				line = line[:i]
			}
			for _, m := range ident.FindAllString(line, -1) {
				emitted[m] = true
			}
		}
	}
	for name := range emitted {
		if !tablenames.Registered(name) || tablenames.By(name)&tablenames.Moo == 0 {
			t.Errorf("the moo table surface emits %s and internal/tablenames does not register it for moo", name)
		}
	}
	for _, name := range tablenames.DefinedBy(tablenames.Moo) {
		if !emitted[name] {
			t.Errorf("internal/tablenames says the moo backend defines %s, but nothing emitted names it", name)
		}
	}
}
