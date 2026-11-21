// A blob record's kind in the variable form (docs/SPEC-TABLES.md §3.1, §6.5;
// docs/rulings.json blob-target-outside-closure-refused). The two reserved
// blob ids are types every reader can name, whatever its root reaches: a slot
// this reader declares that names a blob record of the other kind refuses the
// whole read, and so does a blob record no pointer reaches. A blob record named
// only from a field this reader does not declare is a newer writer's
// evolution: it reads, counted unknown with the field that names it.
//
// The engine (internal/tablewire) is the oracle. Every row of
// testdata/wire/pointer-target-type/blobkind/MANIFEST.tsv and the three
// closure rows of testdata/conformance/feature-witnesses/pointer-targets are
// read by the engine and by every Java read of the variable form (the plain
// load, the retaining load and the region measure), and the Java answer must
// be the engine's: the verdict, the unknown count and whether the declared
// pointer reads set. The Java writer's saves (the plain save, the retaining
// save, and the retaining save of the value read back) of each root with its
// blob null, empty and set are decoded by the engine and must read back
// accepted with nothing unknown: a writer never emits a blob record no pointer
// reaches.
package javatable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// javaBlobKindCase is one wire read at one root, with the engine's answer.
type javaBlobKindCase struct {
	name, root, field string // field: the root's declared pointer
	wire              []byte
	verdict, reason   string // the engine's verdict and, for a refusal, its reason
	unknown           int
	set               bool   // a read: the declared pointer reads set
	measure           string // the engine's region measure: "read" or "refused"
}

// javaBlobKindPaths are the Java reads of the variable form each case runs
// through.
var javaBlobKindPaths = []string{"load", "retain", "measure"}

// javaBlobKindDecoders are the engine's two reads; they must agree on every case.
var javaBlobKindDecoders = []struct {
	name string
	read func(*tabletext.Model, *tabletext.Instance, []byte, *tabletext.Report) (bool, error)
}{
	{"strict", tablewire.Decode},
	{"retaining", func(m *tabletext.Model, inst *tabletext.Instance, wire []byte, report *tabletext.Report) (bool, error) {
		retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
		return tablewire.DecodeRetain(m, inst, wire, &retain, report)
	}},
}

// javaBlobKindOracle is the engine's answer for one wire at one root.
func javaBlobKindOracle(t *testing.T, u *ir.Unit, m *tabletext.Model, c *javaBlobKindCase) {
	t.Helper()
	st := m.Lookup(c.root)
	if st == nil {
		t.Fatalf("%s: no root %s", c.name, c.root)
	}
	for i, d := range javaBlobKindDecoders {
		inst := m.New(st)
		var report tabletext.Report
		ok, err := d.read(m, inst, c.wire, &report)
		verdict, reason, unknown, set := "refused", tablewire.RefusalReason(err), 0, false
		if ok && err == nil && !report.Refused {
			verdict, reason, unknown = "read", "", report.Unknown
			fv, found := inst.FieldByKey(c.field)
			if !found {
				t.Fatalf("%s: %s has no field %s", c.name, c.root, c.field)
			}
			set = fv.Cell.Blob != nil || fv.Cell.Node != nil
		} else if reason == "" {
			t.Fatalf("%s (%s): the engine failed without a refusal: %v %+v", c.name, d.name, err, report)
		}
		if i == 0 {
			c.verdict, c.reason, c.unknown, c.set = verdict, reason, unknown, set
		} else if verdict != c.verdict || reason != c.reason || unknown != c.unknown || set != c.set {
			t.Fatalf("%s: the engine's reads disagree: %s %s %d %v against %s %s %d %v", c.name, verdict, reason, unknown, set, c.verdict, c.reason, c.unknown, c.set)
		}
	}
	_, whole, refused := tablewire.FileRegionMeasure(u, u.Tables[c.root], c.wire)
	c.measure = "refused"
	if whole && !refused {
		c.measure = "read"
	}
}

