package squirrel_test

// A DEGENERATE-RANGE 128-BIT FIXED FIELD (min == max) costs zero bits on the
// wire and the reader materializes raw = min << F from the range alone
// (docs/SPEC.md §4.3, the fixed rows). On the Squirrel leg a 128-bit value is
// the [lo, hi] pair, so the read lands that pair, the same value the writer
// held, and the generator handles every such declaration, including one whose
// raw value is wider than 64 bits, without failing.

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

const degenerate128Schema = `package degen

type D
{
    a fixed(64, 64) = 3  | min = 3, max = 3
    b fixed(120, 8) = -2 | min = -2, max = -2
    c ufixed(64, 64) = 5 | min = 5, max = 5
    e fixed(100, 28) | min = 0, max = 0
    n fixed(16, 16) = 2 | min = 2, max = 2
}
`

const degenerate128Main = `local schema = dofile("schema_module.nut", true);
local serialize = dofile("serialize.nut", true);
local Degen = schema.require("Degen");
local v = Degen.D();
v.A = [0, 3];
v.B = [-512, -1];
v.C = [0, 5];
v.E = [0, 0];
v.N = 131072;
local ws = serialize.WriteStream(blob(64));
if (!Degen.WriteD(ws, v)) throw "write failed: " + ws.error();
ws.flush();
local bytes = ws.data();
local back = Degen.D();
local rs = serialize.ReadStream(bytes);
if (!Degen.ReadD(rs, back)) throw "read failed: " + rs.error();
local show = function(x) {
  if (typeof x == "array") return "[" + x[0] + "," + x[1] + "]";
  return typeof x + ":" + x;
};
print("bytes " + bytes.len() + "\n");
print("a " + show(back.A) + "\n");
print("b " + show(back.B) + "\n");
print("c " + show(back.C) + "\n");
print("e " + show(back.E) + "\n");
print("n " + show(back.N) + "\n");
local fresh = Degen.D();
print("fresh " + show(fresh.A) + show(fresh.B) + show(fresh.C) + show(fresh.E) + show(fresh.N) + " " + (Degen.WriteD(serialize.WriteStream(blob(8)), fresh) ? "writes" : "refused") + "\n");
`

func TestSquirrelDegenerate128FixedReadsBackAsPair(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "Degen.schema")
	if err := os.WriteFile(src, []byte(degenerate128Schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, src)
	var files map[string][]byte
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("the generator failed on a degenerate 128-bit fixed declaration: %v", r)
			}
		}()
		var err error
		if files, err = squirrel.Generate(u); err != nil {
			t.Fatalf("the type surface refused: %v", err)
		}
	}()
	maps.Copy(files, squirrel.Serialize())
	files["main.nut"] = []byte(degenerate128Main)
	run := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(run, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, run, "main.nut", nil)), "\n"), "\n") {
		if k, v, ok := strings.Cut(line, " "); ok {
			got[k] = v
		}
	}
	for k, want := range map[string]string{
		"bytes": "0",
		"a":     "[0,3]",
		"b":     "[-512,-1]",
		"c":     "[0,5]",
		"e":     "[0,0]",
		"n":     "integer:131072",
		"fresh": "[0,3][-512,-1][0,5][0,0]integer:131072 writes",
	} {
		if got[k] != want {
			t.Errorf("%s: %q, want %q", k, got[k], want)
		}
	}
}
