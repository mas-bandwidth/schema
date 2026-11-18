package rubytable_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rubytable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestRubyFixedForm(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned interpreters and the live C++ fixed-form oracle")
	tc := fixedproof.Cases()[0]
	u := fixedproof.ParseCheckUnit(t, tc.Schema)
	oracle, verify := fixedproof.CppOracle(t, tc, u)
	files, err := rubytable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	script := `require_relative "probe"
rows = Probe::T.fixed_load(File.binread("oracle"))
raise "bad rows" unless rows.length == 2 && !rows[0].a && rows[0].b && rows[1].a && !rows[1].b
saved = Probe::T.fixed_save(rows)
raise "bad measure" unless saved.bytesize == Probe::T.fixed_measure(2)
File.binwrite("saved", saved)
puts "fixed-roundtrip"
`
	out, saved := rubyFixedRun(t, files, script, map[string][]byte{"oracle": oracle})
	if strings.TrimSpace(out) != "fixed-roundtrip" || !bytes.Equal(saved, oracle) || verify(saved) != nil {
		t.Fatal("Ruby fixed-form roundtrip mismatch")
	}
}

func TestRubyFixedFormLineage(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned interpreters")
	old := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T {\n a uint16 = 1\n b int32 = 10\n}\n")
	newer := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T {\n a uint32 = 1\n b int64 = 10\n c int32 = 99\n}\n")
	entry, _ := fixedproof.LineageEntryOf(old, "T")
	body := binary.LittleEndian.AppendUint16(nil, 123)
	body = binary.LittleEndian.AppendUint32(body, uint32(0xffffffec))
	wire := fixedproof.MakeFixedFile(entry.Wire, entry.Layout, body, body)
	runner := rubyFixedProofRunner{}
	n, reason, fields, _, counters := runner.LoadLineage(t, newer, "T", map[string][]fixedproof.LineageEntry{"T": {entry}}, wire, 2)
	if n != 2 || fields["r0.a"] != "123" || fields["r0.b"] != "-20" || fields["r0.c"] != "99" || counters.Widened != 4 {
		t.Fatalf("lineage: %d %s %v %+v", n, reason, fields, counters)
	}
	entry.Retired = true
	if n, reason, _, _, _ := runner.LoadLineage(t, newer, "T", map[string][]fixedproof.LineageEntry{"T": {entry}}, wire, 2); n != -1 || reason != "layout_unsupported" {
		t.Fatalf("retirement: %d %s", n, reason)
	}
	if n, reason, _, _, _ := runner.LoadLineage(t, newer, "T", nil, wire, 2); n != -1 || reason != "layout_unknown" {
		t.Fatalf("unknown: %d %s", n, reason)
	}
}

func TestRubyFixedFormNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned interpreters and the live C++ fixed-form oracle")
	tc := fixedproof.Cases()[0]
	u := fixedproof.ParseCheckUnit(t, tc.Schema)
	oracle, _ := fixedproof.CppOracle(t, tc, u)
	base, err := rubytable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, control := range []struct{ name, anchor, planted, probe string }{
		{"file", "if b0 != 3", "if false", `require_relative "probe"
wire = File.binread("oracle").b
wire.setbyte(0, 1)
begin
  Probe::T.fixed_load(wire)
rescue Probe::Refused
  # expected
else
  abort("fixed_file_control")
end
`},
		{"writer", `root["prefix"] + image`, "image.setbyte(0, (image.getbyte(0) || 0) ^ 1)\n      root[\"prefix\"] + image", `require_relative "probe"
wire = File.binread("oracle")
rows = Probe::T.fixed_load(wire)
abort("fixed_writer_control") if Probe::T.fixed_save(rows) != wire
`},
	} {
		t.Run(control.name, func(t *testing.T) {
			t.Parallel()
			files := maps.Clone(base)
			rpath := "probe/_fixed.rb"
			source := string(files[rpath])
			if strings.Count(source, control.anchor) != 1 {
				t.Fatalf("control %s lost its runtime anchor", control.name)
			}
			files[rpath] = []byte(strings.Replace(source, control.anchor, control.planted, 1))
			for _, pin := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
				dir := rubyFixedTree(t, files)
				_, stderr, rerr := rubyrun.Try(t, pin, dir, "main.rb", map[string][]byte{"oracle": oracle, "main.rb": []byte("# frozen_string_literal: true\n" + control.probe)})
				if rerr == nil || !strings.Contains(stderr, "fixed_"+control.name+"_control") {
					t.Fatalf("%s sabotage did not turn its named probe red: %v\n%s", control.name, rerr, stderr)
				}
			}
		})
	}
}

