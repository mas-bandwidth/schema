package pythontable

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"strings"
	"testing"
)

func TestPythonRetainRequiredDefault(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	const schema = `package probe
table Root { n int32 | required }
`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	w := &unionWire{}
	n := unionCat(w.ref("n"), []byte{4}, unionI32(0))
	ghost := unionCat(w.ref("future"), []byte{4}, unionI32(7))
	wire := w.file(n, ghost)
	missing := &unionWire{}
	bad := missing.file(unionCat(missing.ref("future"), []byte{4}, unionI32(7)))
	script := pyrun.ImportPrelude + `from probe import Root, TableRetain, LoadReport, Refused
with open('wire','rb') as f: wire=f.read()
with open('missing','rb') as f: missing=f.read()
keep=TableRetain(bytearray(1024),bytearray(24*8)); report=LoadReport()
value=Root.load_retain(wire,keep,report)
assert value.n==0 and report.retained==1
assert value.measure_retain(keep)==len(wire)
assert value.save_retain(keep,report)==wire and report.retain_lost==0
unchanged=LoadReport(); unchanged.unknown=77
try: Root.load_retain(missing,keep,unchanged)
except Refused as err: assert err.reason=='missing_required' and unchanged.unknown==77
else: raise AssertionError('missing required field accepted')
print('retain-required')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "wire": wire, "missing": bad})
		if err != nil || strings.TrimSpace(out) != "retain-required" {
			t.Fatalf("retaining required: %v\n%s\n%s", err, out, stderr)
		}
		if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
			t.Fatalf("mypy: %v\n%s", err, out)
		}
	}
}
