package rubytable_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rubytable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func init() {
	rubyFixedSetups["shape-text"] = "rows[0].s = \"\\u{e9}\""
	rubyFixedSetups["bounds-old"] = "rows[0].a = [7, 9]"
	rubyFixedSetups["utf8-values"] = "rows[1].s = \"hello\"\n"
	rubyFixedSetups["byte-buffers"] = "rows[1].blob = [255, 0, 90, 1, 2, 3].pack(\"C*\")\n"
	rubyFixedSetups["nested-types"] = "rows[0].badge.title = \"hi\"\n"
	rubyFixedSetups["nested-fixed-tables"] = "rows[0].child.n = -2147483648\nrows[1].child.n = 2147483647\n"
	rubyFixedSetups["fixed-arrays"] = "rows[0].tag = [-2147483648, 0, 2147483647]\n"
	rubyFixedSetups["counted-arrays"] = "rows[1].nums = [10, 65535, 32768]\n"
	rubyFixedSetups["keyed-arrays"] = "rows[0].slots[1] = -2147483648\nrows[0].slots[2] = 2147483647\n"
	rubyFixedSetups["nested-keyed-arrays"] = "rows[0].slots[1].n = 11\nrows[0].slots[2].n = -22\n"
	rubyFixedSetups["text-bytes-flags-defaults"] = "rows[1].s = \"xyz\"\nrows[1].b = [0, 255, 90, 1].pack(\"C*\")\nrows[1].c = 4\n"
}

func TestRubyFixedProofShapes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Ruby interpreters and C++ fixed-form oracle")
	expected := map[string]map[string]string{
		"utf8-values":               {"r0.s": `""`, "r1.s": `"hello"`, "r1.s_length": "5"},
		"byte-buffers":              {"r0.blob_length": "0", "r1.blob_length": "6", "r1.blob[0]": "255", "r1.blob[1]": "0", "r1.blob[2]": "90", "r1.blob[5]": "3"},
		"nested-types":              {"r0.badge.title": `"hi"`, "r0.badge.title_length": "2"},
		"nested-fixed-tables":       {"r0.child.n": "-2147483648", "r1.child.n": "2147483647"},
		"fixed-arrays":              {"r0.tag[0]": "-2147483648", "r0.tag[1]": "0", "r0.tag[2]": "2147483647"},
		"counted-arrays":            {"r0.nums_count": "0", "r1.nums_count": "3", "r1.nums[0]": "10", "r1.nums[1]": "65535", "r1.nums[2]": "32768"},
		"keyed-arrays":              {"r0.slots[0]": "-2147483648", "r0.slots[1]": "2147483647"},
		"nested-keyed-arrays":       {"r0.slots[0].n": "11", "r0.slots[1].n": "-22"},
		"text-bytes-flags-defaults": {"r0.s": `"hi"`, "r0.b_length": "2", "r0.b[0]": "97", "r0.b[1]": "98", "r0.c": "3", "r1.s": `"xyz"`, "r1.b_length": "4", "r1.b[1]": "255", "r1.c": "4"},
	}
	runner := rubyFixedProofRunner{}
	rows := []string{"utf8-values", "byte-buffers", "nested-types", "nested-fixed-tables", "fixed-arrays", "counted-arrays", "keyed-arrays", "nested-keyed-arrays", "text-bytes-flags-defaults"}
	t.Run("value-suite", func(t *testing.T) { fixedproof.RunValueRows(t, runner, rows...) })
	t.Run("negative-controls", func(t *testing.T) { fixedproof.RunNegativeControlRows(t, runner, rows...) })
	allCases := fixedproof.Cases()
	if len(allCases) == 0 {
		t.Fatal("fixedproof.Cases() is empty: the loop below would check nothing")
	}
	for _, tc := range allCases {
		want, selected := expected[tc.ID]
		if !selected {
			continue
		}
		t.Run(tc.ID, func(t *testing.T) {
			t.Parallel()
			u := fixedproof.ParseCheckUnit(t, tc.Schema)
			_, _, body := fixedproof.RootFacts(fixedproof.RootStruct(t, u, tc.Root))
			if body != tc.Body {
				t.Fatalf("body %d, want %d", body, tc.Body)
			}
			oracle, verify, _ := fixedproof.CppOracleWithLoadVerdict(t, tc, u)
			files, err := rubytable.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			dir := rubyFixedTree(t, files)
			rubyrun.Analyze(t, dir, u.Package)
			n, reason, fields, saved, _ := runner.LoadLineage(t, u, tc.Root, nil, oracle, tc.Count)
			if n != tc.Count || !bytes.Equal(saved, oracle) {
				t.Fatalf("%s: count=%d reason=%s", tc.ID, n, reason)
			}
			if err := verify(saved); err != nil {
				t.Fatal(err)
			}
			for k, v := range want {
				if fields[k] != v {
					t.Errorf("%s field %s = %q, want %q", tc.ID, k, fields[k], v)
				}
			}
			if tc.ID == "keyed-arrays" {
				keyProbe := `require_relative "probe"
row = Probe::T.new
[0, -1, 3, nil, "1"].each do |bad|
  begin
    row.slots[bad]
  rescue IndexError
    # expected
  else
    raise "accepted bad key #{bad.inspect}"
  end
  begin
    row.slots[bad] = 5
  rescue IndexError
    # expected
  else
    raise "accepted bad key write #{bad.inspect}"
  end
end
puts "keyed-ok"
`
				out, _ := rubyFixedRun(t, files, keyProbe, nil)
				if strings.TrimSpace(out) != "keyed-ok" {
					t.Fatalf("keyed bounds: %s", out)
				}
			}
		})
	}
}

