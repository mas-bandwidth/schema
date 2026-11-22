package baseline_test

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

// THE RETIRED-DECLARATION CORPUS (issue #441, docs/SPEC-TABLES.md §18.4).
// A DECLARATION'S NAME is in the ledger beside its members: a table, an enum, a
// flags and a union whose name an update frees — by removing it, by a bare
// rename, or inside a package rename — is retired, and a later schema that
// declares a NEW declaration under that name is refused until a
// `--update --reason` acknowledges it. A table's name is its node records'
// type id (§3.1), so a save made before the name was freed would name the new
// table; an enum, a flags and a union are retired the same way, strict, so a
// reused name never reads an old save as plausible new values.
//
// Each case under testdata/retired-decls is three steps of one unit:
// before.schema, then freed.schema (the update that frees the name, whose
// result is the committed freed.baseline), then reused.schema, checked
// against it. `case` says `verdict refuse|pass` and, for a refusal, one
// `names <vocab> <name>` line per retired declaration the check must name.
// The controls reuse a name no update retired, or keep the name under `was`.
//
// go test ./internal/baseline -run TestRetiredDeclarationCorpus
// -update-retired-decls rewrites each freed.baseline.
var updateRetiredDecls = flag.Bool("update-retired-decls", false, "rewrite freed.baseline in testdata/retired-decls")

func TestRetiredDeclarationCorpus(t *testing.T) {
	root := filepath.Join("testdata", "retired-decls")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	cases := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cases++
		t.Run(e.Name(), func(t *testing.T) {
			fixture := filepath.Join(root, e.Name())
			read := func(name string) []byte {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(fixture, name))
				if err != nil {
					t.Fatal(err)
				}
				return data
			}
			verdict, names := retiredDeclCase(t, string(read("case")))
			before, freed, reused := read("before.schema"), read("freed.schema"), read("reused.schema")

			dir, paths := writeUnit(t, string(before))
			const date = "2026-11-21 (UTC)"
			if _, _, err := baseline.UpdateAt(unit(t, string(before)), paths, "original", date); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(paths[0], freed, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := baseline.UpdateAt(unit(t, string(freed)), paths, "free the name", date); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, baseline.FileName)
			got, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			committed := filepath.Join(fixture, "freed.baseline")
			if *updateRetiredDecls {
				if err := os.WriteFile(committed, got, 0o644); err != nil {
					t.Fatal(err)
				}
			} else if want := read("freed.baseline"); !bytes.Equal(got, want) {
				t.Fatalf("the update that frees the name wrote another baseline than the committed one — rerun with -update-retired-decls\n--- got\n%s--- committed\n%s", got, want)
			}

			// the check runs against the COMMITTED baseline, exactly as a
			// repository's does
			if err := os.WriteFile(file, read("freed.baseline"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(paths[0], reused, 0o600); err != nil {
				t.Fatal(err)
			}
			live := unit(t, string(reused))
			_, refusals := baseline.Check(live, paths)
			var retired []string
			for _, r := range refusals {
				if strings.Contains(r.Error(), " was retired on ") {
					retired = append(retired, r.Error())
				}
			}
			switch verdict {
			case "pass":
				if len(refusals) != 0 {
					t.Fatalf("a name no update retired was refused: %v", refusals)
				}
				return
			case "refuse":
				if len(refusals) != len(names) || len(retired) != len(names) {
					t.Fatalf("want exactly the retirements %v refused, got %v", names, refusals)
				}
				for _, n := range names {
					if !slices.ContainsFunc(retired, func(r string) bool { return strings.Contains(r, n+" id=") }) {
						t.Errorf("no refusal names %q: %v", n, retired)
					}
				}
			}

			// a reasoned update acknowledges the reuse, and the check then passes
			if _, changed, err := baseline.UpdateAt(live, paths, "reuse the name", date); err != nil || !changed {
				t.Fatalf("acknowledging update changed=%v, err=%v", changed, err)
			}
			acked, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range names {
				if !bytes.Contains(acked, []byte("    "+n+" id=")) || !bytes.Contains(acked, []byte("revived=")) {
					t.Errorf("the acknowledged ledger does not carry %q revived:\n%s", n, acked)
				}
			}
			if warnings, refusals := baseline.Check(live, paths); len(refusals) != 0 {
				t.Errorf("acknowledged reuse still refused: warnings=%v, refusals=%v", warnings, refusals)
			}
		})
	}
	if cases == 0 {
		t.Fatal("testdata/retired-decls holds no case")
	}
}

// retiredDeclCase reads one `case` file.
func retiredDeclCase(t *testing.T, text string) (verdict string, names []string) {
	t.Helper()
	for line := range strings.SplitSeq(text, "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 0:
		case f[0] == "verdict" && len(f) == 2 && (f[1] == "pass" || f[1] == "refuse"):
			verdict = f[1]
		case f[0] == "names" && len(f) == 3:
			names = append(names, fmt.Sprintf("%s %s", f[1], f[2]))
		default:
			t.Fatalf("case: unreadable line %q", line)
		}
	}
	if verdict == "" || (verdict == "refuse") != (len(names) > 0) {
		t.Fatalf("case: verdict %q with names %v", verdict, names)
	}
	return verdict, names
}
