package pythontable_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestPythonFixedIntegrityCRC(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Python fixed-table CRC32C")
	schema := "package probe\nfixed table T | integrity = \"crc32c\" { n int32 }\n"
	u := fixedproof.ParseCheckUnit(t, schema)
	tc := fixedproof.RowCase{ID: "fixed-crc", Schema: schema, Root: "T", Body: 4, Count: 2, CppSetup: "value[0].n = 7; value[1].n = 8;"}
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
	files, err := pythontable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, files)
		if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
			t.Fatalf("mypy: %v\n%s", err, out)
		}
		probe := pyrun.ImportPrelude + `from probe import T, Refused, LoadReport
with open("multi", "rb") as f: multi = f.read()
with open("single", "rb") as f: single = f.read()
a = T(); a.n = 7
b = T(); b.n = 8
assert a.save() == single and a.measure() == len(single), "single fixed CRC"
assert T.load(single).n == 7
assert T.fixed_save([a,b]) == multi and T.fixed_measure(2) == len(multi), "batch fixed CRC"
assert [x.n for x in T.fixed_load(multi)] == [7,8]
for data, reason in ((multi[:-1] + bytes([multi[-1] ^ 1]), "integrity_mismatch"),
                     (multi[:8] + bytes([multi[8] ^ 1]) + multi[9:], "integrity_mismatch"),
                     (b"\x03", "integrity_mismatch"), (b"", "truncated"),
                     (b"\x01", "previous_form"), (b"\x02", "message_form_as_file")):
    r = LoadReport(); r.unknown = 77
    try: T.fixed_load(data, report=r)
    except Refused as e: assert e.reason == reason and r.unknown == 77, e.reason
    else: raise AssertionError("bad fixed CRC accepted")
print("ok")
`
		stdout, stderr, err := pyrun.Try(t, python, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "multi": cpp, "single": single})
		if err != nil || stdout != "ok\n" {
			t.Fatalf("fixed CRC: %v %s %s", err, stdout, stderr)
		}
		if err := verify(cpp); err != nil {
			t.Fatal(err)
		}
	}
	if bytes.Equal(cpp, single) {
		t.Fatal("batch unexpectedly equals one record")
	}
}

func TestPythonFixedIntegrityCRCNegativeControl(t *testing.T) {
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
	for _, tc := range []struct{ name, anchor, replacement, probe, failure string }{
		{"reader", `if declared_root is not None and declared_root["integrity"]:`, `if False:`, `from probe import T, Refused
with open("wire", "rb") as f: data=f.read()
try: T.fixed_load(data[:-1] + bytes([data[-1] ^ 1]))
except Refused as e: assert e.reason == "integrity_mismatch", "fixed CRC reader bypassed"
else: raise AssertionError("fixed CRC reader bypassed")
`, "fixed CRC reader bypassed"},
		{"writer", `return seal_crc(result) if root["integrity"] else result`, `return result`, `from probe import T
with open("wire", "rb") as f: data=f.read()
v=T();v.n=7
assert v.save() == data, "fixed CRC writer bypassed"
`, "fixed CRC writer bypassed"},
		{"measure", `+ (4 if root["integrity"] else 0)`, `+ 0`, `from probe import T
with open("wire", "rb") as f: data=f.read()
assert T.fixed_measure(1) == len(data), "fixed CRC measure bypassed"
`, "fixed CRC measure bypassed"},
	} {
		files, err := pythontable.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		path := "probe/_schema/fixedform.py"
		if strings.Count(string(files[path]), tc.anchor) != 1 {
			t.Fatalf("%s: missing or repeated anchor", tc.name)
		}
		files[path] = []byte(strings.Replace(string(files[path]), tc.anchor, tc.replacement, 1))
		for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			dir := pythonFixedTree(t, files)
			_, stderr, err := pyrun.Try(t, python, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + tc.probe), "wire": wire})
			if err == nil || !strings.Contains(stderr, tc.failure) {
				t.Fatalf("%s: %v %s", tc.name, err, stderr)
			}
		}
	}
}