func TestRubyFixedProofShapesText(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Ruby interpreters and C++ fixed-form oracle")
	schema := "package probe\nfixed table T { s string(4) }\n"
	u := fixedproof.ParseCheckUnit(t, schema)
	tc := fixedproof.RowCase{ID: "shape-text", Schema: schema, Root: "T", Body: 8, Count: 1, CppSetup: `memcpy(value[0].s, "\xc3\xa9", 2); value[0].s_length = 2;`}
	clean, verify, load := fixedproof.CppOracleWithLoadVerdict(t, tc, u)
	files, err := rubytable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	nulWire := bytes.Clone(clean)
	nulWire[len(nulWire)-8] = 3
	nulWire[len(nulWire)-4+2] = 0
	if load(nulWire) == nil {
		t.Fatal("C++ accepted embedded NUL")
	}
	badUTF8 := bytes.Clone(clean)
	badUTF8[len(badUTF8)-8] = 1
	badUTF8[len(badUTF8)-4] = 0xff
	badUTF8[len(badUTF8)-3] = 0
	if load(badUTF8) == nil {
		t.Fatal("C++ accepted invalid UTF-8")
	}
	overLen := bytes.Clone(clean)
	binary.LittleEndian.PutUint32(overLen[len(overLen)-8:], 5)
	if load(overLen) == nil {
		t.Fatal("C++ accepted over-bound length")
	}
	script := `require_relative "probe"
clean = File.binread("clean")
rows = Probe::T.fixed_load(clean)
raise "utf8 decode" unless rows[0].s == "\u{e9}"
File.binwrite("saved", Probe::T.fixed_save(rows))
%w[nul bad_utf8 over_len].each do |name|
  begin
    Probe::T.fixed_load(File.binread(name))
  rescue Probe::Refused
    # expected
  else
    raise "accepted #{name}"
  end
end
["a\x00b", "abcde", "\xFF".b].each do |bad|
  v = Probe::T.new
  v.s = bad
  begin
    Probe::T.fixed_save([v])
  rescue Probe::InvalidValue
    # expected
  else
    raise "wrote bad string #{bad.inspect}"
  end
end
puts "text-ok"
`
	out, saved := rubyFixedRun(t, files, script, map[string][]byte{"clean": clean, "nul": nulWire, "bad_utf8": badUTF8, "over_len": overLen})
	if strings.TrimSpace(out) != "text-ok" || !bytes.Equal(saved, clean) || verify(saved) != nil {
		t.Fatalf("text proof failed: %s", out)
	}
}

