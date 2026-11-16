package pythontable

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestPythonVariableIntegrityCRC(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Python variable-table CRC32C")
	schema := `package crcprobe
 table Root | integrity = "crc32c" { n int32 }
 table Wrapper { inner Root }
`
	u := loadUnit(t, schema)
	text := `{"n":7}`
	wire := wideEngineWire(t, u, "Root", text)
	cpp := cppWideWire(t, u, wideVTConstruct{root: "Root", json: text})
	if !bytes.Equal(cpp, wire) {
		t.Fatalf("C++ writer differs: %x %x", cpp, wire)
	}
	outer := wideEngineWire(t, u, "Wrapper", `{"inner":{"n":7}}`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	if out, err := pyrun.Analyze(t, dir, "crcprobe"); err != nil {
		t.Fatalf("mypy: %v\n%s", err, out)
	}
	probe := pyrun.ImportPrelude + `from crcprobe import Root, Wrapper, Refused, LoadReport
from crcprobe._schema.integrity import crc32c, open_crc
assert crc32c(b"123456789") == 0xe3069283
with open("wire", "rb") as f: wire = f.read()
with open("outer", "rb") as f: outer = f.read()
v = Root(); v.n = 7
assert v.save() == wire and v.measure() == len(wire), "root CRC save"
assert Root.load(wire).n == 7
assert Root.load(bytearray(wire)).n == 7 and Root.load(memoryview(wire)).n == 7
assert open_crc(wire).obj is wire
w = Wrapper(); w.inner.n = 7
assert w.save() == outer and Wrapper.load(outer).inner.n == 7, "nested CRC must not add a trailer"
for name, data, reason in (("corrupt", wire[:-1] + bytes([wire[-1] ^ 1]), "integrity_mismatch"),
                           ("short", bytes([1]), "integrity_mismatch"),
                           ("body_damage", wire[:1] + b"\xff" + wire[2:], "integrity_mismatch"),
                           ("empty", b"", "wire_damaged"),
                           ("newer", bytes([7]), "newer_form")):
    report = LoadReport(); report.unknown = 77
    try: Root.load(data, report)
    except Refused as err:
        assert err.reason == reason and report.unknown == 77, (name, err.reason)
    else: raise AssertionError(name + " accepted")
print("ok")
`
	inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire, "outer": outer}
	for _, got := range [][]byte{pyrun.Run(t, dir, "main.py", inputs), pyrun.RunFloor(t, dir, "main.py", inputs)} {
		if string(got) != "ok\n" {
			t.Fatalf("probe: %q", got)
		}
	}
	t.Logf("Go/C++ CRC fixture: %x (%d bytes)", wire, len(wire))
}

func TestPythonVariableIntegrityCRCNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Python variable-table CRC32C negative controls")
	schema := `package crcprobe
table Root | integrity = "crc32c" { n int32 }
`
	u := loadUnit(t, schema)
	wire := wideEngineWire(t, u, "Root", `{"n":7}`)
	for _, tc := range []struct{ name, anchor, replacement, probe, failure string }{
		{"reader", `if int.from_bytes(view[-4:], "little") != crc32c(body):`, `if False:`, `from crcprobe import Root, Refused
with open("wire", "rb") as f: wire = f.read()
try: Root.load(wire[:-1] + bytes([wire[-1] ^ 1]))
except Refused: pass
else: raise AssertionError("CRC reader accepted corruption")
`, "CRC reader accepted corruption"},
		{"writer", `return data + crc32c(data).to_bytes(4, "little")`, `return data`, `from crcprobe import Root
with open("wire", "rb") as f: wire = f.read()
v = Root(); v.n = 7
assert v.save() == wire, "CRC writer omitted trailer"
`, "CRC writer omitted trailer"},
	} {
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		path := "crcprobe/_schema/integrity.py"
		if strings.Count(string(files[path]), tc.anchor) != 1 {
			t.Fatalf("%s: sabotage anchor missing or repeated", tc.name)
		}
		files[path] = []byte(strings.Replace(string(files[path]), tc.anchor, tc.replacement, 1))
		dir := t.TempDir()
		writeTree(t, dir, files)
		inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + tc.probe), "wire": wire}
		for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			_, stderr, err := pyrun.Try(t, python, dir, "main.py", inputs)
			if err == nil || !strings.Contains(stderr, tc.failure) {
				t.Fatalf("%s control: %v %s", tc.name, err, stderr)
			}
		}
	}
}

func TestPythonIntegrityEnvelope(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Python CRC envelope form ordering")
	u := loadUnit(t, "package crcprobe\ntable Root { n int32 }\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from crcprobe import Refused
from crcprobe._schema.integrity import seal_crc, open_crc
for form, prefix in ((1, b"\x01abc"), (2, b"\x02\x00abc"), (3, b"\x03abc")):
    sealed = seal_crc(prefix)
    assert bytes(open_crc(sealed, form)) == prefix
    for data, reason in ((bytes([form]), "integrity_mismatch"),
                         (sealed[:-1] + bytes([sealed[-1] ^ 1]), "integrity_mismatch"),
                         (b"", "truncated" if form == 3 else "wire_damaged")):
        try: open_crc(data, form)
        except Refused as e: assert e.reason == reason, (form, e.reason)
        else: raise AssertionError("accepted damaged envelope")
for form, data, reason in ((1, b"\x02", "message_form_as_file"),
                           (1, b"\x03", "fixed_form_as_file"),
                           (2, b"\x01", "newer_form"),
                           (3, b"\x01", "previous_form"),
                           (3, b"\x02", "message_form_as_file")):
    try: open_crc(data, form)
    except Refused as e: assert e.reason == reason, (form, e.reason)
    else: raise AssertionError("accepted wrong form")
print("ok")
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	for _, out := range [][]byte{pyrun.Run(t, dir, "main.py", inputs), pyrun.RunFloor(t, dir, "main.py", inputs)} {
		if string(out) != "ok\n" {
			t.Fatalf("envelope: %q", out)
		}
	}
}
