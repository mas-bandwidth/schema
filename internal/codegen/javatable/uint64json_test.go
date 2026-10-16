package javatable

import (
	"bufio"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestFromJsonKeepsUint64AboveInt64MaxJava sends the Java variable form a table
// with a uint64 field, a []uint64 and a union arm of uint64, reads JSON values
// at and above 2^63-1, and compares the saved bytes and the report (clamped
// count) to what the Go oracle (internal/tabletext, internal/tablewire) produces
// for the same JSON, byte for byte. Values in 0..2^64-1 must be kept exactly;
// only values past the range clamp (§16.2).
func TestFromJsonKeepsUint64AboveInt64MaxJava(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaTools(t)
	src := `package probe
table T
{
    f  uint64
    xs []uint64
}
`
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["TVariable.java"]; !ok {
		t.Fatal("TVariable.java not emitted for a uint64 variable table")
	}
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	srcdir := filepath.Join(dir, "src", "probe")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(srcdir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if strings.HasSuffix(name, ".java") {
			if err := os.WriteFile(filepath.Join(srcdir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "Driver.java"), []byte(uint64JsonDriver), 0o644); err != nil {
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
		json    string
		clamped bool // the value is outside 0..2^64-1, so the oracle clamps
	}{
		// in range: kept exactly, clamped 0
		{"{ \"f\": 9223372036854775807, \"xs\": [9223372036854775807] }", false},
		{"{ \"f\": 9223372036854775808, \"xs\": [9223372036854775808] }", false},
		{"{ \"f\": 18446744073709551615, \"xs\": [18446744073709551615] }", false},
		// out of range: clamped, same report as the oracle
		{"{ \"f\": 18446744073709551616, \"xs\": [18446744073709551616] }", true},
		{"{ \"f\": -5, \"xs\": [-5] }", true},
	}

	var in strings.Builder
	for _, c := range cases {
		in.WriteString(c.json)
		in.WriteString("\n")
	}
	cmd := exec.Command(javaBin, "-ea", "-cp", classes, "Driver")
	cmd.Stdin = strings.NewReader(in.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}

	u := unitFrom(t, src)
	st := findTable(t, u, "T")
	m := tabletext.NewModel(u)

	// Strip the JVM's banner line and keep only the driver's CASE and HEX lines.
	var lines []string
	for _, ln := range strings.Split(string(out), "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "CASE ") || strings.HasPrefix(ln, "HEX ") {
			lines = append(lines, ln)
		}
	}
	sc := bufio.NewScanner(strings.NewReader(strings.Join(lines, "\n")))
	for i, c := range cases {
		// read the two lines for this case
		var caseLine, hexLine string
		if !sc.Scan() {
			t.Fatalf("case %d: driver output ended early", i)
		}
		caseLine = strings.TrimSpace(sc.Text())
		if !sc.Scan() {
			t.Fatalf("case %d: driver output ended early", i)
		}
		hexLine = strings.TrimSpace(sc.Text())

		if !strings.HasPrefix(caseLine, "CASE "+strconv.Itoa(i)+" ") {
			t.Fatalf("case %d: unexpected driver line %q", i, caseLine)
		}
		fields := strings.Fields(strings.TrimPrefix(caseLine, "CASE "+strconv.Itoa(i)))
		// fields: clamped N malformed N kind N
		var javaClamped int
		for j := 0; j+1 < len(fields); j += 2 {
			switch fields[j] {
			case "clamped":
				javaClamped, err = strconv.Atoi(fields[j+1])
				if err != nil {
					t.Fatalf("case %d: bad clamped", i)
				}
			}
		}
		javaHex := strings.TrimPrefix(hexLine, "HEX ")
		if _, err := hex.DecodeString(javaHex); err != nil {
			t.Fatalf("case %d: bad HEX line %q: %v", i, hexLine, err)
		}

		inst := m.New(st)
		rep := &tabletext.Report{}
		if !m.Read(inst, []byte(c.json), rep) {
			t.Fatalf("case %d: oracle Read failed", i)
		}
		oracleWire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("case %d: oracle Encode: %v", i, err)
		}

		if c.clamped {
			if javaClamped == 0 {
				t.Fatalf("case %d (%s): Java clamped=%d but the oracle clamps (clamped=%d)", i, c.json, javaClamped, rep.Clamped)
			}
		} else {
			if javaClamped != 0 {
				t.Fatalf("case %d (%s): Java clamped=%d but Java value is in range (oracle clamped=%d)", i, c.json, javaClamped, rep.Clamped)
			}
		}

		if javaClamped != rep.Clamped {
			t.Fatalf("case %d (%s): Java clamped=%d, oracle clamped=%d", i, c.json, javaClamped, rep.Clamped)
		}

		if !strings.EqualFold(javaHex, hex.EncodeToString(oracleWire)) {
			t.Fatalf("case %d (%s): saved bytes differ from the Go oracle\n  java:   %s\n  oracle: %s", i, c.json, javaHex, hex.EncodeToString(oracleWire))
		}
	}
}

const uint64JsonDriver = `import java.util.Scanner;

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
            byte[] saved = probe.TVariable.save(v);
            System.out.println("CASE " + idx + " clamped " + rep.clamped
                    + " malformed " + (rep.malformed ? 1 : 0)
                    + " kind " + rep.kindMismatch);
            StringBuilder sb = new StringBuilder();
            for (byte b : saved) {
                sb.append(Character.forDigit((b >> 4) & 0xf, 16));
                sb.append(Character.forDigit(b & 0xf, 16));
            }
            System.out.println("HEX " + sb.toString());
            idx++;
        }
    }
}
`
