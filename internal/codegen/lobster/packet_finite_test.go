// The finite float rule on the Lobster packet wire (SPEC §4.3, #2203;
// serialize STANDARD.md, Floating Point, "Finite values only, every one bit
// exact"), the Lobster row of make/checks/packet-finite.mk. The shapes are
// test/packet-finite/Finite.schema's (Flat, Single32, Single64) and one
// compressed float. Lobster's writers and readers place every float bit
// through the vendored serialize.lobster v2.1.1, so the rule is the runtime's:
// a NaN or an infinity TRAPS on write (serialize_abort, exit 70) and is never
// clamped, a read refuses it on the raw wire bits as non_finite_float, and
// negative zero, the subnormals and the largest finite value ride bit exact.
//
// Each failing vector prints "FAILED: <vector>: <detail>"; the controls
// delete one runtime rule at a time in the emitted copy and require exactly
// that rule's vectors to fail.
package lobster

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// finiteCompressed is the compressed float shape beside Finite.schema's.
const finiteCompressed = `package finite

type Squeezed
{
    x float32 | min = -10, max = 10, resolution = 0.01
}
`

// The non-finite and finite edge patterns, as test/packet-finite/go/main.go
// names them.
var (
	nonFinite32 = [][2]string{{"quiet-nan", "0x7fc00000"}, {"signalling-nan", "0x7f800001"}, {"negative-nan", "0xffc00000"}, {"payload-nan", "0x7fc0dead"}, {"positive-inf", "0x7f800000"}, {"negative-inf", "0xff800000"}}
	nonFinite64 = [][2]string{{"quiet-nan", "0x7ff8000000000000"}, {"signalling-nan", "0x7ff0000000000001"}, {"negative-nan", "0xfff8000000000000"}, {"payload-nan", "0x7ff800000000dead"}, {"positive-inf", "0x7ff0000000000000"}, {"negative-inf", "0xfff0000000000000"}}
	finite32    = [][2]string{{"negative-zero", "0x80000000"}, {"min-subnormal", "0x00000001"}, {"max-subnormal", "0x007fffff"}, {"negative-subnormal", "0x80000001"}, {"max-finite", "0x7f7fffff"}, {"negative-max-finite", "0xff7fffff"}}
	finite64    = [][2]string{{"negative-zero", "0x8000000000000000"}, {"min-subnormal", "0x0000000000000001"}, {"max-subnormal", "0x000fffffffffffff"}, {"negative-subnormal", "0x8000000000000001"}, {"max-finite", "0x7fefffffffffffff"}, {"negative-max-finite", "0xffefffffffffffff"}}
)

// finiteShapes are the four float positions: the shape, the width, and how
// a value of that pattern is built, written and laid out raw.
var finiteShapes = []struct {
	name  string
	wide  bool
	value string // a Lobster expression over `x`, the float, building the value
	write string
	read  string
	raw   string // a Lobster statement over `w` and `p`, the bits, writing the raw wire
}{
	{"flat-f32", false, "let v = finite.NewFlat()\nv.A = x\nv.C = true", "finite.WriteFlat", "finite.ReadFlat", "w.serialize_bits(p, 32)\nw.serialize_bits64(0, 64)\nw.serialize_bool(true)"},
	{"flat-f64", true, "let v = finite.NewFlat()\nv.B = x\nv.C = true", "finite.WriteFlat", "finite.ReadFlat", "w.serialize_bits(0, 32)\nw.serialize_bits64(p, 64)\nw.serialize_bool(true)"},
	{"single32", false, "let v = finite.NewSingle32()\nv.X = x", "finite.WriteSingle32", "finite.ReadSingle32", "w.serialize_bits(p, 32)"},
	{"single64", true, "let v = finite.NewSingle64()\nv.X = x", "finite.WriteSingle64", "finite.ReadSingle64", "w.serialize_bits64(p, 64)"},
}

func generateFinite(t *testing.T) map[string][]byte {
	t.Helper()
	src := t.TempDir()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "packet-finite", "Finite.schema"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, src, "Finite.schema", string(data))
	writeFile(t, src, "Squeezed.schema", finiteCompressed)
	files, err := Generate(unitFromDir(t, src))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// finiteProgram is the read and bit-exact half: one run, every vector.