func TestRubyFixedFormRefusedHash(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters")
	old := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T { x int8 }\n")
	newer := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T { x int16 }\n")
	entry, _ := fixedproof.LineageEntryOf(old, "T")
	retired := fixedproof.MakeFixedFile(entry.Wire, entry.Layout, []byte{7})
	unknown := append([]byte(nil), retired...)
	binary.LittleEndian.PutUint64(unknown[8:16], 1)
	binary.LittleEndian.PutUint64(unknown[20+len(entry.Layout):], 1)
	files, err := rubytable.GenerateLineage(newer, map[string][]rubytable.FixedLineageEntry{"T": {{Wire: entry.Wire, Layout: entry.Layout, Record: entry.Record, Writer: entry.Writer, Digest: ir.TableFixedDefinitionsDigest(entry.Writer), Retired: true}}})
	if err != nil {
		t.Fatal(err)
	}
	probe := `require_relative "probe"
[["retired", "layout_unsupported"], ["unknown", "layout_unknown"]].each do |name, reason|
  wire = File.binread(name)
  report = Probe::FixedLoadReport.new
  report.layout_hash = 123
  report.widened = 456
  report.unknown = 789
  begin
    Probe::T.fixed_load(wire, report: report)
  rescue Probe::Refused => err
    raise "wrong reason" unless err.reason == reason
    expected = format("%s: 0x%016x", reason, wire.byteslice(8, 8).unpack1("Q<"))
    abort("fixed_refused_hash_control_#{reason}") unless err.message == expected
  else
    raise "fixed layout unexpectedly accepted"
  end
  raise "report mutated" unless [report.layout_hash, report.widened, report.unknown] == [123, 456, 789]
end
puts "ok"
`
	out, _ := rubyFixedRun(t, files, probe, map[string][]byte{"retired": retired, "unknown": unknown})
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("refused hash: %s", out)
	}
	for _, reason := range []string{"layout_unknown", "layout_unsupported"} {
		broken := maps.Clone(files)
		rpath := "probe/_fixed.rb"
		anchor := `raise Refused.new("` + reason + `", format("0x%016x", wire_hash))`
		if strings.Count(string(broken[rpath]), anchor) != 1 {
			t.Fatalf("%s hash control lost its anchor", reason)
		}
		broken[rpath] = []byte(strings.Replace(string(broken[rpath]), anchor, `raise Refused.new("`+reason+`")`, 1))
		for _, pin := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
			dir := rubyFixedTree(t, broken)
			_, stderr, rerr := rubyrun.Try(t, pin, dir, "main.rb", map[string][]byte{"main.rb": []byte("# frozen_string_literal: true\n" + probe), "retired": retired, "unknown": unknown})
			if rerr == nil || !strings.Contains(stderr, "fixed_refused_hash_control_"+reason) {
				t.Fatalf("%s hash sabotage did not fail by name: %v\n%s", reason, rerr, stderr)
			}
		}
	}
}

