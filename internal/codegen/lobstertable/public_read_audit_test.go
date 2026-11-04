package lobstertable_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

func auditUnit(t *testing.T, source string) *loadUnit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Audit.schema")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return generatePublicLobster(t, []string{path})
}

func auditRun(t *testing.T, g *loadUnit, program string, inputs map[string][]byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)); out != "pass\n" {
		t.Fatal(out)
	}
}

func TestLobsterAppendedFlagsStayWholeInFiles(t *testing.T) {
	g := auditUnit(t, `package audit
flags Cap { Trace, Verbose }
fixed table Fixed { cap Cap }
table Variable { cap Cap }
`)
	auditRun(t, g, `import .AuditTable
import .schema_save
import .json
import .text_form
import .message
for([5, 0x8000000000000001, 0xffffffffffffffff]) bits:
    let typed = audit.NewFixed()
    typed.Cap = bits
    let n, fixed_wire, fixed_reason = audit.FixedFixedSaveChecked([typed], schema_rt.zeros(audit.FixedFixedMeasure(1)))
    assert n > 0 and fixed_reason == ""
    let fixed = schema_rt.load("Fixed", fixed_wire, schema_rt.LoadReport {})
    assert fixed and fixed.fields[0].cell.i == bits
    let again, why = schema_rt.save_reasoned(fixed)
    assert why == "" and again == fixed_wire
    let variable = schema_rt.new_record(audit.VariableDescriptor)
    variable.fields[0].cell.i = bits
    let wire, reason = schema_rt.save_reasoned(variable)
    assert reason == ""
    let loaded = schema_rt.load("Variable", wire, schema_rt.LoadReport {})
    assert loaded and loaded.fields[0].cell.i == bits
    let back, back_reason = schema_rt.save_reasoned(loaded)
    assert back_reason == "" and back == wire
    for([fixed, loaded]) rec:
        let text, text_reason = schema_rt.json_write(rec)
        assert text == "" and text_reason != ""
    assert schema_rt.measure_messages([fixed], schema_rt.LoadReport {}) < 0
    let message, count = schema_rt.save_messages([fixed], schema_rt.LoadReport {})
    assert message == "" and count < 0
let valid = schema_rt.new_record(audit.FixedDescriptor)
valid.fields[0].cell.i = 3
let message, count = schema_rt.save_messages([valid], schema_rt.LoadReport {})
assert count > 0 and length(message) == count
print("pass")
`, nil)
}

// Every enclosing structured payload adds one framed level; the file body is
// depth zero. The text-array case proves that a leaf's length adds no level.
func auditUnknownDepthWire(levels int, leaf string) []byte {
	inner, kind := []byte{0}, byte(13)
	if leaf == "array" {
		inner, kind = []byte{4, 1, 7, 0, 0, 0}, 14
	} else if leaf == "keyed" {
		inner, kind = []byte{4, 1, 1, 4, 7, 0, 0, 0}, 16
	} else if leaf == "text-array" {
		inner, kind = []byte{12, 1, 1, 'x'}, 14
	}
	if leaf == "union-wrapper" {
		for i := 0; i < levels; i++ {
			arm := binary.AppendUvarint([]byte{1, 15}, uint64(len(inner)))
			inner = append(arm, inner...)
		}
		inner = append(append([]byte{1, 15}, inner...), 0)
	} else if leaf == "union-table" {
		for i := 0; i < levels; i++ {
			arm := binary.AppendUvarint([]byte{1, 13}, uint64(len(inner)))
			arm = append(arm, inner...)
			inner = append(append([]byte{1, 15}, arm...), 0)
		}
	} else {
		for level := levels - 1; level >= 0; level-- {
			body := binary.AppendUvarint([]byte{1, kind}, uint64(len(inner)))
			body = append(body, inner...)
			inner, kind = append(body, 0), 13
		}
	}
	wire := append([]byte{1}, inner...)
	wire = binary.LittleEndian.AppendUint64(wire, 0x1234567890abcdef)
	return binary.LittleEndian.AppendUint64(wire, 1)
}

func TestLobsterBinaryDepthEveryPublicRead(t *testing.T) {
	for _, leaf := range []string{"table", "array", "keyed", "text-array", "union-wrapper", "union-table"} {
		for _, depth := range []int{128, 129} {
			t.Run(fmt.Sprintf("%s/depth=%d", leaf, depth), func(t *testing.T) {
				g := auditUnit(t, "package audit\ntable Probe { z int32 }\n")
				program := `import .AuditTable
import .schema_save
import .retain_unknown
import .region_measure
let wire = read_file("wire")
assert wire
let want = WANT
let report = schema_rt.LoadReport {}
let rec = schema_rt.load("Probe", wire, report)
assert (rec != nil) == want
let size, measured, refused, reason = schema_rt.load_measure("Probe", wire)
assert measured == want
let retain = schema_rt.Retain { capacity: 65536, id_capacity: 64 }
let kept = schema_rt.load_retain("Probe", wire, retain, report)
assert (kept != nil) == want
if kept:
    assert kept.fields[0].cell.i == 0
    assert schema_rt.save_retain(kept, retain, report) == wire
else:
    assert retain.used == 0 and length(retain.holders) == 0
print("pass")
`
				want := "false"
				if depth == 128 {
					want = "true"
				}
				program = strings.ReplaceAll(program, "WANT", want)
				auditRun(t, g, program, map[string][]byte{"wire": auditUnknownDepthWire(depth, leaf)})
			})
		}
	}
}
