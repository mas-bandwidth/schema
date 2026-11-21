package pythontable_test

import (
	"encoding/binary"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v3/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/ir"
)

func TestPythonFixedFormRefusedHash(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	old := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T { x int8 }\n")
	newer := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T { x int16 }\n")
	entry, _ := fixedproof.LineageEntryOf(old, "T")
	retired := fixedproof.MakeFixedFile(entry.Wire, entry.Layout, []byte{7})
	unknown := append([]byte(nil), retired...)
	binary.LittleEndian.PutUint64(unknown[8:16], 1)
	binary.LittleEndian.PutUint64(unknown[20+len(entry.Layout):], 1)
	files, err := pythontable.GenerateLineage(newer, map[string][]pythontable.FixedLineageEntry{"T": {{Wire: entry.Wire, Layout: entry.Layout, Record: entry.Record, Writer: entry.Writer, Digest: ir.TableFixedDefinitionsDigest(entry.Writer), Retired: true}}})
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from probe import T, Refused, FixedLoadReport
for name,reason in [('retired','layout_unsupported'),('unknown','layout_unknown')]:
 with open(name,'rb') as f:wire=f.read()
 report=FixedLoadReport();report.layout_hash=123;report.widened=456;report.unknown=789
 try:T.fixed_load(wire,report=report)
 except Refused as err:
  assert err.reason==reason
  expected=reason+': 0x'+format(int.from_bytes(wire[8:16],'little'),'016x')
  assert str(err)==expected,'fixed_refused_hash_control_'+reason
 else:raise AssertionError('fixed layout unexpectedly accepted')
 assert (report.layout_hash,report.widened,report.unknown)==(123,456,789)
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, files)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "retired": retired, "unknown": unknown})
		if err != nil {
			t.Fatalf("refused hash: %v\n%s", err, stderr)
		}
	}
	for _, reason := range []string{"layout_unknown", "layout_unsupported"} {
		broken := maps.Clone(files)
		name := "probe/_schema/fixedform.py"
		anchor := `raise Refused("` + reason + `", f"0x{wire_hash:016x}")`
		if strings.Count(string(broken[name]), anchor) != 1 {
			t.Fatalf("%s hash control lost its anchor", reason)
		}
		broken[name] = []byte(strings.Replace(string(broken[name]), anchor, `raise Refused("`+reason+`")`, 1))
		for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			dir := pythonFixedTree(t, broken)
			_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "retired": retired, "unknown": unknown})
			if err == nil || !strings.Contains(stderr, "fixed_refused_hash_control_"+reason) {
				t.Fatalf("%s hash sabotage did not fail by name: %v\n%s", reason, err, stderr)
			}
		}
	}
}
