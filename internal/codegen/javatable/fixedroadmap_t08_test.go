package javatable

// fixedroadmap_t08 — the java leg's ordinary valid-data capability rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"):
// float32-values, float64-values, compressed-floats, signed-fixed-point,
// unsigned-fixed-point, utf16-values, union-field-arms, payload-free-arms and
// union-arrays. Law: docs/SPEC-TABLES.md §3.4 (the fixed form's record and the
// constant arm storage) and docs/FIXED-FORM-ALGORITHM.md §4.6 (the bounds pass).
// One subtest per task id.
//
// The capability clause is the roadmap's own: "the leg writes and reads the
// construct with non-default and boundary values, asserted, in a target a gate
// runs." Every capability subtest below builds the leg's generated surface for
// one schema, sets non-default values with the writer, and reads them back with
// the reader; the FRESH dump proves the setters moved something and the BACK
// dump must equal the SET dump. A construct the reference refuses is reported by
// its own named rule, never called green: `unknown` in the card's verdicts.
//
// A clause already held by an existing test of this leg is named by that test in
// the card's verdicts and is not repeated here.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t08Walk is the probe's reflection dumper, byte for byte t03SaveLoadProbe's:
// a float prints with its raw bits beside it so a float that requantized onto a
// neighbouring value is red by bits and not by a friendly decimal. A char[] —
// the wstring(8) store — prints its UTF-16 code units as hex, not through
// String.valueOf(char): a lone surrogate appended to a String and written out
// is replaced by '?', so the boundary pair U+1F600 would dump "list:?,?" and a
// one-bit corruption of the store would be invisible. The hex run sees every
// code unit, so the pair U+1F600 is the two units d83d,de00 and d83c,de00 is red.
const t08Walk = `
    static void dump(String tag, Object v) throws Exception {
        walk(tag, "", v);
    }

    static void walk(String tag, String path, Object v) throws Exception {
        if (v == null) { out.append(tag).append(' ').append(path).append("=null\n"); return; }
        Class<?> c = v.getClass();
        if (c.isArray()) {
            int n = Array.getLength(v);
            Class<?> el = c.getComponentType();
            if (el == byte.class) {
                byte[] a = (byte[]) v;
                StringBuilder h = new StringBuilder();
                for (byte x : a) { h.append(String.format("%02x", x)); }
                out.append(tag).append(' ').append(path).append("=bytes:").append(h).append('\n');
                return;
            }
            if (el == char.class) {
                char[] a = (char[]) v;
                StringBuilder h = new StringBuilder();
                for (char x : a) { h.append(String.format("%04x", (int) x)); }
                out.append(tag).append(' ').append(path).append("=codeunits:").append(h).append('\n');
                return;
            }
            if (el.isPrimitive()) {
                StringBuilder s = new StringBuilder();
                for (int i = 0; i < n; i++) { if (i > 0) { s.append(','); } s.append(String.valueOf(Array.get(v, i))); }
                out.append(tag).append(' ').append(path).append("=list:").append(s).append('\n');
                return;
            }
            for (int i = 0; i < n; i++) { walk(tag, path + "[" + i + "]", Array.get(v, i)); }
            return;
        }
        if (v instanceof Float) {
            out.append(tag).append(' ').append(path).append('=').append(String.valueOf(v))
               .append("|0x").append(String.format("%08X", Float.floatToRawIntBits((Float) v))).append('\n');
            return;
        }
        if (v instanceof Double) {
            out.append(tag).append(' ').append(path).append('=').append(String.valueOf(v))
               .append("|0x").append(String.format("%016X", Double.doubleToRawLongBits((Double) v))).append('\n');
            return;
        }
        if (c == String.class || c.isPrimitive() || v instanceof Number || v instanceof Boolean
                || v instanceof Character || c.isEnum()) {
            out.append(tag).append(' ').append(path).append('=').append(String.valueOf(v)).append('\n');
            return;
        }
        Field[] fs = c.getFields();
        java.util.Arrays.sort(fs, (a, b) -> a.getName().compareTo(b.getName()));
        for (Field f : fs) {
            if (Modifier.isStatic(f.getModifiers())) { continue; }
            String p = path.isEmpty() ? f.getName() : path + "." + f.getName();
            walk(tag, p, f.get(v));
        }
    }
}
`

