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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
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
	for name, data := range tables {
		files[name] = data
	}
	var mods []string
	for name := range files {
		if strings.HasSuffix(name, ".goose") {
			mods = append(mods, strings.TrimSuffix(name, ".goose"))
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

// TestGooseFixedFormEvolution reads the reference's OLD files with the NEW
// build of an append-only widening (docs/FIXED-FORM-VERSIONING-TESTS.md,
// NEW-READS-OLD): every old value lands exactly, the appended tail lands its
// declared default, and the counters are zero.
func TestGooseFixedFormEvolution(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	cases := []struct {
		row, oldSchema, newSchema string
		root                      string
		capacity                  int
		print                     []string
		want                      string
	}{
		{
			row: "field_append", oldSchema: "test/tables/VOLD_field_append.schema", newSchema: "test/tables/VNEW_field_append.schema", root: "Lineage",
			print: []string{"values[0].X", "values[0].Y", "values[0].Z", "values[0].W"},
			want:  "1 ok 0 0 0 11 22 33 77",
		},
		{
			row: "enum_append", oldSchema: "test/tables/VOLD_enum_append.schema", newSchema: "test/tables/VNEW_enum_append.schema", root: "Lineage",
			print: []string{"values[0].Tier as i64", "values[0].Seq"},
			want:  "1 ok 0 0 0 3 9",
		},
		{
			row: "flags_append", oldSchema: "test/tables/VOLD_flags_append.schema", newSchema: "test/tables/VNEW_flags_append.schema", root: "Lineage",
			print: []string{"values[0].Caps", "values[0].Seq"},
			want:  "1 ok 0 0 0 3 20",
		},
		{
			row: "field_deprecate", oldSchema: "test/tables/VOLD_field_deprecate.schema", newSchema: "test/tables/VNEW_field_deprecate.schema", root: "Lineage",
			print: []string{"values[0].A", "values[0].B", "values[0].C"},
			want:  "1 ok 0 0 0 1 2 3",
		},
		{
			row: "range_widen", oldSchema: "test/tables/VOLD_range_widen.schema", newSchema: "test/tables/VNEW_range_widen.schema", root: "RangeWiden",
			print: []string{"values[0].Lead", "values[0].V", "values[0].Trail"},
			want:  "1 ok 0 0 0 2863311530 100 3149642683",
		},
		{
			row: "cfloat_res_refine", oldSchema: "test/tables/VOLD_cfloat_res_refine.schema", newSchema: "test/tables/VNEW_cfloat_res_refine.schema", root: "CfloatResRefine",
			print: []string{"values[0].Lead", "schema_rt::fixed_f32_bits(values[0].Aim)", "values[0].Trail"},
			want:  "1 ok 0 0 0 1 1050253722 2",
		},
		{
			row: "cfloat_range_widen", oldSchema: "test/tables/VOLD_cfloat_range_widen.schema", newSchema: "test/tables/VNEW_cfloat_range_widen.schema", root: "CfloatRangeWiden",
			print: []string{"values[0].Lead", "schema_rt::fixed_f32_bits(values[0].Aim)", "values[0].Trail"},
			want:  "1 ok 0 0 0 1 1056964608 2",
		},
		{
			row: "rename_without_was", oldSchema: "test/tables/VOLD_rename_without_was.schema", newSchema: "test/tables/VNEW_rename_without_was.schema", root: "Lineage",
			print: []string{"values[0].B", "values[0].Seq"},
			want:  "1 ok 0 0 0 31 26",
		},
		{
			row: "union_append", oldSchema: "test/tables/VOLD_union_append.schema", newSchema: "test/tables/VNEW_union_append.schema", root: "Lineage",
			print: []string{"values[0].Pick.Type as i64", "values[0].Pick.Alpha.M", "values[0].Seq"},
			want:  "1 ok 0 0 0 1 7 15",
		},
		{
			row: "int_widen", oldSchema: "test/tables/VOLD_int_widen.schema", newSchema: "test/tables/VNEW_int_widen.schema", root: "IntWiden", capacity: 3,
			print: []string{"values[0].V", "values[1].V", "values[2].V"},
			want:  "3 ok 0 3 0 -1 -32768 32767",
		},
		{
			row: "uint_widen", oldSchema: "test/tables/VOLD_uint_widen.schema", newSchema: "test/tables/VNEW_uint_widen.schema", root: "UintWiden",
			print: []string{"values[0].V"},
			want:  "1 ok 0 1 0 65535",
		},
		{
			row: "string_grow", oldSchema: "test/tables/VOLD_string_grow.schema", newSchema: "test/tables/VNEW_string_grow.schema", root: "StringGrow",
			print: []string{"values[0].TextLength", "values[0].Text[0]", "values[0].Text[7]"},
			want:  "1 ok 0 0 0 8 97 104",
		},
		{
			row: "bytes_grow", oldSchema: "test/tables/VOLD_bytes_grow.schema", newSchema: "test/tables/VNEW_bytes_grow.schema", root: "BytesGrow",
			print: []string{"values[0].BlobLength", "values[0].Blob[0]", "values[0].Blob[7]"},
			want:  "1 ok 0 0 0 8 240 247",
		},
		{
			row: "wstring_grow", oldSchema: "test/tables/VOLD_wstring_grow.schema", newSchema: "test/tables/VNEW_wstring_grow.schema", root: "WstringGrow",
			print: []string{"values[0].TextLength", "values[0].Text[0]", "values[0].Text[7]"},
			want:  "1 ok 0 0 0 8 104 65535",
		},
		{
			row: "array_bounded_grow", oldSchema: "test/tables/VOLD_array_bounded_grow.schema", newSchema: "test/tables/VNEW_array_bounded_grow.schema", root: "ArrayBoundedGrow",
			print: []string{"values[0].ValsCount", "values[0].Vals[0]", "values[0].Vals[3]", "values[0].Vals[7]"},
			want:  "1 ok 0 0 0 4 1000 1003 0",
		},
		{
			row: "array_fixed_grow", oldSchema: "test/tables/VOLD_array_fixed_grow.schema", newSchema: "test/tables/VNEW_array_fixed_grow.schema", root: "ArrayFixedGrow",
			print: []string{"values[0].Vals[0].X", "values[0].Vals[3].X", "values[0].Vals[4].X", "values[0].Vals[7].Y"},
			want:  "1 ok 0 0 0 -500 -503 7 9",
		},
		{
			row: "nested_append", oldSchema: "test/tables/VOLD_nested_append.schema", newSchema: "test/tables/VNEW_nested_append.schema", root: "Lineage",
			print: []string{"values[0].V.X", "values[0].V.Y", "values[0].V.Z", "values[0].V.W", "values[0].Seq"},
			want:  "1 ok 0 0 0 1 2 3 88 24",
		},
		{
			row: "enum_width", oldSchema: "test/tables/VOLD_enum_width.schema", newSchema: "test/tables/VNEW_enum_width.schema", root: "Lineage",
			print: []string{"values[0].Tier as i64", "values[0].Seq"},
			want:  "1 ok 0 1 0 200 12",
		},
		{
			row: "bits_grow", oldSchema: "test/tables/VOLD_bits_grow.schema", newSchema: "test/tables/VNEW_bits_grow.schema", root: "BitsGrow",
			print: []string{"values[0].V"},
			want:  "1 ok 0 0 0 255",
		},
		{
			row: "keyed_array_enum_append", oldSchema: "test/tables/VOLD_keyed_array_enum_append.schema", newSchema: "test/tables/VNEW_keyed_array_enum_append.schema", root: "Lineage",
			print: []string{"values[0].Slots[0].N", "values[0].Slots[2].N", "values[0].Slots[3].N", "values[0].Seq"},
			want:  "1 ok 0 0 0 101 103 7 22",
		},
		{
			row: "constant_grow", oldSchema: "test/tables/VOLD_constant_grow.schema", newSchema: "test/tables/VNEW_constant_grow.schema", root: "ConstantGrow",
			print: []string{"values[0].ValsCount", "values[0].Vals[0]", "values[0].Vals[7]"},
			want:  "1 ok 0 0 0 4 7000 0",
		},
		{
			row: "fixed_I_grow", oldSchema: "test/tables/VOLD_fixed_I_grow.schema", newSchema: "test/tables/VNEW_fixed_I_grow.schema", root: "FixedIGrow",
			print: []string{"values[0].V"},
			want:  "1 ok 0 1 0 -1",
		},
	}
	var compared atomic.Int64
	for _, c := range cases {
		c := c
		t.Run(c.row, func(t *testing.T) {
			t.Parallel()
			old := evolutionLoad(t, c.oldSchema)
			entry, ok := FixedEvolutionOf(old, c.root)
			if !ok {
				t.Fatalf("%s: no fixed root %s", c.oldSchema, c.root)
			}
			newer := evolutionLoad(t, c.newSchema)
			u := evolutionUnit{unit: newer, lineage: map[string][]FixedEvolutionEntry{c.root: {entry}}}
			data, err := os.ReadFile(filepath.Join(corpus, "old_"+c.row+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			capacity := c.capacity
			if capacity < 1 {
				capacity = 1
			}
			main := evolutionProgram(t, &u, c.root, dir, capacity, []string{"old.bin"}, c.print)
			got := joinedProbe(t, gooserun.Run(t, dir, main, map[string][]byte{"old.bin": data}))
			if got != c.want {
				t.Fatalf("%s: the NEW reader answered %q, want %q", c.row, got, c.want)
			}
			t.Logf("%s: %s", c.row, got)
			compared.Add(1)
		})
	}
	t.Cleanup(func() {
		t.Logf("%d evolution rows read the reference's OLD file", compared.Load())
	})
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