func finiteProgram() string {
	var b strings.Builder
	b.WriteString(`import .Finite
import .Squeezed
import .serialize

def raw(f) -> string:
    let w = serialize_write_stream(ensure_size("", 64, 0))
    f(w)
    w.flush()
    return substring(w.buffer, 0, w.bytes_processed())

def failed(name:string, detail:string):
    print("FAILED: " + name + ": " + detail)

`)
	for _, sh := range finiteShapes {
		bad, good := nonFinite32, finite32
		from, to := "serialize_float32_from_bits", "serialize_float32_bits"
		field := "A"
		switch sh.name {
		case "flat-f64":
			field = "B"
		case "single32", "single64":
			field = "X"
		}
		if sh.wide {
			bad, good = nonFinite64, finite64
			from, to = "serialize_float64_from_bits", "serialize_float64_bits"
		}
		for _, p := range bad {
			name := fmt.Sprintf("%s/read/%s", sh.name, p[0])
			fmt.Fprintf(&b, "do():\n    let p = %s\n    let wire = raw() w:\n%s\n", p[1], indent(sh.raw, "        "))
			fmt.Fprintf(&b, "    let r = serialize_read_stream_padded(wire)\n    let x = 0.0\n%s\n", indent(sh.value, "    "))
			fmt.Fprintf(&b, "    if %s(r, v): failed(%q, \"read accepted a non-finite pattern\")\n", sh.read, name)
			fmt.Fprintf(&b, "    elif r.refusal_name() != \"non_finite_float\": failed(%q, \"refused as \" + r.refusal_name())\n", name)
		}
		for _, p := range good {
			name := fmt.Sprintf("%s/bitexact/%s", sh.name, p[0])
			fmt.Fprintf(&b, "do():\n    let p = %s\n    let wire = raw() w:\n%s\n", p[1], indent(sh.raw, "        "))
			fmt.Fprintf(&b, "    let x = %s(p)\n%s\n", from, indent(sh.value, "    "))
			fmt.Fprintf(&b, "    let wrote = raw() w: %s(w, v)\n", sh.write)
			fmt.Fprintf(&b, "    if wrote != wire: failed(%q, \"write is not the raw pattern\")\n", name)
			fmt.Fprintf(&b, "    let r = serialize_read_stream_padded(wire)\n    let back = %s\n", strings.Replace(strings.SplitN(sh.value, "\n", 2)[0], "let v = ", "", 1))
			fmt.Fprintf(&b, "    if not %s(r, back) or not r.serialize_end(): failed(%q, \"read refused \" + r.refusal_name())\n", sh.read, name)
			fmt.Fprintf(&b, "    elif %s(back.%s) != p: failed(%q, \"read is not bit exact\")\n", to, field, name)
		}
	}
	b.WriteString("print(\"done\")\n")
	// each vector is its own function, called in order: its names are local
	out, n := b.String(), 0
	for strings.Contains(out, "do():\n") {
		n++
		out = strings.Replace(out, "do():\n", fmt.Sprintf("def vector%d():\n", n), 1)
	}
	var calls strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&calls, "vector%d()\n", i)
	}
	return strings.Replace(out, "print(\"done\")\n", calls.String()+"print(\"done\")\n", 1)
}

func indent(s, by string) string {
	return by + strings.ReplaceAll(s, "\n", "\n"+by)
}

// finiteWriteVectors are the write-side vectors, one program run each: the
// write must trap (exit 70) naming the rule, and write nothing.
func finiteWriteVectors() (names []string, programs map[string]string) {
	programs = map[string]string{}
	add := func(name, value, write, rule string) {
		names = append(names, name)
		programs[name] = fmt.Sprintf(`import .Finite
import .Squeezed
import .serialize

%s
let w = serialize_write_stream(ensure_size("", 64, 0))
%s(w, v)
print("WROTE")
// rule: %s
`, value, write, rule)
	}
	for _, sh := range finiteShapes {
		bad, from, rule := nonFinite32, "serialize_float32_from_bits", "serialize: float value must be finite"
		if sh.wide {
			bad, from, rule = nonFinite64, "serialize_float64_from_bits", "serialize: double value must be finite"
		}
		for _, p := range bad {
			add(fmt.Sprintf("%s/write/%s", sh.name, p[0]), fmt.Sprintf("let x = %s(%s)\n%s", from, p[1], sh.value), sh.write, rule)
		}
	}
	// a compressed float's NaN and infinities are refused, never clamped into
	// the range (#2203); a finite value outside it is clamped by the rule
	for _, p := range [][2]string{{"quiet-nan", "0x7ff8000000000000"}, {"positive-inf", "0x7ff0000000000000"}, {"negative-inf", "0xfff0000000000000"}} {
		add("compressed/write/"+p[0], fmt.Sprintf("let v = finite.NewSqueezed()\nv.X = serialize_float64_from_bits(%s)", p[1]), "finite.WriteSqueezed", "serialize: compressed float value must be finite")
	}
	return names, programs
}

