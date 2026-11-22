package luautable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

// TestLuauCompressedFloatStoresAreGuarded is the fast twin of
// TestLuauCompressedNegZeroCorpus (docs/SPEC-TABLES.md §3.3, docs/rulings.json
// fixed-compressed-negzero): it reads the embedded Luau runtime rather than
// running it. Every Luau writer is data-driven, so a ranged (compressed) float
// is the descriptor carrying `lo` (the generator emits lo, hi and res
// together); at every write site the value is normalised (a -0 that compares
// equal to 0 becomes the literal 0) BEFORE the bits are written, and the
// variable form's elision compares the normalised value through held_float.
// The plain float's path is the control: same_float, the bit comparison a
// plain float elides by, carries no normalisation of its own.
func TestLuauCompressedFloatStoresAreGuarded(t *testing.T) {
	t.Parallel()
	rt := luautable.Runtime()
	src := func(name string) string {
		t.Helper()
		data, ok := rt[name]
		if !ok {
			t.Fatalf("the runtime has no %s", name)
		}
		return string(data)
	}
	// body is the text of the function whose header line starts at header,
	// up to the first line that is exactly "end" at column 0.
	body := func(file, text, header string) string {
		t.Helper()
		at := strings.Index(text, header)
		if at < 0 {
			t.Errorf("%s: no %q", file, header)
			return ""
		}
		rest := text[at:]
		head, _, found := strings.Cut(rest, "\nend\n")
		if !found {
			t.Fatalf("%s: %q has no end", file, header)
		}
		return head
	}
	// before requires guard to occur in fn, ahead of the first write.
	before := func(file, fn, where, guard, write string) {
		t.Helper()
		g := strings.Index(fn, guard)
		w := strings.Index(fn, write)
		switch {
		case g < 0:
			t.Errorf("%s %s: a compressed float's -0 is stored as held: no %q", file, where, guard)
		case w < 0:
			t.Errorf("%s %s: no write %q to guard", file, where, write)
		case g > w:
			t.Errorf("%s %s: the guard %q comes after the write %q", file, where, guard, write)
		}
	}

	save := src("schema_save.luau")
	held := body("schema_save.luau", save, "local function held_float(")
	if !strings.Contains(held, "if e.lo ~= nil and v == 0 then v = 0 end") {
		t.Errorf("schema_save.luau held_float does not hold +0 for a ranged -0:\n%s", held)
	}
	same := body("schema_save.luau", save, "local function same_float(")
	if strings.Contains(same, "held_float") || strings.Contains(same, ".lo") {
		t.Errorf("schema_save.luau same_float normalises: a plain float's -0 must ride bit-exact:\n%s", same)
	}
	before("schema_save.luau", body("schema_save.luau", save, "write_value = function("), "write_value (scalars, elements, keyed slots, union arms)",
		"if e.lo ~= nil and val == 0 then val = 0 end", "narrow_f32(val)")
	if fn := body("schema_save.luau", save, "local function elem_is_default("); !strings.Contains(fn, "same_float(held_float(e, val), 0, e.f32)") {
		t.Errorf("schema_save.luau elem_is_default elides an element by the -0 bits:\n%s", fn)
	}
	if fn := body("schema_save.luau", save, "local function at_default("); !strings.Contains(fn, "same_float(held_float(fd, val), held_float(fd, fd.def), fd.f32)") {
		t.Errorf("schema_save.luau at_default elides a field by the -0 bits:\n%s", fn)
	}

	fixed := src("schema_fixed.luau")
	leaf := body("schema_fixed.luau", fixed, "local function writeLeaf(")
	before("schema_fixed.luau", leaf, "writeLeaf (float32)",
		"if f.lo ~= nil and v == 0 then bits = 0 end", "buffer.writeu32(buf, off, bits)")
	before("schema_fixed.luau", leaf, "writeLeaf (float64)",
		"if f.lo ~= nil and v == 0 then v = 0 end", "buffer.writef64(buf, off, v)")

	cook := src("schema_cook.luau")
	before("schema_cook.luau", body("schema_cook.luau", cook, "write_value = function("), "write_value",
		"if e.lo ~= nil and f == 0 then f = 0 end", "put_float(w, at, f,")

	json := src("schema_json.luau")
	const floatWriter = "  elseif s == \"float\" then\n    if not float_ok(d, val)"
	_, jw, found := strings.Cut(json, floatWriter)
	if !found {
		t.Fatalf("schema_json.luau: no float value writer %q", floatWriter)
	}
	before("schema_json.luau", jw, "the float value writer",
		"if d.lo ~= nil and val == 0 then val = 0 end", "float_text(val, d.f32)")

	msg := src("schema_message.luau")
	before("schema_message.luau", body("schema_message.luau", msg, "local function mEncodeScalar("), "mEncodeScalar",
		"(kind == 10 or kind == 11) and e.lo ~= nil and val == 0 then val = 0 end", "buffer.writef32(msgScratch, 0, val)")
	before("schema_message.luau", body("schema_message.luau", msg, "local function mAtDefault("), "mAtDefault",
		"if val == 0 then val = 0 end", "buffer.writef32(msgScratch, 0, val)")
}
