package pythontable_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func init() {
	pythonFixedSetups["utf8-values"] = "rows[1].s = 'hello'\n"
	pythonFixedSetups["byte-buffers"] = "rows[1].blob = bytes([255, 0, 90, 1, 2, 3])\n"
	pythonFixedSetups["nested-types"] = "rows[0].badge.title = 'hi'\n"
	pythonFixedSetups["nested-fixed-tables"] = "rows[0].child.n = -2147483648\nrows[1].child.n = 2147483647\n"
	pythonFixedSetups["fixed-arrays"] = "rows[0].tag = [-2147483648, 0, 2147483647]\n"
	pythonFixedSetups["counted-arrays"] = "rows[1].nums = [10, 65535, 32768]\n"
	pythonFixedSetups["keyed-arrays"] = "rows[0].slots[1] = -2147483648\nrows[0].slots[2] = 2147483647\n"
	pythonFixedSetups["nested-keyed-arrays"] = "rows[0].slots[1].n = 11\nrows[0].slots[2].n = -22\n"
	pythonFixedSetups["text-bytes-flags-defaults"] = "rows[1].s = 'xyz'\nrows[1].b = bytes([0, 255, 90, 1])\nrows[1].c = 4\n"
}

// TestPythonFixedProofShapes checks each shape against the live C++ writer and
// reader, including independent load values and file/writer negative controls.
func TestPythonFixedProofShapes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters and C++ fixed-form oracle")
	fixedproof.SetMemoryCeiling(t)
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
	runner := pythonFixedProofRunner{}
	rows := []string{"utf8-values", "byte-buffers", "nested-types", "nested-fixed-tables", "fixed-arrays", "counted-arrays", "keyed-arrays", "nested-keyed-arrays", "text-bytes-flags-defaults"}
	t.Run("value-suite", func(t *testing.T) { fixedproof.RunValueRows(t, runner, rows...) })
	t.Run("negative-controls", func(t *testing.T) { fixedproof.RunNegativeControlRows(t, runner, rows...) })
	for _, tc := range fixedproof.Cases() {
		want, selected := expected[tc.ID]
		if !selected {
			continue
		}
		t.Run(tc.ID, func(t *testing.T) {
			u := fixedproof.ParseCheckUnit(t, tc.Schema)
			_, _, body := fixedproof.RootFacts(fixedproof.RootStruct(t, u, tc.Root))
			if body != tc.Body {
				t.Fatalf("body %d, want %d", body, tc.Body)
			}
			oracle, verify := fixedproof.CppOracle(t, tc, u)
			files, err := pythontable.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			dir := pythonFixedTree(t, files)
			if out, err := pyrun.Analyze(t, dir, u.Package); err != nil {
				t.Fatalf("strict mypy: %v\n%s", err, out)
			}
			saved := runner.Save(t, tc, u)
			if !bytes.Equal(saved, oracle) {
				t.Fatalf("Python writer disagrees with C++: got %x want %x", saved, oracle)
			}
			count, reason, fields, roundtrip, _ := runner.LoadLineage(t, u, tc.Root, nil, oracle, tc.Count)
			if count != tc.Count || !bytes.Equal(roundtrip, oracle) {
				t.Fatalf("C++ load/roundtrip count=%d reason=%s", count, reason)
			}
			for key, value := range want {
				if fields[key] != value {
					t.Errorf("loaded %s=%q, want %q", key, fields[key], value)
				}
			}
			if err := verify(saved); err != nil {
				t.Fatalf("C++ load of Python bytes: %v", err)
			}
			damaged := bytes.Clone(oracle)
			damaged[0] ^= 1
			if err := verify(damaged); err == nil {
				t.Error("C++ accepted damaged form")
			}
			if _, err := runner.LoadAndResave(t, tc, u, damaged); err == nil {
				t.Error("Python accepted damaged form")
			}
			mutated, err := runner.SaveMutated(t, tc, u)
			if err != nil {
				t.Fatalf("writer control setup: %v", err)
			}
			if bytes.Equal(mutated, oracle) {
				t.Error("writer control produced unchanged bytes")
			}
			if err := verify(mutated); err == nil {
				t.Error("C++ accepted mutated writer output")
			}
		})
	}
}

// TestPythonFixedProofShapesText checks text bounds, UTF-8, zero bytes and dead
// bytes against a file from the same live C++ reference.
func TestPythonFixedProofShapesText(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters and C++ fixed-form oracle")
	tc := fixedproof.RowCase{ID: "shape-text", Schema: "package probe\nfixed table T { s string(8) }\n", Root: "T", Body: 12, Count: 1, CppSetup: `memcpy(value[0].s, "\xc3\xa9", 2); value[0].s_length = 2;`}
	u := fixedproof.ParseCheckUnit(t, tc.Schema)
	clean, verify := fixedproof.CppOracle(t, tc, u)
	runner := pythonFixedProofRunner{}
	count, reason, fields, saved, _ := runner.LoadLineage(t, u, "T", nil, clean, 1)
	if count != 1 || fields["r0.s"] != `"é"` || !bytes.Equal(saved, clean) {
		t.Fatalf("valid UTF-8: count=%d reason=%s fields=%v", count, reason, fields)
	}
	if err := verify(saved); err != nil {
		t.Fatalf("C++ refused roundtrip: %v", err)
	}
	body := len(clean) - int(tc.Body)
	for _, test := range []struct {
		name   string
		change func([]byte)
		reason string
	}{
		{"length above bound", func(b []byte) { binary.LittleEndian.PutUint32(b[body:], 9) }, "length_over_bound"},
		{"negative length", func(b []byte) { binary.LittleEndian.PutUint32(b[body:], ^uint32(0)) }, "length_over_bound"},
		{"invalid utf8", func(b []byte) { b[body+4] = 0xff }, "text_ill_formed"},
		{"embedded zero", func(b []byte) { b[body+5] = 0 }, "text_ill_formed"},
		{"nonzero dead byte", func(b []byte) { b[body+6] = 0xff }, "wire_damaged"},
	} {
		t.Run(test.name, func(t *testing.T) {
			damaged := bytes.Clone(clean)
			test.change(damaged)
			n, got, _, _, _ := runner.LoadLineage(t, u, "T", nil, damaged, 1)
			if n != -1 || got != test.reason {
				t.Errorf("count=%d reason=%s, want refusal %s", n, got, test.reason)
			}
		})
	}
	files, err := pythontable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	script := `from probe import T, InvalidValue
for v in ('123456789', 'a\0b', '\ud800'):
    x = T(); x.s = v
    try: T.fixed_save([x])
    except InvalidValue: pass
    else: raise AssertionError('invalid text accepted')
print('writer-refused')
`
	out, _ := pythonFixedRun(t, files, script, nil)
	if strings.TrimSpace(out) != "writer-refused" {
		t.Fatalf("writer: %s", out)
	}
}

