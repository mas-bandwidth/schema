package tabletext_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func repeatedUnknownModel(t *testing.T) *tabletext.Model {
	t.Helper()
	source := []byte(`package repeatedunknown
enum Key { Known }
table Leaf { value int32 }
table Root {
    slots [Key]int32
    link *Leaf
    keep int32 = 7
}
`)
	ast, errs := parser.Parse("Repeat.schema", source)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	unit, errors := check.Unit([]check.SourceFile{{Path: "Repeat.schema", Name: "Repeat.schema", Base: "Repeat", Bytes: source, AST: ast}})
	if len(errors) != 0 {
		t.Fatal(errors)
	}
	return tabletext.NewModel(unit)
}

func TestRepeatedDecodedUnknownKeysRefuse(t *testing.T) {
	m := repeatedUnknownModel(t)
	for _, text := range []string{
		`{"new":1,"new":2}`,
		`{"new":1,"\u006eew":2}`,
		`{"slots":{"Future":1,"Future":2}}`,
		`{"slots":{"Future":1,"\u0046uture":2}}`,
		`{"slots":{"None":1,"None":2}}`,
		`{"slots":{"Known":1,"Known":2}}`,
		`{"link":{"new":1,"\u006eew":2}}`,
	} {
		t.Run(text, func(t *testing.T) {
			inst := m.New(m.Lookup("Root"))
			keep, _ := inst.FieldByKey("keep")
			keep.Cell.I = 99
			var report tabletext.Report
			if m.Read(inst, []byte(text), &report) || !report.Refused || !strings.HasPrefix(report.Reason, "malformed") {
				t.Fatalf("repeated decoded key accepted: %+v", report)
			}
			keep, _ = inst.FieldByKey("keep")
			if keep.Cell.I != 7 {
				t.Fatalf("refusal left caller value %d", keep.Cell.I)
			}
		})
	}
	for _, text := range []string{
		`{"new":1,"other":2}`,
		`{"slots":{"Future":1,"None":2}}`,
		`{"link":{"new":1,"other":2}}`,
	} {
		var report tabletext.Report
		if !m.Read(m.New(m.Lookup("Root")), []byte(text), &report) || report.Refused || report.Unknown != 2 {
			t.Fatalf("distinct unknown keys refused: %s: %+v", text, report)
		}
	}
}

// The JSON layer bounds decoded object keys at 255 bytes (§16.12). A key
// beyond the bound refuses; truncating it before equality would merge names.
func TestUnknownObjectKeyBoundDoesNotTruncate(t *testing.T) {
	m := repeatedUnknownModel(t)
	prefix := strings.Repeat("a", 254)
	for _, object := range []string{
		`{"` + prefix + `x":1,"` + prefix + `y":2}`,
		`{"slots":{"` + prefix + `x":1,"` + prefix + `y":2}}`,
		`{"link":{"` + prefix + `x":1,"` + prefix + `y":2}}`,
	} {
		var report tabletext.Report
		if !m.Read(m.New(m.Lookup("Root")), []byte(object), &report) || report.Unknown != 2 {
			t.Fatalf("distinct 255-byte keys refused: %+v", report)
		}
	}
	key := prefix + "xy"
	for _, object := range []string{
		`{"` + key + `":1}`,
		`{"slots":{"` + key + `":1}}`,
		`{"link":{"` + key + `":1}}`,
	} {
		var report tabletext.Report
		if m.Read(m.New(m.Lookup("Root")), []byte(object), &report) || !report.Refused {
			t.Fatalf("256-byte key was truncated and accepted: %+v", report)
		}
	}
}
