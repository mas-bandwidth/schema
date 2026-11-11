package javatable

// messageform_announce_fileread — the announcement is an ordinary form-1 file
// (docs/SPEC-TABLES.md §3.3), so the Java `announceRead` gives the same verdict
// the Java file reader and the engine give on the same bytes for every file
// rule, and an unknown field of any kind of the closed set is skipped and
// counted. The cases are the id table's rules (§3), the body's wire-alone rules
// (§4) and the kinds an announcement may gain (§3.3).

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// announceFileUnit has one fixed root (the announcement's vocabulary) and one
// variable root (the file reader over the same case bytes).
const announceFileUnit = `package annfile

fixed table Fixed
{
    value uint32 = 0
}

table Doc
{
    title string(16)
    count uint32 = 0
}
`

// announceFileCase is one fault or unknown field placed in both an announcement
// and a form-1 file. The field's id, kind and payload are the same in both; the
// announcement also carries its real build-version and vocabulary fields.
type announceFileCase struct {
	name string
	ann  []byte
	file []byte
	// unknown is the count a successful read must report: one for an unknown
	// field of the closed set, zero for the control.
	unknown int
}

// announceFileLEB and announceFileField spell the field header the same way the
// announcement helpers do: a reference, a kind, then the kind's payload.
func announceFileLEB(v uint64) []byte { return leb(v) }

func announceFileField(ref uint64, kind byte, payload []byte) []byte {
	out := append(announceFileLEB(ref), kind)
	return append(out, payload...)
}

// announceFileBytes frames a form-1 file with the given body fields and id
// table; it is the announcement's own spelling and the file's alike (§3).
func announceFileBytes(fields []byte, ids []uint64) []byte {
	out := append([]byte{1}, fields...)
	out = append(out, 0)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
}

