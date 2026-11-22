package execref

import (
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func corpus(t *testing.T) []PolicyCase {
	t.Helper()
	cases, err := ReadPolicyCorpus(filepath.Join("testdata", "policy"))
	if err != nil {
		t.Fatal(err)
	}
	return cases
}

// TestPolicyCorpus holds Find to the committed corpus (the reviewer's F-230):
// a dot import and a constructor taken as a value are references, a local
// value or function that shadows the name is not, and the direct, renamed,
// type-only and bounded controls keep their verdicts.
func TestPolicyCorpus(t *testing.T) {
	t.Parallel()
	for _, c := range corpus(t) {
		refs, err := Find(token.NewFileSet(), c.Fixture, c.Src, "os/exec", "Command", "CommandContext")
		if err != nil {
			t.Fatalf("%s: %v", c.Fixture, err)
		}
		if len(refs) != c.Starts {
			t.Errorf("%s (%s): %d reference(s) %v, want %d", c.Fixture, c.Form, len(refs), refs, c.Starts)
		}
	}
}

// TestFindNamesWhereAndHow: each reference carries its function, the name it
// binds to, and whether it is called or taken as a value.
func TestFindNamesWhereAndHow(t *testing.T) {
	t.Parallel()
	for _, c := range corpus(t) {
		refs, err := Find(token.NewFileSet(), c.Fixture, c.Src, "os/exec", "Command", "CommandContext")
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, r := range refs {
			got = append(got, r.Func+":"+r.Name+":"+strconv.FormatBool(r.Call))
		}
		want := map[string]string{
			"dot_import.go.txt":        "TestDotImport:Command:true TestDotImport:CommandContext:true",
			"constructor_value.go.txt": "TestConstructorValue:Command:false TestConstructorValue:CommandContext:false",
			"direct_call.go.txt":       "TestDirectCall:Command:true TestDirectCall:CommandContext:true",
			"renamed_import.go.txt":    "TestRenamedImport:Command:true",
		}[c.Fixture]
		if strings.Join(got, " ") != want {
			t.Errorf("%s: references %q, want %q", c.Fixture, strings.Join(got, " "), want)
		}
	}
}

// TestFindRefusesWhatItCannotParse: a source the parser refuses is an error,
// never a file with no references.
func TestFindRefusesWhatItCannotParse(t *testing.T) {
	t.Parallel()
	if _, err := Find(token.NewFileSet(), "broken.go", []byte("package p\nimport \"os/exec\"\nfunc f() {\n"), "os/exec", "Command"); err == nil {
		t.Error("Find read an unparsable source and returned no error")
	}
}

// TestReadPolicyCorpusRefusesACorpusWithoutAControl: a corpus whose rows are
// all refused, or all clean, proves nothing.
func TestReadPolicyCorpusRefusesACorpusWithoutAControl(t *testing.T) {
	t.Parallel()
	for name, rows := range map[string]string{
		"all refused": "direct.go.txt\t1\tdirect\n",
		"all clean":   "clean.go.txt\t0\tclean\n",
		"empty":       "# nothing\n",
	} {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "cases.tsv"), rows)
		writeFile(t, filepath.Join(dir, "direct.go.txt"), "package p\n")
		writeFile(t, filepath.Join(dir, "clean.go.txt"), "package p\n")
		if _, err := ReadPolicyCorpus(dir); err == nil {
			t.Errorf("%s: ReadPolicyCorpus returned no error", name)
		}
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
