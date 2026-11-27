package tabletext_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
)

func birthField(t *testing.T, v *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	for i := range v.Fields {
		if v.Fields[i].Def.Name == name {
			return &v.Fields[i]
		}
	}
	t.Fatalf("missing %s", name)
	return nil
}

func TestCountedArrayBirthAndOmittedDefaults(t *testing.T) {
	t.Parallel()
	for _, class := range []string{"table", "fixed table"} {
		t.Run(class, func(t *testing.T) {
			t.Parallel()
			m := checkModel(t, "package birth\ntype Child { n int32 = 55\n sub [1..2]int32 }\n"+class+` Root {
 items [2..3]int32
 rows [1..2]Child
 maybe ?[2..3]int32
 empty [..3]int32
}`)
			check := func(v *tabletext.Instance) {
				t.Helper()
				if got := birthField(t, v, "items").Count; got != 2 {
					t.Errorf("items born count=%d want2", got)
				}
				rows := birthField(t, v, "rows")
				if rows.Count != 1 {
					t.Errorf("rows born count=%d want1", rows.Count)
				}
				if got := birthField(t, rows.Elems[0].Tab, "n").Cell.I; got != 55 {
					t.Errorf("element default=%d want55", got)
				}
				if got := birthField(t, rows.Elems[0].Tab, "sub").Count; got != 1 {
					t.Errorf("nested birth count=%d want1", got)
				}
				optional := birthField(t, v, "maybe")
				if optional.Present {
					t.Error("optional born present")
				}
				if got := birthField(t, v, "empty").Count; got != 0 {
					t.Errorf("zero minimum born count=%d", got)
				}
			}
			fresh := m.New(m.Lookup("Root"))
			check(fresh)
			var report tabletext.Report
			if !m.Read(fresh, []byte(`{}`), &report) {
				t.Fatal(report)
			}
			check(fresh)
			if _, err := m.Write(fresh); err != nil {
				t.Errorf("fresh/omitted value does not write: %v", err)
			}
		})
	}
}
