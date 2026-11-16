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

func TestPythonWireKnownKeyedIDLimit(t *testing.T) {
	slowtest.Gate(t, "both pinned Python interpreters")
	for _, kind := range []string{"scalar", "table"} {
		t.Run(kind, func(t *testing.T) {
			var schema strings.Builder
			schema.WriteString("package probe\nenum Many { ")
			for i := 1; i <= 4094; i++ {
				if i > 1 {
					schema.WriteString(", ")
				}
				fmt.Fprintf(&schema, "V%d", i)
			}
			schema.WriteString(" }\nenum Slot { A, B }\ntable Child { n int32 }\ntable Root { values []Many\n")
			if kind == "scalar" {
				schema.WriteString(" slots [Slot]int32 }\n")
			} else {
				schema.WriteString(" slots [Slot]Child }\n")
			}
			u := loadUnit(t, schema.String())
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			script := pyrun.ImportPrelude + `from probe import Root
value=Root()
`
			if kind == "scalar" {
				script += "value.values=list(range(1,4094))\nvalue.slots[1]=7\n"
			} else {
				script += "value.values=list(range(1,4093))\nvalue.slots[1].n=7\n"
			}
			script += `wire=value.save()
assert int.from_bytes(wire[-8:], 'little') == 4096
assert value.measure() == len(wire)
again=Root.load(wire)
`
			if kind == "scalar" {
				script += "assert again.slots[1]==7 and again.slots[2]==0\n"
			} else {
				script += "assert again.slots[1].n==7 and again.slots[2].n==0\n"
			}
			script += `value.values.append(len(value.values)+1)
for operation in (value.save, value.measure):
    try: operation()
    except ValueError as error: assert '4096' in str(error), error
    else: raise AssertionError('known keyed overflow accepted')
with open('saved', 'wb') as f: f.write(wire)
print('keyed-bound')
`
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				dir := t.TempDir()
				writeTree(t, dir, files)
				out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
				if err != nil || strings.TrimSpace(out) != "keyed-bound" {
					t.Fatalf("keyed limit: %v\n%s\n%s", err, out, stderr)
				}
				saved, err := os.ReadFile(filepath.Join(dir, "saved"))
				if err != nil {
					t.Fatal(err)
				}
				if verdict := unionEngineVerdict(t, u, saved); !strings.HasPrefix(verdict, "read ") {
					t.Fatalf("Go engine rejected keyed boundary: %s", verdict)
				}
			}
		})
	}
}

func TestPythonWireFastWriterIDLimit(t *testing.T) {
	slowtest.Gate(t, "both pinned Python interpreters")
	var schema strings.Builder
	schema.WriteString("package probe\ntable Root {\n")
	for i := 0; i < 4097; i++ {
		fmt.Fprintf(&schema, " f%d int32\n", i)
	}
	schema.WriteString("}\n")
	u := loadUnit(t, schema.String())
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	script := pyrun.ImportPrelude + `import sys
sys.setrecursionlimit(100000)
from probe import Root
value=Root()
for i in range(4096): setattr(value, 'f'+str(i), 1)
wire=value.save()
assert int.from_bytes(wire[-8:], 'little') == 4096
assert value.measure() == len(wire)
value.f4096=1
for operation in (value.save, value.measure):
    try: operation()
    except ValueError as error: assert '4096' in str(error), error
    else: raise AssertionError('fast writer accepted 4097 IDs')
with open('saved', 'wb') as f: f.write(wire)
print('fast-bound')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err != nil || strings.TrimSpace(out) != "fast-bound" {
			t.Fatalf("fast writer: %v\n%s\n%s", err, out, stderr)
		}
		saved, err := os.ReadFile(filepath.Join(dir, "saved"))
		if err != nil {
			t.Fatal(err)
		}
		if verdict := unionEngineVerdict(t, u, saved); !strings.HasPrefix(verdict, "read ") {
			t.Fatalf("Go engine rejected fast boundary: %s", verdict)
		}
	}
}