func TestRubyFixedProofShapesArrayBounds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Ruby interpreters and C++ fixed-form oracle")
	old := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T { a [..2]uint16 }\n")
	newer := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T { a [..4]uint16 }\n")
	tc := fixedproof.RowCase{ID: "bounds-old", Schema: "package probe\nfixed table T { a [..2]uint16 }\n", Root: "T", Body: 8, Count: 1, CppSetup: `value[0].a_count=2; value[0].a[0]=7; value[0].a[1]=9;`}
	clean, verify, load := fixedproof.CppOracleWithLoadVerdict(t, tc, old)
	entry, ok := fixedproof.LineageEntryOf(old, "T")
	if !ok {
		t.Fatal("missing writer lineage")
	}
	lineage := map[string][]fixedproof.LineageEntry{"T": {entry}}
	runner := rubyFixedProofRunner{}
	count, why, fields, saved, _ := runner.LoadLineage(t, newer, "T", lineage, clean, 1)
	newRow := fixedproof.RowCase{ID: "bounds-new", Root: "T", Body: 12, Count: 1, CppSetup: tc.CppSetup}
	newOracle, verifyNew := fixedproof.CppOracle(t, newRow, newer)
	if !bytes.Equal(saved, newOracle) {
		t.Fatal("evolved Ruby bytes differ from C++")
	}
	if err := verifyNew(saved); err != nil {
		t.Fatalf("C++ rejected evolved bytes: %v", err)
	}
	if count != 1 || fields["r0.a_count"] != "2" || fields["r0.a[1]"] != "9" {
		t.Fatalf("writer bound: count=%d reason=%s fields=%v", count, why, fields)
	}
	if err := verify(clean); err != nil {
		t.Fatalf("C++ writer control: %v", err)
	}
	for _, n := range []uint32{3, ^uint32(0)} {
		bad := bytes.Clone(clean)
		binary.LittleEndian.PutUint32(bad[len(bad)-8:], n)
		if err := load(bad); err == nil {
			t.Errorf("C++ accepted writer count %d", n)
		}
		cnt, rsn, _, _, _ := runner.LoadLineage(t, newer, "T", lineage, bad, 1)
		if cnt != -1 || rsn != "count_over_bound" {
			t.Errorf("writer count %d: count=%d reason=%s", n, cnt, rsn)
		}
	}
	files, err := rubytable.Generate(newer)
	if err != nil {
		t.Fatal(err)
	}
	script := `require_relative "probe"
x = Probe::T.new
x.a = [1, 2, 3, 4, 5]
begin
  Probe::T.fixed_save([x])
rescue Probe::InvalidValue
  puts "writer-refused"
else
  raise "over-count accepted"
end
`
	out, _ := rubyFixedRun(t, files, script, nil)
	if strings.TrimSpace(out) != "writer-refused" {
		t.Fatalf("writer: %s", out)
	}
}

func TestRubyFixedProofShapesNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Ruby interpreters and C++ fixed-form oracle")
	cases := []struct {
		name, schema, setup, anchor, replacement string
		body, offset                             int
		value                                    byte
	}{
		{"text", "package probe\nfixed table T { s string(4) }\n", `memcpy(value[0].s, "ab", 2); value[0].s_length=2;`, `if str.include?("\x00")`, `if false`, 8, 5, 0},
		{"array", "package probe\nfixed table T { a [..2]uint16 }\n", `value[0].a_count=2; value[0].a[0]=7; value[0].a[1]=9;`, `if count < min_bound || count > spec["bound"]
          raise Refused.new("count_over_bound")`, `if count < min_bound || count > spec["bound"] + 1
          raise Refused.new("count_over_bound")`, 8, 0, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := fixedproof.ParseCheckUnit(t, tc.schema)
			wire, _ := fixedproof.CppOracle(t, fixedproof.RowCase{Schema: tc.schema, Root: "T", Body: int64(tc.body), Count: 1, CppSetup: tc.setup}, u)
			wire[len(wire)-tc.body+tc.offset] = tc.value
			var files map[string][]byte
			var err error
			if tc.name == "array" {
				reader := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T { a [..4]uint16 }\n")
				entry, ok := fixedproof.LineageEntryOf(u, "T")
				if !ok {
					t.Fatal("missing writer lineage")
				}
				files, err = rubytable.GenerateLineage(reader, rubyFixedLineage(map[string][]fixedproof.LineageEntry{"T": {entry}}))
			} else {
				files, err = rubytable.Generate(u)
			}
			if err != nil {
				t.Fatal(err)
			}
			rpath := "probe/_fixed.rb"
			source := string(files[rpath])
			if strings.Count(source, tc.anchor) != 1 {
				t.Fatalf("%s control anchor count = %d", tc.name, strings.Count(source, tc.anchor))
			}
			files[rpath] = []byte(strings.Replace(source, tc.anchor, tc.replacement, 1))
			probe := `# frozen_string_literal: true
require_relative "probe"
begin
  Probe::T.fixed_load(File.binread("wire"))
rescue Probe::Refused
  # refused
else
  abort("shape_control_detected")
end
`
			for _, pin := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
				dir := rubyFixedTree(t, files)
				_, stderr, rerr := rubyrun.Try(t, pin, dir, "main.rb", map[string][]byte{"wire": wire, "main.rb": []byte(probe)})
				if rerr == nil || !strings.Contains(stderr, "shape_control_detected") {
					t.Fatalf("%s fault was not detected by name: %v %s", tc.name, rerr, stderr)
				}
			}
		})
	}
}
