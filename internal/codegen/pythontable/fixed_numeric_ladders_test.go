package pythontable_test

import (
	"bytes"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestPythonFixedNumericLadders compares every adjacent signed and unsigned
// integer widening, including 64 to 128 bits, against live C++ old/new files.
func TestPythonFixedNumericLadders(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters, strict mypy, and live C++")
	oldSchema := `package probe
fixed table T {
 s8 int8
 s16 int16
 s32 int32
 s64 int64 | min = -9223372036854775807, max = 9223372036854775807
 u8 uint8
 u16 uint16
 u32 uint32
 u64 uint64
}
`
	newSchema := `package probe
fixed table T {
 s8 int16
 s16 int32
 s32 int64
 s64 int128 | min = -9223372036854775807, max = 9223372036854775807
 u8 uint16
 u16 uint32
 u32 uint64
 u64 uint128
 added int32 = 77
}
`
	setup := `value[0].s8 = -127; value[0].s16 = -32768; value[0].s32 = -2147483647 - 1; value[0].s64 = -9223372036854775807ll; value[0].u8 = 255; value[0].u16 = 65535; value[0].u32 = 0xffffffffu; value[0].u64 = 0xffffffffffffffffull;`
	old, newer := fixedproof.ParseCheckUnit(t, oldSchema), fixedproof.ParseCheckUnit(t, newSchema)
	_, _, oldBody := fixedproof.RootFacts(old.Tables["T"])
	_, _, newBody := fixedproof.RootFacts(newer.Tables["T"])
	oldWire, verifyOld, oldLoad := fixedproof.CppOracleWithLoadVerdict(t, fixedproof.RowCase{ID: "ladder-old", Root: "T", Body: oldBody, Count: 1, CppSetup: setup}, old)
	newWire, verifyNew := fixedproof.CppOracle(t, fixedproof.RowCase{ID: "ladder-new", Root: "T", Body: newBody, Count: 1, CppSetup: setup}, newer)
	entry, ok := fixedproof.LineageEntryOf(old, "T")
	if !ok {
		t.Fatal("missing historical writer")
	}
	files, err := pythontable.GenerateLineage(newer, map[string][]pythontable.FixedLineageEntry{"T": {{Wire: entry.Wire, Layout: entry.Layout, Record: entry.Record, Digest: entry.Digest, Writer: old.Tables["T"]}}})
	if err != nil {
		t.Fatal(err)
	}
	script := `from probe import T, LoadReport
with open('old','rb') as f: wire = f.read()
report = LoadReport()
rows = T.fixed_load(wire, report=report)
r = rows[0]
if (r.s8,r.s16,r.s32,r.s64,r.u8,r.u16,r.u32,r.u64,r.added,report.widened) != (-127,-32768,-2147483648,-9223372036854775807,255,65535,4294967295,18446744073709551615,77,8):
    raise AssertionError('ladder_control')
with open('saved','wb') as f: f.write(T.fixed_save(rows))
print('ladder')
`
	out, saved := pythonFixedRun(t, files, script, map[string][]byte{"old": oldWire})
	if strings.TrimSpace(out) != "ladder" || !bytes.Equal(saved, newWire) {
		t.Fatal("widened values or current bytes differ from C++")
	}
	if err := verifyOld(oldWire); err != nil {
		t.Fatalf("old C++: %v", err)
	}
	if err := verifyNew(saved); err != nil {
		t.Fatalf("new C++: %v", err)
	}
	if err := oldLoad(newWire); err == nil {
		t.Fatal("old C++ reader accepted the new layout")
	}
	oldFiles, err := pythontable.Generate(old)
	if err != nil {
		t.Fatal(err)
	}
	backward := `from probe import T, Refused, LoadReport
with open('new','rb') as f: wire = f.read()
report = LoadReport()
report.widened = 19
report.unknown = 23
try:
    T.fixed_load(wire, report=report)
except Refused as e:
    if e.reason != 'layout_unknown': raise AssertionError('wrong_backward_reason')
else:
    raise AssertionError('backward_read_accepted')
if (report.widened, report.unknown) != (19, 23): raise AssertionError('backward_report_changed')
print('backward-refused')
`
	if out, _ := pythonFixedRun(t, oldFiles, backward, map[string][]byte{"new": newWire}); strings.TrimSpace(out) != "backward-refused" {
		t.Fatalf("backward refusal: %s", out)
	}
	dir := pythonFixedTree(t, files)
	if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
		t.Fatalf("strict mypy: %v\n%s", err, out)
	}
	source := string(files["probe/_schema/fixedform.py"])
	anchor := `_integer(source, src, size, op["signed"])`
	if strings.Count(source, anchor) != 1 {
		t.Fatal("signed control anchor changed")
	}
	countAnchor := `widened += _apply(op, body, image)`
	if strings.Count(source, countAnchor) != 2 {
		t.Fatal("widened counter control anchors changed")
	}
	for _, tc := range []struct{ name, changed string }{
		{"signed", strings.Replace(source, anchor, `_integer(source, src, size, False)`, 1)},
		{"counter", strings.ReplaceAll(source, countAnchor, `_apply(op, body, image)`)},
	} {
		mutant := maps.Clone(files)
		mutant["probe/_schema/fixedform.py"] = []byte(tc.changed)
		for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			dir := pythonFixedTree(t, mutant)
			_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + script), "old": oldWire})
			if err == nil || !strings.Contains(stderr, "ladder_control") {
				t.Fatalf("%s mutation escaped: %v\n%s", tc.name, err, stderr)
			}
		}
	}
}