// javaBlobKindReadSource is the Java program that reads every case through
// every read path and prints one ROW line per path:
//
//	ROW <case> <path> read <unknown> <set|null>
//	ROW <case> <path> refused <reason>
//
// The wires are files in the program's working directory, named <case>.bin.
// A refusal's reason is the report's reason up to its first colon, or
// wire_damaged for a load the report marks damaged.
func javaBlobKindReadSource(pkg string, cases []javaBlobKindCase, saves string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s;\n\n", pkg)
	b.WriteString(`import java.nio.file.Files;
import java.nio.file.Path;

public final class BlobKindProbe {
    static byte[] read(String path) throws Exception {
        return Files.readAllBytes(Path.of(path));
    }

    static void write(String path, byte[] data) throws Exception {
        if (data == null) {
            throw new AssertionError("FAIL: a save refused: " + path);
        }
        Files.write(Path.of(path), data);
    }

    static void row(String name, String path, boolean ok, TableVariable.Report report, boolean set) {
        if (ok && !report.refused && !report.damaged) {
            System.out.println("ROW " + name + " " + path + " read " + report.unknown + " " + (set ? "set" : "null"));
            return;
        }
        String reason = "other";
        if (report.refused && report.reason != null) {
            int colon = report.reason.indexOf(':');
            reason = colon < 0 ? report.reason : report.reason.substring(0, colon);
        } else if (report.damaged) {
            reason = "wire_damaged";
        }
        System.out.println("ROW " + name + " " + path + " refused " + reason);
    }
`)
	roots := map[string]string{}
	for _, c := range cases {
		roots[c.root] = c.field
	}
	for _, root := range slices.Sorted(maps.Keys(roots)) {
		fmt.Fprintf(&b, `
    static void probe%[1]s(String name, String path) throws Exception {
        byte[] wire = read(path);
        TableVariable.Report report = new TableVariable.Report();
        %[1]sVariable.Value v = %[1]sVariable.load(wire, report);
        row(name, "load", v != null, report, v != null && v.%[2]s != null);
        TableRetain retain = new TableRetain(1 << 16, 1024);
        TableVariable.Report rreport = new TableVariable.Report();
        %[1]sVariable.Value rv = %[1]sRetain.loadRetain(wire, retain, rreport);
        row(name, "retain", rv != null, rreport, rv != null && rv.%[2]s != null);
        System.out.println("ROW " + name + " measure " + (%[1]sVariable.loadMeasure(wire) >= 0 ? "read" : "refused"));
    }
`, root, roots[root])
	}
	b.WriteString(saves)
	b.WriteString("\n    public static void main(String[] args) throws Exception {\n")
	for _, c := range cases {
		fmt.Fprintf(&b, "        probe%s(%q, %q);\n", c.root, c.name, c.name+".bin")
	}
	b.WriteString("        saves();\n    }\n}\n")
	return b.String()
}

// javaBlobKindSaveSource is the Java writer half: for each root, its blob null
// (0), empty (1) and set to "hello" (2), the plain save, the retaining save,
// and the retaining save of the plain save read back retained, each into
// save-<root>-<case>-<path>.bin.
func javaBlobKindSaveSource(roots map[string]string) string {
	var b strings.Builder
	for _, root := range slices.Sorted(maps.Keys(roots)) {
		field := roots[root]
		value := `which == 0 ? null : which == 1 ? "" : "hello"`
		if field != "body" {
			value = `which == 0 ? null : which == 1 ? new byte[0] : "hello".getBytes(java.nio.charset.StandardCharsets.UTF_8)`
		}
		fmt.Fprintf(&b, `
    static void save%[1]s(int which) throws Exception {
        %[1]sVariable.Value v = new %[1]sVariable.Value();
        v.%[2]s = %[3]s;
        byte[] plain = %[1]sVariable.save(v);
        write("save-%[1]s-" + which + "-plain.bin", plain);
        write("save-%[1]s-" + which + "-retain.bin", %[1]sRetain.saveRetain(v, new TableRetain(1 << 16, 1024), new TableVariable.Report()));
        TableRetain retain = new TableRetain(1 << 16, 1024);
        TableVariable.Report report = new TableVariable.Report();
        %[1]sVariable.Value back = %[1]sRetain.loadRetain(plain, retain, report);
        if (back == null) {
            throw new AssertionError("FAIL: the retaining read of the plain save refused: %[1]s " + which);
        }
        write("save-%[1]s-" + which + "-reread.bin", %[1]sRetain.saveRetain(back, retain, report));
    }
`, root, field, value)
	}
	b.WriteString("\n    static void saves() throws Exception {\n        for (int which = 0; which <= 2; which++) {\n")
	for _, root := range slices.Sorted(maps.Keys(roots)) {
		fmt.Fprintf(&b, "            save%s(which);\n", root)
	}
	b.WriteString("        }\n    }\n")
	return b.String()
}

