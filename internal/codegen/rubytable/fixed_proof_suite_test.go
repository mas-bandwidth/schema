package rubytable_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/ruby"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/rubytable"
	"github.com/mas-bandwidth/schema/v3/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/ir"
)

var rubyFixedSetups = map[string]string{}
var rubyFixedChecks = map[string]string{}

type rubyFixedProofRunner struct{}

func (rubyFixedProofRunner) Name() string { return "ruby" }

func rubyFixedTree(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func rubyFixedLineage(lineage map[string][]fixedproof.LineageEntry) map[string][]rubytable.FixedLineageEntry {
	out := map[string][]rubytable.FixedLineageEntry{}
	for name, entries := range lineage {
		for _, e := range entries {
			out[name] = append(out[name], rubytable.FixedLineageEntry{
				Wire:             e.Wire,
				Layout:           e.Layout,
				Record:           e.Record,
				Retired:          e.Retired,
				Reason:           e.Reason,
				Writer:           e.Writer,
				Digest:           e.Digest,
				LegacyLayoutOnly: e.LegacyLayoutOnly,
			})
		}
	}
	return out
}

func rubyFixedRun(t *testing.T, files map[string][]byte, script string, inputs map[string][]byte) (string, []byte) {
	t.Helper()
	var first string
	var saved []byte
	fullScript := "# frozen_string_literal: true\n\n" + script
	for index, pin := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
		dir := rubyFixedTree(t, files)
		in := map[string][]byte{"main.rb": []byte(fullScript)}
		maps.Copy(in, inputs)
		out, stderr, err := rubyrun.Try(t, pin, dir, "main.rb", in)
		if err != nil || stderr != "" {
			t.Fatalf("fixed probe (%s): %v\nstdout:\n%s\nstderr:\n%s", pin, err, out, stderr)
		}
		value, readErr := os.ReadFile(filepath.Join(dir, "saved"))
		if readErr != nil && !os.IsNotExist(readErr) {
			t.Fatal(readErr)
		}
		if index == 0 {
			first = out
			saved = value
		} else if first != out || string(saved) != string(value) {
			t.Fatalf("the interpreter pins disagree:\ncurrent %s\nfloor %s", first, out)
		}
	}
	return first, saved
}

func (rubyFixedProofRunner) HasFixedRoot(t *testing.T, u *ir.Unit, root string) bool {
	t.Helper()
	return u.Tables[root] != nil && ir.TableFixedEmitted(u, u.Tables[root])
}

func (rubyFixedProofRunner) CheckLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry) string {
	t.Helper()
	files, err := rubytable.GenerateLineage(u, rubyFixedLineage(lineage))
	if err != nil {
		return err.Error()
	}
	mod := ruby.ModuleName(u.Package)
	script := fmt.Sprintf(`require_relative %q
r = %s._s_fixed_roots[%q]
if r.nil?
  puts "no_fixed_root"
else
  hit = r["known"].values.find { |e| !e["refusal"].empty? && e["refusal"] != "layout_unsupported" }
  puts(hit ? hit["refusal"] : "")
end
`, u.Package, mod, root)
	out, _ := rubyFixedRun(t, files, script, nil)
	return strings.TrimSpace(out)
}

