package squirreltable_test

// THE MESSAGE WRITE IS LINEAR IN NESTING DEPTH (docs/SPEC-TABLES.md §3.3): a
// non-optional nested table whose body is all defaults elides, and the writer
// learns that by writing the body once and rolling it back to the mark, as the
// Go engine's encoder does (internal/tablewire/messageencode.go). The test
// counts work, not time: the generated runtime's bit writer is edited to count
// its calls, and a chain of fixed tables nested 13 levels deep is measured and
// saved from every level. Each added level must add the same number of writes,
// with the leaf set (nothing elides) and with every field at its default
// (every level elides), and the saved batch must load back and re-save to the
// same bytes.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const nestedWriteLevels = 13

func nestedWriteSchema() string {
	var b strings.Builder
	b.WriteString("package nestwork\n")
	for i := range nestedWriteLevels + 1 {
		fmt.Fprintf(&b, "\nfixed table N%d\n{\n    v int32\n", i)
		if i < nestedWriteLevels {
			fmt.Fprintf(&b, "    next N%d\n", i+1)
		}
		b.WriteString("}\n")
	}
	return b.String()
}

func TestSquirrelMessageNestedWriteIsLinearInDepth(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	src := nestedWriteSchema()
	ast, perrs := parser.Parse("NestWork.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "NestWork.schema", Name: "NestWork.schema", Base: "NestWork",
		Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs)
	}
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	maps.Copy(files, tables)
	// THE COUNTER: every call of the message runtime's bit writer
	counted := 0
	for name, data := range files {
		s := string(data)
		r := strings.Replace(s, "put = function(w, value, n) {", "put = function(w, value, n) { ::NestPuts <- ::NestPuts + 1;", 1)
		if r != s {
			files[name] = []byte(r)
			counted++
		}
	}
	if counted != 1 {
		t.Fatalf("%d generated files carry the message bit writer, want 1", counted)
	}
	dir := t.TempDir()
	unit := filepath.Join(dir, "nestwork")
	if err := os.MkdirAll(unit, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(unit, name), data)
	}
	var p strings.Builder
	fmt.Fprintf(&p, `::NestPuts <- 0;
local T = dofile("nestwork/schema_module.nut", true).require(%q);
local v = {}; local ar = {};
if (!T.AnnounceRead(v, T.TableMessageAnnouncement, ar)) { print("FAIL announce\n"); return; }
local same = function(a, b) { if (a.len() != b.len()) return false; for (local i = 0; i < a.len(); i++) if (a[i] != b[i]) return false; return true; };
local run = function(root, depth, leaf) {
  local x = T[root]();
  if (leaf) { local c = x; for (local i = 0; i < depth; i++) c = c.Next; c.V = 1; }
  ::NestPuts = 0;
  local mr = {}; local z = T[root + "MeasureMessages"]([x], mr);
  local measured = ::NestPuts;
  local b = blob(z); local sr = {};
  ::NestPuts = 0;
  local w = T[root + "SaveMessages"]([x], b, sr);
  local saved = ::NestPuts;
  local out = [T[root]()]; local rr = {};
  local n = T[root + "LoadMessages"](out, v, b, rr);
  local b2 = blob(z); local sr2 = {};
  local w2 = T[root + "SaveMessages"](out, b2, sr2);
  local ok = z > 0 && w == z && n == 1 && w2 == z && same(b, b2);
  if (leaf) { local c = out[0]; for (local i = 0; i < depth; i++) c = c.Next; ok = ok && c.V == 1; }
  else ok = ok && z == 3;
  print("ROW " + (leaf ? "leaf" : "default") + " " + depth + " " + measured + " " + saved + " " + (ok ? "ok" : "bad") + "\n");
};
`, squirreltable.ModuleName(u))
	for _, leaf := range []bool{true, false} {
		for depth := range nestedWriteLevels + 1 {
			fmt.Fprintf(&p, "run(%q, %d, %v);\n", fmt.Sprintf("N%d", nestedWriteLevels-depth), depth, leaf)
		}
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	out := string(sqrun.Run(t, dir, "main.nut", nil))

	// rows[kind][depth] = {measured, saved}
	rows := map[string]map[int][2]int{"leaf": {}, "default": {}}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 6 || f[0] != "ROW" {
			continue
		}
		depth, _ := strconv.Atoi(f[2])
		measured, _ := strconv.Atoi(f[3])
		saved, _ := strconv.Atoi(f[4])
		if f[5] != "ok" {
			t.Errorf("%s depth %d: the batch did not save, load and re-save to the same bytes", f[1], depth)
		}
		rows[f[1]][depth] = [2]int{measured, saved}
	}
	for kind, byDepth := range rows {
		if len(byDepth) != nestedWriteLevels+1 {
			t.Fatalf("%s: %d rows reported, want %d:\n%s", kind, len(byDepth), nestedWriteLevels+1, out)
		}
		for pass, name := range []string{"measure", "save"} {
			step := byDepth[1][pass] - byDepth[0][pass]
			if step <= 0 {
				t.Errorf("%s %s: one level of nesting added %d writes", kind, name, step)
			}
			for depth := 2; depth <= nestedWriteLevels; depth++ {
				if got := byDepth[depth][pass] - byDepth[depth-1][pass]; got != step {
					t.Errorf("%s %s: level %d added %d writes, level 1 added %d (writes by depth: %v)", kind, name, depth, got, step, byDepth)
					break
				}
			}
		}
	}
}
