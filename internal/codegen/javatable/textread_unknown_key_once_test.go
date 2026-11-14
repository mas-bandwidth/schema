package javatable

// A REPEATED KEY REFUSES THE WHOLE TEXT READ IN EVERY OBJECT OF THE TEXT
// (docs/SPEC-TABLES.md §16.2, §16.12): a table's, a nested table's, and the
// objects inside a value the walk skips. The key set holds every key of the
// object, declared or not, and compares decoded text, so an escaped spelling
// and its plain twin are one key. The Go reader is the oracle for every row.
// Verdicts only: a refusal's reason is per-language detail and never compared
// (docs/rulings.json refusal-verdict-only).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const repeatedKeyEveryObjectSchema = `package probe
table Inner {
 value int32
}
table Root {
 inner Inner
}
`

const repeatedKeyEveryObjectProbe = `package probe;
public final class RepeatedKeyProbe {
    private RepeatedKeyProbe() {}
    public static void main(String[] args) throws Exception {
        for (int i = 0; i + 1 < args.length; i += 2) {
            String name = args[i];
            String file = args[i + 1];
            TableVariable.Report report = new TableVariable.Report();
            byte[] text = java.nio.file.Files.readAllBytes(java.nio.file.Paths.get(file));
            RootVariable.Value value = RootVariable.fromJson(text, report);
            String verdict = (value == null && (report.refused || report.damaged)) ? "refused"
                    : (value != null && !report.refused && !report.damaged) ? "read" : "partial";
            System.out.println(name + "," + verdict + "," + report.unknown);
        }
    }
}
`

type repeatedKeyEveryObjectRow struct {
	name    string
	text    string
	verdict string
	unknown int
}

func TestJavaTextRepeatedKeyEveryObject(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)

	rows := []repeatedKeyEveryObjectRow{
		// A repeated key the build does not declare, at the root.
		{name: "root-unknown", text: `{"nope":1,"nope":2}`, verdict: "refused"},
		// The same key, the second spelling escaped: one decoded key.
		{name: "root-unknown-escaped", text: `{"nope":1,"\u006eope":2}`, verdict: "refused"},
		// A repeated key the build does not declare, in a nested table.
		{name: "nested-unknown", text: `{"inner":{"zz":1,"zz":2}}`, verdict: "refused"},
		{name: "nested-unknown-escaped", text: `{"inner":{"zz":1,"\u007a\u007a":2}}`, verdict: "refused"},
		// A repeated key inside the object a skipped value holds.
		{name: "skipped-object", text: `{"nope":{"a":1,"a":2}}`, verdict: "refused"},
		{name: "skipped-object-escaped", text: `{"nope":{"a":1,"\u0061":2}}`, verdict: "refused"},
		// The same, three levels down inside a skipped array of objects.
		{name: "skipped-array-deep", text: `{"nope":[{"a":{"b":{"zz":1,"zz":2}}}]}`, verdict: "refused"},
		// The empty string is a key like any other.
		{name: "empty-key", text: `{"":1,"":2}`, verdict: "refused"},
		// Controls: distinct unknown keys load, and every one is counted.
		{name: "root-distinct", text: `{"nope":1,"other":2}`, verdict: "read", unknown: 2},
		{name: "nested-distinct", text: `{"inner":{"zz":1,"yy":2}}`, verdict: "read", unknown: 2},
		{name: "skipped-distinct", text: `{"nope":{"a":1,"b":2}}`, verdict: "read", unknown: 1},
		{name: "skipped-array-distinct", text: `{"nope":[{"a":{"b":{"zz":1,"yy":2}}}]}`, verdict: "read", unknown: 1},
		{name: "empty-key-distinct", text: `{"":1,"x":2}`, verdict: "read", unknown: 2},
	}

	// The reference first: the Go reader's verdict on the same texts. Every
	// refused row must refuse there, and every control must read and count
	// exactly the unknowns this row names; otherwise the engine refutes the
	// rule and there is nothing here to hold the Java leg against.
	u := unitFrom(t, repeatedKeyEveryObjectSchema)
	model := tabletext.NewModel(u)
	for _, row := range rows {
		var report tabletext.Report
		ok := model.Read(model.New(model.Lookup("Root")), []byte(row.text), &report)
		verdict := "read"
		if !ok || report.Refused {
			verdict = "refused"
		}
		if verdict != row.verdict {
			t.Fatalf("%s: the Go reader says %s, want %s: %+v", row.name, verdict, row.verdict, report)
		}
		if row.verdict == "read" && report.Unknown != row.unknown {
			t.Fatalf("%s: the Go reader counts %d unknown, want %d: %+v", row.name, report.Unknown, row.unknown, report)
		}
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	probe := filepath.Join(dir, "RepeatedKeyProbe.java")
	if err := os.WriteFile(probe, []byte(repeatedKeyEveryObjectProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	classes := filepath.Join(dir, "classes")
	if err := os.Mkdir(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	args = append(args, probe)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	run := []string{"-ea", "-cp", classes, "probe.RepeatedKeyProbe"}
	for i, row := range rows {
		p := filepath.Join(dir, fmt.Sprintf("row-%d.json", i))
		if err := os.WriteFile(p, []byte(row.text), 0o644); err != nil {
			t.Fatal(err)
		}
		run = append(run, row.name, p)
	}
	out, err := exec.Command(javaBin, run...).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	got := map[string][2]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(strings.TrimSpace(line), ",")
		if len(fields) != 3 {
			t.Fatalf("driver line %q: want name,verdict,unknown", line)
		}
		got[fields[0]] = [2]string{fields[1], fields[2]}
	}
	if len(got) != len(rows) {
		t.Fatalf("driver answered %d of %d rows:\n%s", len(got), len(rows), out)
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			answer, ok := got[row.name]
			if !ok {
				t.Fatalf("%s: no driver answer", row.name)
			}
			if answer[0] != row.verdict {
				t.Fatalf("%s: Java verdict = %s, oracle = %s", row.name, answer[0], row.verdict)
			}
			if row.verdict == "read" && answer[1] != fmt.Sprint(row.unknown) {
				t.Fatalf("%s: Java counts %s unknown, oracle %d", row.name, answer[1], row.unknown)
			}
		})
	}
}
