package javatable

// fixedroadmap_t18 — the java leg's shared byte oracle (java/W13, java/R27)
// and hostile-input (java/P3, java/P4) rows of docs/roadmap.sexp's node
// `fixed-tables`. Law: docs/FIXED-FORM-ALGORITHM.md §2 (the header: form byte
// at 0, hash at 8, layout length at 16, layout at 20) and §7 (the block-equals-
// the-reference, manifest-is-the-wire, hostile sweep and wrong-plan checks);
// docs/SPEC-TABLES.md §3.4 (the record and its constant sizes).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestFixedJavaT18ManifestAndHeader is the unit tier, pure Go: no javac, no
// java, no corpus beside a synthetic manifest written in t.TempDir(). It proves
// the manifest reader (readManifest) returns a forged row's `forged` values and
// a plain row's `values`, and names a refused row; and that a header built by
// hand (§2) parses to its hash and layout, with a one-hash-byte flip caught.
func TestFixedJavaT18ManifestAndHeader(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	manifest := "" +
		"file=hostile_cfloat_range_widen.bin row=cfloat_range_widen side=hostile root=CfloatRangeWiden records=1 " +
		"forged=r0.aim@104=1.5|0x3FC00000 " +
		"values=r0.lead=1,r0.aim=0.5|0x3F000000,r0.trail=2\n" +
		"file=plain.bin row=plain side=none root=Plain records=2 values=a=7,b=8,c=9\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.txt"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	rows := manifestRows(t, dir)
	forged := rows["hostile_cfloat_range_widen.bin"]
	if forged.Values["r0.lead"] != "1" || forged.Values["r0.aim"] != "0.5|0x3F000000" {
		t.Errorf("forged row's values = %v, want the OLD writer's values before the forge", forged.Values)
	}
	if got := forged.Forged["r0.aim@104"]; got != "1.5|0x3FC00000" {
		t.Errorf("forged row's forged r0.aim@104 = %q, want the forged value", got)
	}
	plain := rows["plain.bin"]
	if plain.Values["a"] != "7" || plain.Values["b"] != "8" || plain.Values["c"] != "9" {
		t.Errorf("plain row's values = %v, want the declared values", plain.Values)
	}
	if plain.Forged != nil && len(plain.Forged) != 0 {
		t.Errorf("plain row carries forged values %v, want none", plain.Forged)
	}

	// A refused row is named: the reader must fail loudly for a file the
	// manifest does not carry, never return an empty oracle, and must name the
	// value a section does not hold.
	if _, ok, err := manifestLookup(rows, "no_such_file.bin", "values", "x"); ok {
		t.Error("the lookup found a file the manifest does not carry")
	} else if !strings.Contains(err, "no_such_file.bin") {
		t.Errorf("the refused row was not named: %q", err)
	}
	if _, ok, err := manifestLookup(rows, "plain.bin", "values", "nothere"); ok {
		t.Error("the lookup found a value the section does not hold")
	} else if !strings.Contains(err, "nothere") {
		t.Errorf("the missing value was not named: %q", err)
	}

	// The header (§2): form byte at 0, hash at 8, layout length at 16, layout
	// at 20. Build one in Go and parse it; flip one hash byte and the parse
	// must catch it.
	hash := uint64(0x0123456789ABCDEF)
	layout := []byte{3, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0}
	good := buildFixedHeader(hash, layout)
	gotHash, gotLayout, ok := parseFixedHeader(good)
	if !ok {
		t.Fatal("the good header did not parse")
	}
	if gotHash != hash {
		t.Errorf("header hash = %#x, want %#x", gotHash, hash)
	}
	if string(gotLayout) != string(layout) {
		t.Errorf("header layout = % x, want % x", gotLayout, layout)
	}
	bad := append([]byte(nil), good...)
	bad[8] ^= 0xFF
	badHash, _, ok := parseFixedHeader(bad)
	if !ok {
		t.Fatal("the corrupted header did not parse; the form and lengths are still lawful")
	}
	if badHash == hash {
		t.Error("a header with one hash byte flipped parsed to the same hash; the corruption is not caught")
	}
}

