package rubytable_test

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rubytable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	maps.Copy(rubyFixedSetups, map[string]string{
		"retirement":       "rows[1].a = 10\nrows[1].b = 20\nrows[1].c = 30",
		"retired-runtime":  "rows[1].x = 44\nrows[1].y = 55\nrows[1].z = 66",
		"field-evolution":  "rows[1].b = 100\nrows[1].c = 200",
		"plan-execution":   "rows[1].a = 10\nrows[1].sub.p = 30\nrows[1].sub.q = 40\nrows[1].xs = [50, 60]",
		"interoperability": "rows[1].a = 42\nrows[1].s = \"test\"",
	})
}

var rubyEvolutionRows = []string{"field-evolution", "retirement", "retired-runtime", "plan-execution", "interoperability", "file-envelope"}

func TestRubyFixedProofEvolution(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Ruby interpreters and C++ fixed-form corpus")
	fixedproof.RunEvolutionRows(t, filepath.Join("..", "..", ".."), rubyFixedProofRunner{}, rubyEvolutionRows...)
}

func TestRubyFixedProofEvolutionValues(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Ruby interpreters and live C++ oracle")
	fixedproof.RunValueRows(t, rubyFixedProofRunner{}, "field-evolution", "retirement", "retired-runtime", "plan-execution", "interoperability")
}

func TestRubyFixedProofEvolutionNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Ruby interpreters and live C++ oracle")
	fixedproof.RunNegativeControlRows(t, rubyFixedProofRunner{}, "field-evolution", "retirement", "retired-runtime", "plan-execution", "interoperability")
}

func TestRubyFixedProofEvolutionStrict(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned RuboCop and Steep")
	for _, id := range rubyEvolutionRows {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			allCases := fixedproof.Cases()
			if len(allCases) == 0 {
				t.Fatal("fixedproof.Cases() is empty: the loop below would check nothing")
			}
			for _, tc := range allCases {
				if tc.ID != id {
					continue
				}
				u := fixedproof.ParseCheckUnit(t, tc.Schema)
				files, err := rubytable.Generate(u)
				if err != nil {
					t.Fatal(err)
				}
				dir := rubyFixedTree(t, files)
				rubyrun.Analyze(t, dir, u.Package)
			}
		})
	}
}

func TestRubyFixedProofEvolutionNumeric(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Ruby interpreters and live C++ oracle")
	oldSchema := "package probe\nfixed table T {\n v int16\n f float32\n}\n"
	newSchema := "package probe\nfixed table T {\n v int32\n f float64\n added int32 = 77\n}\n"
	setup := "value[0].v = -7; value[0].f = 1.5f;"
	old := fixedproof.ParseCheckUnit(t, oldSchema)
	newer := fixedproof.ParseCheckUnit(t, newSchema)
	oldCase := fixedproof.RowCase{ID: "evolution-old", Schema: oldSchema, Root: "T", Body: 6, Count: 1, CppSetup: setup}
	newCase := fixedproof.RowCase{ID: "evolution-new", Schema: newSchema, Root: "T", Body: 16, Count: 1, CppSetup: setup}
	oldWire, verifyOld := fixedproof.CppOracle(t, oldCase, old)
	newWire, verifyNew := fixedproof.CppOracle(t, newCase, newer)
	entry, ok := fixedproof.LineageEntryOf(old, "T")
	if !ok {
		t.Fatal("missing writer layout")
	}
	lineage := map[string][]rubytable.FixedLineageEntry{"T": {{Wire: entry.Wire, Layout: entry.Layout, Record: entry.Record, Digest: entry.Digest, Writer: old.Tables["T"]}}}
	files, err := rubytable.GenerateLineage(newer, lineage)
	if err != nil {
		t.Fatal(err)
	}
	script := `require_relative "probe"
r = Probe::LoadReport.new
rows = Probe::T.fixed_load(File.binread("old"), report: r)
abort("numeric_evolution_control") if rows.length != 1 || rows[0].v != -7 || rows[0].f != 1.5 || rows[0].added != 77 || r.widened != 2
File.binwrite("saved", Probe::T.fixed_save(rows))
puts "numeric-evolution"
`
	out, saved := rubyFixedRun(t, files, script, map[string][]byte{"old": oldWire})
	if strings.TrimSpace(out) != "numeric-evolution" || !bytes.Equal(saved, newWire) {
		t.Fatal("widened values or saved bytes differ from C++")
	}
	if err := verifyOld(oldWire); err != nil || verifyNew(saved) != nil {
		t.Fatal("C++ oracle rejected wire")
	}
	dir := rubyFixedTree(t, files)
	rubyrun.Analyze(t, dir, "probe")

	rpath := "probe/_fixed.rb"
	source := string(files[rpath])
	anchor := `_s_fixed_int(source, src, size, op["signed"])`
	if strings.Count(source, anchor) != 1 {
		t.Fatal("signed widening control anchor changed")
	}
	sabotaged := maps.Clone(files)
	sabotaged[rpath] = []byte(strings.Replace(source, anchor, `_s_fixed_int(source, src, size, false)`, 1))
	for _, pin := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
		sdir := rubyFixedTree(t, sabotaged)
		_, stderr, rerr := rubyrun.Try(t, pin, sdir, "main.rb", map[string][]byte{"main.rb": []byte("# frozen_string_literal: true\n" + script), "old": oldWire})
		if rerr == nil || !strings.Contains(stderr, "numeric_evolution_control") {
			t.Fatalf("signed widening control did not go red by name: %v\n%s", rerr, stderr)
		}
	}
}

