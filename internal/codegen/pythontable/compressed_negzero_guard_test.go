package pythontable

import (
	"strings"
	"testing"
)

// TestPythonCompressedFloatStoresAreGuarded is the fast twin of
// TestPythonCompressedNegZeroCorpus (docs/SPEC-TABLES.md §3.3,
// docs/rulings.json fixed-compressed-negzero): it reads the embedded Python
// runtime rather than running it. Every Python writer is data-driven, so a
// ranged (compressed) float is the descriptor carrying float_range (the fixed
// form's spec carries "ranged"); at every write site the value is normalised
// (a -0 that compares equal to 0 becomes the literal 0.0) BEFORE its bits are
// packed, and the variable form's elision compares a ranged float by value.
// The plain float's path is the control: its elision compares bit patterns,
// so its -0 rides.
func TestPythonCompressedFloatStoresAreGuarded(t *testing.T) {
	t.Parallel()
	rt, err := Runtime()
	if err != nil {
		t.Fatal(err)
	}
	src := func(name string) string {
		t.Helper()
		data, ok := rt[name]
		if !ok {
			t.Fatalf("the runtime has no %s", name)
		}
		return string(data)
	}
	// body is the text of the function whose def line starts at header, up to
	// the next line that starts a definition at the same or a lower indent.
	body := func(file, text, header string) string {
		t.Helper()
		at := strings.Index(text, header)
		if at < 0 {
			t.Fatalf("%s: no %q", file, header)
		}
		indent := header[:len(header)-len(strings.TrimLeft(header, " "))]
		rest := text[at+len(header):]
		for _, stop := range []string{"\n" + indent + "def ", "\n" + indent + "class "} {
			if i := strings.Index(rest, stop); i >= 0 {
				rest = rest[:i]
			}
		}
		return header + rest
	}
	// before requires guard to occur in fn, ahead of the write.
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

	// THE VARIABLE FORM AND THE RETAINING SAVE: the float kind's element write
	kf := src("_schema/kind_float.py")
	before("kind_float.py", "write_element", body("kind_float.py", kf, "    def write_element("),
		"if fd.float_range and value == 0.0:\n            value = 0.0", "w.raw(_pack(fd, value))")
	// the elision: a ranged float by value, a plain one by its bits
	desc := src("_schema/descriptor.py")
	f32 := body("descriptor.py", desc, "def _float32_is_default(")
	if !strings.Contains(f32, "if fd.float_range:") || !strings.Contains(f32, "return struct.pack(\"<f\", value) == struct.pack(\"<f\", fd.default)") {
		t.Errorf("descriptor.py _float32_is_default does not elide a ranged -0 by value and a plain one by its bits:\n%s", f32)
	}
	f64 := body("descriptor.py", desc, "def _float64_is_default(")
	if !strings.Contains(f64, "if fd.float_range:\n        return bool(value == fd.default)") || !strings.Contains(f64, "return struct.pack(\"<d\", value) == struct.pack(\"<d\", fd.default)") {
		t.Errorf("descriptor.py _float64_is_default does not elide a ranged -0 by value and a plain one by its bits:\n%s", f64)
	}

	// THE FIXED FORM
	before("fixedform.py", "the float leaf", src("_schema/fixedform.py"),
		"if spec.get(\"ranged\", False) and value == 0:\n            value = 0.0", "encoded = struct.pack(\"<f\" if size == 4 else \"<d\", value)")

	// THE JSON WRITER
	before("json_table.py", "_write_float", body("json_table.py", src("_schema/json_table.py"), "def _write_float("),
		"if value == 0:\n            value = 0.0", "for precision in range(")

	// THE COOK: the scalar write normalises a ranged value, and every float
	// call site says whether its field is ranged
	cook := src("_schema/cookwrite.py")
	before("cookwrite.py", "_write_scalar", body("cookwrite.py", cook, "def _write_scalar("),
		"if ranged and value == 0.0:\n            value = 0.0", "raw = struct.pack(")
	for _, site := range []string{
		"_write_scalar(buf, at, width, fd.codec, current, fd.lo < 0, order, fd.float_range)",
		"_write_scalar(buf, at + position * stride, stride, fd.elem_codec, item, fd.lo < 0, order, fd.float_range)",
		"_write_scalar(buf, at + position * stride, stride, fd.elem.codec, item, fd.elem.lo < 0, order, fd.elem.float_range)",
		"_write_scalar(buf, element_at, fd.elem_size, fd.elem_codec, item, fd.elem.lo < 0 if fd.elem is not None else fd.lo < 0, order, fd.float_range)",
	} {
		if !strings.Contains(cook, site) {
			t.Errorf("cookwrite.py: a float cook site does not pass its range: no %q", site)
		}
	}
	if n, want := strings.Count(cook, "_write_scalar("), 5; n != want {
		t.Errorf("cookwrite.py: %d _write_scalar texts, want %d (the def and four call sites): a new site must pass its range", n, want)
	}
}