func announceFileCases(t *testing.T, u *ir.Unit) []announceFileCase {
	t.Helper()
	real := tablewire.Announce(u)
	bodyEnd := len(real) - 8 - 2*8
	if bodyEnd < 1 || real[0] != 1 {
		t.Fatalf("the unit's announcement is not a form-1 file")
	}
	baseFields := append([]byte(nil), real[1:bodyEnd-1]...)
	buildID := binary.LittleEndian.Uint64(real[bodyEnd:])
	vocabID := binary.LittleEndian.Uint64(real[bodyEnd+8:])
	baseIDs := []uint64{buildID, vocabID}

	const unknownID uint64 = 0x1111111111111111
	const dupID uint64 = 0x2222222222222222

	// annWith places extra ids after the announcement's own two and returns the
	// announcement whose body carries the real fields plus extraFields.
	annWith := func(extraFields []byte, extraIDs ...uint64) []byte {
		fields := append(append([]byte(nil), baseFields...), extraFields...)
		return announceFileBytes(fields, append(append([]uint64(nil), baseIDs...), extraIDs...))
	}
	// fileWith is the file reader's side: the case's field bytes at reference 1.
	fileWith := func(fields []byte, ids ...uint64) []byte {
		return announceFileBytes(fields, ids)
	}

	boolSeven := announceFileField(3, 1, []byte{7})
	textBad := announceFileField(3, 12, append(announceFileLEB(1), 0xff))
	textZero := announceFileField(3, 12, append(announceFileLEB(1), 0x00))
	tableBad := announceFileField(3, 13, append(announceFileLEB(1), 1))
	arrayBad := announceFileField(3, 14, append(announceFileLEB(1), 6))
	nan := announceFileField(3, 11, []byte{
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf8, 0x7f,
	})
	keyedEmpty := announceFileField(3, 16, []byte{2, 6, 0})
	enumNone := announceFileField(3, 30, announceFileLEB(1))
	repeated := append(append([]byte(nil), boolSeven...), boolSeven...)

	boolSevenFile := announceFileField(1, 1, []byte{7})
	textBadFile := announceFileField(1, 12, append(announceFileLEB(1), 0xff))
	textZeroFile := announceFileField(1, 12, append(announceFileLEB(1), 0x00))
	tableBadFile := announceFileField(1, 13, append(announceFileLEB(1), 1))
	arrayBadFile := announceFileField(1, 14, append(announceFileLEB(1), 6))
	nanFile := announceFileField(1, 11, []byte{
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf8, 0x7f,
	})
	keyedEmptyFile := announceFileField(1, 16, []byte{2, 6, 0})
	enumNoneFile := announceFileField(1, 30, announceFileLEB(1))
	repeatedFile := append(append([]byte(nil), boolSevenFile...), boolSevenFile...)

	// 4102 entries: the id table's maximum is 4096 (§3).
	wideAnnIDs := append([]uint64(nil), baseIDs...)
	wideFileIDs := make([]uint64, 0, 4102)
	for i := 0; i < 4100; i++ {
		wideAnnIDs = append(wideAnnIDs, 0x1000+uint64(i))
		wideFileIDs = append(wideFileIDs, 0x2000+uint64(i))
	}

	return []announceFileCase{
		{name: "duplicate id table", ann: announceFileBytes(baseFields, []uint64{buildID, vocabID, dupID, dupID}), file: fileWith(nil, dupID, dupID)},
		{name: "4102 entry id table", ann: announceFileBytes(baseFields, wideAnnIDs), file: fileWith(nil, wideFileIDs...)},
		{name: "unknown field id twice", ann: annWith(repeated, unknownID), file: fileWith(repeatedFile, unknownID)},
		{name: "unknown bool byte 7", ann: annWith(boolSeven, unknownID), file: fileWith(boolSevenFile, unknownID)},
		{name: "unknown text ill-formed", ann: annWith(textBad, unknownID), file: fileWith(textBadFile, unknownID)},
		{name: "unknown text zero byte", ann: annWith(textZero, unknownID), file: fileWith(textZeroFile, unknownID)},
		{name: "unknown table does not frame", ann: annWith(tableBad, unknownID), file: fileWith(tableBadFile, unknownID)},
		{name: "unknown array does not frame", ann: annWith(arrayBad, unknownID), file: fileWith(arrayBadFile, unknownID)},
		{name: "unknown float NaN", ann: annWith(nan, unknownID), file: fileWith(nanFile, unknownID)},
		{name: "unknown empty keyed array", ann: annWith(keyedEmpty, unknownID), file: fileWith(keyedEmptyFile, unknownID), unknown: 1},
		{name: "unknown enum None", ann: annWith(enumNone, unknownID), file: fileWith(enumNoneFile, unknownID), unknown: 1},
		{name: "control", ann: real, file: fileWith(nil)},
	}
}

// engineAnnounceVerdict is the oracle's AnnounceRead over the same bytes.
func engineAnnounceVerdict(t *testing.T, data []byte) (bool, int) {
	t.Helper()
	var v tablewire.Vocabulary
	var report tabletext.Report
	err := v.AnnounceRead(data, &report)
	return err == nil && v.Announced(), report.Unknown
}

// engineFileVerdict is the oracle's form-1 file read over the same case bytes.
func engineFileVerdict(t *testing.T, u *ir.Unit, data []byte) (bool, int) {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Doc"))
	var report tabletext.Report
	ok, err := tablewire.Decode(m, inst, data, &report)
	return ok && err == nil && !report.Refused, report.Unknown
}