// runJavaBlobKind generates the unit into a temporary directory, lets `plant`
// alter the generated files (a negative control), writes each case's wire
// beside them, compiles the program and runs it in that directory.
func runJavaBlobKind(t *testing.T, unitDir string, u *ir.Unit, cases []javaBlobKindCase, source string, plant func(map[string][]byte)) (dir, output string, err error) {
	t.Helper()
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	javac, javaBin := javaTools(t)
	files := generateJavaUnitFiles(t, unitDir)
	if plant != nil {
		plant(files)
	}
	dir = t.TempDir()
	srcDir := filepath.Join(dir, "src", u.Package)
	classes := filepath.Join(dir, "classes")
	for _, d := range []string{srcDir, classes} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		path := filepath.Join(srcDir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	probe := filepath.Join(srcDir, "BlobKindProbe.java")
	if err := os.WriteFile(probe, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, probe)
	for _, c := range cases {
		if err := os.WriteFile(filepath.Join(dir, c.name+".bin"), c.wire, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	compile := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	if out, err := testproc.Command(t, javac, compile...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := testproc.CommandContext(t, ctx, javaBin, "-ea", "-cp", classes, u.Package+".BlobKindProbe")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return dir, string(out), err
}

// javaBlobKindProblems names every Java answer that is not the engine's.
func javaBlobKindProblems(cases []javaBlobKindCase, output string) []string {
	got := map[string]string{}
	for line := range strings.Lines(output) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "ROW "); ok {
			name, tail, _ := strings.Cut(rest, " ")
			path, answer, _ := strings.Cut(tail, " ")
			got[name+" "+path] = answer
		}
	}
	var problems []string
	for _, c := range cases {
		for _, path := range javaBlobKindPaths {
			want := "refused " + c.reason
			switch {
			case path == "measure":
				want = c.measure
			case c.verdict == "read":
				want = "read " + strconv.Itoa(c.unknown) + map[bool]string{true: " set", false: " null"}[c.set]
			}
			answer, ok := got[c.name+" "+path]
			switch {
			case !ok:
				problems = append(problems, fmt.Sprintf("%s (%s): no answer", c.name, path))
			case path != "measure" && c.verdict == "refused" && strings.HasPrefix(answer, "refused "):
				// THE VERDICT IS WHAT CONFORMS (docs/rulings.json
				// refusal-verdict-only): the reason a leg names is its own
				// detail. The Java leg names a wrong-target pointer
				// kind_mismatch (TableVariable.judge), the engine wire_damaged.
				continue
			case answer != want:
				problems = append(problems, fmt.Sprintf("%s (%s): Java answers %q, the engine %q", c.name, path, answer, want))
			}
		}
	}
	return problems
}

// javaBlobKindWriterProblems decodes a saved wire with the engine at `root`
// and names each read that is not accepted with nothing unknown and the
// field's blob as saved (nil for null).
func javaBlobKindWriterProblems(m *tabletext.Model, root, field string, wire, want []byte) []string {
	var problems []string
	for _, d := range javaBlobKindDecoders {
		inst := m.New(m.Lookup(root))
		var report tabletext.Report
		ok, err := d.read(m, inst, wire, &report)
		if !ok || err != nil || report.Refused || report.Unknown != 0 {
			problems = append(problems, fmt.Sprintf("(%s) %v %+v", d.name, err, report))
			continue
		}
		fv, _ := inst.FieldByKey(field)
		if blob := fv.Cell.Blob; (blob == nil) != (want == nil) || (blob != nil && !bytes.Equal(blob.Data, want)) {
			problems = append(problems, fmt.Sprintf("(%s) the field reads %v, want %q", d.name, blob, want))
		}
	}
	return problems
}

// javaBlobOutsideClosureMark is the comment line the generated loads write
// above the count of a blob record whose kind the root's closure lacks.
const javaBlobOutsideClosureMark = "// A BLOB KIND THIS ROOT'S CLOSURE LACKS is counted unknown, never placed.\n"

// javaBlobKindPlantRootClosure plants the old rule in a copy of the generated
// reader: a blob kind this root's pointer closure lacks is a type the reader
// cannot name, so its record reads as no record (its type id cleared) and a
// declared slot naming it reads null instead of refusing.
func javaBlobKindPlantRootClosure(t *testing.T) func(map[string][]byte) {
	return func(files map[string][]byte) {
		planted := 0
		for name, data := range files {
			if !strings.HasSuffix(name, "Variable.java") && !strings.HasSuffix(name, "Retain.java") {
				continue
			}
			src := string(data)
			n := strings.Count(src, javaBlobOutsideClosureMark)
			if n == 0 {
				continue
			}
			files[name] = []byte(strings.ReplaceAll(src, javaBlobOutsideClosureMark, javaBlobOutsideClosureMark+"rec.typeId = 0;\n"))
			planted += n
		}
		if planted == 0 {
			t.Fatalf("plant: no generated reader counts a blob record outside its root's closure (%q)", javaBlobOutsideClosureMark)
		}
	}
}

func TestJavaPointerTargetBlobKindRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	repo := filepath.Join("..", "..", "..")
	vectors := filepath.Join(repo, "testdata", "wire", "pointer-target-type", "blobkind")
	manifest, err := os.ReadFile(filepath.Join(vectors, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	rows := javaBlobKindRows(t, string(manifest))
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	unitDir := filepath.Join(repo, rows[0].unit)
	u := unitFromDir(t, unitDir)
	m := tabletext.NewModel(u)
	fields := map[string]string{"Text": "body", "Bin": "data"}

	// THE MANIFEST, generically: the engine must answer each row's verdict and
	// unknown count, and the Java reads must answer the engine.
	var cases []javaBlobKindCase
	for _, row := range rows {
		if row.unit != rows[0].unit || fields[row.root] == "" {
			t.Fatalf("row %s reads %s at %s; this probe reads %s at Text or Bin", row.name, row.unit, row.root, rows[0].unit)
		}
		wire, err := os.ReadFile(filepath.Join(vectors, row.file))
		if err != nil {
			t.Fatalf("row %s: %v", row.name, err)
		}
		c := javaBlobKindCase{name: row.name, root: row.root, field: fields[row.root], wire: wire}
		javaBlobKindOracle(t, u, m, &c)
		if c.verdict != row.verdict || (row.verdict == "read" && row.unknown >= 0 && c.unknown != row.unknown) {
			t.Fatalf("row %s: the engine answers %s unknown %d; MANIFEST.tsv %s unknown %d", row.name, c.verdict, c.unknown, row.verdict, row.unknown)
		}
		cases = append(cases, c)
	}
	source := javaBlobKindReadSource(u.Package, cases, javaBlobKindSaveSource(fields))

	t.Run("every row answers the engine", func(t *testing.T) {
		t.Parallel()
		dir, output, err := runJavaBlobKind(t, unitDir, u, cases, source, nil)
		if err != nil {
			t.Fatalf("probe: %v\n%s", err, output)
		}
		for _, p := range javaBlobKindProblems(cases, output) {
			t.Error(p)
		}
		// THE WRITER NEVER EMITS A BLOB RECORD NO POINTER REACHES: every save
		// reads back in the engine accepted, nothing unknown, the blob as saved.
		for root, field := range fields {
			for which, want := range [][]byte{nil, {}, []byte("hello")} {
				for _, path := range []string{"plain", "retain", "reread"} {
					file := fmt.Sprintf("save-%s-%d-%s.bin", root, which, path)
					wire, err := os.ReadFile(filepath.Join(dir, file))
					if err != nil {
						t.Fatalf("%s: %v", file, err)
					}
					for _, p := range javaBlobKindWriterProblems(m, root, field, wire, want) {
						t.Errorf("%s: the Java save does not read back clean: %s", file, p)
					}
				}
			}
		}
	})
	t.Run("negative control: the writer criterion refuses an orphan and an unknown record", func(t *testing.T) {
		t.Parallel()
		for _, c := range cases {
			if c.name == "text-orphan-bytes" || c.name == "text-unknown-field-bytes" {
				if len(javaBlobKindWriterProblems(m, "Text", "body", c.wire, nil)) != len(javaBlobKindDecoders) {
					t.Errorf("%s: the writer criterion passed a wire a save must never produce", c.name)
				}
			}
		}
	})
	t.Run("negative control: the root's closure decides which blob ids it can name", func(t *testing.T) {
		t.Parallel()
		_, output, err := runJavaBlobKind(t, unitDir, u, cases, source, javaBlobKindPlantRootClosure(t))
		if err != nil {
			t.Fatalf("probe: %v\n%s", err, output)
		}
		problems := javaBlobKindProblems(cases, output)
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.HasPrefix(p, "text-names-bytes (load)") }) {
			t.Errorf("the planted root-closure rule did not go red on text-names-bytes: %q", problems)
		}
	})
	t.Run("feature witnesses: blobs and tables outside the root closure", func(t *testing.T) {
		t.Parallel()
		testJavaPointerTargetWitnessesOutsideClosure(t, repo)
	})
}

type javaBlobKindRow struct {
	name, unit, root, file, verdict string
	unknown                         int
}

// javaBlobKindRows reads MANIFEST.tsv's rows: case, unit, root, file, verdict
// and unknown (`-` where the row pins none).
func javaBlobKindRows(t *testing.T, manifest string) []javaBlobKindRow {
	t.Helper()
	var rows []javaBlobKindRow
	for i, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		row := javaBlobKindRow{name: f[0], unit: f[1], root: f[2], file: f[3], verdict: f[4], unknown: -1}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, row.verdict)
		}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("MANIFEST.tsv:%d: unknown %q: %v", i+1, f[5], err)
			}
			row.unknown = n
		}
		rows = append(rows, row)
	}
	return rows
}