func TestRubyFixedFormDeprecatedEnum(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters and C++ oracle")
	oldSchema := `package probe
enum Mode {
    Legacy
    Live
}
union Choice {
    value Mode
}
fixed table T {
    mode Mode = Live
    fixed_values [2]Mode
    counted_values [..2]Mode
    choice Choice
}
`
	old := fixedproof.ParseCheckUnit(t, oldSchema)
	oldCase := fixedproof.RowCase{
		ID:     "ruby-fixed-enum-old",
		Schema: oldSchema,
		Root:   "T",
		Body:   ir.TableFixedTypeBytes(old.Tables["T"]),
		Count:  1,
		CppSetup: `
value[0].mode=Mode::Legacy;
value[0].fixed_values[0]=Mode::Legacy; value[0].fixed_values[1]=Mode::Live;
value[0].counted_values_count=2; value[0].counted_values[0]=Mode::Legacy; value[0].counted_values[1]=Mode::Live;
value[0].choice.type=ChoiceType::Value; value[0].choice.value=Mode::Legacy;
`,
	}
	wire, _ := fixedproof.CppOracle(t, oldCase, old)
	currentSchema := strings.Replace(oldSchema, "Legacy\n", "Legacy | deprecated\n", 1)
	current := fixedproof.ParseCheckUnit(t, currentSchema)
	currentCase := oldCase
	currentCase.ID = "ruby-fixed-enum-current"
	currentCase.Schema = currentSchema
	currentCase.CppSetup = `
value[0].mode=Mode::Live;
value[0].fixed_values[1]=Mode::Live;
value[0].counted_values_count=2;
value[0].counted_values[1]=Mode::Live;
value[0].choice.type=ChoiceType::Value;
value[0].choice.value=(Mode)0;
`
	expected, verify := fixedproof.CppOracle(t, currentCase, current)
	files, err := rubytable.Generate(current)
	if err != nil {
		t.Fatal(err)
	}
	rubyrun.Analyze(t, rubyFixedTree(t, files), "probe")
	probe := `require_relative "probe"
report = Probe::FixedLoadReport.new
v = Probe::T.load(File.binread("wire"), report)
abort("fixed_deprecated_enum_control") unless v.mode == 2 && v.fixed_values == [0, 2] && v.counted_values == [0, 2] && v.choice.type == 1 && v.choice.value == 0
raise "counters" unless [report.unknown, report.widened, report.retained, report.retain_lost] == [0, 0, 0, 0]
File.binwrite("saved", v.save)
bad = Probe::T.new
bad.mode = 1
begin
  bad.save
rescue Probe::InvalidValue
  puts "ok"
else
  abort("deprecated_write_accepted")
end
`
	out, saved := rubyFixedRun(t, files, probe, map[string][]byte{"wire": wire})
	if strings.TrimSpace(out) != "ok" || !bytes.Equal(saved, expected) || verify(saved) != nil {
		t.Fatalf("deprecated enum mismatch: %s", out)
	}
}

func TestRubyFixedFormPacketKeyed(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both Ruby pins and the live C++ oracle")
	tc := fixedproof.RowCase{ID: "ruby-fixed-packet-keyed", Schema: `package probe
enum Slot { Alpha, Beta }
type Board { values [Slot]int32 | min = 0, max = 100 }
fixed table T { board Board
 direct [Slot]int32 }
`, Root: "T", Count: 1, CppSetup: `value[0].board.values[0]=7; value[0].board.values[1]=11;`}
	u := fixedproof.ParseCheckUnit(t, tc.Schema)
	tc.Body = ir.TableFixedTypeBytes(u.Tables["T"])
	wire, verify := fixedproof.CppOracle(t, tc, u)
	files, err := rubytable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	rubyrun.Analyze(t, rubyFixedTree(t, files), "probe")
	script := `require_relative "probe"
wire = File.binread("wire")
rows = Probe::T.fixed_load(wire)
raise "board values" unless rows[0].board.values.instance_of?(Array) && rows[0].board.values == [7, 11]
raise "direct keyed" unless rows[0].direct.respond_to?(:_s_values)
File.binwrite("saved", Probe::T.fixed_save(rows))
[[0], [0, 0, 0], [0, 101]].each do |bad|
  rows[0].board.values = bad
  begin
    Probe::T.fixed_save(rows)
  rescue Probe::InvalidValue
    # expected
  else
    raise "packet-keyed validation"
  end
end
puts "ok"
`
	out, saved := rubyFixedRun(t, files, script, map[string][]byte{"wire": wire})
	if strings.TrimSpace(out) != "ok" || !bytes.Equal(saved, wire) || verify(saved) != nil {
		t.Fatalf("packet-keyed fixed: %s", out)
	}
}

