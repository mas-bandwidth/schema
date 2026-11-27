package pythontable

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

// TestPythonIDOrder checks wire first-use order, not reader declaration order.
func TestPythonIDOrder(t *testing.T) {
	t.Parallel()
	writer := loadUnit(t, "package idorder\ntable Root { b int32\n a int32 }\n")
	reader := loadUnit(t, "package idorder\ntable Root { a int32\n b int32 }\n")
	model := tabletext.NewModel(writer)
	inst := model.New(model.Lookup("Root"))
	var report tabletext.Report
	if !model.Read(inst, []byte(`{"a":1,"b":2}`), &report) || report.Refused {
		t.Fatalf("engine fixture: %+v", report)
	}
	valid, err := tablewire.EncodeVariable(model, inst)
	if err != nil {
		t.Fatal(err)
	}
	if len(valid) != 38 || valid[1] != 1 || valid[7] != 2 {
		t.Fatalf("fixture layout changed: %x", valid)
	}
	// Swap both references and trailer entries: field values stay the same,
	// but the table no longer follows the references' first uses.
	reordered := append([]byte(nil), valid...)
	reordered[1], reordered[7] = 2, 1
	copy(reordered[14:22], valid[22:30])
	copy(reordered[22:30], valid[14:22])
	unused := append([]byte(nil), valid[:len(valid)-8]...)
	unused = binary.LittleEndian.AppendUint64(unused, 0x123456789abcdef0)
	unused = binary.LittleEndian.AppendUint64(unused, 3)
	// A structurally valid but mismatched field still reports ID damage first.
	wrongKindReordered := append([]byte(nil), reordered...)
	wrongKindReordered[2] = 8 // uint32 instead of the reader's int32
	wrongKindUnused := append([]byte(nil), unused...)
	wrongKindUnused[2] = 8
	rm := tabletext.NewModel(reader)
	for _, data := range [][]byte{reordered, unused, wrongKindReordered, wrongKindUnused} {
		var r tabletext.Report
		if ok, _ := tablewire.DecodeVariable(rm, rm.New(rm.Lookup("Root")), data, &r); ok || !r.Refused || !strings.Contains(r.Reason, "wire_damaged") {
			t.Fatalf("engine accepted malformed id table: %+v", r)
		}
	}
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from idorder import Root, Refused
with open('valid','rb') as f:v=Root.load(f.read())
assert (v.a,v.b)==(1,2)
for name in ('reordered','unused','wrong_kind_reordered','wrong_kind_unused'):
 with open(name,'rb') as f:data=f.read()
 try:Root.load(data)
 except Refused as e:assert e.reason=='wire_damaged'
 else:raise AssertionError('id_first_use_'+name)
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe), "valid": valid, "reordered": reordered, "unused": unused, "wrong_kind_reordered": wrongKindReordered, "wrong_kind_unused": wrongKindUnused}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("first-use: %v\n%s\n%s", err, out, stderr)
		}
	}
	// Sabotage each check independently: the same probe must expose both losses.
	for _, fault := range []struct{ old, replacement string }{
		{"if self.order != list(range(len(self.used))):", "if False:"},
		{"if len(self.used) != len(self):", "if False:"},
	} {
		broken := make(map[string][]byte, len(files))
		found := false
		for name, contents := range files {
			broken[name] = contents
			if strings.HasSuffix(name, "_schema/load.py") && strings.Contains(string(contents), fault.old) {
				broken[name] = []byte(strings.Replace(string(contents), fault.old, fault.replacement, 1))
				found = true
			}
		}
		if !found {
			t.Fatalf("control anchor absent: %s", fault.old)
		}
		faultDir := t.TempDir()
		writeTree(t, faultDir, broken)
		for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			out, stderr, err := pyrun.Try(t, pin, faultDir, "main.py", inputs)
			if err == nil || !strings.Contains(out+stderr, "id_first_use_") {
				t.Fatalf("control undetected: %s: %v\n%s\n%s", fault.old, err, out, stderr)
			}
		}
	}
	if out, err := pyrun.Analyze(t, dir, "idorder"); err != nil {
		t.Fatalf("mypy: %v\n%s", err, out)
	}
}

// The unknown fields still contribute IDs, including records walked after the root.
func TestPythonIDOrderUnknownGraphCpp(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C++ and both Python pins")
	c := wideVTConstructs[len(wideVTConstructs)-1]
	writer := loadUnit(t, c.schema)
	reader := loadUnit(t, "package wide\ntable Root { n uint128 }\n")
	wire := cppWideWire(t, writer, c)
	if engine := wideEngineWire(t, writer, c.root, c.json); !bytes.Equal(wire, engine) {
		t.Fatalf("C++ and Go differ: %x / %x", wire, engine)
	}
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from wide import Root
with open('wire','rb') as f: value=Root.load(f.read())
assert value.n == (1 << 128)-1
print('ok')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("unknown graph: %v\n%s\n%s", err, out, stderr)
		}
	}
}

func TestPythonIDOrderUnknownNestedCpp(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C++ and both Python pins")
	c := wideVTConstruct{name: "nested", schema: `package idnested
 table Inner { a int32
 b int32 }
 table Root { extra Inner
 known int32 }
 `, root: "Root", json: `{"extra":{"a":7,"b":8},"known":3}`}
	writer := loadUnit(t, c.schema)
	reader := loadUnit(t, "package idnested\ntable Root { known int32 }\n")
	wire := cppWideWire(t, writer, c)
	if engine := wideEngineWire(t, writer, c.root, c.json); !bytes.Equal(wire, engine) {
		t.Fatalf("C++ and Go differ: %x / %x", wire, engine)
	}
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from idnested import Root
with open('wire','rb') as f: value=Root.load(f.read())
assert value.known == 3
print('ok')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("unknown nested: %v\n%s\n%s", err, out, stderr)
		}
	}
}
