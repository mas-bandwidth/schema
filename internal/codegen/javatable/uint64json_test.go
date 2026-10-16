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
// at and above 2^63-1 (both integral and non-integral spellings) as well as
// negative and out-of-range inputs, and compares the decoded values, the saved
// bytes, and the report (clamped and kindMismatch counts) to what the Go oracle
// (internal/tabletext, internal/tablewire) produces for the same JSON, byte for
// byte. Values in 0..2^64-1 must be kept exactly; only values past the range
// clamp (§16.2). Also verifies that int64 stays signed.
func TestFromJsonKeepsUint64AboveInt64MaxJava(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaTools(t)
	src := `package probe
union U
{
    a uint64
}
table T
{
    f  uint64
    xs []uint64
    u  U
    i  int64
}
`
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["TVariable.java"]; !ok {
		t.Fatal("TVariable.java not emitted for a uint64 variable table")
	}
	if _, ok := files["UVariable.java"]; !ok {
		t.Fatal("UVariable.java not emitted for a uint64 union arm")
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
		want    string
		clamped bool // the value is outside 0..2^64-1 (or int64 range), so the oracle clamps
	}{
		// in range (integral): kept exactly, clamped 0
		{`{ "f": 9223372036854775807, "xs": [9223372036854775807], "u": { "a": 9223372036854775807 } }`, "f=9223372036854775807 xs=[9223372036854775807] u.a=9223372036854775807 i=0", false},
		{`{ "f": 9223372036854775808, "xs": [9223372036854775808], "u": { "a": 9223372036854775808 } }`, "f=9223372036854775808 xs=[9223372036854775808] u.a=9223372036854775808 i=0", false},
		{`{ "f": 18446744073709551615, "xs": [18446744073709551615], "u": { "a": 18446744073709551615 } }`, "f=18446744073709551615 xs=[18446744073709551615] u.a=18446744073709551615 i=0", false},
		// out of range (integral): clamped, same report as the oracle
		{`{ "f": 18446744073709551616, "xs": [18446744073709551616], "u": { "a": 18446744073709551616 } }`, "f=18446744073709551615 xs=[18446744073709551615] u.a=18446744073709551615 i=0", true},
		{`{ "f": -5, "xs": [-5], "u": { "a": -5 } }`, "f=0 xs=[0] u.a=0 i=0", true},
		// non-integral and -0 spellings (§16.2):
		{`{ "f": -5.0, "xs": [-5.0], "u": { "a": -5.0 } }`, "f=0 xs=[0] u.a=0 i=0", true},
		{`{ "f": 9223372036854775808.0, "xs": [9223372036854775808.0], "u": { "a": 9223372036854775808.0 } }`, "f=9223372036854775808 xs=[9223372036854775808] u.a=9223372036854775808 i=0", false},
		{`{ "f": 1e19, "xs": [1e19], "u": { "a": 1e19 } }`, "f=10000000000000000000 xs=[10000000000000000000] u.a=10000000000000000000 i=0", false},
		{`{ "f": -0, "xs": [-0], "u": { "a": -0 } }`, "f=0 xs=[0] u.a=0 i=0", false},
		// int64 stays signed:
		{`{ "i": -5 }`, "f=0 xs=[] u.a=none i=-5", false},
		{`{ "i": 9223372036854775808 }`, "f=0 xs=[] u.a=none i=9223372036854775807", true},
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

	// Strip the JVM's banner line and keep only the driver's CASE, VAL, and HEX lines.
	var lines []string
	for _, ln := range strings.Split(string(out), "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "CASE ") || strings.HasPrefix(ln, "VAL ") || strings.HasPrefix(ln, "HEX ") {
			lines = append(lines, ln)
		}
	}
	sc := bufio.NewScanner(strings.NewReader(strings.Join(lines, "\n")))
	for i, c := range cases {
		var caseLine, valLine, hexLine string
		if !sc.Scan() {
			t.Fatalf("case %d: driver output ended early", i)
		}
		caseLine = strings.TrimSpace(sc.Text())
		if !sc.Scan() {
			t.Fatalf("case %d: driver output ended early", i)
		}
		valLine = strings.TrimSpace(sc.Text())
		if !sc.Scan() {
			t.Fatalf("case %d: driver output ended early", i)
		}
		hexLine = strings.TrimSpace(sc.Text())

		if !strings.HasPrefix(caseLine, "CASE "+strconv.Itoa(i)+" ") {
			t.Fatalf("case %d: unexpected driver line %q", i, caseLine)
		}
		fields := strings.Fields(strings.TrimPrefix(caseLine, "CASE "+strconv.Itoa(i)))
		var javaClamped, javaKind int
		for j := 0; j+1 < len(fields); j += 2 {
			switch fields[j] {
			case "clamped":
				javaClamped, err = strconv.Atoi(fields[j+1])
				if err != nil {
					t.Fatalf("case %d: bad clamped", i)
				}
			case "kind":
				javaKind, err = strconv.Atoi(fields[j+1])
				if err != nil {
					t.Fatalf("case %d: bad kind", i)
				}
			}
		}
		javaVal := strings.TrimPrefix(valLine, "VAL ")
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

		if javaVal != c.want || !strings.EqualFold(javaHex, hex.EncodeToString(oracleWire)) || javaClamped != rep.Clamped || javaKind != rep.KindMismatch {
			t.Errorf("case %d (%s): got %s (clamped=%d kind=%d hex=%s), want %s (oracle clamped=%d kind=%d hex=%s)",
				i, c.json, javaVal, javaClamped, javaKind, javaHex, c.want, rep.Clamped, rep.KindMismatch, hex.EncodeToString(oracleWire))
			continue
		}

		if c.clamped && javaClamped == 0 {
			t.Errorf("case %d (%s): Java clamped=%d (%s) but the oracle clamps (clamped=%d)", i, c.json, javaClamped, javaVal, rep.Clamped)
		}
		if !c.clamped && javaClamped != 0 {
			t.Errorf("case %d (%s): Java clamped=%d (%s) but value is in range (oracle clamped=%d)", i, c.json, javaClamped, javaVal, rep.Clamped)
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
            StringBuilder xs = new StringBuilder("[");
            for (int i = 0; i < v.xs.size(); i++) {
                if (i > 0) { xs.append(","); }
                xs.append(Long.toUnsignedString(v.xs.get(i)));
            }
            xs.append("]");
            String uStr = (v.u != null && v.u.tag == 1) ? Long.toUnsignedString(v.u.a) : "none";
            System.out.println("VAL f=" + Long.toUnsignedString(v.f)
                    + " xs=" + xs
                    + " u.a=" + uStr
                    + " i=" + Long.toString(v.i));
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
