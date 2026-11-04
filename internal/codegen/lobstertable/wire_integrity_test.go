package lobstertable_test

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestLobsterVariableWireIntegrity(t *testing.T) {
	slowtest.Gate(t, "lobster variable wire integrity")
	path := filepath.Join(t.TempDir(), "Seal.schema")
	const schema = `package seal
 table Checked | integrity="crc32c" {
 id uint32
 name string(32)
 values []int32
}
 table Plain {
 id uint32
 name string(32)
 values []int32
}
 table Envelope { child Checked }
 table Leaf { id uint32 }
 table PointerChecked | integrity="crc32c" { leaf *Leaf }
 table PointerPlain { leaf *Leaf }
 `
	if err := os.WriteFile(path, []byte(schema), 0644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(g.u)
	inputs := map[string][]byte{}
	for _, name := range []string{"Checked", "Plain", "Envelope", "PointerChecked", "PointerPlain"} {
		v := m.New(m.Lookup(name))
		var rep tabletext.Report
		text := `{"id":42,"name":"ledger-entry","values":[1,2,3]}`
		if name == "Envelope" {
			text = `{"child":` + text + `}`
		}
		if strings.HasPrefix(name, "Pointer") {
			leaf := m.New(m.Lookup("Leaf"))
			leaf.Fields[0].Cell.U = 99
			v.Fields[0].Cell.Node = leaf
		} else if !m.Read(v, []byte(text), &rep) || !rep.Silent() {
			t.Fatalf("%s: %+v", name, rep)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		if name == "Checked" || name == "PointerChecked" {
			n := len(wire) - 4
			if n < 1 || binary.LittleEndian.Uint32(wire[n:]) != crc32.Checksum(wire[:n], crc32.MakeTable(crc32.Castagnoli)) {
				t.Fatal("reference omitted the CRC32C trailer")
			}
		}
		inputs[name] = wire
	}
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString(`import .schema_save
import .strict_and_refusals
import .region_measure
assert schema_rt.wire_crc32c("123456789") == 0xe3069283
assert schema_rt.wire_crc32c("") == 0
`)
	for _, name := range []string{"Checked", "Plain", "Envelope", "PointerChecked", "PointerPlain"} {
		fmt.Fprintf(&program, `let wire_%s = read_file(%q)
assert wire_%s
let rep_%s = schema_rt.LoadReport {}
let rec_%s, reason_%s = schema_rt.load_strict(%q, wire_%s, rep_%s)
assert rec_%s and reason_%s == ""
assert schema_rt.save(rec_%s) == wire_%s
let measured_%s, whole_%s, refused_%s = schema_rt.load_measure(%q, wire_%s)
assert measured_%s > 0 and whole_%s and not refused_%s
schema_rt.release(rec_%s)
`, name, name, name, name, name, name, name, name, name, name, name, name, name, name, name, name, name, name, name, name, name, name)
	}
	program.WriteString(`assert measured_Checked == measured_Plain
assert measured_PointerChecked == measured_PointerPlain
let good = wire_Checked
// Every non-form bit flip, including every checksum bit, refuses before
// the id table or fields can contribute any event.
def corruptions(root:string, sample:string):
    for(length(sample) - 1) j:
        let at = j + 1
        let original = sample[at]
        for(8) bit:
            let bad = write_int8_le(sample, at, original ^ (1 << bit))
            let rep = schema_rt.LoadReport {}
            let rec, reason = schema_rt.load_strict(root, bad, rep)
            assert not rec and reason == "integrity_mismatch"
            assert rep.malformed and rep.damaged and rep.stopped
            assert rep.unknown == 0 and rep.kind_mismatch == 0 and rep.widened == 0 and rep.clamped == 0 and rep.duplicate == 0
            let n, whole, refused, diagnostic = schema_rt.load_measure(root, bad)
            assert n == -1 and not whole and refused and diagnostic != ""
        write_int8_le(sample, at, original)
corruptions("Checked", wire_Checked)
corruptions("PointerChecked", wire_PointerChecked)
for(4) i:
    let rep = schema_rt.LoadReport {}
    let rec, reason = schema_rt.load_strict("Checked", substring(good, 0, i + 1), rep)
    assert not rec and reason == "integrity_mismatch"
let absent = schema_rt.LoadReport {}
let missing, missing_reason = schema_rt.load_strict("Checked", wire_Plain, absent)
assert not missing and missing_reason == "integrity_mismatch"
let unexpected = schema_rt.LoadReport {}
let extra, extra_reason = schema_rt.load_strict("Plain", good, unexpected)
assert not extra and extra_reason != ""
let _, extra_whole, extra_refused = schema_rt.load_measure("Plain", good)
assert not extra_whole and extra_refused
let empty_report = schema_rt.LoadReport {}
let empty, empty_reason = schema_rt.load_strict("Checked", "", empty_report)
assert not empty and empty_reason == "wire_damaged"
for([0, 2, 3, 255]) form:
    let rep = schema_rt.LoadReport {}
    let rec, reason = schema_rt.load_strict("Checked", write_int8_le(good, 0, form), rep)
    assert not rec and reason != ""
print("integrity passed")
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0644); err != nil {
		t.Fatal(err)
	}
	out := lobsterrun.Run(t, g.dir, "main.lobster", inputs)
	if strings.TrimSpace(string(out)) != "integrity passed" {
		t.Fatalf("%s", out)
	}
	// A checksum-only corruption remains structurally valid. Disabling just
	// the comparison must therefore expose acceptance, not a parser crash.
	runtime := filepath.Join(g.dir, "schema_tables.lobster")
	src, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	const guard = "    if read_uint32_le(data, n) != wire_crc32c(data, n):\n"
	if strings.Count(string(src), guard) != 1 {
		t.Fatal("checksum guard not unique")
	}
	if err := os.WriteFile(runtime, []byte(strings.Replace(string(src), guard, "    if false:\n", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	control := `import .SealTable
import .strict_and_refusals
let good = read_file("Checked")
assert good
let bad = write_int8_le(good, length(good) - 1, good[length(good) - 1] ^ 1)
let rep = schema_rt.LoadReport {}
let rec, reason = schema_rt.load_strict("Checked", bad, rep)
print(if rec and reason == "": "checksum accepted" else: "checksum refused")
schema_rt.release(rec)
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(control), 0644); err != nil {
		t.Fatal(err)
	}
	out = lobsterrun.Run(t, g.dir, "main.lobster", inputs)
	if strings.TrimSpace(string(out)) != "checksum accepted" {
		t.Fatalf("disabled guard control: %s", out)
	}
}
