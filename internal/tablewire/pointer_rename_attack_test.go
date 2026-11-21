// THE BARE-RENAME PATH AS AN ATTACK SURFACE (docs/SPEC-TABLES.md §3.1, §2.3,
// §18.3). The tables baseline WARNS on a bare rename of a pointer's target
// table: a file saved before it carries the old name's type id, which the
// renamed reader no longer declares, so the pointer reads null and the record
// counts unknown. A hostile file can forge the same bytes. These tests hold
// what such a file can and cannot do to a reader:
//
//   - A `| required` pointer is a PRESENCE promise, not a non-null one: the
//     writer writes a null required pointer as an explicit index 0 (encode.go)
//     and the reader reads it null. A record of an undeclared type therefore
//     gives a file nothing it could not already say with index 0, and unlike
//     index 0 the read REPORTS it (unknown 1). A required pointer whose field
//     is absent refuses missing_required.
//   - No type id makes a pointer resolve to a table other than its declared
//     target. A record of a type the reader declares but the slot does not
//     name refuses the whole load wire_damaged, and that includes a table a
//     later schema declares under the name a bare rename freed.
package tablewire_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// renameModel compiles one unit from source.
func renameModel(t *testing.T, src string) *tabletext.Model {
	t.Helper()
	f, perrs := parser.Parse("Rename.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Rename.schema", Name: "Rename.schema", Base: "Rename", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}

// renameSave writes `input` (text form) at root Holder under the model given.
func renameSave(t *testing.T, m *tabletext.Model, input string) []byte {
	t.Helper()
	v := m.New(m.Lookup("Holder"))
	var r tabletext.Report
	if !m.Read(v, []byte(input), &r) || !r.Silent() {
		t.Fatalf("the writer's input %s does not read: %+v", input, r)
	}
	wire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return wire
}

// renameOutcome is one read of a wire at root Holder.
type renameOutcome struct {
	accepted bool
	unknown  int
	reason   string
	value    map[string]any
}

func renameRead(t *testing.T, m *tabletext.Model, d pointerTargetDecoder, wire []byte) renameOutcome {
	t.Helper()
	dest := m.New(m.Lookup("Holder"))
	var report tabletext.Report
	ok, err := d.read(m, dest, wire, &report)
	out := renameOutcome{accepted: ok && err == nil && !report.Refused, unknown: report.Unknown, reason: report.Reason}
	if !out.accepted {
		return out
	}
	text, err := m.Write(dest)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := json.Unmarshal(text, &out.value); err != nil {
		t.Fatal(err)
	}
	return out
}

const (
	// the file's writer: Holder.p points at Node
	renameWriterSrc = "package p\ntable Node { v int32 }\ntable Holder {\n    p *Node | required\n    c int32\n}\n"
	// the reader after a BARE rename Node -> Link
	renameReaderSrc = "package p\ntable Link { v int32 }\ntable Holder {\n    p *Link | required\n    c int32\n}\n"
	// an older writer that never had the field
	renameNoFieldSrc = "package p\ntable Holder {\n    c int32\n}\n"
	// a later reader that declares a NEW table under the freed name Node
	renameReuseSrc = "package p\ntable Link { v int32 }\ntable Node { s string(8) }\ntable Holder {\n    p *Link | required\n    c int32\n    q *Node\n}\n"
)

func TestARequiredPointerToAnUndeclaredTableReadsNullAndCountsUnknown(t *testing.T) {
	t.Parallel()
	writer := renameModel(t, renameWriterSrc)
	reader := renameModel(t, renameReaderSrc)
	forged := renameSave(t, writer, `{"p": {"v": 7}, "c": 5}`)
	lawfulNull := renameSave(t, reader, `{"c": 5}`)
	absent := renameSave(t, renameModel(t, renameNoFieldSrc), `{"c": 5}`)
	if !strings.Contains(string(lawfulNull), "\x11\x00") {
		// kind 17, index 0: a required null pointer RIDES (§2.3, §3.1)
		t.Fatalf("the reader's own null required pointer did not ride as an explicit index 0: % x", lawfulNull)
	}
	nullValue := map[string]any{"p": nil, "c": float64(5)}
	for _, d := range pointerTargetDecoders {
		t.Run(d.name, func(t *testing.T) {
			// the record names a table this reader does not declare: the
			// required pointer reads null AND the read says so
			got := renameRead(t, reader, d, forged)
			if !got.accepted || got.unknown != 1 || !reflect.DeepEqual(got.value, nullValue) {
				t.Errorf("undeclared target: got %+v, want accepted, unknown 1, %v", got, nullValue)
			}
			// CONTROL: the writer's own null for a required pointer is the
			// same value with nothing unknown, so the forged record gives a
			// file no value it could not already write
			got = renameRead(t, reader, d, lawfulNull)
			if !got.accepted || got.unknown != 0 || !reflect.DeepEqual(got.value, nullValue) {
				t.Errorf("lawful null: got %+v, want accepted, unknown 0, %v", got, nullValue)
			}
			// CONTROL: a required pointer that is ABSENT refuses
			got = renameRead(t, reader, d, absent)
			if got.accepted || !strings.Contains(got.reason, "missing_required") {
				t.Errorf("absent required pointer: got %+v, want refused missing_required", got)
			}
			// CONTROL: the writer's own reader resolves the pointer
			got = renameRead(t, writer, d, forged)
			if !got.accepted || got.unknown != 0 || !reflect.DeepEqual(got.value, map[string]any{"p": map[string]any{"v": float64(7)}, "c": float64(5)}) {
				t.Errorf("original reader: got %+v, want the pointer resolved", got)
			}
		})
	}
}

func TestAPointerResolvesOnlyToItsDeclaredTarget(t *testing.T) {
	t.Parallel()
	writer := renameModel(t, renameWriterSrc)
	reuse := renameModel(t, renameReuseSrc)
	wire := renameSave(t, writer, `{"p": {"v": 7}, "c": 5}`)
	for _, d := range pointerTargetDecoders {
		t.Run(d.name, func(t *testing.T) {
			// the record's type id is Node's, which this reader DECLARES (a new
			// table under the name the bare rename freed), and p's target is
			// Link: the whole load refuses, and p never resolves to a Node
			got := renameRead(t, reuse, d, wire)
			if got.accepted {
				t.Errorf("a *Link slot naming a Node record was accepted: %+v", got)
			}
			if !strings.Contains(got.reason, "wire_damaged") {
				t.Errorf("reason %q, want wire_damaged", got.reason)
			}
		})
	}
}
