package javatable

// TestJavaDescriptorDocAndTags holds the two PERSON-WRITTEN columns of the
// reflection surface (docs/SPEC-TABLES.md §8.1, §8.3): every declaration and
// every field's `doc` and `tags`, and the same pair on the registry's enum,
// flags and union rows, their variants and arms, and the unit's constants. The
// Java runtime is compiled against the generated package and walked through
// TableRegistry.unitView(); each string is compared with the declaration the
// compiler read, so a doc or tag that did not reach Java is red here. An
// UNANNOTATED unit is walked too: its rows must carry the empty doc and no
// tags, the spelling §8.1 gives absence.

import (
	"encoding/base64"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const docProbeSource = `import java.nio.charset.StandardCharsets;
import java.util.Base64;

public final class DocProbe {
    private DocProbe() {}

    private static final StringBuilder out = new StringBuilder();

    private static String b64(String s) {
        return Base64.getEncoder().encodeToString(s.getBytes(StandardCharsets.UTF_8));
    }

    private static void row(String kind, String a, String b, String doc, String[] tags) {
        out.append(kind).append('\t').append(a).append('\t').append(b).append('\t').append(b64(doc == null ? "" : doc));
        out.append('\t').append(tags == null ? 0 : tags.length);
        if (tags != null) {
            for (String tag : tags) out.append('\t').append(b64(tag));
        }
        out.append('\n');
    }

    private static void declarations(String kind, TableViewEntry[] entries) {
        for (TableViewEntry e : entries) {
            row(kind, e.name, "", e.doc, e.tags);
            for (TableFieldInfo f : e.type.fields) {
                row("F", e.name, f.name, f.doc, f.tags);
            }
        }
    }

    private static void vocabulary(String kind, String variantKind, TableVocabularyInfo[] set) {
        for (TableVocabularyInfo v : set) {
            row(kind, v.name, "", v.doc, v.tags);
            for (TableVariantInfo r : v.variants) {
                row(variantKind, v.name, r.name, r.doc, r.tags);
                if (r.field != null) row("A", v.name, r.name, r.field.doc, r.field.tags);
            }
        }
    }

    public static void main(String[] args) {
        TableUnitView u = TableRegistry.unitView();
        declarations("T", u.tables);
        declarations("Y", u.types);
        vocabulary("E", "EV", u.enums);
        vocabulary("G", "GV", u.flags);
        vocabulary("U", "UV", u.unions);
        for (TableConstantInfo c : u.constants) row("C", c.name, "", c.doc, c.tags);
        System.out.print(out);
    }
}
`

type docRow struct {
	kind   string
	first  string
	second string
	doc    string
	tags   []string
}

func (r docRow) String() string {
	return fmt.Sprintf("%s %s %s doc=%q tags=%q", r.kind, r.first, r.second, r.doc, r.tags)
}

// testDocRowsFromIR is the expectation the test holds Java to, read straight
// from the checked unit — never from the generated Java or from viewlisting.
func testDocRowsFromIR(u *ir.Unit) []docRow {
	types, tables := testViewDeclarations(u)
	var rows []docRow
	decls := func(kind string, set []*ir.Struct) {
		for _, st := range set {
			rows = append(rows, docRow{kind, st.Name, "", st.Doc, st.Tags})
			for _, f := range st.Fields {
				rows = append(rows, docRow{"F", st.Name, f.Name, f.Doc, f.Tags})
			}
		}
	}
	decls("T", tables)
	decls("Y", types)
	for _, n := range slices.Sorted(maps.Keys(u.Enums)) {
		e := u.Enums[n]
		rows = append(rows, docRow{"E", n, "", e.Doc, e.Tags})
		rows = append(rows, docRow{"EV", n, "None", "", nil})
		for i, variant := range e.Variants {
			rows = append(rows, docRow{"EV", n, variant, e.VariantDocs[i], e.VariantTags[i]})
		}
	}
	for _, n := range slices.Sorted(maps.Keys(u.Flags)) {
		f := u.Flags[n]
		rows = append(rows, docRow{"G", n, "", f.Doc, f.Tags})
		for i, variant := range f.Variants {
			rows = append(rows, docRow{"GV", n, variant, f.VariantDocs[i], f.VariantTags[i]})
		}
	}
	all := map[string]*ir.Union{}
	maps.Copy(all, u.Unions)
	maps.Copy(all, u.TableUnions)
	for _, n := range slices.Sorted(maps.Keys(all)) {
		un := all[n]
		rows = append(rows, docRow{"U", n, "", un.Doc, un.Tags})
		rows = append(rows, docRow{"UV", n, "None", "", nil})
		for _, arm := range un.Variants {
			rows = append(rows, docRow{"UV", n, arm.Name, arm.Doc, arm.Tags})
			if arm.Void() || arm.Body() {
				continue
			}
			// A general arm is also a field descriptor, and §8.7 holds the two
			// records to the same doc and tags.
			rows = append(rows, docRow{"A", n, arm.Name, arm.Doc, arm.Tags})
		}
	}
	for _, n := range slices.Sorted(maps.Keys(u.Consts)) {
		c := u.Consts[n]
		rows = append(rows, docRow{"C", n, "", c.Doc, c.Tags})
	}
	return rows
}

func javaDocRows(t *testing.T, javac, javaBin string, u *ir.Unit) []docRow {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate %s: %v", u.Package, err)
	}
	root := t.TempDir()
	srcdir := filepath.Join(root, "src")
	classes := filepath.Join(root, "classes")
	for _, d := range []string{srcdir, classes} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var srcs []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(srcdir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		srcs = append(srcs, p)
	}
	probe := filepath.Join(srcdir, "DocProbe.java")
	body := strings.Replace(docProbeSource, "\npublic final class DocProbe", "\nimport "+u.Package+".*;\n\npublic final class DocProbe", 1)
	if err := os.WriteFile(probe, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-Xlint:all", "-Werror", "-d", classes}, srcs...)
	args = append(args, probe)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac %s: %v\n%s", u.Package, err, out)
	}
	out, err := exec.Command(javaBin, "-cp", classes, "DocProbe").Output()
	if err != nil {
		t.Fatalf("run DocProbe %s: %v", u.Package, err)
	}
	var rows []docRow
	for line := range strings.SplitSeq(strings.TrimSuffix(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 5 {
			t.Fatalf("%s: malformed probe row %q", u.Package, line)
		}
		doc, err := base64.StdEncoding.DecodeString(f[3])
		if err != nil {
			t.Fatalf("%s: probe doc: %v", u.Package, err)
		}
		n, err := strconv.Atoi(f[4])
		if err != nil || len(f) != 5+n {
			t.Fatalf("%s: malformed probe tags in %q", u.Package, line)
		}
		var tags []string
		for _, encoded := range f[5:] {
			tag, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatalf("%s: probe tag: %v", u.Package, err)
			}
			tags = append(tags, string(tag))
		}
		rows = append(rows, docRow{f[0], f[1], f[2], string(doc), tags})
	}
	return rows
}

