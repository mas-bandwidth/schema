package javatable

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestJavaTextInt64DomainBeforeCast holds a signed 64-bit field's text read to
// the Go reader's verdict and value. A token whose binary64 value is 2^63 or
// greater is outside int64's range and refuses out_of_range before any cast;
// the largest double below 2^63 and -2^63 load with the reader's values
// (docs/SPEC-TABLES.md §16.2).
func TestJavaTextInt64DomainBeforeCast(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)
	src := `package probe
table T
{
    i64 int64
}
`
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	srcdir := filepath.Join(dir, "src", "probe")
	for _, d := range []string{classes, srcdir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range files {
		if strings.HasSuffix(name, ".java") {
			if err := os.WriteFile(filepath.Join(srcdir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "Driver.java"), []byte(textInt64DomainDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	srcs, err := filepath.Glob(filepath.Join(srcdir, "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, srcs...)
	args = append(args, filepath.Join(dir, "Driver.java"))
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	cases := []struct {
		token   string
		refused bool
		value   int64
	}{
		{"9223372036854775808.0", true, 0},
		{"9223372036854775808e0", true, 0},
		{"9.223372036854775808e18", true, 0},
		{"9223372036854775807.0", true, 0},
		{"9223372036854774784.0", false, 9223372036854774784},
		{"-9223372036854775808.0", false, -9223372036854775808},
	}

	var in strings.Builder
	for _, c := range cases {
		fmt.Fprintf(&in, "{\"i64\":%s}\n", c.token)
	}
	cmd := exec.Command(javaBin, "-ea", "-cp", classes, "Driver")
	cmd.Stdin = strings.NewReader(in.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}

	var lines []string
	for ln := range strings.SplitSeq(string(out), "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "CASE ") || strings.HasPrefix(ln, "VAL ") {
			lines = append(lines, ln)
		}
	}
	sc := bufio.NewScanner(strings.NewReader(strings.Join(lines, "\n")))
	for i, c := range cases {
		var caseLine, valLine string
		if !sc.Scan() {
			t.Fatalf("case %d: driver output ended early", i)
		}
		caseLine = strings.TrimSpace(sc.Text())
		if !sc.Scan() {
			t.Fatalf("case %d: driver output ended early", i)
		}
		valLine = strings.TrimSpace(sc.Text())
		prefix := "CASE " + strconv.Itoa(i) + " "
		if !strings.HasPrefix(caseLine, prefix) {
			t.Fatalf("case %d: unexpected driver line %q", i, caseLine)
		}
		javaRefused := strings.TrimPrefix(caseLine, prefix) == "refused"
		javaVal, err := strconv.ParseInt(strings.TrimPrefix(valLine, "VAL "), 10, 64)
		if !javaRefused && err != nil {
			t.Fatalf("case %d: bad value line %q: %v", i, valLine, err)
		}

		u := unitFrom(t, src)
		st := findTable(t, u, "T")
		m := tabletext.NewModel(u)
		inst := m.New(st)
		rep := &tabletext.Report{}
		ok := m.Read(inst, []byte(fmt.Sprintf("{\"i64\":%s}", c.token)), rep)
		if !ok != c.refused {
			t.Fatalf("case %d (%s): the Go reader read=%v refused=%v, want refused=%v (%+v)", i, c.token, ok, rep.Refused, c.refused, rep)
		}
		if !ok {
			if !rep.Refused {
				t.Fatalf("case %d (%s): the Go reader failed without refusing (%+v)", i, c.token, rep)
			}
			if !javaRefused {
				t.Errorf("case %d (%s): Java read %d, the Go reader refuses", i, c.token, javaVal)
			}
			continue
		}
		var oracle int64
		found := false
		for _, f := range inst.Fields {
			if f.Def.Name == "i64" {
				oracle, found = f.Cell.I, true
			}
		}
		if !found {
			t.Fatalf("case %d: field i64 not found in the oracle instance", i)
		}
		if oracle != c.value {
			t.Fatalf("case %d (%s): the Go reader value=%d, want %d", i, c.token, oracle, c.value)
		}
		if javaRefused {
			t.Errorf("case %d (%s): Java refused a value in range (%d)", i, c.token, oracle)
		} else if javaVal != oracle {
			t.Errorf("case %d (%s): Java read %d, the Go reader reads %d", i, c.token, javaVal, oracle)
		}
	}
}

const textInt64DomainDriver = `import java.util.Scanner;

public final class Driver {
    private Driver() {}

    public static void main(String[] args) {
        Scanner sc = new Scanner(System.in, "UTF-8");
        int idx = 0;
        while (sc.hasNextLine()) {
            String line = sc.nextLine();
            if (line.trim().isEmpty()) { continue; }
            byte[] text = line.getBytes(java.nio.charset.StandardCharsets.UTF_8);
            probe.TableVariable.Report rep = new probe.TableVariable.Report();
            probe.TVariable.Value v = probe.TVariable.fromJson(text, rep);
            if (v == null || rep.refused || rep.damaged) {
                System.out.println("CASE " + idx + " refused");
                System.out.println("VAL -");
                idx++;
                continue;
            }
            System.out.println("CASE " + idx + " read");
            System.out.println("VAL " + Long.toString(v.i64));
            idx++;
        }
    }
}
`
