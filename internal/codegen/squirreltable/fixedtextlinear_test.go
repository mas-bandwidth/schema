package squirreltable_test

// THE FIXED-FORM READER BUILDS A string(N) IN WORK LINEAR IN N (docs/SECURITY.md;
// SQUIRREL-3): Squirrel strings are immutable, so a per-byte append copies the
// prefix each step and the work grows with the square of the length. The test
// counts the bytes the load's text concatenations copy, never a clock: the
// product's textCat in the fixed runtime is the seam, and before it existed
// the emitted per-byte append is counted in place. The work at 2n bytes must
// stay under three times the work at n, on the reader's own layout and on a
// file written under an older layout (the lineage read), and the text read
// back is the text written.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

const fixedTextLinearOld = `package ftl

fixed table Root
{
    text string(4096)
}
`

// the reader's layout adds a field, so a file written under the old layout
// is read through the lineage path.
const fixedTextLinearNew = `package ftl

fixed table Root
{
    text string(4096)
    extra int32
}
`

const fixedTextLinearMain = `::textWork <- 0;
local O = dofile("old/schema_module.nut", true).require(%q);
local N = dofile("new/schema_module.nut", true).require(%q);
local text = function(n) {
  local parts = [];
  local i = 0;
  while (i < n) {
    if (i + 2 <= n && (i %% 3) == 1) { parts.append(format("%%c%%c", 0xc3, 0xa9)); i += 2; }
    else { parts.append("a"); i++; }
  }
  local s = "";
  foreach (p in parts) s += p;
  return s;
};
local run = function(key, W, R, n) {
  local v = W.Root();
  v.Text = text(n);
  local out = blob(W.RootFixedMeasure(1));
  if (W.RootFixedSave(out, [v]) < 0) { print(key + " unwritten\n"); return; }
  local back = [R.Root()];
  ::textWork = 0;
  local k = R.RootFixedLoad(out, back, null);
  local same = k == 1 && back[0].Text == v.Text;
  print(key + " " + ::textWork + " " + (same ? "same" : "differs:" + k + ":" + R.FixedReason) + "\n");
};
run("own_n", N, N, %d);
run("own_2n", N, N, %d);
run("lineage_n", O, N, %d);
run("lineage_2n", O, N, %d);
`

// instrumentFixedTextLinear routes the fixed runtime's text concatenation, or
// the emitted per-byte append it replaced, through the counter.
func instrumentFixedTextLinear(t *testing.T, dir string) {
	t.Helper()
	const plain = `local textCat = function(a, b) { return a + b; };`
	const counted = `local textCat = function(a, b) { ::textWork = ::textWork + a.len() + b.len(); return a + b; };`
	appendAt := regexp.MustCompile(`s \+= (b\[[^;\n]*\]\.tochar\(\));`)
	planted := 0
	for _, sub := range []string{"old", "new"} {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			path := filepath.Join(dir, sub, e.Name())
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			src := string(b)
			n := strings.Count(src, plain) + len(appendAt.FindAllStringIndex(src, -1))
			if n == 0 {
				continue
			}
			planted += n
			src = strings.ReplaceAll(src, plain, counted)
			src = appendAt.ReplaceAllString(src, "{ ::textWork = ::textWork + s.len() + 1; s += $1; }")
			writeFile(t, path, []byte(src))
		}
	}
	if planted == 0 {
		t.Fatal("neither the fixed runtime's textCat nor an emitted per-byte append was found to count")
	}
}

func TestSquirrelFixedReadTextLinear(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	const n = 1024
	dir := t.TempDir()
	older, newer := unitFromSchema(t, fixedTextLinearOld), unitFromSchema(t, fixedTextLinearNew)
	modOld := emitUnit(t, dir, "old", older)
	// the newer reader locks the older layout, so it reads the older file
	files, err := sqGenerateAll(newer, map[string][]squirreltable.FixedLineageEntry{"Root": {sqEvoEntry(t, older, "Root")}})
	if err != nil {
		t.Fatalf("generate the newer reader: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "new"), 0o755); err != nil {
		t.Fatal(err)
	}
	sqWriteFiles(t, filepath.Join(dir, "new"), files)
	modNew := squirreltable.ModuleName(newer)
	instrumentFixedTextLinear(t, dir)
	writeFile(t, filepath.Join(dir, "main.nut"), fmt.Appendf(nil, fixedTextLinearMain, modOld, modNew, n, 2*n, n, 2*n))
	got := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", nil)), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) > 0 {
			got[f[0]] = f[1:]
		}
	}
	for _, path := range []string{"own", "lineage"} {
		small, large := got[path+"_n"], got[path+"_2n"]
		if len(small) != 2 || len(large) != 2 || small[1] != "same" || large[1] != "same" {
			t.Errorf("%s: the reads answered %v and %v, want the written text read back", path, small, large)
			continue
		}
		a, _ := strconv.Atoi(small[0])
		b, _ := strconv.Atoi(large[0])
		if a <= 0 || b >= 3*a {
			t.Errorf("%s: building the text copied %d bytes at %d and %d at %d, want under three times (linear, not quadratic)", path, a, n, b, 2*n)
		}
	}
}
