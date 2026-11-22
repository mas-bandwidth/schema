package js

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/check"
	"github.com/mas-bandwidth/schema/v3/internal/parser"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"

	"github.com/mas-bandwidth/schema/v3/internal/testproc"
)

// The flat tier's cursor guards refuse an emission that names the runtime
// cursor (br on the read side, sb and wi on the write side) while the cursor
// is static, because those locals are stale until it is materialized. They
// matched with a word-boundary regex, so a schema field spelt sb, wi or br —
// reached only ever as a property, value.sb — panicked the generator:
//
//	panic: flat emitter: names the write cursor while it is static:
//	  if (!validText(value.sb, value.sbLength)) {
//
// A generator's private local names are not the user's problem: a field may
// be called anything the checker admits.

// cursorFieldsSchema puts each name where the cursor is still static: br first
// (the read cursor and the write cursor both static), sb next (the string's
// validText guard is emitted before the string materializes the cursor, the
// reported panic), and wi first in its own type (an array's element hoist).
const cursorFieldsSchema = `package cursornames

type Inner
{
    x int32
}

type Cursors
{
    br [3]int32
    sb string(5)
    wi [..4]Inner
}

type Elems
{
    wi [..4]Inner
    sb string(5)
}
`

func generateCursorFields(t *testing.T) map[string][]byte {
	t.Helper()
	ast, perrs := parser.Parse("Cursors.schema", []byte(cursorFieldsSchema))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Cursors.schema", Name: "Cursors.schema", Base: "Cursors", Bytes: []byte(cursorFieldsSchema), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestJsFieldsNamedAfterTheFlatCursorGenerate pins that fields named sb, wi
// and br generate in both tiers, and that the flat tier reaches each as the
// property it is.
func TestJsFieldsNamedAfterTheFlatCursorGenerate(t *testing.T) {
	t.Parallel()
	out := generateCursorFields(t)
	flat := string(out["CursorsFlat.js"])
	if flat == "" {
		t.Fatalf("no flat module: %v", keys(out))
	}
	for _, want := range []string{"value.sb", "value.wi[", "value.br["} {
		if !strings.Contains(flat, want) {
			t.Errorf("the flat tier never reaches %q", want)
		}
	}
	if _, ok := out["Cursors.js"]; !ok {
		t.Fatalf("no runtime module: %v", keys(out))
	}

	t.Run("wire", func(t *testing.T) {
		slowtest.Gate(t, "node (round-trips fields named sb, wi and br through both JS tiers)")
		node := jsFlatNode(t)
		serializeDir := filepath.Join(filepath.Dir(repoRootForRuntimeProbe(t)), "serialize.js")
		if _, err := os.Stat(filepath.Join(serializeDir, "src", "index.js")); err != nil {
			t.Skipf("serialize.js sibling not staged at %s: %v", serializeDir, err)
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		nm := filepath.Join(dir, "node_modules", "@mas-bandwidth")
		if err := os.MkdirAll(nm, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(serializeDir, filepath.Join(nm, "serialize")); err != nil {
			t.Fatal(err)
		}
		for name, data := range out {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(cursorFieldsHarness), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, production := range []bool{false, true} {
			cmd := testproc.Command(t, node, "run.mjs")
			cmd.Dir = dir
			cmd.Env = os.Environ()
			if production {
				cmd.Env = append(cmd.Env, "NODE_ENV=production")
			}
			b, err := cmd.CombinedOutput()
			if err != nil || strings.TrimSpace(string(b)) != "ok" {
				t.Fatalf("production=%v: err=%v\n%s", production, err, b)
			}
		}
	})
}

// cursorFieldsHarness writes each type through the runtime tier and the flat
// tier, requires the same bytes from both, and reads them back through both.
const cursorFieldsHarness = `import assert from "node:assert/strict";
import * as S from "@mas-bandwidth/serialize";
import * as M from "./Cursors.js";
import * as F from "./CursorsFlat.js";

function inner(x) { const e = new M.Inner(); e.x = x; return e; }

const cursors = new M.Cursors();
cursors.br = [-7, 0, 2147483647];
cursors.sb = Uint8Array.from([0x73, 0x62, 0xc3, 0xa9, 0x21]);
cursors.sbLength = 5;
cursors.wi[0] = inner(11);
cursors.wi[1] = inner(-12);
cursors.wi[2] = inner(13);
cursors.wiCount = 3;

const elems = new M.Elems();
elems.wi[0] = inner(-2147483648);
elems.wiCount = 1;
elems.sb = Uint8Array.from([0x77, 0x69, 0, 0, 0]);
elems.sbLength = 2;

for (const [name, value] of [["Cursors", cursors], ["Elems", elems]]) {
  const ws = new S.WriteStream(new Uint8Array(256));
  assert(M["Write" + name](ws, value), name + " runtime write");
  const bits = ws.bitsProcessed();
  ws.flush();
  const bytes = ws.data().slice();

  const buf = new Uint8Array(256);
  const n = F["Write" + name + "Flat"](new DataView(buf.buffer), value);
  assert.deepEqual(buf.slice(0, n), bytes, name + " flat bytes equal runtime bytes");

  const r = new M[name]();
  assert(M["Read" + name](new S.ReadStream(bytes), r), name + " runtime read");
  assert.deepEqual(r, value, name + " runtime round trip");

  const f = new M[name]();
  assert(F["Read" + name + "Flat"](new DataView(buf.buffer), bits, f), name + " flat read");
  assert.deepEqual(f, value, name + " flat round trip");
}
console.log("ok");
`

// TestJsFlatCursorGuardsStillCatchTheCursor is the guards' negative control:
// narrowed to identifiers, they must still refuse an emission that names the
// cursor while it is static, and pass a property, a comment or a string.
func TestJsFlatCursorGuardsStillCatchTheCursor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		s           string
		write, read bool
	}{
		{"  sb += 8;\n", true, false},
		{"  view.setUint32(wi, lo, true);\n", true, false},
		{"  if (sb >= 32) {\n", true, false},
		{"  v = view.getUint32(br >>> 3, true);\n", false, true},
		{"  br += 5;\n", false, true},
		{"  ...sb\n", true, false},
		{"  if (!validText(value.sb, value.sbLength)) {\n", false, false},
		{"    const e0 = value.wi[i0];\n", false, false},
		{"  if (value.br[i0] > 7) {\n", false, false},
		{"  value.inner.sb = 1; // sb wi br\n", false, false},
		{"  throw new Error('sb wi br');\n", false, false},
		{"  value.sbx = sbx + wib + brr;\n", false, false},
	} {
		if got := namesWriteCursor(tc.s); got != tc.write {
			t.Errorf("namesWriteCursor(%q) = %v, want %v", tc.s, got, tc.write)
		}
		if got := namesReadCursor(tc.s); got != tc.read {
			t.Errorf("namesReadCursor(%q) = %v, want %v", tc.s, got, tc.read)
		}
	}

	refuses := func(name string, g *fgen, s string) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: pf(%q) did not refuse the stale cursor", name, s)
			}
		}()
		g.pf("%s", s)
	}
	refuses("write", &fgen{wKnown: true}, "  sb += 8;\n")
	refuses("write", &fgen{wKnown: true}, "  view.setUint32(wi, lo, true);\n")
	refuses("read", &fgen{rKnown: true}, "  br += 5;\n")

	g := &fgen{wKnown: true, rKnown: true}
	g.pf("%s", "  if (!validText(value.sb, value.sbLength)) {\n")
	g.pf("%s", "    const e0 = value.wi[i0];\n")
	g.pf("%s", "  if (value.br[i0] > 7) {\n")
	if got := g.fn.String(); !strings.Contains(got, "value.sb") || !strings.Contains(got, "value.br[i0]") {
		t.Errorf("the property emissions were not written: %q", got)
	}
}
