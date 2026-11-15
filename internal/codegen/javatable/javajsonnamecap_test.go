package javatable

// A SCHEMA-NAME STRING IS CAPPED AT 255 BYTES, wherever the read meets one
// (docs/SPEC-TABLES.md §16.2, §16.12): an object key naming a field, an enum
// variant name, a flags element, a union arm key and an `&node` name, in every
// object and inside a skipped value. A longer one refuses the read, an unknown
// enum name over the cap included; a map key and a string value are data and
// are never refused for their length. The vectors hold the verdicts, and this
// test reads each name-cap row through the generated Java walk.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const javaJsonNameCapSchema = `package closed

enum Kind { Alpha, Beta }

flags Perms { Read, Write }

type Buff
{
    mult float32 = 1.0
}

union Effect
{
    buff Buff
    ping
}

table Node
{
    value int32 = 0
}

table Scene
{
    head *Node
}

table Closed
{
    kind   Kind
    perms  Perms
    effect Effect
    tags   map[string]int32
    note   string(300)
}
`

// javaJsonNameCapRows is the name-cap half of the vectors' manifest: the 255
// and 256 spellings at every name site, and the two data strings a long length
// never refuses.
var javaJsonNameCapRows = []string{
	"field-key-255",
	"field-key-256",
	"skipped-key-255",
	"skipped-key-256",
	"enum-name-255",
	"enum-name-256",
	"flags-element-255",
	"flags-element-256",
	"union-arm-255",
	"union-arm-256",
	"ampersand-255",
	"ampersand-256",
	"map-key-long",
	"string-value-long",
}

const javaJsonNameCapProbe = `package closed;
public final class NameCapProbe {
    private NameCapProbe() {}
    public static void main(String[] args) throws Exception {
        for (int i = 0; i + 2 < args.length; i += 3) {
            String name = args[i];
            String root = args[i + 1];
            String file = args[i + 2];
            TableVariable.Report report = new TableVariable.Report();
            byte[] text = java.nio.file.Files.readAllBytes(java.nio.file.Paths.get(file));
            Class<?> codec = Class.forName("closed." + root + "Variable");
            Object value = codec.getMethod("fromJson", byte[].class, TableVariable.Report.class)
                    .invoke(null, text, report);
            String verdict = (value == null && (report.refused || report.damaged)) ? "refused"
                    : (value != null && !report.refused && !report.damaged) ? "read" : "partial";
            System.out.println(name + "," + verdict);
        }
    }
}
`

type javaJsonNameCapRow struct {
	root    string
	json    string
	verdict string
}

// TestJavaJsonNameCap holds the name-cap rows of
// testdata/wire/json-read-closed/: the 255-byte spelling reads, the 256-byte
// spelling refuses, at every name site, while the long map key and string
// value read.
func TestJavaJsonNameCap(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	javac, javaBin := javaToolsRequired(t)

	const dir = "../../../testdata/wire/json-read-closed"
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("the vectors' manifest: %v", err)
	}
	named := make(map[string]javaJsonNameCapRow)
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want name, root, json, verdict, description, got %d columns", dir, i+1, len(fields))
		}
		named[fields[0]] = javaJsonNameCapRow{root: fields[1], json: fields[2], verdict: fields[3]}
	}
	want := make(map[string]javaJsonNameCapRow, len(javaJsonNameCapRows))
	for _, name := range javaJsonNameCapRows {
		row, ok := named[name]
		if !ok {
			t.Fatalf("%s/MANIFEST.tsv names no row %q", dir, name)
		}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("%s: verdict %q, want read or refused", name, row.verdict)
		}
		want[name] = row
	}

	u := unitFrom(t, javaJsonNameCapSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	files["NameCapProbe.java"] = []byte(javaJsonNameCapProbe)
	root := t.TempDir()
	var sources []string
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".java") {
			sources = append(sources, p)
		}
	}
	classes := filepath.Join(root, "classes")
	if err := os.Mkdir(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	args = nil
	for i, name := range javaJsonNameCapRows {
		row := want[name]
		p := filepath.Join(root, fmt.Sprintf("row-%d.json", i))
		body, err := os.ReadFile(filepath.Join(dir, row.json))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
		args = append(args, name, row.root, p)
	}
	cmd := exec.Command(javaBin, "-ea", "-cp", classes, "closed.NameCapProbe")
	cmd.Args = append(cmd.Args, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	got := make(map[string]string, len(javaJsonNameCapRows))
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(strings.TrimSpace(line), ",")
		if len(fields) != 2 {
			t.Fatalf("probe line %q: want name,verdict", line)
		}
		got[fields[0]] = fields[1]
	}
	if len(got) != len(javaJsonNameCapRows) {
		t.Fatalf("probe answered %d of %d rows:\n%s", len(got), len(javaJsonNameCapRows), out)
	}
	for _, name := range javaJsonNameCapRows {
		row := want[name]
		if answer := got[name]; answer != row.verdict {
			t.Errorf("%s: Java verdict = %s, vector says %s", name, answer, row.verdict)
		}
	}
}
