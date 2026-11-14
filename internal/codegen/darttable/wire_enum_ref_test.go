package darttable

// AN ENUM VARIANT REFERENCE IS AT MOST THE ID TABLE'S ENTRY COUNT
// (docs/SPEC-TABLES.md §3, §6.5): a reference above the count is damage and the
// whole load refuses, in a field this reader names and in one it only skips,
// inside an unknown field and inside an unknown array of enums alike. The
// Dart wire-alone walk holds a field id, a nested id, a union arm and a keyed
// key to the count; this is the enum payload's own check.

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The clean fixture is listdemo.Mixed's own wire (tables/lists): the root's
// fields are present and its id table is the writer's first-use order. The
// unknown field is appended after them, under an id the root does not name.
func TestDartWireEnumRefHeldToIdCount(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	dart := dartBinary(t)

	repo := filepath.Join("..", "..", "..")
	u := loadUnit(t, mustSchemaGlob(t, filepath.Join(repo, "tables", "lists"))...)
	root := u.Tables["Mixed"]
	if root == nil {
		t.Fatal("the listdemo unit has no Mixed root")
	}
	clean, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "list_mixed.bin"))
	if err != nil {
		t.Fatal(err)
	}

	m := tabletext.NewModel(u)
	var cleanReport tabletext.Report
	if ok, err := tablewire.Decode(m, m.New(root), clean, &cleanReport); !ok || err != nil || cleanReport.Refused {
		t.Fatalf("the clean fixture is not a clean read: ok=%v err=%v report=%+v", ok, err, cleanReport)
	}

	count := int(binary.LittleEndian.Uint64(clean[len(clean)-8:]))
	start := len(clean) - 8 - count*8
	if start < 1 || clean[start-1] != 0 {
		t.Fatalf("the clean fixture's root body does not end at its terminator (trailer at %d)", start)
	}
	future := ir.TableWireId("future_enum_field")
	for i := 0; i < count; i++ {
		if binary.LittleEndian.Uint64(clean[start+i*8:]) == future {
			t.Fatal("the clean fixture already carries the future field's id")
		}
	}

	// appendUnknown puts one field after the root's own fields and names it
	// through a newly appended id table entry.
	appendUnknown := func(kind byte, payload []byte) []byte {
		var field []byte
		field = binary.AppendUvarint(field, uint64(count+1))
		field = append(field, kind)
		if kind == byte(ir.TableKindArray) {
			field = binary.AppendUvarint(field, uint64(len(payload)))
		}
		field = append(field, payload...)
		out := append([]byte{1}, clean[1:start-1]...)
		out = append(out, field...)
		out = append(out, 0)
		out = append(out, clean[start:start+count*8]...)
		out = binary.LittleEndian.AppendUint64(out, future)
		return binary.LittleEndian.AppendUint64(out, uint64(count+1))
	}

	over := binary.AppendUvarint(nil, uint64(count+2))
	huge := binary.AppendUvarint(nil, uint64(1)<<32+1)
	atCount := binary.AppendUvarint(nil, uint64(count+1))
	arrayOver := append([]byte{byte(ir.TableKindEnum), 1}, over...)

	cases := []struct {
		name   string
		wire   []byte
		refuse bool
	}{
		{"enum_ref_over_count", appendUnknown(byte(ir.TableKindEnum), over), true},
		{"enum_ref_over_count_2_32", appendUnknown(byte(ir.TableKindEnum), huge), true},
		{"enum_array_ref_over_count", appendUnknown(byte(ir.TableKindArray), arrayOver), true},
		{"enum_ref_at_count", appendUnknown(byte(ir.TableKindEnum), atCount), false},
		{"enum_ref_none", appendUnknown(byte(ir.TableKindEnum), []byte{0}), false},
	}

	for _, c := range cases {
		var report tabletext.Report
		ok, err := tablewire.Decode(m, m.New(root), c.wire, &report)
		if c.refuse {
			if ok || !report.Refused || !tablewire.ReadRefusal(err) {
				t.Fatalf("%s: the reference does not refuse the wire: ok=%v err=%v report=%+v", c.name, ok, err, report)
			}
		} else if !ok || err != nil || report.Refused {
			t.Fatalf("%s: the reference refuses a lawful wire: ok=%v err=%v report=%+v", c.name, ok, err, report)
		}
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))

	var probe strings.Builder
	probe.WriteString(`import 'dart:typed_data';
import 'ListdemoWire.dart' as w;

var failed = false;
void check(bool ok, String what) {
  if (!ok) {
    print('FAIL: $what');
    failed = true;
  }
}

void main() {
`)
	for _, c := range cases {
		fmt.Fprintf(&probe, `  {
    final r = w.TableWireReport();
    w.mixedWireLoad(Uint8List.fromList(%s), r);
    check(r.refused == %t, '%s refused=${r.refused}');
  }
`, dartWireBytes(c.wire), c.refuse, c.name)
	}
	probe.WriteString(`  if (failed) throw StateError('an enum reference was not held to the id table');
}
`)

	dir := t.TempDir()
	writeFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "wire_enum_ref_probe.dart"), []byte(probe.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dart, "--enable-asserts", "wire_enum_ref_probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the enum reference probe failed: %v\n%s", err, out)
	}
}

func dartWireBytes(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprint(v)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
