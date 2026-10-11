package javatable

// fixedroadmap_t07 — the java leg's integer-conformance rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"):
// bool-values, signed-integers, unsigned-integers, signed-128, unsigned-128,
// ranged-integers and bits-values (the [capability] rows), plus the
// write-read-write byte-identity property (java/P2). Law: docs/SPEC-TABLES.md
// §3.4 (the record and its constant sizes), docs/FIXED-FORM-ALGORITHM.md §4.5
// (the scatter) and §4.6 (the bounds pass).
//
// A [capability] task writes the construct with non-default and boundary
// values through the leg's own generated save, and reads it back through the
// leg's own generated load, asserting the exact round trip and a clean report.
// The 128-bit rows ride the emulated pair the fixed form scatters (Int128 /
// UInt128), whose emission the emitter owes when the fixed closure reaches one.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
)

// TestFixedRoadmapJavaT07ConformanceIntegers is the card's one harness: a
// subtest per task this card proves, table-driven, t.Parallel() first.
func TestFixedRoadmapJavaT07ConformanceIntegers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"bool-values/java/valid-data", t07BoolValues},
		{"signed-integers/java/valid-data", t07SignedIntegers},
		{"unsigned-integers/java/valid-data", t07UnsignedIntegers},
		{"signed-128/java/valid-data", t07Signed128},
		{"unsigned-128/java/valid-data", t07Unsigned128},
		{"ranged-integers/java/valid-data", t07RangedIntegers},
		{"bits-values/java/valid-data", t07BitsValues},
		{"java/P2", t07P2WriteReadWrite},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t07Capability is one capability row: the schema (one fixed table), the root
// name, the probe setters that write non-default and boundary values, and the
// landed field oracle. The probe writes with the leg's own save, reads with its
// own load, and dumps the value; every landed field must equal the oracle and
// the report must be clean (n == 1, no refusal, no counter moved).
func t07Capability(t *testing.T, schema, root, setters string, want map[string]string) {
	t.Helper()
	classes := t07Build(t, schema, root, setters)
	out := t07Run(t, classes)
	r := parseProbe(t, out)
	if r.n != 1 || r.refused || r.malformed {
		t.Fatalf("the round trip did not read: %+v", r)
	}
	if r.clamped != 0 || r.widened != 0 || r.unknown != 0 || r.kindMismatch != 0 {
		t.Errorf("a clean round trip moved a counter: %+v", r)
	}
	for name, want := range want {
		if got, ok := r.value[name]; !ok {
			t.Errorf("the probe dumped no %s", name)
		} else if got != want {
			t.Errorf("%s landed %s, want the exact written %s", name, got, want)
		}
	}
}