// TestPythonFixedUnsignedFixedpoint checks that increasing I preserves the
// unsigned raw scale and does not sign-extend the high bit.
func TestPythonFixedUnsignedFixedpoint(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both Python pins, strict mypy and live C++")
	old := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T { q ufixed(4,4) | min = 0, max = 15 }\n")
	newer := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T { q ufixed(12,4) | min = 0, max = 15 }\n")
	_, _, oldBody := fixedproof.RootFacts(old.Tables["T"])
	_, _, newBody := fixedproof.RootFacts(newer.Tables["T"])
	setup := "value[0].q = 240;"
	oldWire, verifyOld := fixedproof.CppOracle(t, fixedproof.RowCase{ID: "ufixed-old", Root: "T", Body: oldBody, Count: 1, CppSetup: setup}, old)
	newWire, verifyNew := fixedproof.CppOracle(t, fixedproof.RowCase{ID: "ufixed-new", Root: "T", Body: newBody, Count: 1, CppSetup: setup}, newer)
	entry, ok := fixedproof.LineageEntryOf(old, "T")
	if !ok {
		t.Fatal("missing writer")
	}
	files, err := pythontable.GenerateLineage(newer, map[string][]pythontable.FixedLineageEntry{"T": {{Wire: entry.Wire, Layout: entry.Layout, Record: entry.Record, Digest: entry.Digest, Writer: old.Tables["T"]}}})
	if err != nil {
		t.Fatal(err)
	}
	script := `from probe import T, LoadReport
with open('old','rb') as f: wire=f.read()
r=LoadReport(); rows=T.fixed_load(wire,report=r)
if rows[0].q != 240 or r.widened != 1: raise AssertionError('ufixed_control')
with open('saved','wb') as f: f.write(T.fixed_save(rows))
print('ufixed')
`
	out, saved := pythonFixedRun(t, files, script, map[string][]byte{"old": oldWire})
	if strings.TrimSpace(out) != "ufixed" || !bytes.Equal(saved, newWire) {
		t.Fatal("unsigned fixed-point bytes differ from C++")
	}
	if err := verifyOld(oldWire); err != nil {
		t.Fatal(err)
	}
	if err := verifyNew(saved); err != nil {
		t.Fatal(err)
	}
	dir := pythonFixedTree(t, files)
	if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
		t.Fatalf("mypy: %v\n%s", err, out)
	}
	path := "probe/_schema/fixedform.py"
	source := string(files[path])
	anchor := `_integer(source, src, size, op["signed"])`
	if strings.Count(source, anchor) != 1 {
		t.Fatal("unsigned control anchor changed")
	}
	mutant := maps.Clone(files)
	mutant[path] = []byte(strings.Replace(source, anchor, `_integer(source, src, size, True)`, 1))
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, mutant)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + script), "old": oldWire})
		if err == nil || (!strings.Contains(stderr, "ufixed_control") && !strings.Contains(stderr, "OverflowError")) {
			t.Fatalf("sign-extension control escaped: %v\n%s", err, stderr)
		}
	}
}