const rubyFixedDump = `def json_str(s)
  out = String.new("\"")
  s.each_char do |ch|
    cp = ch.ord
    case ch
    when "\"" then out << "\\\""
    when "\\" then out << "\\\\"
    when "\b" then out << "\\b"
    when "\f" then out << "\\f"
    when "\n" then out << "\\n"
    when "\r" then out << "\\r"
    when "\t" then out << "\\t"
    else
      if cp < 0x20
        out << format("\\u%04x", cp)
      else
        out << ch
      end
    end
  end
  out << "\""
  out
end

def dump(spec, value, prefix)
  shape = spec["shape"]
  if shape == "table"
    spec["children"].each do |field|
      fname = field["name"]
      dump(field, value.public_send(fname.to_sym), "#{prefix}.#{fname}")
    end
  elsif shape == "optional"
    puts "FIELD #{prefix}_present=#{value.nil? ? 0 : 1}"
    dump(spec["children"][0], value, prefix) unless value.nil?
  elsif shape == "array" || shape == "keyed"
    values = shape == "keyed" && !spec["positional"] ? value._s_values : value
    puts "FIELD #{prefix}_count=#{values.length}" if spec["counted"]
    values.each_with_index do |item, i|
      dump(spec["children"][0], item, "#{prefix}[#{i}]")
    end
  elsif shape == "union"
    puts "FIELD #{prefix}.type=#{value.type}"
    if value.type > 0
      arm = spec["children"][value.type - 1]
      if arm["shape"] != "void"
        aname = arm["name"]
        dump(arm, value.public_send(aname.to_sym), "#{prefix}.#{aname}")
      end
    end
  elsif shape == "string"
    puts "FIELD #{prefix}=#{json_str(value)}"
    puts "FIELD #{prefix}_length=#{value.bytesize}"
  elsif shape == "bytes"
    puts "FIELD #{prefix}_length=#{value.bytesize}"
    value.bytes.each_with_index do |b, i|
      puts "FIELD #{prefix}[#{i}]=#{b}"
    end
  elsif shape == "float"
    if spec["size"] == 4
      bits = [value].pack("e").unpack1("L<")
      puts format("FIELD %s=0x%08X", prefix, bits)
    else
      bits = [value].pack("E").unpack1("Q<")
      puts format("FIELD %s=0x%016X", prefix, bits)
    end
  elsif shape == "bool"
    puts "FIELD #{prefix}=#{value ? 1 : 0}"
  elsif shape != "void"
    puts "FIELD #{prefix}=#{value}"
  end
end
`

func (rubyFixedProofRunner) LoadLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry, wire []byte, capacity int) (int, string, map[string]string, []byte, fixedproof.LoadCounters) {
	t.Helper()
	files, err := rubytable.GenerateLineage(u, rubyFixedLineage(lineage))
	if err != nil {
		return -1, err.Error(), nil, nil, fixedproof.LoadCounters{}
	}
	capArg := "4096"
	if capacity >= 0 {
		capArg = strconv.Itoa(capacity)
	}
	mod := ruby.ModuleName(u.Package)
	script := fmt.Sprintf(`require_relative %[1]q
%[4]s
report = %[2]s::LoadReport.new
begin
  rows = %[2]s::%[3]s.fixed_load(File.binread("wire"), %[5]s, report)
rescue %[2]s::Refused => err
  puts "REFUSED #{err.reason}"
else
  puts "OK #{rows.length} #{report.widened}"
  spec = %[2]s._s_fixed_roots[%[3]q]["spec"]
  rows.first(8).each_with_index do |row, i|
    dump(spec, row, "r#{i}")
  end
  File.binwrite("saved", %[2]s::%[3]s.fixed_save(rows))
end
`, u.Package, mod, root, rubyFixedDump, capArg)
	out, saved := rubyFixedRun(t, files, script, map[string][]byte{"wire": wire})
	fields := map[string]string{}
	count := -1
	reason := ""
	var counters fixedproof.LoadCounters
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if value, ok := strings.CutPrefix(line, "REFUSED "); ok {
			return -1, value, fields, nil, fixedproof.LoadCounters{}
		}
		if value, ok := strings.CutPrefix(line, "OK "); ok {
			fmt.Sscanf(value, "%d %d", &count, &counters.Widened)
			reason = "ok"
		}
		if value, ok := strings.CutPrefix(line, "FIELD "); ok {
			if k, v, ok := strings.Cut(value, "="); ok {
				fields[k] = v
			}
		}
	}
	return count, reason, fields, saved, counters
}