// t08Probe is the capability probe: dump a FRESH value, SET the fields the card
// names, save with the leg's own writer, load them back into a FRESH value with
// its own reader, dump that, and print the report. The setters are spelled in
// the probe's source by the Go side; the probe itself is generated, never
// hand-edited.
func t08Probe(class, pkg, root, setters string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `import java.lang.reflect.*;

// GENERATED BY internal/codegen/javatable/fixedroadmap_t08_test.go. The valid-data
// capability probe: write with the leg's own writer, read with its own reader.
public final class %s {
    static final StringBuilder out = new StringBuilder();

    public static void main(String[] args) throws Exception {
        dump("FRESH", new %s.%sFixed.Value());
        %s.%sFixed.Value[] vals = { new %s.%sFixed.Value() };
%s
        dump("SET", vals[0]);
        final byte[] bytes = new byte[%s.%sFixed.measure(1)];
        %s.%sFixed.save(vals, 1, bytes);
        %s.%sFixed.Value[] back = { new %s.%sFixed.Value() };
        %s.TableFixed.Report report = new %s.TableFixed.Report();
        final int n = %s.%sFixed.load(back, 1, bytes,
                %s.TableFixed.plan(1 << 14), new short[1 << 14], %s.%sFixed.image(), report);
        out.append("REPORT n=").append(n)
           .append(" refused=").append(report.refused)
           .append(" malformed=").append(report.malformed)
           .append(" unknown=").append(report.unknown)
           .append(" kindMismatch=").append(report.kindMismatch)
           .append(" widened=").append(report.widened)
           .append(" clamped=").append(report.clamped)
           .append('\n');
        dump("BACK", back[0]);
        System.out.print(out);
    }
`, class, pkg, root, pkg, root, pkg, root, setters, pkg, root, pkg, root, pkg, root, pkg, root, pkg, pkg, pkg, root, pkg, pkg, root)
	b.WriteString(t08Walk)
	return b.String()
}

