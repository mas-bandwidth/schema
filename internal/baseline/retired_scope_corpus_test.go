package baseline_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

func TestRetiredScopeCorpus(t *testing.T) {
	for _, kind := range []string{"field", "enum-variant", "union-arm"} {
		for _, sibling := range []string{"same", "other"} {
			t.Run(kind+"-"+sibling, func(t *testing.T) {
				fixture := filepath.Join("testdata", "retired-scope", kind+"-"+sibling)
				before, err := os.ReadFile(filepath.Join(fixture, "before.schema"))
				if err != nil {
					t.Fatal(err)
				}
				removed, err := os.ReadFile(filepath.Join(fixture, "removed.schema"))
				if err != nil {
					t.Fatal(err)
				}
				old := unit(t, string(before))
				live := unit(t, string(removed))
				dir, paths := writeUnit(t, string(before))
				if _, _, err := baseline.Update(old, paths, "original"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(paths[0], removed, 0o600); err != nil {
					t.Fatal(err)
				}
				if _, changed, err := baseline.Update(live, paths, "retire only First's member"); err != nil || !changed {
					t.Fatalf("retirement update changed=%v, err=%v", changed, err)
				}
				read := func() []byte {
					t.Helper()
					data, err := os.ReadFile(filepath.Join(dir, baseline.FileName))
					if err != nil {
						t.Fatal(err)
					}
					return data
				}
				retired := read()
				qualified := "First.value"
				if kind == "enum-variant" {
					qualified = "First.Value"
				}
				if !strings.Contains(string(retired), "retired "+kind+" "+qualified) {
					t.Errorf("missing retirement history for %s", qualified)
				}
				if warnings, refusals := baseline.Check(live, paths); len(warnings)+len(refusals) != 0 {
					t.Errorf("the sibling was untouched: warnings=%v, refusals=%v", warnings, refusals)
				}
				if _, changed, err := baseline.Update(live, paths, "repeat without an edit"); err != nil || changed {
					t.Errorf("unchanged update changed=%v, err=%v", changed, err)
				}
				repeated := read()
				if !bytes.Equal(retired, repeated) || bytes.Contains(repeated, []byte("revived=")) {
					t.Error("repeated update recorded a revival while the retired member was absent")
				}
				if err := os.WriteFile(paths[0], before, 0o600); err != nil {
					t.Fatal(err)
				}
				if _, refusals := baseline.Check(old, paths); len(refusals) == 0 {
					t.Error("restoring the member in its own scope should require acknowledgment")
				}
				if _, changed, err := baseline.Update(old, paths, "restore the actual retired member"); err != nil || !changed {
					t.Fatalf("genuine revival changed=%v, err=%v", changed, err)
				}
				if !bytes.Contains(read(), []byte("revived=")) {
					t.Error("genuine revival missing from ledger")
				}
				if warnings, refusals := baseline.Check(old, paths); len(warnings)+len(refusals) != 0 {
					t.Errorf("acknowledged same-scope revival: warnings=%v, refusals=%v", warnings, refusals)
				}
			})
		}
	}
}