func TestRubyFixedNumericLadders(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters, Steep strict, and live C++")
	oldSchema := `package probe
fixed table T {
 s8 int8
 s16 int16
 s32 int32
 u8 uint8
 u16 uint16
 u32 uint32
 p8 fixed(4,4) | min = -8, max = 7
 p16 fixed(12,4) | min = -8, max = 7
 p32 fixed(28,4) | min = -8, max = 7
 p64 fixed(60,4) | min = -8, max = 7
 q8 ufixed(4,4) | min = 0, max = 15
 q16 ufixed(12,4) | min = 0, max = 4095
 q32 ufixed(28,4) | min = 0, max = 268435455
 q64 ufixed(60,4) | min = 0, max = 1152921504606846975
}
`
	newSchema := `package probe
fixed table T {
 s8 int16
 s16 int32
 s32 int64
 u8 uint16
 u16 uint32
 u32 uint64
 p8 fixed(12,4) | min = -8, max = 7
 p16 fixed(28,4) | min = -8, max = 7
 p32 fixed(60,4) | min = -8, max = 7
 p64 fixed(124,4) | min = -8, max = 7
 q8 ufixed(12,4) | min = 0, max = 15
 q16 ufixed(28,4) | min = 0, max = 4095
 q32 ufixed(60,4) | min = 0, max = 268435455
 q64 ufixed(124,4) | min = 0, max = 1152921504606846975
 added int32 = 77
}
`
	setup := `value[0].s8 = -127; value[0].s16 = -32768; value[0].s32 = -2147483647 - 1; value[0].u8 = 255; value[0].u16 = 65535; value[0].u32 = 0xffffffffu; value[0].p8 = -1; value[0].p16 = -1; value[0].p32 = -1; value[0].p64 = -1; value[0].q8 = 240; value[0].q16 = 65520; value[0].q32 = 0xfffffff0u; value[0].q64 = 0xfffffffffffffff0ull;`
	old, newer := fixedproof.ParseCheckUnit(t, oldSchema), fixedproof.ParseCheckUnit(t, newSchema)
	_, _, oldBody := fixedproof.RootFacts(old.Tables["T"])
	_, _, newBody := fixedproof.RootFacts(newer.Tables["T"])
	oldWire, verifyOld := fixedproof.CppOracle(t, fixedproof.RowCase{ID: "ladder-old", Root: "T", Body: oldBody, Count: 1, CppSetup: setup}, old)
	newWire, verifyNew := fixedproof.CppOracle(t, fixedproof.RowCase{ID: "ladder-new", Root: "T", Body: newBody, Count: 1, CppSetup: setup}, newer)
	entry, ok := fixedproof.LineageEntryOf(old, "T")
	if !ok {
		t.Fatal("missing historical writer")
	}
	files, err := rubytable.GenerateLineage(newer, map[string][]rubytable.FixedLineageEntry{"T": {{Wire: entry.Wire, Layout: entry.Layout, Record: entry.Record, Digest: entry.Digest, Writer: old.Tables["T"]}}})
	if err != nil {
		t.Fatal(err)
	}
	rubyrun.Analyze(t, rubyFixedTree(t, files), "probe")
	script := `require_relative "probe"
report = Probe::LoadReport.new
rows = Probe::T.fixed_load(File.binread("old"), report: report)
r = rows[0]
abort("ladder_control") if [r.s8, r.s16, r.s32, r.u8, r.u16, r.u32, r.added, report.widened] != [-127, -32768, -2147483648, 255, 65535, 4294967295, 77, 14]
abort("ladder_control") if [r.p8, r.p16, r.p32, r.p64, r.q8, r.q16, r.q32, r.q64] != [-1, -1, -1, -1, 240, 65520, 4294967280, 18446744073709551600]
File.binwrite("saved", Probe::T.fixed_save(rows))
puts "ladder"
`
	out, saved := rubyFixedRun(t, files, script, map[string][]byte{"old": oldWire})
	if strings.TrimSpace(out) != "ladder" || !bytes.Equal(saved, newWire) || verifyOld(oldWire) != nil || verifyNew(saved) != nil {
		t.Fatal("widened values or current bytes differ from C++")
	}
}

