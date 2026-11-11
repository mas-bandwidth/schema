package javatable

// messageform_construct — a vocabulary in its constructed state has declared no
// storage, so announceRead either reads the announcement into storage it may
// grow or refuses by name, and it never throws (docs/SPEC-TABLES.md §3.3). The
// verdict is the Go leg's on a zero TableVocabulary, and the batch that follows
// answers the same comparison. The reset(capacity) path is unchanged: a capacity
// smaller than the announcement still refuses by name.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const constructedVocabularySchema = `package vocabprobe

fixed table Root
{
    n int32
    m int32
}
`

const constructedVocabularyGoProbe = `package vocabprobe

import (
	"fmt"
	"testing"
)

func TestZeroVocabularyVerdict(t *testing.T) {
	announcement := make([]byte, AnnounceMeasure())
	if Announce(announcement) != int64(len(announcement)) {
		t.Fatal("announce")
	}
	var vocab TableVocabulary
	var announceReport TableReport
	announced := AnnounceRead(&vocab, announcement, &announceReport)

	value := Root{N: 3, M: 4}
	values := []*Root{&value}
	batch := make([]byte, RootMeasureMessages(values, nil))
	var saveReport TableReport
	if RootSaveMessages(values, batch, &saveReport) != int64(len(batch)) {
		t.Fatal("save")
	}
	out := make([]Root, 1)
	var loadReport TableReport
	n, ok := RootLoadMessages(out, &vocab, batch, &loadReport)
	batchOK := ok && n == 1
	fmt.Printf("ORACLE announce=%t batch=%t\n", announced, batchOK)
}
`

func goZeroVocabularyVerdict(t *testing.T, u *ir.Unit) (announce, batch bool) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	files, err := gotable.GenerateRuntime(u, nil, gotable.RuntimeStandalone)
	if err != nil {
		t.Fatalf("generate the Go leg: %v", err)
	}
	files["go.mod"] = []byte("module vocabprobe\n\ngo 1.26\n")
	files["zero_vocabulary_test.go"] = []byte(constructedVocabularyGoProbe)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestZeroVocabularyVerdict$", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod", "GOSUMDB=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the Go leg's zero vocabulary: %v\n%s", err, out)
	}
	var found bool
	for line := range strings.SplitSeq(string(out), "\n") {
		if !strings.HasPrefix(line, "ORACLE ") {
			continue
		}
		if _, err := fmt.Sscanf(line, "ORACLE announce=%t batch=%t", &announce, &batch); err != nil {
			t.Fatalf("the Go leg's verdict %q: %v", line, err)
		}
		found = true
	}
	if !found {
		t.Fatalf("the Go leg answered no verdict:\n%s", out)
	}
	return announce, batch
}

func runConstructedVocabularyJava(t *testing.T, u *ir.Unit, main string) string {
	t.Helper()
	javac, javaBin := javaToolsRequired(t)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src", u.Package)
	classes := filepath.Join(dir, "classes")
	for _, d := range []string{srcDir, classes} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var srcs []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(srcDir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		srcs = append(srcs, p)
	}
	driverPath := filepath.Join(srcDir, "Driver.java")
	driver := "package " + u.Package + ";\n\n" + driverPrologue + "\n" + main + "}\n"
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	srcs = append(srcs, driverPath)
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, srcs...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	run := exec.Command(javaBin, "-ea", "-cp", classes, u.Package+".Driver")
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	var lines []string
	for l := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(l, "Picked up JAVA_TOOL_OPTIONS") {
			continue
		}
		lines = append(lines, l)
	}
	return strings.Join(lines, "\n")
}

const constructedVocabularyMain = `    public static void main(String[] args) {
        final byte[] ann = new byte[TableVocabulary.announceMeasure()];
        if (TableVocabulary.announce(ann) != ann.length) { fail("announce"); }

        final TableVocabulary constructed = new TableVocabulary();
        final TableVocabulary.Report announceReport = new TableVocabulary.Report();
        final boolean announced = TableVocabulary.announceRead(constructed, ann, announceReport);

        final RootFixed.Value value = new RootFixed.Value();
        value.n = 3;
        value.m = 4;
        final RootFixed.Value[] values = { value };
        final TableVocabulary.Report saveReport = new TableVocabulary.Report();
        final int measured = RootMessage.measureMessages(values, saveReport);
        final byte[] batch = new byte[measured];
        if (RootMessage.saveMessages(values, batch, saveReport) != measured) { fail("save"); }

        final RootFixed.Value[] out = { new RootFixed.Value() };
        final TableVocabulary.Report loadReport = new TableVocabulary.Report();
        final int loaded = RootMessage.loadMessages(out, 1, constructed, batch, loadReport);
        final boolean batchOK = loaded == 1 && !loadReport.damaged && !loadReport.refused;

        final TableVocabulary reset = new TableVocabulary();
        reset.reset(4096);
        final boolean resetOK = TableVocabulary.announceRead(reset, ann, new TableVocabulary.Report());

        final TableVocabulary small = new TableVocabulary();
        small.reset(1);
        final boolean smallOK = TableVocabulary.announceRead(small, ann, new TableVocabulary.Report());

        System.out.println("constructed_announce=" + announced
                + " constructed_batch=" + batchOK
                + " reset_announce=" + resetOK
                + " small_announce=" + smallOK);
    }
`

// TestJavaVocabularyUsableAsConstructed holds §3.3's constructed-state rule on
// the Java leg: a new TableVocabulary has announced no storage, and its
// announceRead answers exactly as the Go leg's zero vocabulary does instead of
// dereferencing what the caller never declared.
func TestJavaVocabularyUsableAsConstructed(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, constructedVocabularySchema)
	wantAnnounce, wantBatch := goZeroVocabularyVerdict(t, u)
	if wantAnnounce || wantBatch {
		t.Fatalf("the Go leg's zero vocabulary does not refuse the unit's own announcement and batch: announce=%t batch=%t", wantAnnounce, wantBatch)
	}
	got := strings.TrimSpace(runConstructedVocabularyJava(t, u, constructedVocabularyMain))
	want := fmt.Sprintf("constructed_announce=%t constructed_batch=%t reset_announce=true small_announce=false", wantAnnounce, wantBatch)
	if got != want {
		t.Fatalf("constructed vocabulary: %s, want %s", got, want)
	}
}
