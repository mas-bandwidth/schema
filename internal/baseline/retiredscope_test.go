package baseline_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

// TestRetiredNameIsScopedToItsDeclaration: a field's wire id is the hash of
// its bare name, an enum variant's and a union arm's likewise, so the same id
// rides in every table, enum or union that declares a member of that name. The
// retired-names ledger refuses a name reused IN THE SAME SCOPE
// (docs/SPEC-TABLES.md §18.4): removing `A.id` while `B.id` stands must leave
// a unit that passes the baseline `--update` just wrote from it, and the same
// for a variant and an arm. Re-adding the retired member in its own scope is
// still refused.
func TestRetiredNameIsScopedToItsDeclaration(t *testing.T) {
	cases := []struct {
		name, src, old, new, retired string
	}{
		{
			name:    "a field",
			src:     "package fixture\n\ntable A\n{\n    id uint32\n    hp int32\n}\n\ntable B\n{\n    id uint32\n    name string(16)\n}\n",
			old:     "    id uint32\n    hp int32\n",
			new:     "    hp int32\n",
			retired: "field A.id",
		},
		{
			name:    "an enum variant",
			src:     "package fixture\n\nenum E { Red, Green }\n\nenum F { Red, Blue }\n\ntable T\n{\n    e E\n    f F\n}\n",
			old:     "enum E { Red, Green }",
			new:     "enum E { Green }",
			retired: "enum-variant E.Red",
		},
		{
			name:    "a union arm",
			src:     "package fixture\n\nunion U\n{\n    a int32\n    b int32\n}\n\nunion V\n{\n    a int32\n    c int32\n}\n\ntable T\n{\n    u U\n    v V\n}\n",
			old:     "    a int32\n    b int32\n",
			new:     "    b int32\n",
			retired: "union-arm U.a",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, paths := writeUnit(t, c.src)
			if _, _, err := baseline.UpdateAt(unit(t, c.src), paths, "the first baseline", "2026-11-21"); err != nil {
				t.Fatal(err)
			}
			edited := editOf(t, c.src, c.old, c.new)
			if err := os.WriteFile(paths[0], []byte(edited), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, _, err := baseline.UpdateAt(unit(t, edited), paths, "the member is retired", "2026-11-21"); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, baseline.FileName))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), c.retired) {
				t.Fatalf("the ledger must record %q:\n%s", c.retired, data)
			}
			if strings.Contains(string(data), "revived=") {
				t.Errorf("a removal must not write a revived= mark on its own entry:\n%s", data)
			}
			// the unit passes the baseline written from it one step earlier: a
			// member of the same name in ANOTHER declaration is not a reuse
			if warns, errs := baseline.Check(unit(t, edited), paths); len(errs) != 0 {
				t.Errorf("the unit must pass its own updated baseline: %v %v", warns, errs)
			}
			// and the retirement still guards its own scope
			if _, errs := baseline.Check(unit(t, c.src), paths); len(errs) != 1 || !strings.Contains(errs[0].Error(), strings.Fields(c.retired)[1]) {
				t.Errorf("re-adding %s in its own scope must be refused once, by its own entry, got: %v", c.retired, errs)
			}
		})
	}
}