type rubyFixedKindDecl struct {
	name, decl, setup string
	family            string
	rung              int
}

var rubyFixedKindDecls = []rubyFixedKindDecl{
	{"i8", "int8", "-128", "int", 0},
	{"i16", "int16", "-32768", "int", 1},
	{"i32", "int32", "-2147483647 - 1", "int", 2},
	{"i64", "int64", "-9223372036854775807ll - 1", "int", 3},
	{"i128", "int128 | min = -100, max = 100", "-5", "int128", 0},
	{"u8", "uint8", "255", "uint", 0},
	{"u16", "uint16", "65535", "uint", 1},
	{"u32", "uint32", "0xffffffffu", "uint", 2},
	{"u64", "uint64", "0xffffffffffffffffull", "uint", 3},
	{"u128", "uint128", "~(serialize::uint128_t)0", "uint128", 0},
	{"f32", "float32", "-1234.5625f", "float", 0},
	{"f64", "float64", "-1234.5625", "float", 1},
	{"x8", "fixed(4,4) | min = -8, max = 7", "-128", "fixed", 0},
	{"x16", "fixed(12,4) | min = -8, max = 7", "-128", "fixed", 1},
	{"x32", "fixed(28,4) | min = -8, max = 7", "-128", "fixed", 2},
	{"x64", "fixed(60,4) | min = -8, max = 7", "-128", "fixed", 3},
	{"x128", "fixed(124,4) | min = -8, max = 7", "-128", "fixed", 4},
	{"q8", "ufixed(4,4) | min = 0, max = 15", "240", "ufixed", 0},
	{"q16", "ufixed(12,4) | min = 0, max = 15", "240", "ufixed", 1},
	{"q32", "ufixed(28,4) | min = 0, max = 15", "240", "ufixed", 2},
	{"q64", "ufixed(60,4) | min = 0, max = 15", "240", "ufixed", 3},
	{"q128", "ufixed(124,4) | min = 0, max = 15", "240", "ufixed", 4},
}