// runFinite runs every vector over files and returns the sorted names of the
// failing ones.
func runFinite(t *testing.T, files map[string][]byte) []string {
	t.Helper()
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.lobster", finiteProgram())
	out := string(lobsterrun.Run(t, dir, "main.lobster", nil))
	var failed []string
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if rest, ok := strings.CutPrefix(line, "FAILED: "); ok {
			name, _, _ := strings.Cut(rest, ":")
			failed = append(failed, name)
			continue
		}
		if line != "done" {
			t.Fatalf("finite program printed %q:\n%s", line, out)
		}
	}
	names, programs := finiteWriteVectors()
	for _, name := range names {
		main := strings.ReplaceAll(name, "/", "_") + ".lobster"
		writeFile(t, dir, main, programs[name])
		got, code := lobsterrun.RunExit(t, dir, main, nil)
		rule := programs[name][strings.Index(programs[name], "// rule: ")+len("// rule: ") : len(programs[name])-1]
		if code != 70 || strings.TrimSpace(string(got)) != rule {
			failed = append(failed, name)
		}
	}
	slices.Sort(failed)
	return failed
}

func TestLobsterPacketFiniteFloats(t *testing.T) {
	t.Parallel()
	if failed := runFinite(t, generateFinite(t)); len(failed) > 0 {
		t.Fatalf("finite float vectors failed: %v", failed)
	}
}

// TestLobsterPacketFiniteFloatsNegativeControls deletes one finite rule of
// the vendored runtime at a time, in the emitted copy only, and requires
// exactly that rule's vectors to fail: the read check at each width, the
// write check at each width, and the compressed float's value check.
func TestLobsterPacketFiniteFloatsNegativeControls(t *testing.T) {
	t.Parallel()
	vectors := func(prefixes ...string) []string {
		var out []string
		names, _ := finiteWriteVectors()
		var all []string
		all = append(all, names...)
		for _, sh := range finiteShapes {
			for _, p := range nonFinite32 {
				all = append(all, sh.name+"/read/"+p[0])
			}
		}
		for _, n := range all {
			for _, p := range prefixes {
				if strings.HasPrefix(n, p) && !slices.Contains(out, n) {
					out = append(out, n)
				}
			}
		}
		slices.Sort(out)
		return out
	}
	for _, tc := range []struct {
		name, file, anchor, with string
		red                      []string
	}{
		{"read32", "serialize_float.lobster", "    if not serialize_float32_bits_are_finite(bits):\n        return value, s.fail(serialize_refusal_non_finite_float)\n", "", vectors("flat-f32/read/", "single32/read/")},
		{"read64", "serialize_float.lobster", "    if not serialize_float64_bits_are_finite(bits):\n        return value, s.fail(serialize_refusal_non_finite_float)\n", "", vectors("flat-f64/read/", "single64/read/")},
		{"write32", "serialize_float.lobster", "    if not serialize_float32_bits_are_finite(pattern):\n        serialize_abort(\"serialize: float value must be finite\")\n", "    pass()\n", vectors("flat-f32/write/", "single32/write/")},
		{"write64", "serialize_float.lobster", "    if not serialize_float64_bits_are_finite(pattern):\n        serialize_abort(\"serialize: double value must be finite\")\n", "    pass()\n", vectors("flat-f64/write/", "single64/write/")},
		{"compressed", "serialize_compressed_float.lobster", "def serialize_compressed_float_abort_value():\n    serialize_abort(\"serialize: compressed float value must be finite\")\n", "def serialize_compressed_float_abort_value():\n    pass()\n", vectors("compressed/write/")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files := generateFinite(t)
			text := string(files[tc.file])
			if strings.Count(text, tc.anchor) != 1 {
				t.Fatalf("the %s anchor matched %d times in %s", tc.name, strings.Count(text, tc.anchor), tc.file)
			}
			files[tc.file] = []byte(strings.Replace(text, tc.anchor, tc.with, 1))
			if failed := runFinite(t, files); !slices.Equal(failed, tc.red) {
				t.Fatalf("deleting the %s rule failed %v, want exactly %v", tc.name, failed, tc.red)
			}
		})
	}
}
