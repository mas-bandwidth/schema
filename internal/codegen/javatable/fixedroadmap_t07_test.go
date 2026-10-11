package javatable

// fixedroadmap_t07 — the java leg's capability rows of docs/roadmap.sexp's node
// `fixed-tables`: bool-values, signed/unsigned integers at 8/16/32/64,
// ranged-integers and bits-values. Law: docs/FIXED-FORM-ALGORITHM.md §4 (the
// storage widths) and §4.6 (ranged and bits clamping), docs/SPEC-TABLES.md §3.4
// (the fixed form's scalar rows).
//
// THE TWO 128-BIT ROWS ARE NOT HERE, by name: signed-128 and unsigned-128 name
// an `int128`/`uint128`, whose generated storage is Int128/UInt128, but this
// leg's fixed emitter (internal/codegen/javatable) references those classes
// without emitting Int128.java/UInt128.java — the support pair lives in
// internal/codegen/java, outside this card's PATHS. The rows are `unknown` in
// the card's verdicts for that reason and not because the values fail to land.
//
// THE CLAUSE EACH SUBTEST PROVES is the task's own title: "valid-data write/read
// acceptance" — the leg WRITES the construct with non-default and boundary
// values through its own generated writer and READS them back, exact, through
// its own generated reader. Every probe is generated with this leg's own
// toolchain (t03SaveLoadProbe, the leg's existing capability probe) and the
// oracle is the value the Go side set, never a number restated by the probe.
//
// A task whose clause another test of this leg already holds is named by that
// test in the card's verdicts and is not repeated here.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// t07WriteAll writes every generated java file for a unit into src/pkg, so the
// support pair Int128.java / UInt128.java rides beside the table classes that
// name them (writeFixedJava drops them).
func t07WriteAll(t *testing.T, src, pkg string, files map[string][]byte) {
	t.Helper()
	out := filepath.Join(src, pkg)
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	n := 0
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n == 0 {
		t.Fatalf("%s: the generator emitted no java files", pkg)
	}
}