func TestRubyFixedProofEvolutionKindPairs(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned Ruby interpreters and live C++ kind-pair oracle")
	type writerArtifact struct {
		decl  rubyFixedKindDecl
		unit  *ir.Unit
		entry fixedproof.LineageEntry
		wire  []byte
	}
	writers := make([]writerArtifact, len(rubyFixedKindDecls))
	for i, d := range rubyFixedKindDecls {
		src := "package probe\nfixed table T {\n lead uint32 = 1\n v " + d.decl + "\n trail uint32 = 2\n}\n"
		u := fixedproof.ParseCheckUnit(t, src)
		_, _, body := fixedproof.RootFacts(u.Tables["T"])
		wire, _ := fixedproof.CppOracle(t, fixedproof.RowCase{ID: "kind-" + d.name, Root: "T", Body: body, Count: 1, CppSetup: "value[0].v = " + d.setup + ";"}, u)
		entry, _ := fixedproof.LineageEntryOf(u, "T")
		writers[i] = writerArtifact{decl: d, unit: u, entry: entry, wire: wire}
	}
	for _, r := range writers {
		t.Run(r.decl.name, func(t *testing.T) {
			t.Parallel()
			var entries []rubytable.FixedLineageEntry
			var fpEntries []fixedproof.LineageEntry
			inputs := map[string][]byte{}
			var names []string
			for _, w := range writers {
				if w.decl.name == r.decl.name {
					continue
				}
				entries = append(entries, rubytable.FixedLineageEntry{Wire: w.entry.Wire, Layout: w.entry.Layout, Record: w.entry.Record, Digest: w.entry.Digest, Writer: w.unit.Tables["T"]})
				fpEntries = append(fpEntries, w.entry)
				inputs[w.decl.name] = w.wire
				names = append(names, w.decl.name)
			}
			inputs["names"] = []byte(strings.Join(names, "\n"))
			files, err := rubytable.GenerateLineage(r.unit, map[string][]rubytable.FixedLineageEntry{"T": entries})
			if err != nil {
				t.Fatal(err)
			}
			script := `require_relative "probe"
File.read("names").split.each do |name|
  wire = File.binread(name)
  report = Probe::LoadReport.new
  begin
    rows = Probe::T.fixed_load(wire, report: report)
  rescue Probe::Refused => err
    puts "#{name} refused #{err.reason}"
    next
  end
  saved = Probe::T.fixed_save(rows)
  File.binwrite("saved_#{name}", saved)
  puts "#{name} ok #{rows.length} #{report.widened}"
end
`
			cppReader := fixedproof.CppLineageReader(t, "ruby-kind-to-"+r.decl.name, r.unit, "T", fpEntries, 1)
			for _, pin := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
				dir := rubyFixedTree(t, files)
				in := maps.Clone(inputs)
				in["main.rb"] = []byte("# frozen_string_literal: true\n" + script)
				out, stderr, rerr := rubyrun.Try(t, pin, dir, "main.rb", in)
				if rerr != nil || stderr != "" {
					t.Fatalf("%s: %v\n%s\n%s", r.decl.name, rerr, out, stderr)
				}
				lines := strings.Split(strings.TrimSpace(out), "\n")
				byName := map[string]string{}
				for _, ln := range lines {
					parts := strings.Fields(ln)
					if len(parts) >= 2 {
						byName[parts[0]] = strings.Join(parts[1:], " ")
					}
				}
				for _, w := range writers {
					if w.decl.name == r.decl.name {
						continue
					}
					cppV := cppReader(w.wire)
					got := byName[w.decl.name]
					wantWidens := w.decl.family == r.decl.family && r.decl.rung > w.decl.rung
					if cppV.Refused != !wantWidens {
						t.Fatalf("%s -> %s: C++ verdict %v disagrees with ladder %v", w.decl.name, r.decl.name, cppV, wantWidens)
					}
					if wantWidens && r.decl.rung == 4 {
						_, _, body := fixedproof.RootFacts(r.unit.Tables["T"])
						canonical, _ := fixedproof.CppOracle(t, fixedproof.RowCase{ID: "kind-" + w.decl.name + "-as-" + r.decl.name, Root: "T", Body: body, Count: 1, CppSetup: "value[0].v = " + w.decl.setup + ";"}, r.unit)
						cppV.Saved = canonical
					}
					if cppV.Refused {
						if !strings.HasPrefix(got, "refused ") {
							t.Fatalf("%s -> %s: ruby %q, want refused", w.decl.name, r.decl.name, got)
						}
					} else {
						wantLine := fmt.Sprintf("ok %d %d", cppV.Count, cppV.Widened)
						if got != wantLine {
							t.Fatalf("%s -> %s: ruby %q, want %q", w.decl.name, r.decl.name, got, wantLine)
						}
						saved, serr := os.ReadFile(filepath.Join(dir, "saved_"+w.decl.name))
						if serr != nil || !bytes.Equal(saved, cppV.Saved) {
							t.Fatalf("%s -> %s: saved bytes differ from C++", w.decl.name, r.decl.name)
						}
					}
				}
			}
		})
	}
}

func TestRubyFixedProofEvolutionKindPairsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters and the live C++ reference")
	from, to := rubyFixedKindDecls[0], rubyFixedKindDecls[1]
	old := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T {\n lead uint32 = 1\n v "+from.decl+"\n trail uint32 = 2\n}\n")
	newer := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T {\n lead uint32 = 1\n v "+to.decl+"\n trail uint32 = 2\n}\n")
	_, _, body := fixedproof.RootFacts(old.Tables["T"])
	wire, _ := fixedproof.CppOracle(t, fixedproof.RowCase{ID: "ruby-kind-ctrl-old", Root: "T", Body: body, Count: 1, CppSetup: "value[0].v = " + from.setup + ";"}, old)
	entry, _ := fixedproof.LineageEntryOf(old, "T")
	older := []fixedproof.LineageEntry{entry}
	cppV := fixedproof.CppLineageReader(t, "ruby-kind-ctrl", newer, "T", older, 1)(wire)
	files, err := rubytable.GenerateLineage(newer, rubyFixedLineage(map[string][]fixedproof.LineageEntry{"T": older}))
	if err != nil {
		t.Fatal(err)
	}
	sabotaged := maps.Clone(files)
	rpath := "probe/_fixed.rb"
	anchor := `_s_fixed_int(source, src, size, op["signed"])`
	if strings.Count(string(sabotaged[rpath]), anchor) != 1 {
		t.Fatal("kind-pair sign anchor changed")
	}
	sabotaged[rpath] = []byte(strings.Replace(string(sabotaged[rpath]), anchor, `_s_fixed_int(source, src, size, false)`, 1))
	probe := `require_relative "probe"
rows = Probe::T.fixed_load(File.binread("wire"))
File.binwrite("saved", Probe::T.fixed_save(rows))
`
	_, badSaved := rubyFixedRun(t, sabotaged, probe, map[string][]byte{"wire": wire})
	if bytes.Equal(badSaved, cppV.Saved) {
		t.Fatal("dropped sign extension in kind-pair widen did not change saved bytes")
	}
}

