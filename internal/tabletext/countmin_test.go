package tabletext_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// A present counted array must meet both declared ends. An absent optional
// and a short fixed array keep their separate default/padding rules.
func TestReadCountedArrayMinimum(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, decl, text string
		ok               bool
	}{
		{"below", "[2..3]int32", `[1]`, false},
		{"empty", "[2..3]int32", `[]`, false},
		{"minimum", "[2..3]int32", `[1,2]`, true},
		{"maximum", "[2..3]int32", `[1,2,3]`, true},
		{"above", "[2..3]int32", `[1,2,3,4]`, false},
		{"zero minimum", "[..3]int32", `[]`, true},
		{"fixed padding", "[3]int32", `[1]`, true},
		{"optional absent", "?[2..3]int32", `null`, true},
		{"optional empty", "?[2..3]int32", `[]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := checkModel(t, "package probe\ntable Root { items "+tc.decl+" }")
			v := m.New(m.Lookup("Root"))
			var report tabletext.Report
			ok := m.Read(v, []byte(`{"items":`+tc.text+`}`), &report)
			if ok != tc.ok {
				t.Fatalf("read admitted=%v want %v: %+v", ok, tc.ok, report)
			}
			if ok {
				if _, err := m.Write(v); err != nil {
					t.Fatalf("lawful read no longer writes: %v", err)
				}
			} else {
				if !report.Refused || !strings.Contains(report.Reason, "count_over_bound") {
					t.Fatalf("count refusal: %+v", report)
				}
				if v.Fields[0].Count != 0 {
					t.Fatalf("refused count retained: %d", v.Fields[0].Count)
				}
			}
		})
	}
	// The writer check is an independent control: it already refuses the same
	// count even when the value did not enter through a reader.
	m := checkModel(t, "package probe\ntable Root { items [2..3]int32 }")
	v := m.New(m.Lookup("Root"))
	v.Fields[0].Count = 1
	if _, err := m.Write(v); err == nil {
		t.Fatal("writer admitted below-minimum value")
	}
}