// t07Build generates one unit's fixed form, writes the generated Java and the
// capability probe beside it, and compiles the lot with this leg's toolchain.
func t07Build(t *testing.T, schema, root, setters string) string {
	t.Helper()
	u := unitFrom(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	pkg := javaPackageOf(u)
	if pkg == "" {
		t.Fatal("the unit declares no package")
	}
	javac, _ := javaTools(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	// THE PACKET EMITTER OWNS Int128/UInt128 (java.Generate's unitNeeds128 walks
	// Tables too), so a fixed closure that reaches the emulated pair gets the
	// two support classes from the packet side, exactly as the driver
	// compiler/target_java.go hands them in — the fixed form names them but
	// does not emit them (they are one-public-class-per-file, and a second
	// emission collides).
	pkt, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatalf("packet generate: %v", err)
	}
	writeFixedJava(t, src, pkg, files)
	for _, name := range []string{"Int128.java", "UInt128.java"} {
		if data, ok := pkt[name]; ok {
			if err := os.WriteFile(filepath.Join(src, pkg, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	probe := t03SaveLoadProbe("Probe_capability", pkg, root, setters)
	if err := os.WriteFile(filepath.Join(src, "Probe_capability.java"), []byte(probe), 0o644); err != nil {
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
		t.Fatalf("javac: %v\n%s", err, out)
	}
	return classes
}

// t07Run runs one compiled capability probe and answers its combined output.
func t07Run(t *testing.T, classes string) string {
	t.Helper()
	_, javaBin := javaTools(t)
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "Probe_capability").CombinedOutput()
	if err != nil {
		t.Fatalf("java Probe_capability: %v\n%s", err, out)
	}
	return string(out)
}

// t07BoolValues: bool values (§3.4 record, bool). A bool lands `byte != 0`
// and writes 0 or 1; the two values are the boundary, and a declared default
// of true is the non-default. The setter writes a=false, b=true, both against
// their defaults.
func t07BoolValues(t *testing.T) {
	t07Capability(t, `package probe

fixed table T
{
    a bool
    b bool = true
}
`, "T", `
        vals[0].a = true;
        vals[0].b = false;`,
		map[string]string{"a": "true", "b": "false"})
}

// t07SignedIntegers: signed 8/16/32/64 (§3.4 record, int8..int64). The
// boundary is each width's minimum, which is the value a two's-complement
// store and load must not lose: MIN rides all four widths bit-exact.
func t07SignedIntegers(t *testing.T) {
	t07Capability(t, `package probe

fixed table T
{
    a int8
    b int16
    c int32
    d int64
}
`, "T", `
        vals[0].a = -128;
        vals[0].b = -32768;
        vals[0].c = -2147483648;
        vals[0].d = Long.MIN_VALUE;`,
		map[string]string{"a": "-128", "b": "-32768", "c": "-2147483648", "d": "-9223372036854775808"})
}

// t07UnsignedIntegers: unsigned 8/16/32/64 (§3.4 record, uint8..uint64).
// Java has no unsigned integer, so each width is the same-width SIGNED type
// and the top value is -1 on the value surface; the boundary is the unsigned
// maximum riding all four widths, which a store or load that sign-extended
// would drop.
func t07UnsignedIntegers(t *testing.T) {
	t07Capability(t, `package probe

fixed table T
{
    a uint8
    b uint16
    c uint32
    d uint64
}
`, "T", `
        vals[0].a = (byte) 0xFF;
        vals[0].b = (short) 0xFFFF;
        vals[0].c = -1;
        vals[0].d = -1L;`,
		map[string]string{"a": "-1", "b": "-1", "c": "-1", "d": "-1"})
}

// t07Signed128: signed 128-bit (§3.4 record, int128). The field rides sixteen
// bytes — the low half then the high — and scatters `new Int128(hi, lo)`; the
// boundary is the signed minimum and maximum, whose hi halves are the sign.
func t07Signed128(t *testing.T) {
	t07Capability(t, `package probe

fixed table T
{
    a int128 | min = -9000000000000000000, max = 0
    b int128 | min = 0, max = 9000000000000000000
}
`, "T", `
        vals[0].a = new probe.Int128(-1L, -9000000000000000000L);
        vals[0].b = new probe.Int128(0L, 9000000000000000000L);`,
		map[string]string{
			"a.hi": "-1", "a.lo": "-9000000000000000000",
			"b.hi": "0", "b.lo": "9000000000000000000",
		})
}

// t07Unsigned128: unsigned 128-bit (§3.4 record, uint128). The boundary is the
// unsigned maximum — both halves all ones — plus a non-default pattern whose
// hi and lo are distinct, so a half swapped in the store or scatter lands a
// different value.
func t07Unsigned128(t *testing.T) {
	t07Capability(t, `package probe

fixed table T
{
    a uint128
    b uint128
}
`, "T", `
        vals[0].a = new probe.UInt128(-1L, -1L);
        vals[0].b = new probe.UInt128(0x0123456789ABCDEFL, 0xFEDCBA9876543210L);`,
		map[string]string{
			"a.hi": "-1", "a.lo": "-1",
			"b.hi": "81985529216486895", "b.lo": "-81985529216486896",
		})
}

// t07RangedIntegers: ranged integer fields (§3.4 record, a ranged integer
// rides its declared storage width, the bounds do not ride). The boundary is
// each range's end, written and read back exact with no clamp.
func t07RangedIntegers(t *testing.T) {
	t07Capability(t, `package probe

fixed table T
{
    a int32 | min = -100, max = 100
    b uint16 = 10 | min = 10, max = 20
}
`, "T", `
        vals[0].a = -100;
        vals[0].b = 20;`,
		map[string]string{"a": "-100", "b": "20"})
}

// t07BitsValues: bits(N) fields (§3.4 record, bits(N) rides its declared
// storage width). The boundary is each width's top, 2^N - 1, which the
// storage byte count alone would not enforce.
func t07BitsValues(t *testing.T) {
	t07Capability(t, `package probe

fixed table T
{
    a bits(8)
    b bits(12)
    c bits(33)
}
`, "T", `
        vals[0].a = 255;
        vals[0].b = 4095;
        vals[0].c = 0x1FFFFFFFFL;`,
		map[string]string{"a": "255", "b": "4095", "c": "8589934591"})
}

// t07P2WriteReadWrite: java/P2 "write-read-write byte-identical". The
// reference's P2 property: a value written, read back into a fresh value, and
// written again is byte-identical to the first write — a reader and a writer
// that share one offset or slack mistake round-trip perfectly and are both
// wrong, so the second write is the oracle. The schema exercises the fields a
// mistake hides in: text and bytes (span vs used length), a counted array
// (live count vs slack) and a narrower float.
func t07P2WriteReadWrite(t *testing.T) {
	classes := t07BuildProbe(t, `package probe

fixed table T
{
    s string(8)
    b bytes(6)
    marks [..4]int32
    v float32
}
`, t07P2Probe)
	out := t07RunClass(t, classes, "probe.Probe_p2")
	if !strings.Contains(out, "P2-OK") {
		t.Fatalf("P2: write-read-write is not byte-identical:\n%s", out)
	}
}

// t07BuildProbe generates one unit's fixed form, writes a custom probe beside
// it, and compiles the lot.
func t07BuildProbe(t *testing.T, schema, probe string) string {
	t.Helper()
	u := unitFrom(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	pkg := javaPackageOf(u)
	if pkg == "" {
		t.Fatal("the unit declares no package")
	}
	javac, _ := javaTools(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixedJava(t, src, pkg, files)
	if err := os.WriteFile(filepath.Join(src, "Probe_p2.java"), []byte(probe), 0o644); err != nil {
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
		t.Fatalf("javac: %v\n%s", err, out)
	}
	return classes
}

// t07RunClass runs one compiled probe by class name.
func t07RunClass(t *testing.T, classes, class string) string {
	t.Helper()
	_, javaBin := javaTools(t)
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, class).CombinedOutput()
	if err != nil {
		t.Fatalf("java %s: %v\n%s", class, err, out)
	}
	return string(out)
}

// t07P2Probe is the write-read-write byte-identity probe (the reference's P2):
// write a value, read it into a fresh value, write it again, and demand the
// two writes are byte-identical.
const t07P2Probe = `package probe;

// GENERATED BY internal/codegen/javatable/fixedroadmap_t07_test.go. P2:
// write, read, write again — the second write must equal the first.
public final class Probe_p2 {
    public static void main(String[] args) {
        final TFixed.Value v = new TFixed.Value();
        v.sLength = 5;
        v.s[0] = (byte) 'h'; v.s[1] = (byte) 'e'; v.s[2] = (byte) 'l';
        v.s[3] = (byte) 'l'; v.s[4] = (byte) 'o';
        java.util.Arrays.fill(v.s, 5, 8, (byte) 0x5A);
        v.bLength = 3;
        v.b[0] = (byte) 0xAA; v.b[1] = (byte) 0xBB; v.b[2] = (byte) 0xCC;
        java.util.Arrays.fill(v.b, 3, 6, (byte) 0x5A);
        v.marksCount = 2;
        v.marks[0] = 7; v.marks[1] = -9;
        v.marks[2] = 0x5A5A5A5A; v.marks[3] = 0x5A5A5A5A;
        v.v = Float.intBitsToFloat(0x40490FDB);

        final byte[] first = new byte[TFixed.measure(1)];
        TFixed.save(new TFixed.Value[] { v }, 1, first);

        final TFixed.Value got = new TFixed.Value();
        final TableFixed.Report r = new TableFixed.Report();
        final int n = TFixed.load(new TFixed.Value[] { got }, 1, first,
                TableFixed.plan(1 << 14), new short[1 << 14], TFixed.image(), r);
        if (n != 1 || r.refused || r.malformed) {
            System.out.println("P2-FAIL load n=" + n + " refused=" + r.refused + " malformed=" + r.malformed);
            System.exit(1);
        }

        final byte[] second = new byte[TFixed.measure(1)];
        TFixed.save(new TFixed.Value[] { got }, 1, second);

        if (first.length != second.length) {
            System.out.println("P2-FAIL length " + first.length + " != " + second.length);
            System.exit(1);
        }
        for (int i = 0; i < first.length; i++) {
            if (first[i] != second[i]) {
                System.out.println("P2-FAIL byte " + i + " differs: "
                        + String.format("%02x", first[i]) + " vs " + String.format("%02x", second[i]));
                System.exit(1);
            }
        }
        System.out.println("P2-OK");
    }
}
`
