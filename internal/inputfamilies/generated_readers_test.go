package inputfamilies

import (
	"slices"
	"testing"
)

// TestTheTwinHeadersSelectFixedtwin: a diff that changes only the C or C++
// twin header under generated/bench/paired, or the paired unit under
// bench/corpus, selects ./tools/fixedtwin, which reads the headers with
// os.ReadFile and loads the unit by path. Neither is a Go import nor
// a go:embed, so the dependents expansion never sees them. THE REVERSED
// WITNESS: the rule before generatedInputs (isTestInput alone) selects no
// fixedtwin for the same paths, the miss this reverses.
func TestTheTwinHeadersSelectFixedtwin(t *testing.T) {
	t.Parallel()
	tree := repoTree(t)
	for _, p := range []string{
		"generated/bench/paired/c/FixedTableTable.h",
		"generated/bench/paired/cpp/FixedTableTable.h",
		"bench/corpus/FixedTable.schema",
	} {
		if !slices.Contains(tree.Files, p) {
			t.Errorf("%s is not in the tree: the witness names a file that is gone", p)
			continue
		}
		if got := selectWith(tree, []string{p}, false, true, isTestInput).Packages; slices.Contains(got, "./tools/fixedtwin") {
			t.Errorf("CONTROL: the test-input rule alone selected fixedtwin for %s (%v): the miss is not reproduced", p, got)
		}
		if got := Select(tree, []string{p}, false, true).Packages; !slices.Contains(got, "./tools/fixedtwin") {
			t.Errorf("a change to %s selected %v, without ./tools/fixedtwin", p, got)
		}
	}
}

// TestAGeneratedPathIsNotSelectedByItsLanguageName: a generated path's last
// directory is a language name, and naming "c" is not reading
// generated/bench/paired/c. A package with no other mention of the path is
// not selected.
func TestAGeneratedPathIsNotSelectedByItsLanguageName(t *testing.T) {
	t.Parallel()
	tree := Tree{GoFiles: map[string]string{
		"internal/lang/l.go":  `var langs = []string{"c", "cpp"}`,
		"tools/reader/r.go":   `var p = filepath.Join("..", "..", "generated", "bench", "paired", "c", "FixedTableTable.h")`,
		"tools/bydir/d.go":    `const d = "generated/bench/paired/c"`,
		"internal/tdata/t.go": `var p = filepath.Join("testdata", "wire", "c")`,
	}}
	got := Select(tree, []string{"generated/bench/paired/c/Other.h"}, false, true).Packages
	if want := []string{"./tools/bydir", "./tools/reader"}; !slices.Equal(got, want) {
		t.Errorf("a generated C header selected %v, want %v", got, want)
	}
	// a test input keeps the quoted last directory, as before
	got = Select(tree, []string{"testdata/wire/c/x.bits"}, false, true).Packages
	if !slices.Contains(got, "./internal/tdata") || !slices.Contains(got, "./internal/lang") {
		t.Errorf("a test input selected %v, want its quoted-directory readers kept", got)
	}
}
