package javatable

// A wide text token is answered by its significant digits alone: one scan of
// the token decides the decimal point and the field's band before any
// big-number arithmetic (docs/SPEC-TABLES.md §16.2; internal/tabletext
// decimalBand and ParseWide). This holds the Java text read to the Go reader
// on long 128-bit tokens and on a long numeric map key, with lawful 39-digit
// neighbours as the controls.

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const wideLinearSchema = `package probe
table Root {
 wide int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
 entries map[uint64]int32
}
`

const wideLinearDriver = `import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;

public final class WideLinearProbe {
    private WideLinearProbe() {}

    static String hex(byte[] b) {
        StringBuilder s = new StringBuilder(b.length * 2);
        for (byte x : b) { s.append(String.format("%02x", x & 0xff)); }
        return s.toString();
    }

    public static void main(String[] args) throws Exception {
        Path dir = Paths.get(args[0]);
        int n = Integer.parseInt(args[1]);
        for (int i = 0; i < n; i++) {
            byte[] text = Files.readAllBytes(dir.resolve("case-" + i + ".json"));
            probe.TableVariable.Report r = new probe.TableVariable.Report();
            probe.RootVariable.Value v = probe.RootVariable.fromJson(text, r);
            if (v == null || r.damaged || r.refused) {
                System.out.println(i + " refused");
            } else {
                byte[] wire = probe.RootVariable.save(v);
                System.out.println(i + " ok " + hex(wire));
            }
        }
    }
}
`

// TestJavaTextLongWideNumberFunctional reads long wide tokens and long numeric
// map keys in one Java process and holds every verdict and value to the Go
// reader. The 800,000-digit shapes are the cost bound: the answer is fixed
// from the token's significant digits, not from a whole-token decimal parse.
func TestJavaTextLongWideNumberFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "an external toolchain")
	javac, javaBin := javaToolsRequired(t)

	type textCase struct {
		name string
		text string
	}
	var cases []textCase
	add := func(name, text string) {
		cases = append(cases, textCase{name: name, text: text})
	}
	for _, n := range []int{100000, 200000, 400000, 800000} {
		nines := strings.Repeat("9", n)
		fine := "1." + strings.Repeat("0", n) + "1"
		whole := "1." + strings.Repeat("0", n)
		add(fmt.Sprintf("wide-nines-%d", n), `{"wide":`+nines+`}`)
		add(fmt.Sprintf("wide-fine-%d", n), `{"wide":`+fine+`}`)
		add(fmt.Sprintf("wide-whole-%d", n), `{"wide":`+whole+`}`)
		add(fmt.Sprintf("map-key-nines-%d", n), `{"entries":{"`+nines+`":1}}`)
		add(fmt.Sprintf("map-key-fine-%d", n), `{"entries":{"`+fine+`":1}}`)
	}
	add("wide-int128-max", `{"wide":170141183460469231731687303715884105727}`)
	add("wide-int128-max-past", `{"wide":170141183460469231731687303715884105728}`)
	add("wide-int128-min-past", `{"wide":-170141183460469231731687303715884105729}`)
	add("map-key-uint64-max", `{"entries":{"18446744073709551615":1}}`)
	add("map-key-uint64-past", `{"entries":{"18446744073709551616":1}}`)

	u := unitFrom(t, wideLinearSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatalf("generate packet: %v", err)
	}
	for _, name := range []string{"Int128.java", "UInt128.java"} {
		if data := packet[name]; data != nil {
			files[name] = data
		}
	}

	root := t.TempDir()
	srcdir := filepath.Join(root, "src", "probe")
	classes := filepath.Join(root, "classes")
	casesDir := filepath.Join(root, "cases")
	for _, d := range []string{srcdir, classes, casesDir} {
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
	for i, c := range cases {
		p := filepath.Join(casesDir, fmt.Sprintf("case-%d.json", i))
		if err := os.WriteFile(p, []byte(c.text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	driver := filepath.Join(root, "WideLinearProbe.java")
	if err := os.WriteFile(driver, []byte(wideLinearDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, srcs...)
	args = append(args, driver)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "WideLinearProbe", casesDir, fmt.Sprint(len(cases))).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("java printed %d lines for %d cases:\n%s", len(lines), len(cases), out)
	}

	m := tabletext.NewModel(u)
	st := findTable(t, u, "Root")
	for i, c := range cases {
		inst := m.New(st)
		rep := new(tabletext.Report)
		okRead := m.Read(inst, []byte(c.text), rep) && !rep.Refused
		want := fmt.Sprintf("%d refused", i)
		if okRead {
			wire, err := tablewire.Encode(m, inst)
			if err != nil {
				t.Fatalf("%s: oracle Encode: %v", c.name, err)
			}
			want = fmt.Sprintf("%d ok %s", i, hex.EncodeToString(wire))
		}
		if got := lines[i]; got != want {
			t.Errorf("%s: java %q, oracle %q (%+v)", c.name, got, want, *rep)
		}
	}
}