func (rubyFixedProofRunner) SaveCustom(t *testing.T, u *ir.Unit, root, scenario string) ([]byte, string) {
	t.Helper()
	files, err := rubytable.Generate(u)
	if err != nil {
		return nil, err.Error()
	}
	mod := ruby.ModuleName(u.Package)
	setup := map[string]string{
		"optional_absent_zeros": "rows = [T.new]\nrows[0].plain.a = 11\nrows[0].plain.b = 22\nrows[0].opt = nil\n",
		"live_extents_base":     "rows = [T.new]\nrows[0].arr = [1]\nrows[0].opt = nil\nrows[0].tail = 77\n",
		"writing_slack_batch":   "rows = [T.new, T.new]\nrows[1].arr = [0x1122]\nrows[1].s = \"A\"\nrows[1].b = \"B\".b\nrows[1].pick.type = 1\nrows[1].pick.narrow = 7\n",
		"writing_overcount":     "rows = [T.new]\nrows[0].arr = [1, 2, 3, 4]\n",
		"closure_small_42":      "rows = [T.new]\nrows[0].x = 42\n",
		"outer_nested":          "rows = [T.new]\nrows[0].head = 7\nrows[0].inner.a = 11\nrows[0].inner.b = \"one\"\nrows[0].items[0].a = 10\nrows[0].items[0].b = \"hi\"\nrows[0].items[1].a = 33\nrows[0].items[1].b = \"six\"\nrows[0].maybe = rows[0].inner.class.new\nrows[0].maybe.a = 44\nrows[0].maybe.b = \"hi\"\nrows[0].tail = 9\n",
		"w14_writer_offsets":    "rows = [T.new]\nrows[0].a = 0xA1\nrows[0].b = 0x0B0B0B0B\nrows[0].inner.p = 0x1E1E1E1E\nrows[0].inner.q = 0x2E2E\nrows[0].opt = 0x0D0D0D0D\nrows[0].arr = [0x3E3E, 0x4E4E]\nrows[0].s = \"seven\"\nrows[0].tail = 0x7A7A7A7A\n",
	}[scenario]
	if scenario == "writing_slack_unzeroed" {
		setup = "rows = [T.new, T.new]\nrows[1].arr = [0x1122]\nrows[1].s = \"A\"\nrows[1].b = \"B\".b\nrows[1].pick.type = 1\nrows[1].pick.narrow = 7\n"
		name := u.Package + "/_fixed.rb"
		s := string(files[name])
		anchor := `image = "\x00".b * root["body"]`
		if strings.Count(s, anchor) != 2 {
			t.Fatalf("fixed image sabotage anchor count = %d, want 2", strings.Count(s, anchor))
		}
		files[name] = []byte(strings.Replace(s, anchor, `image = "\xAA".b * root["body"]`, 1))
	}
	if setup == "" {
		return nil, "unknown scenario " + scenario
	}
	script := fmt.Sprintf(`require_relative %q
T = %s::%s
%s
begin
  wire = T.fixed_save(rows)
rescue %s::InvalidValue => err
  puts "REFUSED #{err.rule}"
else
  File.binwrite("saved", wire)
  puts "OK"
end
`, u.Package, mod, root, setup, mod)
	out, saved := rubyFixedRun(t, files, script, nil)
	if reason, ok := strings.CutPrefix(strings.TrimSpace(out), "REFUSED "); ok {
		return nil, reason
	}
	return saved, ""
}

func (rubyFixedProofRunner) Save(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) []byte {
	t.Helper()
	setup, ok := rubyFixedSetups[tc.ID]
	if !ok {
		t.Fatalf("no Ruby construction for proof row %s", tc.ID)
	}
	files, err := rubytable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	mod := ruby.ModuleName(u.Package)
	script := fmt.Sprintf(`require_relative %[1]q
include %[2]s
rows = Array.new(%[4]d) { %[3]s.new }
%[5]s
%[6]s
wire = %[3]s.fixed_save(rows)
raise "measure mismatch" unless wire.bytesize == %[3]s.fixed_measure(rows.length)
File.binwrite("saved", wire)
`, u.Package, mod, tc.Root, tc.Count, setup, rubyFixedChecks[tc.ID])
	_, saved := rubyFixedRun(t, files, script, nil)
	return saved
}