// buildFixedHeader writes a form-3 file header per FIXED-FORM-ALGORITHM §2:
// the form byte at 0, seven zero bytes, the eight-byte hash at 8, the u32
// layout length at 16, and the layout at 20.
func buildFixedHeader(hash uint64, layout []byte) []byte {
	b := make([]byte, 20+len(layout))
	b[0] = 3
	for i := 0; i < 8; i++ {
		b[8+i] = byte(hash >> (8 * i))
	}
	lePut32(b[16:], uint32(len(layout)))
	copy(b[20:], layout)
	return b
}

// parseFixedHeader answers the header's hash and layout, or false when the form
// byte is not 3, the file is shorter than the fixed header, or the declared
// layout length runs past the file.
func parseFixedHeader(b []byte) (hash uint64, layout []byte, ok bool) {
	if len(b) < 20 || b[0] != 3 {
		return 0, nil, false
	}
	for i := 0; i < 8; i++ {
		hash |= uint64(b[8+i]) << (8 * i)
	}
	l := uint64(le32(b[16:]))
	if 20+l > uint64(len(b)) {
		return 0, nil, false
	}
	return hash, b[20 : 20+l], true
}

func lePut32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// ---------------------------------------------------------------------------
// THE FUNCTIONAL TIER (the java leg's hostile-input rows): TestFixedRoadmapJavaT18OracleHostile.
// The breadth (java/P3, java/P4) are the driven reader's own loop compiled and
// run with this leg's toolchain (docs/FIXED-FORM-ALGORITHM.md §7; docs/SPEC-TABLES.md §3.4).
// ---------------------------------------------------------------------------

// TestFixedRoadmapJavaT18OracleHostile runs the functional tier. W13 and R27
// (the shared byte oracle) are carried by the same corpus gates as the
// versioning harness: they are not yet written on this branch.
func TestFixedRoadmapJavaT18OracleHostile(t *testing.T) {
	t.Parallel()
	t.Run("java/P3", testFixedJavaT18P3)
	t.Run("java/P4", testFixedJavaT18P4)
}

// t18BuildProbe is t07BuildProbe (fixedroadmap_t07_test.go) generalized to an
// arbitrary probe class name and body.
func t18BuildProbe(t *testing.T, schema, probeClass, probeName, probeSource string) string {
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
	if err := os.WriteFile(filepath.Join(src, probeName+".java"), []byte(probeSource), 0o644); err != nil {
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

func t18RunClass(t *testing.T, classes, class string) string {
	t.Helper()
	_, javaBin := javaTools(t)
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, class).CombinedOutput()
	if err != nil {
		t.Fatalf("java %s: %v\n%s", class, err, out)
	}
	return string(out)
}

// t18RequireJava fails by name under SCHEMA_REQUIRE_CORPUS=1 when no JDK is on
// PATH, and otherwise lets javaTools skip.
func t18RequireJava(t *testing.T) {
	t.Helper()
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		if _, err := exec.LookPath("javac"); err != nil {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and no javac is on PATH (run with JAVAC/JAVA set)")
		}
		if _, err := exec.LookPath("java"); err != nil {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and no java is on PATH (run with JAVA set)")
		}
	}
}

// testFixedJavaT18P3 is java/P3: hostile bytes, sweep + sanitizer
// (docs/FIXED-FORM-ALGORITHM.md §7 item 5). An inline two-field fixed table;
// one record through the generated save; every bit of every byte flipped in
// turn and each copy loaded through the generated load. Each load answers one
// of three ways — refused by name, malformed, or values landed — and a thrown
// exception or any fourth answer fails by name; all three answers must occur.
func testFixedJavaT18P3(t *testing.T) {
	t18RequireJava(t)
	schema := `package probe

fixed table T
{
    a int32
    b int32
}
`
	classes := t18BuildProbe(t, schema, "probe.Probe_p3", "Probe_p3", t18P3Probe)
	out := t18RunClass(t, classes, "probe.Probe_p3")
	if !strings.Contains(out, "P3-OK") {
		t.Fatalf("P3: the hostile sweep did not reach all three answers:\n%s", out)
	}
	t.Logf("P3: %s", strings.TrimSpace(out))
}

