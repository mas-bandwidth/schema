// AN ENUM VALUE OUTSIDE ITS VARIANTS IS REFUSED BY THE WRITER IN A LIST
// ELEMENT AS IT IS AT A FIELD (docs/SPEC.md §5.1 row 5, docs/SPEC-TABLES.md
// §5). The ordinal is §2.15's: 0 is None and variant k is k, so listdemo's
// `enum Grade { A, B, C }` holds 0..3 and 4 is the first ordinal no variant
// names. Review 2026-11-21: the corpus row wref_var_enum wrote ordinal 3 (C,
// a declared variant) and expected a refusal; this pins where the edge is.
package tablewire_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestEngineVariableSaveRefusesAnOutOfSetEnumListElement(t *testing.T) {
	t.Parallel()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../tables/lists"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	root := m.Lookup("Mixed")
	for _, c := range []struct {
		ordinal uint64
		refused bool
	}{{0, false}, {1, false}, {3, false}, {4, true}, {200, true}} {
		inst := m.New(root)
		var r tabletext.Report
		if !m.Read(inst, []byte(`{"grades":["A","B"]}`), &r) {
			t.Fatal(r.Reason)
		}
		f, ok := inst.FieldByKey("grades")
		if !ok || len(f.Elems) < 2 {
			t.Fatalf("grades did not read as two elements: %+v", f)
		}
		f.Elems[1].U = c.ordinal
		f.Elems[1].I = int64(c.ordinal)
		_, err := tablewire.EncodeVariable(m, inst)
		if c.refused && err == nil {
			t.Errorf("grades[1] = ordinal %d (no variant) was written", c.ordinal)
		}
		if !c.refused && err != nil {
			t.Errorf("grades[1] = ordinal %d (a declared value) was refused: %v", c.ordinal, err)
		}
	}
}
