package luatable

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaCompressedFloatStoresAreGuarded is the fast twin of
// TestLuaCompressedNegZeroCorpus (docs/SPEC-TABLES.md §3.3, docs/rulings.json
// fixed-compressed-negzero): it reads the embedded Lua runtime and the typed
// save the generator writes rather than running them. Every Lua writer is
// data-driven, and one helper, descriptor.lua's D.cf, holds +0 for a ranged
// (compressed) float's -0 (the descriptor carrying fmin). Each writer passes
// the value through D.cf BEFORE its bits are written or its elision compares
// them; the typed save normalises in the body it emits. The plain float's path
// is the control: same_float, the bit comparison a plain float elides by,
// carries no normalisation of its own.
func TestLuaCompressedFloatStoresAreGuarded(t *testing.T) {
	t.Parallel()
	src := func(name string) string {
		t.Helper()
		data, err := runtime.ReadFile("runtime/" + name)
		if err != nil {
			t.Fatalf("the runtime has no %s: %v", name, err)
		}
		return string(data)
	}
	// body is the text of the function whose header starts at header, up to
	// the first line that is exactly "end" at column 0.
	body := func(file, text, header string) string {
		t.Helper()
		at := strings.Index(text, header)
		if at < 0 {
			t.Fatalf("%s: no %q", file, header)
		}
		head, _, found := strings.Cut(text[at:], "\nend\n")
		if !found {
			t.Fatalf("%s: %q has no end", file, header)
		}
		return head
	}
	// has requires each site, by its exact text, in a writer's source.
	has := func(file, text, where string, sites ...string) {
		t.Helper()
		for _, s := range sites {
			if !strings.Contains(text, s) {
				t.Errorf("%s %s: a compressed float's -0 is stored as held: no %q", file, where, s)
			}
		}
	}

	desc := src("descriptor.lua")
	if cf := body("descriptor.lua", desc, "function D.cf("); !strings.Contains(cf, "if d.fmin ~= nil and v == 0 then return 0.0 end") {
		t.Errorf("descriptor.lua D.cf does not hold +0 for a ranged -0:\n%s", cf)
	}

	// the variable save and the retaining save share the walk's shape: the
	// array elision, the field elision, the keyed slot, the field write, the
	// element write and the fixed-class body write
	for _, file := range []string{"save.lua", "retain.lua"} {
		text := src(file)
		has(file, text, "the variable form",
			"same_float(e.f32, D.cf(e, e1), def)",
			"same_float(fd.f32, D.cf(fd, x), D.cf(fd, fd.default))",
			"val = D.cf(e, val)",
			"w_float(w, fd.f32, D.cf(fd, x))",
			"w_float(body, e.f32, D.cf(e, val or 0.0))",
			"w_float(body, f.f32, D.cf(f, val))")
	}
	if same := body("save.lua", src("save.lua"), "local function same_float("); strings.Contains(same, "D.cf") || strings.Contains(same, "fmin") {
		t.Errorf("save.lua same_float normalises: a plain float's -0 must ride bit-exact:\n%s", same)
	}
	has("fixed.lua", src("fixed.lua"), "the fixed leaf write", "pack_float(d.f32, D.cf(d, v or 0.0))")
	has("cookwrite.lua", src("cookwrite.lua"), "the cooked float", "v = D.cf(f, v)")
	has("json.lua", src("json.lua"), "the float value writer", "float_text(D.cf(d, val), d.f32)")
	has("message.lua", src("message.lua"), "the message elision",
		"same_float(d.f32, D.cf(d, v), D.cf(d, def))",
		"not same_float(d.f32, D.cf(d, v), 0.0)")

	// THE TYPED SAVE: the generator emits the normalisation into the body of a
	// compressed field, and never into a plain one's
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "p.schema"), "package probe\n\ntable V\n{\n    c float32 | min = 0, max = 0.1, resolution = 0.01\n    p float32\n}\n")
	files, err := Generate(luarun.LoadUnit(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	var module string
	for name, data := range files {
		if strings.Contains(string(data), "_M.TypedBodies") {
			module = name
		}
	}
	if module == "" {
		t.Fatal("no generated module carries TypedBodies")
	}
	text := string(files[module])
	const norm = "if x == 0 then x = 0.0 end"
	if n := strings.Count(text, norm); n != 1 {
		t.Errorf("%s: the typed save normalises %d floats, want 1 (the compressed c, not the plain p)", module, n)
	}
	if !strings.Contains(text, "local x = v.c\n      "+norm+"\n") {
		t.Errorf("%s: the typed save does not normalise the compressed c before its compare and write", module)
	}
	if !strings.Contains(text, "local x = v.p\n      if not same(") {
		t.Errorf("%s: the typed save's plain p is not written as held", module)
	}
}