func TestRubyFixedIntegrityCRC(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "fixed-table CRC32C")
	schema := "package probe\nfixed table T | integrity = \"crc32c\" { n int32 }\n"
	u := fixedproof.ParseCheckUnit(t, schema)
	tc := fixedproof.RowCase{ID: "ruby-fixed-crc", Schema: schema, Root: "T", Body: 4, Count: 2, CppSetup: "value[0].n = 7; value[1].n = 8;"}
	cpp, verify := fixedproof.CppOracle(t, tc, u)
	m := tabletext.NewModel(u)
	value := m.New(m.Lookup("T"))
	var report tabletext.Report
	if !m.Read(value, []byte(`{"n":7}`), &report) {
		t.Fatalf("Go JSON: %+v", report)
	}
	single, err := tablewire.EncodeFixed(m, value)
	if err != nil {
		t.Fatal(err)
	}
	files, err := rubytable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	rubyrun.Analyze(t, rubyFixedTree(t, files), "probe")
	probe := `require_relative "probe"
multi = File.binread("multi")
single = File.binread("single")
a = Probe::T.new; a.n = 7
b = Probe::T.new; b.n = 8
raise "single fixed CRC" unless a.save == single && a.measure == single.bytesize
raise "load single" unless Probe::T.load(single).n == 7
raise "batch fixed CRC" unless Probe::T.fixed_save([a, b]) == multi && Probe::T.fixed_measure(2) == multi.bytesize
raise "load batch" unless Probe::T.fixed_load(multi).map(&:n) == [7, 8]
corrupt_tail = multi.byteslice(0, multi.bytesize - 1) + [(multi.getbyte(multi.bytesize - 1) || 0) ^ 1].pack("C")
corrupt_body = multi.byteslice(0, 8) + [(multi.getbyte(8) || 0) ^ 1].pack("C") + (multi.byteslice(9, multi.bytesize - 9) || "".b)
[[corrupt_tail, "integrity_mismatch"], [corrupt_body, "integrity_mismatch"], ["\x03".b, "integrity_mismatch"], ["".b, "truncated"], ["\x01".b, "previous_form"], ["\x02".b, "message_form_as_file"]].each do |data, reason|
  r = Probe::LoadReport.new
  r.unknown = 77
  begin
    Probe::T.fixed_load(data, report: r)
  rescue Probe::Refused => err
    raise "wrong reason #{err.reason}" unless err.reason == reason && r.unknown == 77
  else
    raise "bad fixed CRC accepted"
  end
end
puts "ok"
`
	out, _ := rubyFixedRun(t, files, probe, map[string][]byte{"multi": cpp, "single": single})
	if strings.TrimSpace(out) != "ok" || verify(cpp) != nil {
		t.Fatalf("fixed CRC: %s", out)
	}
}

func TestRubyFixedIntegrityCRCNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "fixed-table CRC32C negative controls")
	schema := "package probe\nfixed table T | integrity = \"crc32c\" { n int32 }\n"
	u := fixedproof.ParseCheckUnit(t, schema)
	m := tabletext.NewModel(u)
	value := m.New(m.Lookup("T"))
	var report tabletext.Report
	if !m.Read(value, []byte(`{"n":7}`), &report) {
		t.Fatalf("Go JSON: %+v", report)
	}
	wire, err := tablewire.EncodeFixed(m, value)
	if err != nil {
		t.Fatal(err)
	}
	base, err := rubytable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	files := maps.Clone(base)
	rpath := "probe/_fixed.rb"
	anchor := `if !declared_root.nil? && declared_root["integrity"]`
	if strings.Count(string(files[rpath]), anchor) != 1 {
		t.Fatal("CRC reader anchor changed")
	}
	files[rpath] = []byte(strings.Replace(string(files[rpath]), anchor, "if false", 1))
	probe := `# frozen_string_literal: true
require_relative "probe"
wire = File.binread("wire").b
wire.setbyte(wire.bytesize - 1, (wire.getbyte(wire.bytesize - 1) || 0) ^ 1)
begin
  Probe::T.load(wire)
rescue Probe::Refused => err
  abort("crc_control_detected") if err.reason != "integrity_mismatch"
else
  abort("crc_control_detected")
end
`
	for _, pin := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
		dir := rubyFixedTree(t, files)
		_, stderr, rerr := rubyrun.Try(t, pin, dir, "main.rb", map[string][]byte{"wire": wire, "main.rb": []byte(probe)})
		if rerr == nil || !strings.Contains(stderr, "crc_control_detected") {
			t.Fatalf("CRC sabotage did not fail by name: %v\n%s", rerr, stderr)
		}
	}
}

