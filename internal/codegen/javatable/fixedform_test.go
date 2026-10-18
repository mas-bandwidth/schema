package javatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

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

// TestFixedFormFU1FU2 is the TEXT-UNDER-AN-ARM pin (docs/SPEC-TABLES.md §3.4,
// §15; docs/FIXED-FORM-ALGORITHM.md §4.1 fix 12). FU1 writes a string(8) in
// the union's SECOND arm; FU2 appends `extra` so a read of those bytes is a
// COMPILED plan. Both reads go through FuRootFixed.load — the same one-path
// load the rest of this form uses. The compiled read has to see "hello".
// RETIRED BY §5.6, AND SKIPPED BY NAME RATHER THAN DELETED (§5.7 step 5): this
// is the Java leg's twin of the pilot's TestFixedFormArgLaneTextUnderSecondArm —
// it asserts that a DIFFERENT hash compiles a plan from the stranger's layout
// and reads it, which is the forward read "the fixed table reads backward, never
// forward" removed. Under §5.3 the FU1 file comes back `layout_newer` on an FU2
// build that never locked it. THE COVERAGE IS OWED as a LINEAGE PAIR (FU1/FU2,
// one of §5.7's seven pairs) in internal/codegen/javatable/fixedversioning_test.go,
// where the newer build is given the older layout in its lineage; the arm's
// guard and flavour lanes are watched meanwhile by
// `make tables-java-fixedform-arm-negative-control`, whose case reads a record
// through parse/compile/run directly and not through the load path.
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
	for line := range strings.SplitSeq(src, "\n") {
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
