package jstable

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestJsCompressedFloatStoresAreGuarded is the fast twin of
// TestJsCompressedNegZeroCorpus (docs/SPEC-TABLES.md §3.3, docs/rulings.json
// fixed-compressed-negzero): it reads the JavaScript generated for the corpus's
// Probe.schema rather than running it. A compressed (ranged) float's -0 is held
// as +0 BEFORE its bits are written, in every build: the variable save, the
// retaining save and the JSON writer through TableVariableHeldFloat (a
// descriptor carries lo only for a ranged float), the variable form's elision
// comparing the held value, the cook writer through its descriptor's rg mark,
// the fixed save through a value compare (-0 === 0), and the message form's
// elision by value. The plain float `p` is the control: it carries no
// normalisation and its -0 rides bit-exact.
func TestJsCompressedFloatStoresAreGuarded(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join(jsNegZeroCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(unitFrom(t, string(src)))
	if err != nil {
		t.Fatal(err)
	}
	file := func(name string) string {
		t.Helper()
		data, ok := files[name]
		if !ok {
			t.Fatalf("the unit has no %s", name)
		}
		return string(data)
	}
	has := func(name, text, want, why string) {
		t.Helper()
		if !strings.Contains(text, want) {
			t.Errorf("%s: %s: no %q", name, why, want)
		}
	}
	lacks := func(name, text, bad, why string) {
		t.Helper()
		if strings.Contains(text, bad) {
			t.Errorf("%s: %s: %q", name, why, bad)
		}
	}
	// before requires guard ahead of write inside the text that starts at
	// header and runs to the next line that is exactly "}" at its indent.
	before := func(name, text, header, guard, write string) {
		t.Helper()
		at := strings.Index(text, header)
		if at < 0 {
			t.Errorf("%s: no %q", name, header)
			return
		}
		body := text[at:]
		g, w := strings.Index(body, guard), strings.Index(body, write)
		switch {
		case g < 0:
			t.Errorf("%s %s: a compressed float's -0 is stored as held: no %q", name, header, guard)
		case w < 0:
			t.Errorf("%s %s: no write %q to guard", name, header, write)
		case g > w:
			t.Errorf("%s %s: the guard %q comes after the write %q", name, header, guard, write)
		}
	}

	const v = "NzgoVariable.js"
	vs := file(v)
	has(v, vs, "function TableVariableHeldFloat(d, v) {\n  return d.lo !== null && d.lo !== undefined && v === 0 ? 0 : v;\n}",
		"the held value does not take a ranged -0 to +0")
	before(v, vs, "function TableVariableWriteFloat(w, d, v) {", "v = TableVariableHeldFloat(d, v);", "w.f32(v)")
	has(v, vs, "if (!TableVariableSameFloat(fd.f32, TableVariableHeldFloat(fd, val), TableVariableHeldFloat(fd, fd.def))) {",
		"a field elides by its -0 bits")
	has(v, vs, "return TableVariableSameFloat(e.f32, TableVariableHeldFloat(e, x), TableVariableHeldFloat(e, e.def));",
		"an element or keyed slot elides by its -0 bits")
	has(v, vs, "const t = TableVariableFloatText(TableVariableHeldFloat(d, val), d.f32);",
		"the JSON writer spells a compressed -0 as -0")
	same := vs[strings.Index(vs, "function TableVariableSameFloat("):]
	same = same[:strings.Index(same, "\n}\n")]
	lacks(v, same, "Held", "the bit compare normalises, so a plain float's -0 no longer rides")
	before(v, vs, "function writeValue(view, at, fd, value, le, ctx) {", "if (fd.rg === 1 && value === 0) { value = 0; }", "view.setFloat32(at, value, le)")
	has(v, vs, `{k:"f32",n:"a",at:0,rg:1}`, "the cook descriptor of a compressed float is not marked")
	has(v, vs, `{k:"f32",n:"p",at:48}`, "the cook descriptor of the plain float moved or was marked")

	const n = "NzgoTable.js"
	ns := file(n)
	has(n, ns, "view.setFloat32(at, (value.a === 0 ? 0 : value.a), true);", "the fixed save stores a compressed -0")
	has(n, ns, "view.setFloat32(at + 32 + i * 4, (value.ks[i] === 0 ? 0 : value.ks[i]), true);", "the fixed save stores a keyed slot's -0")
	has(n, ns, "view.setFloat32(at + 41, (value.pick.v === 0 ? 0 : value.pick.v), true);", "the fixed save stores a union arm's -0")
	has(n, ns, "view.setFloat32(at + 45, value.p, true);", "the fixed save normalises the plain float")
	has(n, ns, "if (value.z !== 0) {", "the message form elides a compressed field by its -0 bits")
	if !regexp.MustCompile(`if \(!\(value\.ks\[_i\d+\] !== 0\)\) \{ continue; \}`).MatchString(ns) {
		t.Errorf("%s: the message form elides a keyed slot by its -0 bits", n)
	}
	has(n, ns, "if (!globalThis.Object.is(value.p, 0)) {", "the message form elides the plain float by value")
	lacks(n, ns, "globalThis.Object.is(value.z,", "the message form compares a compressed float's bits")
}