// testJavaPointerTargetWitnessesOutsideClosure holds rows 8, 9 and 10 of
// testdata/conformance/feature-witnesses/pointer-targets/cases.json: a value
// the engine saves at Old and the Java reads at OnlyB, OnlyString and
// OnlyBytes, where the slot's target kind is outside the root's closure. Each
// refuses.
func testJavaPointerTargetWitnessesOutsideClosure(t *testing.T, repo string) {
	dir := filepath.Join(repo, "testdata", "conformance", "feature-witnesses", "pointer-targets")
	data, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Name, Writer, Reader string
		Input                json.RawMessage
		KindMismatch         int `json:"kind_mismatch"`
		Unknown              int
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	u := unitFromDir(t, dir)
	m := tabletext.NewModel(u)
	fields := map[string]string{"OnlyB": "left", "OnlyString": "payload", "OnlyBytes": "text"}
	want := []string{"table outside root closure", "bytes outside root closure", "string outside root closure"}
	if len(rows) < 10 {
		t.Fatalf("cases.json holds %d rows, want rows 8 to 10", len(rows))
	}
	var cases []javaBlobKindCase
	for i, row := range rows[7:10] {
		if row.Name != want[i] || fields[row.Reader] == "" {
			t.Fatalf("cases.json row %d is %q read at %s, want %q", i+8, row.Name, row.Reader, want[i])
		}
		v := m.New(m.Lookup(row.Writer))
		var r tabletext.Report
		if !m.Read(v, row.Input, &r) || !r.Silent() {
			t.Fatalf("%s: the writer's input does not read: %+v", row.Name, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatalf("%s: Encode: %v", row.Name, err)
		}
		c := javaBlobKindCase{name: strings.ReplaceAll(row.Name, " ", "-"), root: row.Reader, field: fields[row.Reader], wire: wire}
		javaBlobKindOracle(t, u, m, &c)
		if (c.verdict == "refused") != (row.KindMismatch > 0) || (c.verdict == "read" && c.unknown != row.Unknown) {
			t.Fatalf("%s: the engine answers %s unknown %d; cases.json kind_mismatch %d unknown %d", row.Name, c.verdict, c.unknown, row.KindMismatch, row.Unknown)
		}
		cases = append(cases, c)
	}
	source := javaBlobKindReadSource(u.Package, cases, "\n    static void saves() {}\n")
	_, output, err := runJavaBlobKind(t, dir, u, cases, source, nil)
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, output)
	}
	for _, p := range javaBlobKindProblems(cases, output) {
		t.Error(p)
	}
}
