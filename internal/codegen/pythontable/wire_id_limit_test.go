package pythontable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestPythonWireKnownIDLimit(t *testing.T) {
	slowtest.Gate(t, "both pinned Python interpreters")
	var schema strings.Builder
	schema.WriteString("package probe\nenum Many { ")
	for i := 1; i <= 4096; i++ {
		if i > 1 {
			schema.WriteString(", ")
		}
		fmt.Fprintf(&schema, "V%d", i)
	}
	schema.WriteString(" }\ntable Root { values []Many }\n")
	u := loadUnit(t, schema.String())
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	script := pyrun.ImportPrelude + `from probe import Root
value = Root()
value.values = list(range(1, 4096))
wire = value.save()
assert int.from_bytes(wire[-8:], 'little') == 4096
assert value.measure() == len(wire) and Root.load(wire).values == value.values
value.values.append(4096)
for operation in (value.measure, value.save):
    try: operation()
    except ValueError as error: assert '4096' in str(error), error
    else: raise AssertionError('4097 known ids accepted')
value.values.pop()
assert value.save() == wire
with open('saved', 'wb') as f: f.write(wire)
print('known-wire-bound')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err != nil || strings.TrimSpace(out) != "known-wire-bound" {
			t.Fatalf("known wire limit: %v\n%s\n%s", err, out, stderr)
		}
		saved, err := os.ReadFile(filepath.Join(dir, "saved"))
		if err != nil {
			t.Fatal(err)
		}
		if verdict := unionEngineVerdict(t, u, saved); !strings.HasPrefix(verdict, "read ") {
			t.Fatalf("Go engine rejected boundary: %s", verdict)
		}
	}
}