// t08Build generates the schema's java fixed surface and compiles it beside the
// capability probe, answering the classes directory.
func t08Build(t *testing.T, dir, name, schema, setters string) string {
	t.Helper()
	u := unitFrom(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("t08 %s: generate: %v", name, err)
	}
	pkg := javaPackageOf(u)
	root := rootOf(t, u)
	src := filepath.Join(dir, "src")
	writeFixedJava(t, src, pkg, files)
	class := "Probe_" + name
	if err := os.WriteFile(filepath.Join(src, class+".java"), []byte(t08Probe(class, pkg, root.Name, setters)), 0o644); err != nil {
		t.Fatal(err)
	}
	javac, _ := javaTools(t)
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	sources, err := filepath.Glob(filepath.Join(src, "*", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	probes, err := filepath.Glob(filepath.Join(src, "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	args = append(args, probes...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("t08 %s: javac: %v\n%s", name, err, out)
	}
	return classes
}

// t08Dump is one probe run's three dumps and its report line.
type t08Dump struct {
	fresh  map[string]string
	set    map[string]string
	back   map[string]string
	report string
}

// t08Run runs the probe and reads the FRESH/SET/BACK dumps and the report line.
func t08Run(t *testing.T, classes, class string) t08Dump {
	t.Helper()
	_, javaBin := javaTools(t)
	out, err := runJava(t, javaBin, classes, class)
	if err != nil {
		t.Fatalf("java %s: %v\n%s", class, err, out)
	}
	d := t08Dump{fresh: map[string]string{}, set: map[string]string{}, back: map[string]string{}}
	for line := range strings.SplitSeq(out, "\n") {
		switch {
		case strings.HasPrefix(line, "FRESH "):
			k, v, _ := strings.Cut(strings.TrimPrefix(line, "FRESH "), "=")
			d.fresh[k] = v
		case strings.HasPrefix(line, "SET "):
			k, v, _ := strings.Cut(strings.TrimPrefix(line, "SET "), "=")
			d.set[k] = v
		case strings.HasPrefix(line, "BACK "):
			k, v, _ := strings.Cut(strings.TrimPrefix(line, "BACK "), "=")
			d.back[k] = v
		case strings.HasPrefix(line, "REPORT "):
			d.report = line
		}
	}
	return d
}

// t08RoundTrip is the capability clause, once: build the schema, set the named
// fields to non-default values, read them back, and require that the write
// moved each field off its fresh default (so a setter that never ran cannot
// make the round trip pass by comparing two defaults) and that the read landed
// every one of them unchanged.
func t08RoundTrip(t *testing.T, name, schema, setters string, moved []string) {
	t.Helper()
	classes := t08Build(t, t.TempDir(), name, schema, setters)
	d := t08Run(t, classes, "Probe_"+name)
	if !strings.Contains(d.report, "n=1") || !strings.Contains(d.report, "refused=false") ||
		!strings.Contains(d.report, "malformed=false") {
		t.Fatalf("%s: the read must accept the writer's own file: %s", name, d.report)
	}
	if !strings.Contains(d.report, "clamped=0") {
		t.Errorf("%s: a value inside the declared bounds moved clamped: %s", name, d.report)
	}
	// EVERY field the writer stored comes back as it was, selected or not: an
	// unselected union arm is a field too, and it must round-trip its default.
	for f, set := range d.set {
		got, ok := d.back[f]
		if !ok {
			t.Errorf("%s: the BACK dump has no field %q", name, f)
			continue
		}
		if got != set {
			t.Errorf("%s: field %q did not round-trip: wrote %q, read %q", name, f, set, got)
		}
	}
	// And every field the card names moved off the fresh default, so a setter
	// that never ran cannot pass the round trip by comparing two defaults.
	for _, f := range moved {
		set, ok := d.set[f]
		if !ok {
			t.Errorf("%s: the SET dump has no field %q", name, f)
			continue
		}
		if got, ok := d.fresh[f]; !ok || got == set {
			t.Errorf("%s: field %q did not move off its fresh default %q; the setter never took effect", name, f, got)
		}
	}
}

// t08Unsupported asserts a task's construct is refused by the ONE rule the card
// names — ir.TableFixedSupported (§3.4: "a union arm carrying text or an array,
// and ... a void arm"). The capability clause is not proved by a refusal, so the
// card records these three tasks `unknown`; this assertion is their evidence.
func t08Unsupported(t *testing.T, name, schema string) {
	t.Helper()
	if ir.TableFixedSupported(rootOf(t, unitFrom(t, schema)), 0) {
		t.Fatalf("%s: ir.TableFixedSupported accepts the construct the card records as unproved", name)
	}
}

// ---- the nine task ids -----------------------------------------------------

// float32-values verifies the §3.4 record's float32 leaf at both storage
// boundaries: the largest finite value and the smallest positive subnormal keep
// their exact bits across a save/load.
func t08Float32(t *testing.T) {
	t08RoundTrip(t, "float32", `package probe

fixed table T
{
    x float32
    y float32
}
`, `        vals[0].x = Float.intBitsToFloat(0x7f7fffff);
        vals[0].y = Float.intBitsToFloat(0x00000001);`, []string{"x", "y"})
}

// float64-values verifies the same for the float64 leaf at its boundaries.
func t08Float64(t *testing.T) {
	t08RoundTrip(t, "float64", `package probe

fixed table T
{
    x float64
    y float64
}
`, `        vals[0].x = Double.longBitsToDouble(0x7fefffffffffffffL);
        vals[0].y = Double.longBitsToDouble(0x0000000000000001L);`, []string{"x", "y"})
}

// compressed-floats verifies SPEC §3.4's "a compressed float rides as the
// float32 itself": the declared min/max/resolution do not requantize on the
// fixed form, so a value inside the declared range keeps its exact float32 bits
// and the read clamps nothing.
func t08Compressed(t *testing.T) {
	t08RoundTrip(t, "compressed", `package probe

fixed table T
{
    v float32 | min = -1, max = 1, resolution = 0.01
}
`, `        vals[0].v = 0.375f;`, []string{"v"})
}

// signed-fixed-point verifies ALGORITHM §4.6's raw scaled storage at both
// bounds: fixed(4, 4) stores a byte whose declared value units are -8..7, so the
// raw bytes -128 and 112 round-trip exactly.
func t08SignedFixed(t *testing.T) {
	t08RoundTrip(t, "signed_fixed", `package probe

fixed table T
{
    lo fixed(4, 4) | min = -8, max = 7
    hi fixed(4, 4) | min = -8, max = 7
}
`, `        vals[0].lo = (byte) -128;
        vals[0].hi = (byte) 112;`, []string{"lo", "hi"})
}

// unsigned-fixed-point verifies the same storage rule with the unsigned
// spelling: ufixed(4, 4)'s declared units are 0..15, the raw bytes 16 and 240
// round-trip exactly, and the read clamps neither.
func t08UnsignedFixed(t *testing.T) {
	t08RoundTrip(t, "unsigned_fixed", `package probe

fixed table T
{
    lo ufixed(4, 4) | min = 0, max = 15
    hi ufixed(4, 4) | min = 0, max = 15
}
`, `        vals[0].lo = (byte) 16;
        vals[0].hi = (byte) 240;`, []string{"lo", "hi"})
}

// utf16-values is recorded `unknown`: this exercises SPEC §3.4's kind 33 leaf
// directly through javatable.Generate, laying wide text out as UTF-16 code
// units with a used length beside them and round-tripping the PAIRED surrogate
// pair U+1F600. The Java target refuses a wstring(N) field in a table closure
// (compiler/widetext.go:49, java absent from tableWideTextTargets), so the
// emitter branch below is unreachable in a target the gate runs.
func t08UTF16(t *testing.T) {
	t08RoundTrip(t, "utf16", `package probe

fixed table T
{
    s wstring(8)
}
`, `        vals[0].s[0] = 0xD83D;
        vals[0].s[1] = 0xDE00;
        vals[0].sLength = 2;`, []string{"s", "sLength"})
}

// union-field-arms is recorded `unknown`: the title's scalar, text and array
// clauses are all unreachable in the Java target, which refuses any union a
// table closure holds (compiler/builtin.go:94, java registered without table
// arms). The scalar round trip below exercises the internal emitter directly;
// the direct text and array arms are refused by ir.TableFixedSupported and by
// the same target refusal, so no clause is proved in a target a gate runs.
func t08UnionFieldArms(t *testing.T) {
	// The internal-emitter branch for scalars: it writes and reads the selected
	// arm with non-default values through javatable.Generate, but the Java
	// target refuses the same schema, so this is not a gated target capability.
	t08RoundTrip(t, "union_field_arms", `package probe

union Cell
{
    num int32
    ratio float32
}

fixed table T
{
    item Cell
}
`, `        vals[0].item.type = probe.CellFixed.ratio;
        vals[0].item.ratio = 2.5f;`, []string{"item.type", "item.ratio"})
	// The refused clause: a text arm.
	t08Unsupported(t, "union-field-arms (text arm)", `package probe

union Cell
{
    label string(8)
    num int32
}

fixed table T
{
    item Cell
}
`)
	// The refused clause: an array arm.
	t08Unsupported(t, "union-field-arms (array arm)", `package probe

union Cell
{
    vals [4]int32
    num int32
}

fixed table T
{
    item Cell
}
`)
}

// payload-free-arms is recorded `unknown`: a void arm is refused by
// ir.TableFixedSupported, so the construct cannot be written or read.
func t08PayloadFree(t *testing.T) {
	t08Unsupported(t, "payload-free-arms", `package probe

union Cell
{
    num int32
    empty
}

fixed table T
{
    item Cell
}
`)
}

// union-arrays is recorded `unknown`: this exercises SPEC §3.4's array of a
// union directly through javatable.Generate, every element carrying its own tag
// and arm and non-default selected arms round-tripping element by element. The
// Java target refuses any union a table closure holds (compiler/builtin.go:94,
// java registered with arms=false and unionArrays=false), so the emitter branch
// below is unreachable in a target the gate runs.
func t08UnionArrays(t *testing.T) {
	t08RoundTrip(t, "union_arrays", `package probe

union Cell
{
    num int32
    ratio float32
}

fixed table T
{
    g [4]Cell
}
`, `        vals[0].g[0].type = probe.CellFixed.ratio;
        vals[0].g[0].ratio = 3.25f;
        vals[0].g[1].type = probe.CellFixed.num;
        vals[0].g[1].num = 4242;`,
		[]string{"g[0].type", "g[0].ratio", "g[1].type", "g[1].num"})
}

func TestFixedRoadmapJavaT08FloatsUnions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"float32-values/java/valid-data", t08Float32},
		{"float64-values/java/valid-data", t08Float64},
		{"compressed-floats/java/valid-data", t08Compressed},
		{"signed-fixed-point/java/valid-data", t08SignedFixed},
		{"unsigned-fixed-point/java/valid-data", t08UnsignedFixed},
		{"utf16-values/java/valid-data", t08UTF16},
		{"union-field-arms/java/valid-data", t08UnionFieldArms},
		{"payload-free-arms/java/valid-data", t08PayloadFree},
		{"union-arrays/java/valid-data", t08UnionArrays},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}