// testFixedJavaT18P4 is java/P4: wrong plan goes red
// (docs/FIXED-FORM-ALGORITHM.md §7 item 4). An inline unit with fixed tables A
// (one int64) and B (two int32); B's record body is run through A's compiled
// plan via the emitted reader's own loop (TableFixed.run + AFixed.scatter) and
// must not reproduce B's record.
func testFixedJavaT18P4(t *testing.T) {
	t18RequireJava(t)
	schema := `package probe

fixed table A
{
    p int64
}

fixed table B
{
    x int32
    y int32
}
`
	classes := t18BuildProbe(t, schema, "probe.Probe_p4", "Probe_p4", t18P4Probe)
	out := t18RunClass(t, classes, "probe.Probe_p4")
	if !strings.Contains(out, "P4-OK") {
		t.Fatalf("P4: the wrong plan did not go red:\n%s", out)
	}
}

// t18P3Probe is the hostile sweep: flip every bit of every byte of a saved
// record and load each copy; the load must answer refused/malformed/landed and
// never throw or give a fourth answer.
const t18P3Probe = `package probe;

public final class Probe_p3 {
    public static void main(String[] args) {
        final TFixed.Value v = new TFixed.Value();
        v.a = 12345;
        v.b = -67890;
        final byte[] good = new byte[TFixed.measure(1)];
        TFixed.save(new TFixed.Value[] { v }, 1, good);
        int refusals = 0, malformed = 0, landed = 0;
        for (int off = 0; off < good.length; off++) {
            for (int bit = 0; bit < 8; bit++) {
                final byte[] f = good.clone();
                f[off] ^= (byte) (1 << bit);
                try {
                    final TFixed.Value back = new TFixed.Value();
                    final TableFixed.Report r = new TableFixed.Report();
                    final int n = TFixed.load(new TFixed.Value[] { back }, 1, f,
                            TableFixed.plan(1 << 14), new short[1 << 14], TFixed.image(), r);
                    if (r.refused) { refusals++; }
                    else if (r.malformed) { malformed++; }
                    else if (n >= 1) { landed++; }
                    else {
                        System.out.println("P3-FAIL flip at " + off + "/" + bit
                                + " is a fourth answer: n=" + n + " reason=" + r.reason);
                        System.exit(1);
                    }
                } catch (Throwable e) {
                    System.out.println("P3-FAIL flip at " + off + "/" + bit + " threw: " + e);
                    System.exit(1);
                }
            }
        }
        if (refusals == 0 || malformed == 0 || landed == 0) {
            System.out.println("P3-FAIL sweep saw refusals=" + refusals + " malformed=" + malformed
                    + " landed=" + landed + ", want all three answers and never a fourth");
            System.exit(1);
        }
        System.out.println("P3-OK refusals=" + refusals + " malformed=" + malformed + " landed=" + landed);
    }
}
`

// t18P4Probe runs B's record body through A's compiled plan: the emitted
// reader's own loop (TableFixed.run) fed the wrong plan. The body is B's
// header-stripped record (¶2: form at 0, hash at 8, layout length at 16,
// layout at 20, then per record an eight-byte hash and the body).
const t18P4Probe = `package probe;

public final class Probe_p4 {
    public static void main(String[] args) {
        final BFixed.Value b = new BFixed.Value();
        b.x = 111;
        b.y = 222;
        final byte[] wb = new byte[BFixed.measure(1)];
        BFixed.save(new BFixed.Value[] { b }, 1, wb);
        final int layoutLen = ((wb[16] & 0xFF) | ((wb[17] & 0xFF) << 8)
                | ((wb[18] & 0xFF) << 16) | ((wb[19] & 0xFF) << 24));
        final int bodyAt = TableFixed.fileHeaderBytes + 4 + layoutLen + 8;
        final byte[] body = java.util.Arrays.copyOfRange(wb, bodyAt, wb.length);
        final AFixed.Value a = new AFixed.Value();
        final TableFixed.Report r = new TableFixed.Report();
        final byte[] image = AFixed.image();
        final TableFixed.Entry[] plan = AFixed.identityPlan();
        TableFixed.run(plan, plan.length, new short[1 << 14], body, 0, body.length, image, r);
        AFixed.scatter(image, 0, a, r);
        if (!r.refused && a.p == b.x) {
            System.out.println("P4-FAIL this build's identity plan reproduced another schema's record: p=" + a.p);
            System.exit(1);
        }
        System.out.println("P4-OK");
    }
}
`

