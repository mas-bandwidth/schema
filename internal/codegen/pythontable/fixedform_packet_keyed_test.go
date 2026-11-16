package pythontable_test

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestPythonFixedFormPacketKeyed(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both Python pins and the live C++ oracle")
	tc := fixedproof.RowCase{ID: "python-fixed-packet-keyed", Schema: `package probe
enum Slot { Alpha, Beta }
type Board { values [Slot]int32 | min = 0, max = 100 }
fixed table T { board Board
 direct [Slot]int32 }
`, Root: "T", Count: 1, CppSetup: `value[0].board.values[0]=7; value[0].board.values[1]=11;`}
	u := fixedproof.ParseCheckUnit(t, tc.Schema)
	tc.Body = ir.TableFixedTypeBytes(u.Tables["T"])
	wire, verify := fixedproof.CppOracle(t, tc, u)
	files, err := pythontable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	script := pyrun.ImportPrelude + `from probe import T, InvalidValue
with open('wire','rb') as f: wire=f.read()
rows=T.fixed_load(wire)
assert type(rows[0].board.values) is list and rows[0].board.values==[7,11]
assert hasattr(rows[0].direct,'_values')
assert T.fixed_save(rows)==wire
with open('saved','wb') as f: f.write(T.fixed_save(rows))
for bad in ([0],[0,0,0],[0,101]):
 rows[0].board.values=bad
 try: T.fixed_save(rows)
 except InvalidValue: pass
 else: raise AssertionError('packet-keyed validation')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "wire": wire})
		if err != nil {
			t.Fatalf("packet-keyed fixed: %v\n%s\n%s", err, out, stderr)
		}
		saved, err := os.ReadFile(filepath.Join(dir, "saved"))
		if err != nil {
			t.Fatal(err)
		}
		if err := verify(saved); err != nil {
			t.Fatal(err)
		}
		if output, err := pyrun.Analyze(t, dir, u.Package); err != nil {
			t.Fatalf("strict mypy: %v\n%s", err, output)
		}
	}
	// Misclassifying the plain packet list as a table wrapper breaks the
	// actual load path, which the same probe must catch by name.
	path := u.Package + "/_fixed.py"
	const marker = `"positional":True`
	if !strings.Contains(string(files[path]), marker) {
		t.Fatal("packet-keyed control lost its anchor")
	}
	broken := maps.Clone(files)
	broken[path] = []byte(strings.Replace(string(files[path]), marker, `"positional":False`, 1))
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, broken)
		control := pyrun.ImportPrelude + `from probe import T
with open('wire','rb') as f: wire=f.read()
try: T.fixed_load(wire)
except AttributeError: raise SystemExit('fixed_packet_keyed_control')
raise AssertionError('packet-keyed sabotage was not detected')
`
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(control), "wire": wire})
		if err == nil || !strings.Contains(stderr, "fixed_packet_keyed_control") {
			t.Fatalf("packet-keyed sabotage did not fail by name: %v\n%s", err, stderr)
		}
	}
}