func (r rubyFixedProofRunner) LoadAndResave(t *testing.T, tc fixedproof.RowCase, u *ir.Unit, wire []byte) ([]byte, error) {
	t.Helper()
	n, reason, _, saved, _ := r.LoadLineage(t, u, tc.Root, nil, wire, tc.Count)
	if n != tc.Count {
		return nil, fmt.Errorf("%s: %s", tc.ID, reason)
	}
	return saved, nil
}

func (r rubyFixedProofRunner) SaveMutated(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) ([]byte, error) {
	t.Helper()
	files, err := rubytable.Generate(u)
	if err != nil {
		return nil, err
	}
	setup, ok := rubyFixedSetups[tc.ID]
	if !ok {
		return nil, fmt.Errorf("no Ruby construction for %s", tc.ID)
	}
	name := u.Package + "/_fixed.rb"
	s := string(files[name])
	anchor := `root["prefix"] + image`
	if strings.Count(s, anchor) != 1 {
		return nil, fmt.Errorf("fixed writer sabotage anchor changed")
	}
	files[name] = []byte(strings.Replace(s, anchor, "image.setbyte(0, (image.getbyte(0) || 0) ^ 1)\n      "+anchor, 1))
	mod := ruby.ModuleName(u.Package)
	script := fmt.Sprintf(`require_relative %q
include %s
rows = Array.new(%d) { %s.new }
%s
File.binwrite("saved", %s.fixed_save(rows))
`, u.Package, mod, tc.Count, tc.Root, setup, tc.Root)
	_, saved := rubyFixedRun(t, files, script, nil)
	return saved, nil
}

func TestRubyFixedProofSuite(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both Ruby pins and the C++ fixed-form corpus")
	fixedproof.RunEvolutionRows(t, filepath.Join("..", "..", ".."), rubyFixedProofRunner{},
		"fixed-file-roundtrip", "file-envelope", "batch-capacity", "plan-selection",
		"compiled-plans", "definition-hash", "fixed-closure", "live-extents", "writing")
}

func TestRubyFixedShapeCompare(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned interpreters and C++ oracle")
	tc := fixedproof.RowCase{ID: "ruby-fixed-compare", Schema: "package probe\nfixed table T { x int32 = 1\n y int32 = 2 }\n", Root: "T", Body: 8, Count: 1}
	u := fixedproof.ParseCheckUnit(t, tc.Schema)
	wire, _ := fixedproof.CppOracle(t, tc, u)
	files, err := rubytable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	script := `# frozen_string_literal: true
require_relative "probe"
wire = File.binread("wire")
values = Probe::T.fixed_load(wire)
raise "single mismatch" unless Probe::T.fixed_save(values) == wire && wire.bytesize == Probe::T.fixed_measure(1)
batch = values * 1024
many = Probe::T.fixed_save(batch)
raise "batch mismatch" unless many.bytesize == Probe::T.fixed_measure(1024) && Probe::T.fixed_save(Probe::T.fixed_load(many)) == many
puts "ok"
`
	const runtimePath = "probe/_fixed.rb"
	const direct = `direct = entry["direct"]`
	if strings.Count(string(files[runtimePath]), direct) != 1 {
		t.Fatalf("%s carries no single direct identity switch %q", runtimePath, direct)
	}
	image := maps.Clone(files)
	image[runtimePath] = []byte(strings.Replace(string(files[runtimePath]), direct, "direct = false", 1))
	for _, pin := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
		for _, shape := range []struct {
			name  string
			files map[string][]byte
		}{{"direct decode", files}, {"record image", image}} {
			dir := rubyFixedTree(t, shape.files)
			out, stderr, rerr := rubyrun.Try(t, pin, dir, "main.rb", map[string][]byte{"main.rb": []byte(script), "wire": wire})
			if rerr != nil || stderr != "" || strings.TrimSpace(out) != "ok" {
				t.Fatalf("fixed shape compare (%s): %v\n%s\n%s", shape.name, rerr, out, stderr)
			}
		}
	}
}