// TestJavaAnnounceReadIsAFileRead holds the announcement to the file reader's
// own verdicts (docs/SPEC-TABLES.md §3, §3.3, §4). It compiles the emitted unit
// beside a driver and compares each case to the engine, asserting verdicts and
// the unknown count only.
func TestJavaAnnounceReadIsAFileRead(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)

	u := unitFrom(t, announceFileUnit)
	cases := announceFileCases(t, u)

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src", u.Package)
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		path := filepath.Join(srcDir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}

	casesDir := filepath.Join(dir, "cases")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		if err := os.WriteFile(filepath.Join(casesDir, fmt.Sprintf("ann-%d.bin", i)), c.ann, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(casesDir, fmt.Sprintf("file-%d.bin", i)), c.file, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	driver := fmt.Sprintf(`package %s;

import java.nio.file.Files;
import java.nio.file.Path;

public final class Driver {
    private Driver() {}

    public static void main(String[] args) throws Exception {
        final Path dir = Path.of(args[0]);
        for (int i = 0; i < %d; i++) {
            final TableVocabulary v = new TableVocabulary();
            v.reset(%d);
            final TableVocabulary.Report ar = new TableVocabulary.Report();
            final boolean aok = TableVocabulary.announceRead(
                    v, Files.readAllBytes(dir.resolve("ann-" + i + ".bin")), ar);
            final boolean av = aok && !ar.damaged && !ar.refused;
            final TableVariable.Report fr = new TableVariable.Report();
            final DocVariable.Value dv = DocVariable.load(
                    Files.readAllBytes(dir.resolve("file-" + i + ".bin")), fr);
            final boolean fv = dv != null && !fr.damaged && !fr.refused;
            System.out.println(i + " " + (av ? 1 : 0) + " " + (fv ? 1 : 0)
                    + " " + ar.unknown + " " + fr.unknown);
        }
    }
}
`, u.Package, len(cases), len(tablewire.MessageVocabulary(u))+1)
	if err := os.WriteFile(filepath.Join(srcDir, "Driver.java"), []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, filepath.Join(srcDir, "Driver.java"))

	compile := exec.Command(javac, append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)...)
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	run := exec.Command(javaBin, "-ea", "-cp", classes, u.Package+".Driver", casesDir)
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}

	type verdict struct {
		announce, file bool
		annUnknown     int
		fileUnknown    int
	}
	got := make([]verdict, len(cases))
	var lines []string
	for l := range strings.SplitSeq(string(out), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "Picked up JAVA_TOOL_OPTIONS") {
			continue
		}
		lines = append(lines, l)
	}
	if len(lines) != len(cases) {
		t.Fatalf("driver printed %d rows, want %d:\n%s", len(lines), len(cases), out)
	}
	for _, l := range lines {
		var i int
		var av, fv int
		var au, fu int
		if _, err := fmt.Sscanf(l, "%d %d %d %d %d", &i, &av, &fv, &au, &fu); err != nil {
			t.Fatalf("driver row %q: %v", l, err)
		}
		got[i] = verdict{announce: av != 0, file: fv != 0, annUnknown: au, fileUnknown: fu}
	}

	for i, c := range cases {
		wantAnn, wantAnnUnknown := engineAnnounceVerdict(t, c.ann)
		wantFile, wantFileUnknown := engineFileVerdict(t, u, c.file)
		g := got[i]
		if g.announce != wantAnn {
			t.Errorf("%s: Java announceRead = %v, engine AnnounceRead = %v", c.name, g.announce, wantAnn)
		}
		if g.file != wantFile {
			t.Errorf("%s: Java file reader = %v, engine file read = %v", c.name, g.file, wantFile)
		}
		if g.announce != g.file {
			t.Errorf("%s: Java announceRead = %v, Java file reader = %v on the same case", c.name, g.announce, g.file)
		}
		if wantAnn && wantFile {
			if g.annUnknown != wantAnnUnknown || g.fileUnknown != wantFileUnknown {
				t.Errorf("%s: unknown counts announce=%d/%d file=%d/%d", c.name,
					g.annUnknown, wantAnnUnknown, g.fileUnknown, wantFileUnknown)
			}
			if c.unknown != 0 && (g.annUnknown != c.unknown || g.fileUnknown != c.unknown) {
				t.Errorf("%s: unknown = %d/%d, want %d", c.name, g.annUnknown, g.fileUnknown, c.unknown)
			}
		}
	}
	if !got[len(cases)-1].announce {
		t.Fatalf("control announcement was not read")
	}
}
