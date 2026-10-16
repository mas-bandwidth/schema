package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
)

// TestUnitViewDescriptorsJava holds the Java reflection surface
// (docs/SPEC-TABLES.md §8) for EVERY declaration of three corpus units, not the
// card's slice:
//
//   - a descriptor per table and per `type` (§8.1, §8.2), each carrying its own
//     doc comment and tags and every field's;
//   - the unit registry naming every one of them, and the unit view (§8.3);
//   - the LISTING test/java-view/View.java prints from TableRegistry.unitView(),
//     compiled against the generated package and run, byte-identical to the
//     compiler's own (internal/viewlisting, §8.7) — the enum, flags and union
//     vocabulary, arm payload descriptors, constants, wire ids and text keys
//     included. `make tables-java-view` runs the same comparison over the whole
//     VIEW_CORPUS; this test keeps it in `go test`.
func TestUnitViewDescriptorsJava(t *testing.T) {
	t.Parallel()
	javac, java := javaTools(t)
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
			registry := string(files["TableRegistry.java"])
			if !strings.Contains(registry, "TableUnitView unitView()") {
				t.Fatalf("%s: TableRegistry has no unitView()", dir)
			}
			if _, ok := files["TableUnitView.java"]; !ok {
				t.Fatalf("%s: no TableUnitView.java", dir)
			}
			types, tables := viewDeclarations(u)
			if len(types)+len(tables) == 0 {
				t.Fatalf("%s: the unit declares nothing to describe", dir)
			}
			for _, st := range append(types, tables...) {
				desc, ok := files[st.Name+"Descriptor.java"]
				if !ok {
					t.Fatalf("%s: no descriptor for %s %s", dir, declWhat(st), st.Name)
				}
				if !strings.Contains(registry, st.Name+"Descriptor.tableType()") {
					t.Errorf("%s: the registry does not list %s", dir, st.Name)
				}
				text := string(desc)
				if want := "t.doc = " + javaString(st.Doc) + ";"; !strings.Contains(text, want) {
					t.Errorf("%s: %s's descriptor does not carry its doc: want %s", dir, st.Name, want)
				}
				if want := "t.tags = " + javaTags(st.Tags) + ";"; !strings.Contains(text, want) {
					t.Errorf("%s: %s's descriptor does not carry its tags: want %s", dir, st.Name, want)
				}
				if n := strings.Count(text, "TableFieldInfo.of("); n != len(st.Fields) {
					t.Errorf("%s: %s's descriptor carries %d fields, the declaration %d", dir, st.Name, n, len(st.Fields))
				}
				for _, f := range st.Fields {
					if want := javaString(f.Doc) + ", " + javaTags(f.Tags) + ")"; !strings.Contains(text, want) {
						t.Errorf("%s: %s.%s's descriptor row does not end in its doc and tags: want %s", dir, st.Name, f.Name, want)
					}
				}
			}

			// the listing, printed by the generated registry and compared
			// with the compiler's
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
			view := filepath.Join(src, "View.java")
			if err := os.WriteFile(view, []byte(strings.Replace(string(printer), unitImport, "import "+u.Package+".*; // UNIT", 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			args = append(args, view)
			if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
				t.Fatalf("%s: javac: %v\n%s", dir, err, out)
			}
			printed, err := exec.Command(java, "-cp", classes, "View").Output()
			if err != nil {
				t.Fatalf("%s: the listing program failed: %v", dir, err)
			}
			pin := viewlisting.Listing(u)
			if string(printed) != pin {
				t.Fatalf("%s: the Java listing is not the compiler's.\n%s", dir, firstListingDifference(pin, string(printed)))
			}
		})
	}
}

func firstListingDifference(pin, printed string) string {
	want, got := strings.Split(pin, "\n"), strings.Split(printed, "\n")
	for i := 0; i < len(want) || i < len(got); i++ {
		w, g := "", ""
		if i < len(want) {
			w = want[i]
		}
		if i < len(got) {
			g = got[i]
		}
		if w != g {
			return "  compiler: " + w + "\n  java:     " + g
		}
	}
	return "the listings differ in trailing bytes alone"
}