func compareDocRows(t *testing.T, unit string, want, got []docRow) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s: Java listed %d annotation rows, the unit declares %d", unit, len(got), len(want))
	}
	for i := range want {
		if want[i].kind != got[i].kind || want[i].first != got[i].first || want[i].second != got[i].second {
			t.Fatalf("%s: row %d is Java's %s, the unit's %s", unit, i, got[i], want[i])
		}
		if want[i].doc != got[i].doc {
			t.Fatalf("%s: %s carries doc %q, the unit says %q", unit, want[i], got[i].doc, want[i].doc)
		}
		if !slices.Equal(want[i].tags, got[i].tags) {
			t.Fatalf("%s: %s carries tags %q, the unit says %q", unit, want[i], got[i].tags, want[i].tags)
		}
	}
}

func TestJavaDescriptorDocAndTags(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaTools(t)
	inUnit := func(dir string) *ir.Unit {
		return unitFromDir(t, filepath.Join("..", "..", "..", "tables", dir))
	}

	// An ANNOTATED unit: every doc and tag Java reports is the one the unit
	// declares, on every table, type, field and vocabulary row.
	for _, dir := range []string{"examples", "arms"} {
		u := inUnit(dir)
		want := testDocRowsFromIR(u)
		got := javaDocRows(t, javac, javaBin, u)
		compareDocRows(t, dir, want, got)
		var docs, tags int
		for _, r := range got {
			if r.doc != "" {
				docs++
			}
			if len(r.tags) > 0 {
				tags++
			}
		}
		if docs == 0 || tags == 0 {
			t.Fatalf("%s: the annotated unit reached Java with %d docs and %d tag rows; the fixture no longer proves the columns", dir, docs, tags)
		}
	}

	// An UNANNOTATED unit: the empty spelling, on every row.
	u := inUnit("maps")
	got := javaDocRows(t, javac, javaBin, u)
	compareDocRows(t, "maps", testDocRowsFromIR(u), got)
	if len(got) == 0 {
		t.Fatal("maps: the unit listed no annotation rows")
	}
	for _, r := range got {
		if r.doc != "" || len(r.tags) != 0 {
			t.Fatalf("maps: unannotated %s carries doc %q tags %q", r, r.doc, r.tags)
		}
	}
}
