package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
)

// TestUnitViewRegistryJava holds the §8.3 name set for three corpus units that
// between them declare tables, `type`s, enums, flags and unions: the registry
// and the UnitView name every declaration, and the listing a generated program
// prints from TableRegistry.unitView() is byte-identical to the compiler's own
// (internal/viewlisting, §8.7). The Go leg is the model
// (internal/codegen/gotable/view.go); the Java UnitView is what
// test/java-view/View.java walks, and `make tables-java-view` runs the same
// comparison over every VIEW_CORPUS unit.
func TestUnitViewRegistryJava(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)
	printer, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "java-view", "View.java"))
	if err != nil {
		t.Fatal(err)
	}
	const unitImport = "import tabledemo.*; // UNIT"
	if !strings.Contains(string(printer), unitImport) {
		t.Fatalf("test/java-view/View.java carries no %q line for the unit to be substituted into", unitImport)
	}
	for _, dir := range []string{"examples", "arms", "maps"} {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()
			u := unitFromDir(t, filepath.Join("..", "..", "..", "tables", dir))
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			types, tables := testViewDeclarations(u)
			registry := string(files["TableRegistry.java"])
			// google-java-format wraps a long call between its name and its
			// first argument, so the name check is made on the whitespace-folded
			// text: the name is the argument either way.
			view := strings.Join(strings.Fields(string(files["TableUnitView.java"])), "")
			for _, st := range append(slices.Clone(tables), types...) {
				// the registry names the descriptor of every table and type
				if !strings.Contains(registry, st.Name+"Descriptor.tableType()") {
					t.Errorf("%s: TableRegistry does not name %s %s", dir, testDeclWhat(st), st.Name)
				}
				// and the UnitView names the declaration itself
				if !strings.Contains(view, "TableViewEntry.of(\""+st.Name+"\"") {
					t.Errorf("%s: TableUnitView does not name %s %s", dir, testDeclWhat(st), st.Name)
				}
			}
			for _, n := range sortedKeys(u.Enums) {
				if !strings.Contains(view, "TableVocabularyInfo.of(\""+n+"\"") {
					t.Errorf("%s: TableUnitView does not name enum %s", dir, n)
				}
			}
			for _, n := range sortedKeys(u.Flags) {
				if !strings.Contains(view, "TableVocabularyInfo.of(\""+n+"\"") {
					t.Errorf("%s: TableUnitView does not name flags %s", dir, n)
				}
			}
			unions := map[string]bool{}
			for n := range u.Unions {
				unions[n] = true
			}
			for n := range u.TableUnions {
				unions[n] = true
			}
			for _, n := range sortedKeys(unions) {
				if !strings.Contains(view, "TableVocabularyInfo.of(\""+n+"\"") {
					t.Errorf("%s: TableUnitView does not name union %s", dir, n)
				}
			}

			work := t.TempDir()
			src, classes := filepath.Join(work, "src"), filepath.Join(work, "classes")
			for _, d := range []string{src, classes} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"--release", "17", "-Xlint:all", "-Werror", "-d", classes}
			for name, body := range files {
				p := filepath.Join(src, name)
				if err := os.WriteFile(p, body, 0o644); err != nil {
					t.Fatal(err)
				}
				args = append(args, p)
			}
			main := filepath.Join(src, "View.java")
			if err := os.WriteFile(main, []byte(strings.Replace(string(printer), unitImport, "import "+u.Package+".*; // UNIT", 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			args = append(args, main)
			if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
				t.Fatalf("%s: javac: %v\n%s", dir, err, out)
			}
			printed, err := exec.Command(javaBin, "-cp", classes, "View").Output()
			if err != nil {
				t.Fatalf("%s: the listing program failed: %v", dir, err)
			}
			pin := viewlisting.Listing(u)
			if string(printed) != pin {
				t.Fatalf("%s: the Java UnitView listing is not the compiler's.\n%s", dir, firstListingDifference(pin, string(printed)))
			}
		})
	}
}

// sortedKeys is a string-keyed unit declaration map's key set in ascending
// order. It keeps the test's expectations derived from the IR rather than
// restated here.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
