package mootable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
)

// TestMooCompressedFloatStoresAreGuarded is the fast twin of
// TestMooCompressedNegZeroCorpus (docs/SPEC-TABLES.md §3.3, docs/rulings.json
// fixed-compressed-negzero): it reads the embedded moo runtime rather than
// running it. Every moo writer is data-driven, and a float is held as its
// binary pattern, so a compressed float's -0 is the descriptor carrying `flo`
// and the value 0x80000000 (or the Uint64 with the sign bit alone):
// V.storedFloat turns it into +0, and at every write site that call comes
// BEFORE the bits are written, in every build. The elision of the variable
// and message forms compares the stored values. The plain float is the
// control: storedFloat hands back a value whose descriptor has no `flo` as
// it is.
func TestMooCompressedFloatStoresAreGuarded(t *testing.T) {
	t.Parallel()
	rt := mootable.Runtime()
	src := func(name string) string {
		t.Helper()
		data, ok := rt[name]
		if !ok {
			t.Fatalf("the runtime has no %s", name)
		}
		return string(data)
	}
	// body is the text of the function whose header starts at header, up to
	// the first line that is exactly "end" at column 0.
	body := func(file, text, header string) string {
		t.Helper()
		at := strings.Index(text, header)
		if at < 0 {
			t.Errorf("%s: no %q", file, header)
			return ""
		}
		head, _, found := strings.Cut(text[at:], "\nend\n")
		if !found {
			t.Fatalf("%s: %q has no end", file, header)
		}
		return head
	}
	// before requires guard to occur in fn, ahead of the first write.
	before := func(file, where, fn, guard, write string) {
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

	values := src("table_values.moo")
	stored := body("table_values.moo", values, "function V.storedFloat(")
	for _, want := range []string{
		"if fd.flo == nil then\n\t\treturn v\n\tend",
		"return if v == 0x80000000 then 0 else v",
		"if Uint64.is(v) and v[1] == 0 and v[2] == 0x80000000 then\n\t\treturn Uint64.new(0, 0)",
	} {
		if !strings.Contains(stored, want) {
			t.Errorf("table_values.moo V.storedFloat does not hold +0 for a compressed -0 and the bits for a plain one: no %q:\n%s", want, stored)
		}
	}

	save := src("table_save.moo")
	before("table_save.moo", "write_float (the variable form and the retaining save)", body("table_save.moo", save, "local function write_float("),
		"val = V.storedFloat(e, val)", "w_u32(w, val)")
	if fn := body("table_save.moo", save, "local function elem_is_default("); !strings.Contains(fn, "V.sameStorage(e, V.storedFloat(e, val), V.storedFloat(e, e.def))") {
		t.Errorf("table_save.moo elem_is_default elides an element by the -0 bits:\n%s", fn)
	}
	if fn := body("table_save.moo", save, "local function at_default("); !strings.Contains(fn, "V.sameStorage(fd, V.storedFloat(fd, val), V.storedFloat(fd, fd.def))") {
		t.Errorf("table_save.moo at_default elides a field by the -0 bits:\n%s", fn)
	}

	fixed := src("table_fixed.moo")
	before("table_fixed.moo", "writeValue", body("table_fixed.moo", fixed, "writeValue = function("),
		"v = V.storedFloat(f, v)", "buffer.writeu32(out, off, v)")

	cook := src("table_cookwrite.moo")
	before("table_cookwrite.moo", "writeScalar", body("table_cookwrite.moo", cook, "local function writeScalar("),
		"x = V.storedFloat(e, x)", "putUN(ctx, at, 4, x)")

	json := src("json.moo")
	before("json.moo", "writeScalar", body("json.moo", json, "local function writeScalar("),
		"v = V.storedFloat(fd, v)", "J.formatFloat32(v)")

	msg := src("table_message.moo")
	before("table_message.moo", "encodeValue", body("table_message.moo", msg, "local function encodeValue("),
		"val = V.storedFloat(e, val)", "put(w, val[1], 32)")
	if fn := body("table_message.moo", msg, "local function atDefault("); !strings.Contains(fn, "V.sameStorage(fd, V.storedFloat(fd, val), V.storedFloat(fd, fd.def))") {
		t.Errorf("table_message.moo atDefault elides a field by the -0 bits:\n%s", fn)
	}
}
