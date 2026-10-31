package goosetable

// THE FIXED FORM'S EVOLUTION (docs/FIXED-FORM-VERSIONING-TESTS.md;
// docs/FIXED-FORM-ALGORITHM.md §5): a NEW build reads an OLD file through the
// lineage it was handed, and every refusal carries the Go engine's own name.
//
// The corpus files are the C++ reference's (make tables-fixedform-corpus) and
// the schema pairs are the ones it was dumped from, so the bytes under test
// are the reference's and the counters are docs/FIXED-FORM-VERSIONING-TESTS.md
// §5.4's. The reader is the NEW generation; the OLD layout is handed in with
// GenerateLineage, the lock played in one line.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// evolutionProgram emits the NEW unit's surfaces and a main that reads each
// input through <Root>FixedEvolveLoad, printing the verdict and the counters,
// then the record's fields through print (a Goose expression list).
func evolutionProgram(t *testing.T, u *evolutionUnit, root string, dir string, capacity int, inputs []string, print []string) string {
	t.Helper()
	files, err := goose.Generate(u.unit)
	if err != nil {
		t.Fatalf("goose type surface: %v", err)
	}
	tables, err := GenerateLineage(u.unit, u.lineage)
	if err != nil {
		t.Fatalf("goose table surface: %v", err)
	}
	maps.Copy(files, tables)
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	sort.Strings(mods)
	ns := goose.Namespace(u.unit)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString("\n" + gooserun.HexProbe + "\n")
	fmt.Fprintf(&b, "fn probe(name: const u8[:]) {\n")
	b.WriteString("    var bytes: u8[>..] = [];\n    if !read_file(name, bytes) { print(\"read-failed\"); return; }\n")
	fmt.Fprintf(&b, "    var values: %s::%s[%d] = [%s::%s { .. }; %d];\n", ns, root, capacity, ns, root, capacity)
	b.WriteString("    var report = schema_rt::FixedEvolveReport { .. };\n")
	fmt.Fprintf(&b, "    let n = %s::%sFixedEvolveLoad(values[0..%d], bytes, report);\n", ns, root, capacity)
	b.WriteString("    print(n, \" \", schema_rt::fixed_evolve_refusal_name(report.reason), \" \", report.clamped, \" \", report.widened, \" \", report.unknown);\n")
	b.WriteString("    if n < 0 { return; }\n")
	for _, p := range print {
		fmt.Fprintf(&b, "    print(\" \", %s);\n", p)
	}
	b.WriteString("}\n\nfn main() {\n")
	for _, in := range inputs {
		fmt.Fprintf(&b, "    probe(%q);\n", in)
	}
	b.WriteString("}\n")
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return "main.goose"
}

// evolutionUnit is a unit and the lineage the build is handed for it.
type evolutionUnit struct {
	unit    *ir.Unit
	lineage map[string][]FixedEvolutionEntry
}

func evolutionLoad(t *testing.T, schema string) *ir.Unit {
	t.Helper()
	path := filepath.Join(fixedRoot, filepath.FromSlash(schema))
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("load %s: %v", schema, err)
	}
	return u
}

// joinedProbe folds a probe's one input into one line: the verdict line then
// the field lines it printed (one print per field).
func joinedProbe(t *testing.T, out []byte) string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) == 0 {
		t.Fatalf("the probe printed nothing")
	}
	var parts []string
	for _, line := range lines {
		parts = append(parts, strings.TrimSpace(line))
	}
	return strings.Join(parts, " ")
}

// evolutionLines splits a probe's output into its one line per input.
func evolutionLines(t *testing.T, out []byte, want int) []string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != want {
		t.Fatalf("the probe printed %d lines, want %d:\n%s", len(lines), want, out)
	}
	return lines
}