func TestRubyFixedProofEvolutionHostile(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Ruby interpreters and live C++ oracle")
	const src = `package probe
enum E { One, Two }
type Point { x int32 }
union Choice {
    point Point
    number int32
    empty
}
fixed table T {
    live bool
    f float32
    b bytes(3)
    a [..2]int16
    o ?int16
    e E
    u Choice
}
`
	u := fixedproof.ParseCheckUnit(t, src)
	bodySize := ir.TableFixedTypeBytes(u.Tables["T"])
	tc := fixedproof.RowCase{ID: "ruby-hostile-shapes", Schema: src, Root: "T", Body: bodySize, Count: 2, CppSetup: "value[1].live = true; value[1].f = 1.25f; value[1].e = E::Two;"}
	oracle, verify, cppLoad := fixedproof.CppOracleWithLoadVerdict(t, tc, u)
	files, err := rubytable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	rubyrun.Analyze(t, rubyFixedTree(t, files), "probe")
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(u.Tables["T"]))
	body := 20 + len(layout) + 8
	last := 20 + len(layout) + 8 + int(bodySize)
	inputs := map[string][]byte{"oracle": oracle}
	for _, item := range []struct {
		name string
		at   int
		bit  byte
	}{
		{"invalid-bool", body, 2},
		{"last-hash", last, 1},
		{"reserved", 1, 1},
		{"layout", 20, 1},
		{"unknown", 8, 1},
	} {
		d := bytes.Clone(oracle)
		d[item.at] ^= item.bit
		inputs[item.name] = d
		if cppLoad(d) == nil {
			t.Fatalf("C++ accepted %s", item.name)
		}
	}
	script := `require_relative "probe"
ref = File.binread("oracle")
rows = Probe::T.fixed_load(ref)
report = Probe::LoadReport.new
report.widened = 19
%w[invalid-bool last-hash reserved layout unknown].each do |name|
  begin
    Probe::T.fixed_load(File.binread(name), report: report)
  rescue Probe::Refused
    # expected
  else
    abort("hostile_control_#{name}")
  end
  abort("hostile_control_transaction") if report.widened != 19 || Probe::T.fixed_save(rows) != ref
end
puts "hostile-ok"
`
	out, _ := rubyFixedRun(t, files, script, inputs)
	if strings.TrimSpace(out) != "hostile-ok" || verify(oracle) != nil {
		t.Fatalf("hostile: %s", out)
	}
	rpath := "probe/_fixed.rb"
	source := string(files[rpath])
	anchor := "if value > 1\n      raise Refused.new(\"wire_damaged\", \"a bool or presence byte is not 0 or 1\")"
	if strings.Count(source, anchor) != 1 {
		t.Fatal("bool guard anchor changed")
	}
	sabotaged := maps.Clone(files)
	sabotaged[rpath] = []byte(strings.Replace(source, anchor, "if false\n      raise Refused.new(\"wire_damaged\", \"a bool or presence byte is not 0 or 1\")", 1))
	for _, pin := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
		dir := rubyFixedTree(t, sabotaged)
		in := maps.Clone(inputs)
		in["main.rb"] = []byte("# frozen_string_literal: true\n" + script)
		_, stderr, rerr := rubyrun.Try(t, pin, dir, "main.rb", in)
		if rerr == nil || !strings.Contains(stderr, "hostile_control_invalid-bool") {
			t.Fatalf("bool guard sabotage escaped: %v\n%s", rerr, stderr)
		}
	}
}
