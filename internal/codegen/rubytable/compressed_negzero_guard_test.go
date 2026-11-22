package rubytable

import (
	"strings"
	"testing"
)

// TestRubyCompressedFloatStoresAreGuarded is the fast twin of
// TestRubyCompressedNegZeroCorpus (docs/SPEC-TABLES.md §3.3, docs/rulings.json
// fixed-compressed-negzero): it reads the embedded Ruby runtime rather than
// running it. Every Ruby writer is data-driven, so a compressed (ranged) float
// is the descriptor carrying float_range (the fixed form's spec carries
// "ranged"); at every write site the value is held as +0 (a -0 that compares
// equal to 0 becomes 0.0) BEFORE its bits are written, in every build: the
// variable and retaining saves, the fixed save, the JSON writer and the cook
// writer, and the variable and message forms' elision compares the value, not
// the bits. The plain float's path is the control: its elision is the bit
// compare, which carries no normalisation of its own.
func TestRubyCompressedFloatStoresAreGuarded(t *testing.T) {
	t.Parallel()
	src := func(name string) string {
		t.Helper()
		data, err := runtimeFS.ReadFile(name)
		if err != nil {
			t.Fatalf("the runtime has no %s: %v", name, err)
		}
		return string(data)
	}
	// body is the text of the method whose header starts at header, up to
	// the first `end` at the method's own indent.
	body := func(file, text, header string) string {
		t.Helper()
		at := strings.Index(text, header)
		if at < 0 {
			t.Errorf("%s: no %q", file, header)
			return ""
		}
		line := text[strings.LastIndex(text[:at], "\n")+1 : at]
		head, _, found := strings.Cut(text[at:], "\n"+line+"end\n")
		if !found {
			t.Fatalf("%s: %q has no end", file, header)
		}
		return head
	}
	// before requires guard to occur in fn, ahead of the first write.
	before := func(file, where, fn, guard, write string) {
		t.Helper()
		g, w := strings.Index(fn, guard), strings.Index(fn, write)
		switch {
		case g < 0:
			t.Errorf("%s %s: a compressed float's -0 is stored as held: no %q", file, where, guard)
		case w < 0:
			t.Errorf("%s %s: no write %q to guard", file, where, write)
		case g > w:
			t.Errorf("%s %s: the guard %q comes after the write %q", file, where, guard, write)
		}
	}

	const save = "runtime/_table/save.rb"
	ss := src(save)
	before(save, "write_float_element (variable and retaining saves)", body(save, ss, "def self.write_float_element("),
		"v = fd.float_range && value.zero? ? 0.0 : value", ".pack('e')")

	const desc = "runtime/_table/descriptor.rb"
	ds := src(desc)
	for _, want := range []string{
		"return SchemaTable.float32_round(value) == SchemaTable.float32_round(def_val) if @float_range\n\n          SchemaTable.float32_bits_eq?(value, def_val)",
		"return value == def_val if @float_range\n\n          SchemaTable.float64_bits_eq?(value, def_val)",
	} {
		if !strings.Contains(ds, want) {
			t.Errorf("%s: the default test (the variable and message forms' elision) compares a compressed float's -0 by its bits: no %q", desc, want)
		}
	}

	const fixed = "runtime/_fixed.rb"
	fs := src(fixed)
	_, fw, found := strings.Cut(fs, "elsif shape == \"float\"\n      unless value.is_a?(::Float) && value.finite?")
	if !found {
		t.Fatalf("%s: no fixed float writer", fixed)
	}
	before(fixed, "the fixed float writer (float32)", fw, "if spec[\"ranged\"] && value == 0.0\n        value = 0.0\n      end", "out[at, 4] = [stored].pack(\"e\")")
	before(fixed, "the fixed float writer (float64)", fw, "if spec[\"ranged\"] && value == 0.0\n        value = 0.0\n      end", "out[at, 8] = [value].pack(\"E\")")

	const json = "runtime/_table/json_table.rb"
	js := src(json)
	jw := body(json, js, "def self.write_json_float(")
	before(json, "write_json_float", jw, "if field.float_range", "value = 0.0 if value.zero?")
	if strings.Count(jw, "value = 0.0 if value.zero?") != 1 || strings.Index(jw, "value = 0.0 if value.zero?") > strings.Index(jw, "(low..high).each") {
		t.Errorf("%s write_json_float: a compressed float's -0 is not held as +0 before it is spelled:\n%s", json, jw)
	}

	const cook = "runtime/_table/cook.rb"
	cs := src(cook)
	cw := body(cook, cs, "def self.cook_write_scalar(")
	before(cook, "cook_write_scalar (float32)", cw, "float32_round(ranged && val.zero? ? 0.0 : val)", "buf[at, 4] =")
	before(cook, "cook_write_scalar (float64)", cw, "[ranged && val.zero? ? 0.0 : val]", ".pack(big ? 'G' : 'E')")
	// every caller hands the field's float_range, so no float site writes unmarked
	rec := body(cook, cs, "def self.cook_write_record(")
	if n, marked := strings.Count(rec, "cook_write_scalar("), strings.Count(rec, "float_range)")+strings.Count(rec, ", ranged)"); n == 0 || n != marked {
		t.Errorf("%s cook_write_record: %d cook_write_scalar calls, %d hand the field's float_range", cook, n, marked)
	}
}