// TestGooseFixedFormNamesEveryRefusal holds every refusal the evolution load
// can answer to the Go engine's own name (internal/codegen/gotable's
// TableRefuseReason): a hash in no entry, a retired entry below the floor, a
// known hash whose bytes differ, a damaged framing, and a short capacity.
func TestGooseFixedFormNamesEveryRefusal(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	old := evolutionLoad(t, "test/tables/VOLD_field_append.schema")
	entry, ok := FixedEvolutionOf(old, "Lineage")
	if !ok {
		t.Fatal("no fixed root Lineage in the old schema")
	}
	data, err := os.ReadFile(filepath.Join(corpus, "old_field_append.bin"))
	if err != nil {
		t.Fatal(err)
	}
	newer := evolutionLoad(t, "test/tables/VNEW_field_append.schema")
	live := evolutionUnit{unit: newer, lineage: map[string][]FixedEvolutionEntry{"Lineage": {entry}}}
	retiredEntry := entry
	retiredEntry.Retired = true
	retiredEntry.Reason = "the test retires the old layout"
	retired := evolutionUnit{unit: newer, lineage: map[string][]FixedEvolutionEntry{"Lineage": {retiredEntry}}}

	unknown := append([]byte(nil), data...)
	unknown[8] ^= 0x01
	malformed := append([]byte(nil), data...)
	// A layout byte under a KNOWN hash is layout_malformed (§1.1).
	malformed[20+4] ^= 0x01
	reserved := append([]byte(nil), data...)
	reserved[1] = 1
	truncated := data[:10]
	previous := append([]byte(nil), data...)
	previous[0] = 1
	newerForm := append([]byte(nil), data...)
	newerForm[0] = 9
	messageForm := append([]byte(nil), data...)
	messageForm[0] = 2
	noLayout := append([]byte(nil), data...)
	noLayout[20+len(entry.Layout)] ^= 0x01
	short := append(append([]byte(nil), data...), data[len(data)-20:]...)

	dir := t.TempDir()
	inputs := []string{"unknown.bin", "malformed.bin", "reserved.bin", "truncated.bin", "previous.bin", "newer.bin", "message.bin", "nolayout.bin"}
	main := evolutionProgram(t, &live, "Lineage", dir, 1, inputs, nil)
	lines := evolutionLines(t, gooserun.Run(t, dir, main, map[string][]byte{
		"unknown.bin": unknown, "malformed.bin": malformed, "reserved.bin": reserved, "truncated.bin": truncated,
		"previous.bin": previous, "newer.bin": newerForm, "message.bin": messageForm, "nolayout.bin": noLayout,
	}), len(inputs))

	retireDir := t.TempDir()
	retireMain := evolutionProgram(t, &retired, "Lineage", retireDir, 1, []string{"old.bin"}, nil)
	retireLine := evolutionLines(t, gooserun.Run(t, retireDir, retireMain, map[string][]byte{"old.bin": data}), 1)

	shortDir := t.TempDir()
	shortMain := evolutionProgram(t, &live, "Lineage", shortDir, 1, []string{"old.bin"}, nil)
	shortLine := evolutionLines(t, gooserun.Run(t, shortDir, shortMain, map[string][]byte{"old.bin": short}), 1)

	want := []struct {
		line, reason string
	}{
		{lines[0], "layout_unknown"},
		{lines[1], "layout_malformed"},
		{lines[2], "reserved_not_zero"},
		{lines[3], "truncated"},
		{lines[4], "previous_form"},
		{lines[5], "newer_form"},
		{lines[6], "message_form_as_file"},
		{lines[7], "no_layout"},
		{retireLine[0], "layout_unsupported"},
		{shortLine[0], "batch_too_large"},
	}
	for i, w := range want {
		fields := strings.Fields(w.line)
		if len(fields) < 2 || fields[1] != w.reason {
			t.Fatalf("refusal %d answered %q, want the Go name %q", i, w.line, w.reason)
		}
		t.Logf("refusal %d: %s", i, w.reason)
	}
}

// TestGooseFixedFormKindMismatchCensus pins the other half of §5.4's census:
// a named field whose KIND moved outside the widening ladder is dropped and
// counted once per FIELD per peer — like unknown, never once per array element.
// The pair is deliberately unlawful (the lock refuses it), so the lineage
// entry is handed in from the OLD build, exactly as unknown_census does it.
func TestGooseFixedFormKindMismatchCensus(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	old := evolutionLoad(t, "test/tables/VOLD_unknown_census.schema")
	entry, ok := FixedEvolutionOf(old, "Census")
	if !ok {
		t.Fatal("no fixed root Census in the old schema")
	}
	dirSchema := t.TempDir()
	path := filepath.Join(dirSchema, "VNEW_kind_mismatch.schema")
	const text = `package vnew_kind_mismatch

fixed table Item
{
    a    int32 = 0
    drop string(8)
}

fixed table Census
{
    lead  uint32 = 1
    items [4]Item
    trail uint32 = 2
}
`
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	newer, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("load kind_mismatch: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(corpus, "old_unknown_census.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := evolutionUnit{unit: newer, lineage: map[string][]FixedEvolutionEntry{"Census": {entry}}}
	out := t.TempDir()
	main := evolutionProgram(t, &u, "Census", out, 1, []string{"old.bin"}, []string{
		"values[0].Lead", "values[0].Items[0].A", "values[0].Items[1].A",
		"values[0].Items[2].A", "values[0].Items[3].A", "values[0].Trail", "report.kind_mismatch",
	})
	got := joinedProbe(t, gooserun.Run(t, out, main, map[string][]byte{"old.bin": data}))
	const want = "1 ok 0 0 0 1 10 11 12 13 2 1"
	if got != want {
		t.Fatalf("kind_mismatch: the NEW reader answered %q, want %q", got, want)
	}
	t.Logf("kind_mismatch: %s", got)
}