func TestRubyFixedIdentityTrustedLayoutOnly(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both Ruby pins")
	repo, err := rubyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(repo, "internal", "lockfile", "testdata", "writer-v6")
	dir := t.TempDir()
	for _, name := range []string{"Writer.schema", lockfile.FileName} {
		data, rerr := os.ReadFile(filepath.Join(base, name))
		if rerr != nil {
			t.Fatal(rerr)
		}
		if werr := os.WriteFile(filepath.Join(dir, name), data, 0o600); werr != nil {
			t.Fatal(werr)
		}
	}
	paths := []string{filepath.Join(dir, "Writer.schema")}
	c := compiler.New()
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := compiler.UpdateSchemaLock(u, paths); err != nil {
		t.Fatal(err)
	}
	lk, ok, err := lockfile.Open(paths)
	if err != nil || !ok {
		t.Fatalf("open lock: %v %v", ok, err)
	}
	legacy := lk.Table("Row").Lineage[0]
	if _, err := c.Generate(u, "ruby", compiler.Options{}); err == nil || !strings.Contains(err.Error(), "checked writer declaration snapshot") {
		t.Fatalf("unknown historical writer did not hold generation: %v", err)
	}
	target := fmt.Sprintf("Row@0x%016x", legacy.Wire)
	if _, _, err := compiler.ImportSchemaLockWriterWithOptions(u, paths, target, u, compiler.SchemaLockWriterImportOptions{LegacyLayoutOnly: true}); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[0], []byte(strings.Replace(string(source), "max = 10", "max = 20", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err = c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := compiler.UpdateSchemaLock(u, paths); err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(u, "ruby", compiler.Options{})
	if err != nil {
		t.Fatal(err)
	}
	good := fixedproof.MakeFixedFile(legacy.Wire, legacy.Layout, binary.LittleEndian.AppendUint32(nil, 10))
	bad := fixedproof.MakeFixedFile(legacy.Wire, legacy.Layout, binary.LittleEndian.AppendUint32(nil, 11))
	probe := `require_relative "lockdemo"
raise "good" unless Lockdemo::Row.load(File.binread("good")).a.n == 10
begin
  Lockdemo::Row.load(File.binread("bad"))
rescue Lockdemo::Refused => err
  raise "wrong reason" unless err.reason == "out_of_range"
else
  raise "trusted historical bound lost"
end
v = Lockdemo::Row.new
v.a.n = 11
raise "roundtrip" unless Lockdemo::Row.load(v.save).a.n == 11
puts "ok"
`
	out, _ := rubyFixedRun(t, files, probe, map[string][]byte{"good": good, "bad": bad})
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("trusted legacy identity: %s", out)
	}
}

func TestRubyFixedIdentityRejectsUnrecordedSchemes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both Ruby pins")
	u := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T { x int32 | min = 0, max = 10 }\n")
	e, _ := fixedproof.LineageEntryOf(u, "T")
	for _, control := range []string{"future", "erased", "layout-only-v2"} {
		entry := rubytable.FixedLineageEntry{Wire: e.Wire, Layout: e.Layout, Record: e.Record, Writer: e.Writer, Digest: e.Digest}
		switch control {
		case "future":
			entry.Digest = append([]byte(nil), entry.Digest...)
			entry.Digest[len("\x00schema-fixed-definitions")] = 3
		case "erased":
			entry.Digest = nil
		case "layout-only-v2":
			entry.LegacyLayoutOnly = true
		}
		if control != "layout-only-v2" {
			entry.Wire = ir.TableFixedLayoutHashRecorded(entry.Layout, entry.Digest)
		}
		wire := fixedproof.MakeFixedFile(entry.Wire, entry.Layout, binary.LittleEndian.AppendUint32(nil, 5))
		files, err := rubytable.GenerateLineage(u, map[string][]rubytable.FixedLineageEntry{"T": {entry}})
		if err != nil {
			t.Fatal(err)
		}
		probe := `require_relative "probe"
begin
  Probe::T.load(File.binread("wire"))
rescue Probe::Refused => err
  raise "wrong reason #{err.reason}" unless err.reason == "layout_malformed"
else
  raise "unrecorded writer identity accepted"
end
puts "ok"
`
		out, _ := rubyFixedRun(t, files, probe, map[string][]byte{"wire": wire})
		if strings.TrimSpace(out) != "ok" {
			t.Fatalf("%s scheme: %s", control, out)
		}
	}
}

func TestRubyFixedCodegenDeterministic(t *testing.T) {
	t.Parallel()
	const src = "package demo\n\nenum Mode { Off, On }\n\nfixed table Sample | integrity = \"crc32c\" {\n  id uint32 = 7\n  mode Mode = On\n  tags ?[2]uint16\n}\n"
	u := fixedproof.ParseCheckUnit(t, src)
	a, err := rubytable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	b, err := rubytable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("file count mismatch: %d vs %d", len(a), len(b))
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || !bytes.Equal(va, vb) {
			t.Fatalf("non-deterministic output for %s", k)
		}
	}
}
