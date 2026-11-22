package squirreltable_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// TestSquirrelCompressedFloatStoresAreGuarded is the fast twin of
// TestSquirrelCompressedNegZeroCorpus (docs/SPEC-TABLES.md §3.3,
// docs/rulings.json fixed-compressed-negzero): it reads the embedded Squirrel
// runtime and the generated fixed writer rather than running them. A ranged
// (compressed) float is the descriptor carrying `fmin`; at every write site
// the value is normalised (a -0 becomes +0) BEFORE the bits are written, in
// every build, and the variable form's elision compares a compressed float by
// value. The plain float is the control: the generated fixed writer stores
// its value as held.
func TestSquirrelCompressedFloatStoresAreGuarded(t *testing.T) {
	t.Parallel()
	rt := squirreltable.Runtime()
	src := func(name string) string {
		t.Helper()
		data, ok := rt[squirreltable.RuntimePrefix+name]
		if !ok {
			t.Fatalf("the runtime has no %s", name)
		}
		return string(data)
	}
	// body is the text of the function whose header starts at header, up to
	// the first line that is exactly "};" at column 0.
	body := func(file, text, header string) string {
		t.Helper()
		at := strings.Index(text, header)
		if at < 0 {
			t.Errorf("%s: no %q", file, header)
			return ""
		}
		head, _, found := strings.Cut(text[at:], "\n};\n")
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

	save := src("save.nut")
	before("save.nut", "writePrim (the variable form and the retaining save)", body("save.nut", save, "writePrim = function("),
		"if (f == 0) val = 0.0;", "wfixed(w, narrowF32(val.tofloat()), 4)")
	if fn := body("save.nut", save, "local floatIsDefault = function("); !strings.Contains(fn, "((a & 0x7fffffff) == 0 && (b & 0x7fffffff) == 0)") {
		t.Errorf("save.nut floatIsDefault elides a compressed float by the -0 bits:\n%s", fn)
	}
	before("save.nut", "fixedWriteElement (the fixed form a fixed table's Save writes)", body("save.nut", save, "fixedWriteElement = function("),
		"e.fmin != null && (bits & (e.f32 ? 0x7fffffff : 0x7fffffffffffffff)) == 0) bits = 0;", "putFixedU(out, off, e.f32 ? 4 : 8, bits)")

	cook := src("cookwrite.nut")
	if fn := body("cookwrite.nut", cook, "local joinField = function("); !strings.Contains(fn, `fmin = ("fmin" in fd) ? fd.fmin : null`) {
		t.Errorf("cookwrite.nut joinField drops the descriptor's range, so the cook writer cannot tell a compressed float:\n%s", fn)
	}
	before("cookwrite.nut", "scalarWrite", body("cookwrite.nut", cook, "scalarWrite = function("),
		"f.fmin != null && (bits & ((f.f32 || w == 4) ? 0x7fffffff : 0x7fffffffffffffff)) == 0) bits = 0;", "put(buf, at, bits, (f.f32 || w == 4) ? 4 : 8, order)")

	json := src("json.nut")
	_, jw, found := strings.Cut(json, "    case \"float\": {\n      // only a number is a float's value")
	if !found {
		t.Fatalf("json.nut: no float value writer")
	}
	before("json.nut", "the float value writer", jw,
		`if (has(d, "fmin") && (d.f32 ? widenF32(narrowF32(val.tofloat(), 0)[0]) : val) == 0) val = 0.0;`, "floatText(val, d.f32)")

	msg := src("message.nut")
	before("message.nut", "writeScalar", body("message.nut", msg, "local writeScalar = function("),
		"if (x == 0) value = 0.0;", "put(w,index,fd.qbits)")
	if fn := body("message.nut", msg, "defaultValue = function("); !strings.Contains(fn, "if (fd.f32 && fd.fmin != null) return f32(value) == f32(fd.def);") {
		t.Errorf("message.nut defaultValue elides a compressed float by the -0 bits:\n%s", fn)
	}

	// THE GENERATED FIXED WRITER: every compressed cell of the corpus's
	// fixed table stores +0 for -0; the plain float rides as held.
	paths, err := compiler.GatherPaths([]string{filepath.Join(sqNegZeroCorpusDir, "Probe.schema")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var unit string
	for _, data := range files {
		if strings.Contains(string(data), "_M.FixFixedWriteBody <- function(") {
			unit = string(data)
		}
	}
	wb := body("the generated unit", unit, "_M.FixFixedWriteBody <- function(")
	for _, cell := range []string{"v.A", "v.Z", "v.Xs[i]", "v.Ys[i]", "v.Ks[i]", "v.Pick.V"} {
		if want := "(_rt_fixed.f32(" + cell + ") == 0 ? 0.0 : " + cell + ")"; !strings.Contains(wb, want) {
			t.Errorf("FixFixedWriteBody: the compressed cell %s is stored as held: no %q", cell, want)
		}
	}
	if !strings.Contains(wb, "_rt_fixed.putF32(b, at + 45, v.P)") {
		t.Errorf("FixFixedWriteBody: the plain float p is not stored as held:\n%s", wb)
	}
}
