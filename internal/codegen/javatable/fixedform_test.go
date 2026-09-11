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
	javac, err := exec.LookPath("javac")
	if err != nil {
		t.Skip("javac not on PATH")
	}
	java, err := exec.LookPath("java")
	if err != nil {
		t.Skip("java not on PATH")
	}
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