// TestPythonFixedProofShapesArrayBounds holds the writer's bound when the
// reader widened it, alongside the writer's own over-count refusal.
func TestPythonFixedProofShapesArrayBounds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters and C++ fixed-form oracle")
	old := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T { a [..2]uint16 }\n")
	newer := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T { a [..4]uint16 }\n")
	tc := fixedproof.RowCase{ID: "bounds-old", Schema: "package probe\nfixed table T { a [..2]uint16 }\n", Root: "T", Body: 8, Count: 1, CppSetup: `value[0].a_count=2; value[0].a[0]=7; value[0].a[1]=9;`}
	clean, verify := fixedproof.CppOracle(t, tc, old)
	entry, ok := fixedproof.LineageEntryOf(old, "T")
	if !ok {
		t.Fatal("missing writer lineage")
	}
	lineage := map[string][]fixedproof.LineageEntry{"T": {entry}}
	runner := pythonFixedProofRunner{}
	count, why, fields, _, _ := runner.LoadLineage(t, newer, "T", lineage, clean, 1)
	if count != 1 || fields["r0.a_count"] != "2" || fields["r0.a[1]"] != "9" {
		t.Fatalf("writer bound: count=%d reason=%s fields=%v", count, why, fields)
	}
	if err := verify(clean); err != nil {
		t.Fatalf("C++ writer control: %v", err)
	}
	for _, n := range []uint32{3, ^uint32(0)} {
		bad := bytes.Clone(clean)
		binary.LittleEndian.PutUint32(bad[len(bad)-8:], n)
		count, why, _, _, _ := runner.LoadLineage(t, newer, "T", lineage, bad, 1)
		if count != -1 || why != "count_over_bound" {
			t.Errorf("writer count %d: count=%d reason=%s", n, count, why)
		}
	}
	files, err := pythontable.Generate(newer)
	if err != nil {
		t.Fatal(err)
	}
	script := `from probe import T, InvalidValue
x=T(); x.a=[1,2,3,4,5]
try: T.fixed_save([x])
except InvalidValue: print('writer-refused')
else: raise AssertionError('over-count accepted')
`
	out, _ := pythonFixedRun(t, files, script, nil)
	if strings.TrimSpace(out) != "writer-refused" {
		t.Fatalf("writer: %s", out)
	}
}

// TestPythonFixedProofShapesNegativeControl plants missing read checks in the
// generated runtime and requires each named probe to detect the fault.
func TestPythonFixedProofShapesNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters and C++ fixed-form oracle")
	cases := []struct {
		name, schema, setup, anchor, replacement string
		body, offset                             int
		value                                    byte
	}{
		{"text", "package probe\nfixed table T { s string(4) }\n", `memcpy(value[0].s, "ab", 2); value[0].s_length=2;`, `if "\0" in value:`, `if False:`, 8, 5, 0},
		{"array", "package probe\nfixed table T { a [..2]uint16 }\n", `value[0].a_count=2; value[0].a[0]=7; value[0].a[1]=9;`, `if count < spec.get("minimum", 0) or count > spec["bound"]:
                raise Refused("count_over_bound")`, `if count < spec.get("minimum", 0) or count > spec["bound"] + 1:
                raise Refused("count_over_bound")`, 8, 0, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := fixedproof.ParseCheckUnit(t, tc.schema)
			wire, _ := fixedproof.CppOracle(t, fixedproof.RowCase{Schema: tc.schema, Root: "T", Body: int64(tc.body), Count: 1, CppSetup: tc.setup}, u)
			wire[len(wire)-tc.body+tc.offset] = tc.value
			files, err := pythontable.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			path := "probe/_schema/fixedform.py"
			source := string(files[path])
			if strings.Count(source, tc.anchor) != 1 {
				t.Fatalf("%s control anchor changed", tc.name)
			}
			files[path] = []byte(strings.Replace(source, tc.anchor, tc.replacement, 1))
			probe := `from probe import T, Refused
with open('wire','rb') as f: wire=f.read()
try: T.fixed_load(wire)
except Refused: pass
else: raise SystemExit('shape_control_detected')
`
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				dir := pythonFixedTree(t, files)
				_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"wire": wire, "main.py": []byte(pyrun.ImportPrelude + probe)})
				if err == nil || !strings.Contains(stderr, "shape_control_detected") {
					t.Fatalf("%s fault was not detected by name: %v %s", tc.name, err, stderr)
				}
			}
		})
	}
}
