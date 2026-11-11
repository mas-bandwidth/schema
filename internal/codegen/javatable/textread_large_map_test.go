package javatable

// A TEXT MAP'S REPEATED-KEY TEST IS A HASH LOOKUP (docs/SPEC-TABLES.md §16.2):
// the read of a large map costs time linear in its key count, so a text of
// 80,000 distinct keys loads, its entry count is the number of keys, and a
// text with one key repeated at the end refuses. The Go reader is the oracle
// for every verdict. The cost bound is the package's go test -timeout.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const textReadLargeMapSchema = `package probe
table Root {
 ids map[uint64]int32
 names map[string(16)]int32
}
`

// textReadLargeMapProbe reads one row per argument triple (name, field, file)
// through the generated fromJson and prints name,verdict,count: refused when
// no value comes back and the report refuses or says damaged, read when a
// value comes back with neither, with the named map's entry count.
const textReadLargeMapProbe = `import java.nio.file.Files;
import java.nio.file.Paths;

public final class LargeMapProbe {
    private LargeMapProbe() {}

    public static void main(String[] args) throws Exception {
        for (int i = 0; i + 2 < args.length; i += 3) {
            String name = args[i];
            String field = args[i + 1];
            String file = args[i + 2];
            byte[] text = Files.readAllBytes(Paths.get(file));
            probe.TableVariable.Report report = new probe.TableVariable.Report();
            probe.RootVariable.Value v = probe.RootVariable.fromJson(text, report);
            String verdict = (v == null && (report.refused || report.damaged)) ? "refused"
                    : (v != null && !report.refused && !report.damaged) ? "read" : "partial";
            long count = v == null ? -1 : field.equals("ids") ? v.ids.size() : v.names.size();
            System.out.println(name + "," + verdict + "," + count);
        }
    }
}
`

const textReadLargeMapKeys = 80000

func textReadLargeMapText(field string, keys int, repeat bool) string {
	var b strings.Builder
	b.WriteString(`{"`)
	b.WriteString(field)
	b.WriteString(`":{`)
	for i := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		if field == "ids" {
			fmt.Fprintf(&b, `"%d":1`, i)
		} else {
			fmt.Fprintf(&b, `"k%d":1`, i)
		}
	}
	if repeat {
		if field == "ids" {
			b.WriteString(`,"0":2`)
		} else {
			b.WriteString(`,"k0":2`)
		}
	}
	b.WriteString("}}")
	return b.String()
}

type textReadLargeMapWant struct {
	verdict string
	count   int
}

// TestJavaTextLargeMapFunctional holds the Java text read to the oracle on two
// 80,000-key maps and their repeated-key twins, in one Java process.
func TestJavaTextLargeMapFunctional(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)
	u := unitFrom(t, textReadLargeMapSchema)
	model := tabletext.NewModel(u)
	st := u.Tables["Root"]

	cases := []struct {
		name  string
		field string
		text  string
	}{
		{"uint64", "ids", textReadLargeMapText("ids", textReadLargeMapKeys, false)},
		{"string", "names", textReadLargeMapText("names", textReadLargeMapKeys, false)},
		{"uint64_duplicate", "ids", textReadLargeMapText("ids", textReadLargeMapKeys, true)},
		{"string_duplicate", "names", textReadLargeMapText("names", textReadLargeMapKeys, true)},
	}

	want := make(map[string]textReadLargeMapWant, len(cases))
	for _, c := range cases {
		inst := model.New(st)
		var report tabletext.Report
		ok := model.Read(inst, []byte(c.text), &report)
		w := textReadLargeMapWant{verdict: "read", count: -1}
		if !ok || report.Refused {
			w.verdict = "refused"
		} else {
			for i := range inst.Fields {
				if inst.Fields[i].Def.Name == c.field {
					w.count = len(inst.Fields[i].Entries)
				}
			}
		}
		want[c.name] = w
		if strings.HasSuffix(c.name, "_duplicate") {
			if w.verdict != "refused" {
				t.Fatalf("%s: the oracle read a repeated key: %+v", c.name, report)
			}
		} else if w.verdict != "read" || w.count != textReadLargeMapKeys {
			t.Fatalf("%s: the oracle did not read %d keys: verdict %s count %d report %+v", c.name, textReadLargeMapKeys, w.verdict, w.count, report)
		}
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	srcdir := filepath.Join(root, "src", "probe")
	classes := filepath.Join(root, "classes")
	for _, d := range []string{srcdir, classes} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var srcs []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(srcdir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		srcs = append(srcs, p)
	}
	probeMain := filepath.Join(root, "LargeMapProbe.java")
	if err := os.WriteFile(probeMain, []byte(textReadLargeMapProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, srcs...)
	args = append(args, probeMain)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	var runArgs []string
	for i, c := range cases {
		p := filepath.Join(root, fmt.Sprintf("case-%d.json", i))
		if err := os.WriteFile(p, []byte(c.text), 0o644); err != nil {
			t.Fatal(err)
		}
		runArgs = append(runArgs, c.name, c.field, p)
	}
	cmd := exec.Command(javaBin, "-ea", "-cp", classes, "LargeMapProbe")
	cmd.Args = append(cmd.Args, runArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	got := make(map[string]textReadLargeMapWant, len(cases))
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Split(strings.TrimSpace(line), ",")
		if len(parts) != 3 {
			t.Fatalf("probe line %q: want name,verdict,count", line)
		}
		count, err := strconv.Atoi(parts[2])
		if err != nil {
			t.Fatalf("probe line %q: count: %v", line, err)
		}
		got[parts[0]] = textReadLargeMapWant{verdict: parts[1], count: count}
	}
	if len(got) != len(cases) {
		t.Fatalf("probe answered %d of %d rows:\n%s", len(got), len(cases), out)
	}
	for _, c := range cases {
		g, w := got[c.name], want[c.name]
		if g.verdict != w.verdict {
			t.Errorf("%s: Java verdict = %s, oracle = %s", c.name, g.verdict, w.verdict)
		}
		if g.count != w.count {
			t.Errorf("%s: Java count = %d, oracle = %d", c.name, g.count, w.count)
		}
	}
}
