package lobstertable_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
)

func TestLobsterUnitViewListing(t *testing.T) {
	t.Parallel()
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	entries := viewlisting.CorpusEntries(string(makefile))
	if len(entries) == 0 {
		t.Fatal("empty VIEW_CORPUS")
	}
	units := map[string][]string{}
	for _, entry := range entries {
		dir, _, ok := strings.Cut(entry, ":")
		if !ok {
			t.Fatal(entry)
		}
		source := filepath.Join(root, "tables", dir)
		if dir == "wide" {
			source = filepath.Join(root, "examples-wide")
		}
		paths, err := compiler.GatherPaths([]string{source})
		if err != nil {
			t.Fatal(err)
		}
		units["corpus-"+dir] = paths
	}
	manifest, _ := readManifest(t)
	for key, paths := range manifest {
		units["manifest-"+key] = prefix(paths)
	}
	printer, err := os.ReadFile(filepath.Join(root, "test/tables/view_lobster.lobstertext"))
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for key := range units {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			g, err := generateLoadUnit(t, units[key])
			if err != nil {
				t.Fatalf("view generation refused: %v", err)
			}
			program := strings.ReplaceAll(string(printer), "VIEW_MODULE", g.u.Package+"View")
			program = strings.ReplaceAll(program, "VIEW_PACKAGE", g.u.Package)
			if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program), 0644); err != nil {
				t.Fatal(err)
			}
			run := func() string { return string(lobsterrun.Run(t, g.dir, "main.lobster", nil)) }
			want := viewlisting.Listing(g.u)
			if got := run(); got != want {
				t.Fatalf("view listing differs:\n%s", firstViewDifference(want, got))
			}
			if key == "corpus-examples" {
				// The printer reads generated descriptors: changing their field IDs
				// must break identity with the independent IR pin.
				path := filepath.Join(g.dir, "schema_tables.lobster")
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				const anchor = "def table(d:TableDescriptor) -> TableDescriptor:"
				if strings.Count(string(raw), anchor) != 1 {
					t.Fatal("table registration mutation anchor missing")
				}
				patched := strings.Replace(string(raw), anchor, anchor+"\n    for(d.fields) altered:\n        altered.id = altered.id ^ 1", 1)
				if err := os.WriteFile(path, []byte(patched), 0644); err != nil {
					t.Fatal(err)
				}
				if run() == want {
					t.Fatal("descriptor ID mutation escaped listing gate")
				}
			}
		})
	}
}

func firstViewDifference(want, got string) string {
	a, b := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return "want: " + a[i] + "\n got: " + b[i]
		}
	}
	return "different listing length\nwant:\n" + want + "\ngot:\n" + got
}
