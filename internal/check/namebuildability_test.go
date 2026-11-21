// The name-buildability corpus (testdata/name-buildability), held at the
// checker: a unit `schema check` accepts generates code that builds and loads
// in all 18 targets (Rowan's ruling, 2026-11-21, strict wins), so every name
// one target cannot express is refused for every target, naming that target
// (SPEC §4.6, File-name safety and Platform-name safety). The nightly
// tools/namebuild generates and builds every accepted row in every target.
package check

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// nameBuildabilityRow is one line of cases.tsv: the case directory, its
// verdict (refuse-at-check, builds, or refuse-at-generate), and for a refusal
// at check the target its diagnostic must name, or for a refusal at generate
// the --lang spellings that refuse it.
type nameBuildabilityRow struct {
	name, verdict, names string
}

func readNameBuildabilityRows(t *testing.T, dir string) []nameBuildabilityRow {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []nameBuildabilityRow
	for line := range strings.Lines(string(b)) {
		line = strings.TrimRight(line, "\n")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("cases.tsv: %q has %d columns, want 4 (case, verdict, names, note)", line, len(f))
		}
		rows = append(rows, nameBuildabilityRow{name: f[0], verdict: f[1], names: f[2]})
	}
	return rows
}

// checkCorpusCase parses and checks one case directory's files as one unit,
// returning every diagnostic, parse errors included.
func checkCorpusCase(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []SourceFile
	var diags []string
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".schema") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f, perrs := parser.Parse(path, src)
		for _, pe := range perrs {
			diags = append(diags, pe.Error())
		}
		files = append(files, SourceFile{Path: path, Name: e.Name(), Base: strings.TrimSuffix(e.Name(), ".schema"), Bytes: src, AST: f})
	}
	if len(diags) > 0 {
		return diags
	}
	_, errs := Unit(files)
	for _, e := range errs {
		diags = append(diags, e.Error())
	}
	return diags
}

// TestNameBuildabilityCorpus: every row of the committed corpus takes the
// verdict cases.tsv states. A refuse-at-check row is refused and its
// diagnostic names the target that cannot express it; a builds or
// refuse-at-generate row passes the checker (what an emitter then does is
// the nightly's to prove). The rows and the directories are one set.
func TestNameBuildabilityCorpus(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("..", "..", "testdata", "name-buildability")
	rows := readNameBuildabilityRows(t, dir)
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range ents {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	var named []string
	for _, r := range rows {
		named = append(named, r.name)
	}
	slices.Sort(named)
	if !slices.Equal(dirs, named) {
		t.Fatalf("cases.tsv and the case directories differ:\n rows %v\n dirs %v", named, dirs)
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			diags := checkCorpusCase(t, filepath.Join(dir, r.name))
			switch r.verdict {
			case "refuse-at-check":
				if len(diags) == 0 {
					t.Fatalf("%s: schema check accepted it; want a refusal naming %s", r.name, r.names)
				}
				if r.names != "-" && !slices.ContainsFunc(diags, func(d string) bool { return strings.Contains(d, r.names) }) {
					t.Fatalf("%s: refused, but no diagnostic names %s:\n%s", r.name, r.names, strings.Join(diags, "\n"))
				}
			case "builds", "refuse-at-generate":
				if len(diags) > 0 {
					t.Fatalf("%s: schema check refused it; want it accepted (%s):\n%s", r.name, r.verdict, strings.Join(diags, "\n"))
				}
			default:
				t.Fatalf("%s: unknown verdict %q", r.name, r.verdict)
			}
		})
	}
}

// TestBasenamesDifferingOnlyByCase is the corpus's b_other row, held in
// memory: other.schema beside Other.schema cannot be a pair of files in a tree
// macOS and Windows check out, which is the rule's own reason. The pair is
// refused for every target, naming the targets; Other_ beside Other, which no
// file system folds together, is the control.
func TestBasenamesDifferingOnlyByCase(t *testing.T) {
	t.Parallel()
	const inner = "package probe\n\ntype Inner\n{\n    a uint8\n}\n"
	const holder = "package probe\n\ntype Holder\n{\n    first Inner\n}\n"
	unit := func(a, b string) []string {
		var files []SourceFile
		for _, f := range []struct{ base, src string }{{a, inner}, {b, holder}} {
			ast, perrs := parser.Parse(f.base+".schema", []byte(f.src))
			if len(perrs) > 0 {
				t.Fatal(perrs)
			}
			files = append(files, SourceFile{Path: f.base + ".schema", Name: f.base + ".schema", Base: f.base, Bytes: []byte(f.src), AST: ast})
		}
		_, errs := Unit(files)
		var out []string
		for _, e := range errs {
			out = append(out, e.Error())
		}
		return out
	}
	errs := unit("other", "Other")
	if !slices.ContainsFunc(errs, func(e string) bool { return strings.Contains(e, "differ only by case") && strings.Contains(e, "Go") }) {
		t.Fatalf("other.schema beside Other.schema: want a refusal that they differ only by case, naming Go; got %v", errs)
	}
	if errs := unit("Other_", "Other"); len(errs) > 0 {
		t.Fatalf("the control, Other_.schema beside Other.schema: %v", errs)
	}
}