// t07RoundTrip is the capability probe's whole path: generate one unit, write
// the saved boundary values, run the generated writer and the generated reader,
// and answer the report and the landed fields.
func t07RoundTrip(t *testing.T, src, root, setters string) probeRun {
	t.Helper()
	javac, javaBin := javaTools(t)
	u := unitFrom(t, src)
	pkg := u.Package
	if pkg == "" {
		t.Fatal("the fixture declares no package")
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	t07WriteAll(t, srcDir, pkg, files)
	if err := os.WriteFile(filepath.Join(srcDir, "Probe_capability.java"),
		[]byte(t03SaveLoadProbe("Probe_capability", pkg, root, setters)), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgSrc, err := filepath.Glob(filepath.Join(srcDir, pkg, "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, pkgSrc...)
	args = append(args, filepath.Join(srcDir, "Probe_capability.java"))
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := runJava(t, javaBin, classes, "Probe_capability")
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	return parseProbe(t, out)
}

// t07Landed is the shared assertion: the round trip read one record with no
// refusal and no counter moved, and every field the subtest named landed the
// exact value the writer was handed.
func t07Landed(t *testing.T, what string, r probeRun, want map[string]string) {
	t.Helper()
	if r.n != 1 || r.refused || r.malformed {
		t.Fatalf("%s: the round trip did not read: %+v", what, r)
	}
	if r.clamped != 0 || r.widened != 0 || r.unknown != 0 || r.kindMismatch != 0 {
		t.Errorf("%s: a clean round trip moved a counter: %+v", what, r)
	}
	for name, w := range want {
		if got, ok := r.value[name]; !ok {
			t.Errorf("%s: the probe dumped no %s", what, name)
		} else if got != w {
			t.Errorf("%s: %s landed %s, want the exact written %s", what, name, got, w)
		}
	}
}

func TestFixedRoadmapJavaT07ConformanceIntegers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"bool-values/java/valid-data", t07Bool},
		{"signed-integers/java/valid-data", t07Signed},
		{"unsigned-integers/java/valid-data", t07Unsigned},
		{"ranged-integers/java/valid-data", t07Ranged},
		{"bits-values/java/valid-data", t07Bits},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t07Bool: "Boolean values: valid-data write/read acceptance". Both values of a
// bool are written and read back: the default false and the set true, and a
// bool ARRAY's set elements, which is the storage row §3.4 carries.
func t07Bool(t *testing.T) {
	r := t07RoundTrip(t, `package probe

fixed table B
{
    off  bool
    on   bool
    flags [3]bool
}
`, "B", `
        vals[0].on = true;
        vals[0].flags[0] = true;
        vals[0].flags[2] = true;`)
	t07Landed(t, "bool-values/java/valid-data", r, map[string]string{
		"off":   "false",
		"on":    "true",
		"flags": "list:true,false,true",
	})
}

// t07Signed: "Signed integers: 8, 16, 32 and 64 bits: valid-data write/read
// acceptance". Both ends of each signed width are written and read back exact.
func t07Signed(t *testing.T) {
	r := t07RoundTrip(t, `package probe

fixed table S
{
    i8lo  int8
    i8hi  int8
    i16lo int16
    i16hi int16
    i32lo int32
    i32hi int32
    i64lo int64
    i64hi int64
}
`, "S", `
        vals[0].i8lo = Byte.MIN_VALUE;
        vals[0].i8hi = Byte.MAX_VALUE;
        vals[0].i16lo = Short.MIN_VALUE;
        vals[0].i16hi = Short.MAX_VALUE;
        vals[0].i32lo = Integer.MIN_VALUE;
        vals[0].i32hi = Integer.MAX_VALUE;
        vals[0].i64lo = Long.MIN_VALUE;
        vals[0].i64hi = Long.MAX_VALUE;`)
	t07Landed(t, "signed-integers/java/valid-data", r, map[string]string{
		"i8lo":  "-128",
		"i8hi":  "127",
		"i16lo": "-32768",
		"i16hi": "32767",
		"i32lo": "-2147483648",
		"i32hi": "2147483647",
		"i64lo": "-9223372036854775808",
		"i64hi": "9223372036854775807",
	})
}

// t07Unsigned: "Unsigned integers: 8, 16, 32 and 64 bits: valid-data write/read
// acceptance". The unsigned top of each width is written and read back: the
// storage is the same-width SIGNED type, bit-transparent (fixedScalarType), so
// the dump shows the raw bits the wire carries.
func t07Unsigned(t *testing.T) {
	r := t07RoundTrip(t, `package probe

fixed table U
{
    u8  uint8
    u16 uint16
    u32 uint32
    u64 uint64
}
`, "U", `
        vals[0].u8  = (byte) 0xFF;          // 255
        vals[0].u16 = (short) 0xFFFF;       // 65535
        vals[0].u32 = (int) 0xFFFFFFFFL;    // 4294967295
        vals[0].u64 = 0xFFFFFFFFFFFFFFFFL;  // 18446744073709551615`)
	t07Landed(t, "unsigned-integers/java/valid-data", r, map[string]string{
		"u8":  "-1",
		"u16": "-1",
		"u32": "-1",
		"u64": "-1",
	})
}

// t07Ranged: "Ranged integer fields: valid-data write/read acceptance". A ranged
// scalar is stored at its own width with its bounds declared; both ends of the
// band are written and read back on the VALUE scale.
func t07Ranged(t *testing.T) {
	r := t07RoundTrip(t, `package probe

fixed table R
{
    lo  int32 | min = -5, max = 5
    hi  int16 | min = 0, max = 100
    mid int32 | min = -1000, max = 1000
}
`, "R", `
        vals[0].lo = -5;
        vals[0].hi = 100;
        vals[0].mid = 0;`)
	t07Landed(t, "ranged-integers/java/valid-data", r, map[string]string{
		"lo":  "-5",
		"hi":  "100",
		"mid": "0",
	})
}

// t07Bits: "bits(N) fields: valid-data write/read acceptance". A bits(N) field
// holds exactly N bits; the top of each declared width is written and read back.
func t07Bits(t *testing.T) {
	r := t07RoundTrip(t, `package probe

fixed table Bt
{
    wide  bits(12)
    narrow bits(3)
}
`, "Bt", `
        vals[0].wide = 4095;   // 2^12 - 1
        vals[0].narrow = 7;    // 2^3 - 1`)
	t07Landed(t, "bits-values/java/valid-data", r, map[string]string{
		"wide":   "4095",
		"narrow": "7",
	})
}
