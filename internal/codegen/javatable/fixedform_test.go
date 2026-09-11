package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A `bytes(N)` DESTINATION ROW IS AN ARRAY'S: dest the buffer, aux the live
// count. The TEXT row is the other way round (dest the length, aux the buffer),
// and a `bytes(N)` written under that convention hands compileEntry a count
// destination that is the buffer's first four bytes. Identity lands the field
// with one copy of the body and never reads those columns, so only the compiled
// path saw it.
func TestBytesNDstRowIsAnArray(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table Probe
{
    label string(8)
    blob  bytes(6)
    wide  wstring(3)
    marks [..4]int32
}
`)
	st := findTable(t, u, "Probe")
	w := fixedWalkRoot(st)
	row := map[string]fixedDst{}
	for i, e := range w.entries {
		row[e.note] = w.dst[i]
	}
	label, ok := row["label"]
	if !ok {
		t.Fatal("no destination row for label")
	}
	if label.dst != 0 || label.aux != 4 || label.counted != 0 || label.arg != fixedTextUtf8 {
		t.Fatalf("label dst row = %+v, want dest=length 0, aux=buffer 4, counted=0, arg=utf8", label)
	}
	blob, ok := row["blob"]
	if !ok {
		t.Fatal("no destination row for blob")
	}
	if blob.dst != 16 || blob.aux != 12 || blob.counted != 1 || blob.stride != 1 || blob.arg != fixedTextBytes {
		t.Fatalf("blob dst row = %+v, want dest=buffer 16, aux=length 12, counted=1, stride=1, arg=bytes", blob)
	}
	wide, ok := row["wide"]
	if !ok {
		t.Fatal("no destination row for wide")
	}
	if wide.dst != 22 || wide.aux != 26 || wide.counted != 0 || wide.arg != fixedTextWide {
		t.Fatalf("wide dst row = %+v, want dest=length 22, aux=buffer 26, counted=0, arg=wide", wide)
	}
	marks, ok := row["marks"]
	if !ok {
		t.Fatal("no destination row for marks")
	}
	if marks.dst != 36 || marks.aux != 32 || marks.counted != 1 || marks.stride != 4 {
		t.Fatalf("marks dst row = %+v, want dest=buffer 36, aux=count 32, counted=1, stride=4", marks)
	}
}

// THE PREFILL IS THE BYTES THE PLAN DOES NOT WRITE. Identity's hole list is
// empty; the load still runs the hole loop rather than skipping the prefill.
func TestFixedLoadPrefillsHolesOnly(t *testing.T) {
	files, err := Generate(unitFrom(t, `package probe

fixed table ByRoot
{
    blob bytes(6)
    marks [..4]int32
}
`))
	if err != nil {
		t.Fatal(err)
	}
	load := string(files["ByRootFixed.java"])
	if !strings.Contains(load, "TableFixed.holes(") {
		t.Fatal("load does not prefill holes")
	}
	if strings.Contains(load, "System.arraycopy(defaults, 0, image, 0, bodyBytes)") {
		t.Fatal("load still prefills the whole image")
	}
	if !strings.Contains(load, "for (int h = 0; h < holeN; h++)") {
		t.Fatal("load skips the hole loop; identity empty is the list, not a skip")
	}
	runtime := string(files["TableFixed.java"])
	if !strings.Contains(runtime, "public static int holes(") {
		t.Fatal("TableFixed has no holes")
	}
}

func TestWriteAndScatterWalkLiveCount(t *testing.T) {
	files, err := Generate(unitFrom(t, `package probe

fixed table ByRoot
{
    blob bytes(6)
    marks [..4]int32
}
`))
	if err != nil {
		t.Fatal(err)
	}
	body := string(files["ByRootFixed.java"])
	if !strings.Contains(body, "for (int i = 0; i < v.marksCount; i++)") {
		t.Fatal("scatter must walk Count, not the bound")
	}
	if !strings.Contains(body, "System.arraycopy(v.blob, 0, b, at +") {
		t.Fatal("bytes(N) write must copy Length units onto the template")
	}
}

// Pin identity==compiled on a sibling ByRoot `blob bytes(N)`, and the live-count
// stain: count=1, slack 0xFF, after load object slack is zeros.
func TestIdentityCompiledBytesN(t *testing.T) {
	javac, java := javaTools(t)
	files, err := Generate(unitFrom(t, `package probe

fixed table ByRoot
{
    blob bytes(6)
    marks [..4]int32
}
`))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(filepath.Join(src, "probe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if name != "TableFixed.java" && name != "ByRootFixed.java" {
			continue
		}
		if err := os.WriteFile(filepath.Join(src, "probe", name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "probe", "Driver.java"), []byte(identityCompiledDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(javac, "--release", "17", "-d", classes,
		filepath.Join(src, "probe", "TableFixed.java"),
		filepath.Join(src, "probe", "ByRootFixed.java"),
		filepath.Join(src, "probe", "Driver.java"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	run := exec.Command(java, "-ea", "-cp", classes, "probe.Driver")
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("driver: %s", out)
	}
}

const identityCompiledDriver = `package probe;

public final class Driver {
    private Driver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    public static void main(String[] args) {
        final ByRootFixed.Value v = new ByRootFixed.Value();
        v.blobLength = 2;
        v.blob[0] = (byte) 0xAA;
        v.blob[1] = (byte) 0xBB;
        java.util.Arrays.fill(v.blob, 2, 6, (byte) 0xFF);
        v.marksCount = 1;
        v.marks[0] = 7;
        v.marks[1] = 0xFF;
        v.marks[2] = 0xFF;
        v.marks[3] = 0xFF;

        final byte[] buf = new byte[ByRootFixed.measure(1)];
        if (ByRootFixed.save(new ByRootFixed.Value[] { v }, 1, buf) != buf.length) {
            fail("save");
        }
        final int rec = ByRootFixed.headerBytes + 8;
        for (int i = 2; i < 6; i++) {
            if (buf[rec + 4 + i] != 0) { fail("write blob slack is not template zeros"); }
        }
        // counted-array WRITE on this SHA still walks the bound, matching the
        // C++ oracle; scatter walks Count. After load, slack is zeros.

        final ByRootFixed.Value got = new ByRootFixed.Value();
        java.util.Arrays.fill(got.blob, (byte) 0xFF);
        java.util.Arrays.fill(got.marks, 0xFF);
        final TableFixed.Report r = new TableFixed.Report();
        final int n = ByRootFixed.load(new ByRootFixed.Value[] { got }, 1, buf,
                TableFixed.plan(256), new short[256], ByRootFixed.image(), r);
        if (n != 1 || r.refused || r.malformed) { fail("identity load"); }
        if (got.blobLength != 2 || got.blob[0] != (byte) 0xAA || got.blob[1] != (byte) 0xBB) {
            fail("identity blob live");
        }
        for (int i = 2; i < 6; i++) {
            if (got.blob[i] != 0) { fail("identity blob slack"); }
        }
        if (got.marksCount != 1 || got.marks[0] != 7) { fail("identity marks live"); }
        for (int i = 1; i < 4; i++) {
            if (got.marks[i] != 0) { fail("identity marks slack"); }
        }

        final byte[] cover = new byte[ByRootFixed.bodyBytes];
        final int[] hole = new int[Math.max(2, ByRootFixed.bodyBytes * 2)];
        if (TableFixed.holes(ByRootFixed.identityPlan(), 1, cover, hole) != 0) {
            fail("identity holes must be empty");
        }

        final TableFixed.Layout mine = TableFixed.parse(ByRootFixed.layout, 0, ByRootFixed.layout.length, r);
        if (mine == null) { fail("own layout"); }
        final TableFixed.Entry[] plan = TableFixed.plan(256);
        final short[] remap = new short[256];
        final int made = TableFixed.compile(mine, mine, ByRootFixed.dest, plan, remap, r);
        if (made <= 0) { fail("compile from own layout"); }
        boolean textBlob = false;
        boolean arrayCount = false;
        for (int i = 0; i < made; i++) {
            if (plan[i].op == TableFixed.opText && plan[i].dst == 0 && plan[i].aux == 4) {
                textBlob = true;
            }
            if (plan[i].op == TableFixed.opCount && plan[i].dst == 0) { arrayCount = true; }
        }
        if (textBlob) { fail("compiled dest-row is TEXT convention"); }
        if (!arrayCount) { fail("compiled dest-row is not ARRAY (count dest is the length)"); }

        final byte[] image = ByRootFixed.image();
        java.util.Arrays.fill(image, (byte) 0x7F);
        final int holeN = TableFixed.holes(plan, made, cover, hole);
        for (int h = 0; h < holeN; h++) {
            System.arraycopy(ByRootFixed.defaults, hole[h * 2], image, hole[h * 2], hole[h * 2 + 1]);
        }
        TableFixed.run(plan, made, remap, buf, rec, ByRootFixed.bodyBytes, image, r);
        final ByRootFixed.Value compiled = new ByRootFixed.Value();
        java.util.Arrays.fill(compiled.blob, (byte) 0xFF);
        java.util.Arrays.fill(compiled.marks, 0xFF);
        ByRootFixed.scatter(image, 0, compiled, r);
        if (compiled.blobLength != got.blobLength) { fail("compiled blob length"); }
        for (int i = 0; i < 6; i++) {
            if (compiled.blob[i] != got.blob[i]) { fail("compiled-from-own-layout blob matches identity"); }
        }
        if (compiled.marksCount != got.marksCount) { fail("compiled marks count"); }
        for (int i = 0; i < 4; i++) {
            if (compiled.marks[i] != got.marks[i]) { fail("compiled-from-own-layout marks match identity"); }
        }
        System.out.println("ok");
    }
}
`

func findTable(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if st.Name == name {
				return st
			}
		}
	}
	t.Fatalf("table %s not found", name)
	return nil
}

func unitFrom(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

func generate(t *testing.T, src string) map[string][]byte {
	t.Helper()
	out, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return out
}

func methodBody(src, sig string) string {
	i := strings.Index(src, sig)
	if i < 0 {
		return ""
	}
	body := src[i:]
	if j := strings.Index(body[1:], "\n    public "); j >= 0 {
		body = body[:j+1]
	}
	return body
}

// A COUNTED ARRAY'S SCATTER WALKS THE LIVE COUNT, never the bound. Identity
// and compiled share this scatter; clamp and ordinal counters live in it, so
// a loop over ArrayBound would count slack. Write still stores MAX (Dart #834).
func TestScatterCountedArrayWalksLiveCount(t *testing.T) {
	files := generate(t, `package probe

enum Grade
{
    Bronze
    Silver
    Gold
}

fixed table LiveRoot
{
    grades [1..4]Grade
}
`)
	src := string(files["LiveRootFixed.java"])
	if src == "" {
		t.Fatal("no LiveRootFixed.java in the generated unit")
	}
	scatter := methodBody(src, "public static void scatter(")
	if scatter == "" {
		t.Fatal("no scatter in LiveRootFixed.java")
	}
	if !strings.Contains(scatter, "i < v.gradesCount") {
		t.Fatalf("counted-array scatter must walk the live count, not the bound:\n%s", scatter)
	}
	if strings.Contains(scatter, "i < 4") {
		t.Fatalf("counted-array scatter still walks the declared bound:\n%s", scatter)
	}
	write := methodBody(src, "public static void writeBody(")
	if write == "" {
		t.Fatal("no writeBody in LiveRootFixed.java")
	}
	if !strings.Contains(write, "i < v.gradesCount") {
		t.Fatalf("counted-array write must walk the live count, then zero slack:\n%s", write)
	}
	if strings.Contains(write, "i < 4") {
		t.Fatalf("counted-array write still walks the declared bound:\n%s", write)
	}
	if !strings.Contains(write, "Arrays.fill") {
		t.Fatalf("counted-array write must zero slack past the live count:\n%s", write)
	}
}

// THE GUARD IS COMPARED AT ArgW BYTES, NEVER AS A PREFIX. A one-byte compare
// fires arm 1 on a foreign 0x0101. Flavour stays in Meta. Identity is still
// one COPY of the body — ArgW on that entry is 1, which is the width a tag
// had when this field did not exist. Hash chooses the plan and nothing else.
func TestFixedGuardComparedAtArgW(t *testing.T) {
	files := generate(t, `package probe

fixed table Root { n int32 }
`)
	src := string(files["TableFixed.java"])
	if src == "" {
		t.Fatal("no TableFixed.java in the generated unit")
	}
	if !strings.Contains(src, "public static long tagAt(") {
		t.Error("the runtime never names tagAt")
	}
	if !strings.Contains(src, "public byte argw = 1;") {
		t.Error("the plan entry omits ArgW")
	}
	if strings.Contains(src, "(src[srcAt + p.guard] & 0xFF) != (p.arg & 0xFF)") {
		t.Error("the run loop still compares the union guard as one byte")
	}
	if !strings.Contains(src, "tagAt(src, srcAt + p.guard, p.argw & 0xFF) != (p.arg & 0xFFL)") {
		t.Error("the run loop does not compare the guard at ArgW")
	}
	if strings.Contains(src, "if (identity)") {
		t.Error("the load grew a second reader; hash chooses the plan and nothing else")
	}
	if !strings.Contains(src, "c.argw = (theirTag >= 1 && theirTag <= 8) ? (byte) theirTag : (byte) 1;") {
		t.Error("the compiler does not stamp ArgW from the writer's tag width")
	}
	load := string(files["RootFixed.java"])
	if !strings.Contains(load, "p[0].argw = 1;") {
		t.Error("the identity plan omitted ArgW (1, the width a tag had when this field did not exist)")
	}
	if !strings.Contains(src, "e.meta = meta;") {
		t.Error("flavour left the Meta lane")
	}
	if strings.Contains(src, "opFU1") || strings.Contains(src, "opFU2") {
		t.Error("this leg took FU1/FU2; ArgW only")
	}
}

// Control: 0x0101 does not run arm 1; 0x0001 does. Four-byte twin.
func TestFixedGuardComparedAtArgWRun(t *testing.T) {
	javac, java := javaTools(t)
	files := generate(t, `package probe

fixed table Root { n int32 }
`)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(filepath.Join(src, "probe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "probe", "TableFixed.java"), files["TableFixed.java"], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "probe", "Driver.java"), []byte(argwGuardDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(javac, "--release", "17", "-d", classes,
		filepath.Join(src, "probe", "TableFixed.java"),
		filepath.Join(src, "probe", "Driver.java"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	run := exec.Command(java, "-ea", "-cp", classes, "probe.Driver")
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("driver: %s", out)
	}
}

// TestFixedFormFU1FU2 is the TEXT-UNDER-AN-ARM pin (docs/SPEC-TABLES.md §3.4,
// §15; docs/FIXED-FORM-ALGORITHM.md §4.1 fix 12). FU1 writes a string(8) in
// the union's SECOND arm; FU2 appends `extra` so a read of those bytes is a
// COMPILED plan. Both reads go through FuRootFixed.load — the same one-path
// load the rest of this form uses. The compiled read has to see "hello".
func TestFixedFormFU1FU2(t *testing.T) {
	fu1Files, err := Generate(schemaUnit(t, "FU1.schema"))
	if err != nil {
		t.Fatal(err)
	}
	fu2Files, err := Generate(schemaUnit(t, "FU2.schema"))
	if err != nil {
		t.Fatal(err)
	}
	fu1Load := string(fu1Files["FuRootFixed.java"])
	fu2Load := string(fu2Files["FuRootFixed.java"])
	if fu1Load == "" || fu2Load == "" {
		t.Fatal("FU1/FU2 did not emit FuRootFixed.java")
	}
	assertOnePathLoad(t, fu1Load, "FU1")
	assertOnePathLoad(t, fu2Load, "FU2")
	h1 := fixedHashLine(fu1Load)
	h2 := fixedHashLine(fu2Load)
	if h1 == "" || h2 == "" {
		t.Fatal("FU1/FU2 did not emit a layout hash")
	}
	if h1 == h2 {
		t.Fatal("FU2 extra did not change the layout hash; compiled path is not a compile trigger")
	}

	javac, javaBin := javaTools(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixedJava(t, src, "tblfu1", fu1Files)
	writeFixedJava(t, src, "tblfu2", fu2Files)
	if err := os.WriteFile(filepath.Join(src, "Driver.java"), []byte(fu1fu2Driver), 0o644); err != nil {
		t.Fatal(err)
	}
	fu1src, err := filepath.Glob(filepath.Join(src, "tblfu1", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	fu2src, err := filepath.Glob(filepath.Join(src, "tblfu2", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, fu1src...)
	args = append(args, fu2src...)
	args = append(args, filepath.Join(src, "Driver.java"))
	cmd := exec.Command(javac, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	run := exec.Command(javaBin, "-ea", "-cp", classes, "Driver")
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("driver: %s", out)
	}
}

func schemaUnit(t *testing.T, name string) *ir.Unit {
	t.Helper()
	path := filepath.Join("..", "..", "..", "test", "tables", name)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f, perrs := parser.Parse(name, src)
	if len(perrs) > 0 {
		t.Fatalf("parse %s: %v", name, perrs[0])
	}
	base := strings.TrimSuffix(name, ".schema")
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: name, Name: name, Base: base, Bytes: src, AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", name, cerrs[0])
	}
	return u
}

func assertOnePathLoad(t *testing.T, src, who string) {
	t.Helper()
	if !strings.Contains(src, "public static int load(") {
		t.Fatalf("%s did not emit load", who)
	}
	if strings.Contains(src, "if (identity)") {
		t.Errorf("%s: identity flag still forks the record loop", who)
	}
	if !strings.Contains(src, "TableFixed.holes(") || !strings.Contains(src, "TableFixed.run(") {
		t.Errorf("%s load is not the one-path load", who)
	}
	if !strings.Contains(src, "for (int h = 0; h < holeN; h++)") {
		t.Errorf("%s skips the hole loop; identity empty is the list, not a skip", who)
	}
	if strings.Contains(src, "System.arraycopy(defaults, 0, image, 0, bodyBytes)") {
		t.Errorf("%s still prefills the whole image", who)
	}
}

func fixedHashLine(src string) string {
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, "public static final long hash =") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func writeFixedJava(t *testing.T, dir, pkg string, files map[string][]byte) {
	t.Helper()
	out := filepath.Join(dir, pkg)
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	n := 0
	for name, data := range files {
		if name != "TableFixed.java" && !strings.HasSuffix(name, "Fixed.java") {
			continue
		}
		if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n == 0 {
		t.Fatalf("%s: no fixed-form files", pkg)
	}
}

func javaTools(t *testing.T) (javac, javaBin string) {
	t.Helper()
	try := func(jc, j string) bool {
		if jc == "" || j == "" {
			return false
		}
		if st, err := os.Stat(jc); err != nil || st.IsDir() {
			return false
		}
		if st, err := os.Stat(j); err != nil || st.IsDir() {
			return false
		}
		if err := exec.Command(jc, "-version").Run(); err != nil {
			return false
		}
		return true
	}
	if try(os.Getenv("JAVAC"), os.Getenv("JAVA")) {
		return os.Getenv("JAVAC"), os.Getenv("JAVA")
	}
	if p, err := exec.LookPath("javac"); err == nil {
		j, jerr := exec.LookPath("java")
		if jerr == nil && try(p, j) {
			return p, j
		}
	}
	homes := []string{
		"/Users/glenn/toolchains/schema-dist/jdk-21.0.12.1/Contents/Home",
	}
	if wd, err := os.Getwd(); err == nil {
		homes = append(homes, filepath.Join(wd, "..", "..", "..", "dist", "jdk-21.0.12.1", "Contents", "Home"))
	}
	for _, home := range homes {
		jc := filepath.Join(home, "bin", "javac")
		j := filepath.Join(home, "bin", "java")
		if try(jc, j) {
			return jc, j
		}
	}
	t.Skip("no working javac (set JAVAC/JAVA or install a JDK)")
	return "", ""
}

const argwGuardDriver = `package probe;

public final class Driver {
    private Driver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    static byte run(byte argw, int srcOff, byte[] src) {
        TableFixed.Entry[] plan = TableFixed.plan(1);
        plan[0].src = srcOff;
        plan[0].dst = 0;
        plan[0].size = 1;
        plan[0].guard = 0;
        plan[0].op = TableFixed.opCopy;
        plan[0].arg = 1;
        plan[0].argw = argw;
        byte[] image = new byte[1];
        TableFixed.Report r = new TableFixed.Report();
        TableFixed.run(plan, 1, new short[1], src, 0, src.length, image, r);
        if (r.malformed || r.refused) { fail("run malformed argw=" + argw); }
        return image[0];
    }

    public static void main(String[] args) {
        if (TableFixed.tagAt(new byte[] { 0x01, 0x01 }, 0, 2) != 0x0101L) {
            fail("tagAt 0x0101");
        }
        if (TableFixed.tagAt(new byte[] { 0x01, 0x00 }, 0, 2) != 0x0001L) {
            fail("tagAt 0x0001");
        }
        if (TableFixed.tagAt(new byte[] { 0x01, 0x01, 0x00, 0x00 }, 0, 4) != 0x00000101L) {
            fail("tagAt four-byte 0x00000101");
        }
        if (TableFixed.tagAt(new byte[] { 0x01, 0x00, 0x00, 0x00 }, 0, 4) != 0x00000001L) {
            fail("tagAt four-byte 0x00000001");
        }
        if (TableFixed.tagAt(new byte[] { 0x01, 0x01 }, 0, 0) != 0x01L) {
            fail("tagAt ArgW 0 means 1");
        }
        if (run((byte) 2, 2, new byte[] { 0x01, 0x01, (byte) 0xAA }) != 0) {
            fail("0x0101 does not run arm 1");
        }
        if (run((byte) 2, 2, new byte[] { 0x01, 0x00, (byte) 0xAA }) != (byte) 0xAA) {
            fail("0x0001 does run arm 1");
        }
        if (run((byte) 0, 2, new byte[] { 0x01, 0x01, (byte) 0xAA }) != (byte) 0xAA) {
            fail("ArgW 0 means 1, so a 0x0101 prefix matches arm 1");
        }
        if (run((byte) 4, 4, new byte[] { 0x01, 0x01, 0x00, 0x00, (byte) 0xBB }) != 0) {
            fail("four-byte 0x00000101 does not run arm 1");
        }
        if (run((byte) 4, 4, new byte[] { 0x01, 0x00, 0x00, 0x00, (byte) 0xBB }) != (byte) 0xBB) {
            fail("four-byte 0x00000001 does run arm 1");
        }
        if (TableFixed.tagAt(new byte[] { 1, 0, 0, 0, 0, 0, 0, 2 }, 0, 9) != 0x0200000000000001L) {
            fail("ArgW >8 clamps to 8");
        }
        System.out.println("ok");
    }
}
`

const fu1fu2Driver = `public final class Driver {
    private Driver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    static void setText(byte[] buf, String s) {
        final byte[] raw = s.getBytes(java.nio.charset.StandardCharsets.UTF_8);
        System.arraycopy(raw, 0, buf, 0, raw.length);
    }

    static String textOf(byte[] buf, int length) {
        return new String(buf, 0, length, java.nio.charset.StandardCharsets.UTF_8);
    }

    public static void main(String[] args) {
        if (tblfu1.FuRootFixed.hash == tblfu2.FuRootFixed.hash) {
            fail("FU2 extra did not change the layout hash");
        }

        final byte[] cover = new byte[tblfu1.FuRootFixed.bodyBytes];
        final int[] hole = new int[Math.max(2, tblfu1.FuRootFixed.bodyBytes * 2)];
        if (tblfu1.TableFixed.holes(tblfu1.FuRootFixed.identityPlan(), 1, cover, hole) != 0) {
            fail("identity holes must be empty; empty is the skip, not a second path");
        }

        final tblfu1.FuRootFixed.Value labelled = new tblfu1.FuRootFixed.Value();
        labelled.flag = true;
        labelled.notePresent = true;
        labelled.note = 44;
        labelled.pick.type = tblfu1.PickFixed.labelled;
        labelled.pick.labelled.lead = 101;
        setText(labelled.pick.labelled.label, "hello");
        labelled.pick.labelled.labelLength = 5;
        labelled.pick.labelled.trail = 202;
        labelled.tail = 11;

        final tblfu1.FuRootFixed.Value plain = new tblfu1.FuRootFixed.Value();
        plain.pick.type = tblfu1.PickFixed.plain;
        plain.pick.plain.n = 303;
        plain.tail = 12;

        final byte[] file = new byte[tblfu1.FuRootFixed.measure(2)];
        if (tblfu1.FuRootFixed.save(new tblfu1.FuRootFixed.Value[] { labelled, plain }, 2, file) != file.length) {
            fail("FU1 save");
        }

        final tblfu1.FuRootFixed.Value[] mine = {
            new tblfu1.FuRootFixed.Value(), new tblfu1.FuRootFixed.Value()
        };
        final tblfu1.TableFixed.Report r = new tblfu1.TableFixed.Report();
        final int n = tblfu1.FuRootFixed.load(mine, 2, file, tblfu1.TableFixed.plan(1024),
                new short[1024], tblfu1.FuRootFixed.image(), r);
        if (n != 2) { fail("identity n=" + n + " refused=" + r.refused + " malformed=" + r.malformed); }
        if (mine[0].pick.type != tblfu1.PickFixed.labelled) { fail("identity arm"); }
        if (mine[0].pick.labelled.lead != 101) { fail("identity lead"); }
        if (mine[0].pick.labelled.labelLength != 5
                || !textOf(mine[0].pick.labelled.label, mine[0].pick.labelled.labelLength).equals("hello")) {
            fail("identity text");
        }
        if (mine[0].pick.labelled.trail != 202) { fail("identity trail"); }
        if (!mine[0].flag || !mine[0].notePresent || mine[0].note != 44 || mine[0].tail != 11) {
            fail("identity labelled rest");
        }
        if (mine[1].pick.type != tblfu1.PickFixed.plain || mine[1].pick.plain.n != 303 || mine[1].tail != 12) {
            fail("identity plain");
        }
        if (r.refused || r.malformed || r.clamped != 0) { fail("identity report"); }

        final tblfu2.FuRootFixed.Value[] theirs = {
            new tblfu2.FuRootFixed.Value(), new tblfu2.FuRootFixed.Value()
        };
        final tblfu2.TableFixed.Report r2 = new tblfu2.TableFixed.Report();
        final int n2 = tblfu2.FuRootFixed.load(theirs, 2, file, tblfu2.TableFixed.plan(1024),
                new short[1024], tblfu2.FuRootFixed.image(), r2);
        if (n2 != 2) { fail("compiled n=" + n2 + " refused=" + r2.refused + " malformed=" + r2.malformed + " reason=" + r2.reason); }
        if (theirs[0].pick.type != tblfu2.PickFixed.labelled) { fail("compiled arm"); }
        if (theirs[0].pick.labelled.lead != 101) { fail("compiled lead"); }
        final String got = textOf(theirs[0].pick.labelled.label, theirs[0].pick.labelled.labelLength);
        if (theirs[0].pick.labelled.labelLength != 5 || !got.equals("hello")) {
            fail("ARG LANE BITES: compiled read of string under union arm 2 landed '" + got
                    + "' (len " + theirs[0].pick.labelled.labelLength + ")");
        }
        if (theirs[0].pick.labelled.trail != 202) { fail("compiled trail"); }
        if (!theirs[0].flag || !theirs[0].notePresent || theirs[0].note != 44 || theirs[0].tail != 11) {
            fail("compiled labelled rest");
        }
        if (theirs[0].extra != 11) { fail("compiled extra default"); }
        if (theirs[1].pick.type != tblfu2.PickFixed.plain || theirs[1].pick.plain.n != 303 || theirs[1].tail != 12) {
            fail("compiled plain");
        }
        if (theirs[1].extra != 11) { fail("compiled extra default on plain"); }
        if (r2.refused || r2.malformed || r2.clamped != 0) { fail("compiled report"); }
        System.out.println("ok");
    }
}
`
