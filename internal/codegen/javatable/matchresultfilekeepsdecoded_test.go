package javatable

import (
	"bytes"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// The minimal damaged MatchResult FILE (form 1) of this cause: a random mutant
// of the match_full corpus seed that both engines read with kind_mismatch=1
// and malformed, where the Go oracle keeps 174 bytes and the Java leg must
// keep the same bytes (docs/SPEC-TABLES.md §3.3).
const matchResultFileKeepsDecodedMutant = "0101090101010101010101020ed1010d0a110309e8030000000000000404d204000000140309e90300000000000004041f05000005060200140309ea0300000000000004046c05000005060300140309eb030000000000000404b905000005060400140309ec03000000000000040406060000050fd3060500140309ed0300000000000004045306000005060600140309ee030000000000000404a006000005060700140309ef030000000000000404ed06000005060800140309f00300000000000004043a07000005060900140309f10300000000000004048707000005060a0000266814ba79e336f3298b551875ace39838c48d74ffe34ac1f524f6e7fb630ec1204a75304802a3980500000000000000"

// TestJavaFixMatchResultFileKeepsDecoded pins the Java FILE read of
// backenddemo.MatchResult to the Go oracle on a damaged input: the kind
// mismatch is skipped, the parent reads on, and every field decoded before
// the damage stands, byte for byte (§3.3, §4).
func TestJavaFixMatchResultFileKeepsDecoded(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	cardMutant, err := hex.DecodeString(matchResultFileKeepsDecodedMutant)
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "match_full.bin"))
	if err != nil {
		t.Fatal(err)
	}
	early := append([]byte(nil), base...)
	early[28] = 0x00
	cases := []struct {
		name         string
		mutant       []byte
		kindMismatch int
	}{
		{"card truncated unknown union", cardMutant, 1},
		{"array element early terminator", early, 0},
	}
	u := unitFromDir(t, filepath.Join(repo, "tables", "backend"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if files["MatchResultVariable.java"] == nil {
		t.Fatal("MatchResultVariable.java is not emitted")
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Int128.java", "UInt128.java"} {
		if data := packet[name]; data != nil {
			files[name] = data
		}
	}
	pkg := javaPackageOf(u)
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	driverName := "MatchResultFileDriver"
	driverPath := filepath.Join(dir, driverName+".java")
	driver := strings.ReplaceAll(matchResultFileDriver(pkg), "Driver", driverName)
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driverPath)
	sort.Strings(sources)
	javac, javaBin := javaTools(t)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := tabletext.NewModel(u)
			value := model.New(u.Tables["MatchResult"])
			report := new(tabletext.Report)
			if _, derr := tablewire.Decode(model, value, tc.mutant, report); derr != nil {
				t.Fatalf("Go reference: err=%v report=%+v", derr, report)
			}
			if !report.Malformed || report.KindMismatch != tc.kindMismatch {
				t.Fatalf("Go reference report does not show this cause: %+v", report)
			}
			golden, err := tablewire.Encode(model, value)
			if err != nil {
				t.Fatal(err)
			}
			wirePath := filepath.Join(t.TempDir(), "mutant.bin")
			if err := os.WriteFile(wirePath, tc.mutant, 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(javaBin, "-ea", "-cp", classes, driverName, wirePath).CombinedOutput()
			if err != nil {
				t.Fatalf("java driver: %v\n%s", err, out)
			}
			line := strings.TrimSpace(string(out))
			var savedHex string
			javaKind, javaMalformed := -1, ""
			for _, field := range strings.Fields(line) {
				switch {
				case strings.HasPrefix(field, "saveHex="):
					savedHex = strings.TrimPrefix(field, "saveHex=")
				case strings.HasPrefix(field, "kindMismatch="):
					javaKind, _ = strconv.Atoi(strings.TrimPrefix(field, "kindMismatch="))
				case strings.HasPrefix(field, "malformed="):
					javaMalformed = strings.TrimPrefix(field, "malformed=")
				}
			}
			if savedHex == "" {
				t.Fatalf("driver printed no saveHex: %q", line)
			}
			if javaKind != report.KindMismatch || javaMalformed != strconv.FormatBool(report.Malformed) {
				t.Errorf("Java report kindMismatch=%d malformed=%s, Go oracle kindMismatch=%d malformed=%v",
					javaKind, javaMalformed, report.KindMismatch, report.Malformed)
			}
			got, err := hex.DecodeString(savedHex)
			if err != nil {
				t.Fatalf("bad saveHex %q: %v", savedHex, err)
			}
			if !bytes.Equal(got, golden) {
				d := 0
				for d < len(got) && d < len(golden) && got[d] == golden[d] {
					d++
				}
				t.Errorf("Java saved %d bytes, Go oracle %d; first difference at byte %d\nJava %s\nGo   %s",
					len(got), len(golden), d, hex.EncodeToString(got), hex.EncodeToString(golden))
			}
			t.Logf("MatchResult file keeps the oracle's decoded bytes: %d bytes, kindMismatch=%d malformed=%v", len(got), report.KindMismatch, report.Malformed)
		})
	}
}

func matchResultFileDriver(pkg string) string {
	return `import java.nio.file.Files;
import java.nio.file.Paths;

public final class Driver {
    private Driver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        ` + pkg + `.TableVariable.Report report = new ` + pkg + `.TableVariable.Report();
        ` + pkg + `.MatchResultVariable.Value v = ` + pkg + `.MatchResultVariable.load(wire, report);
        if (v == null) { fail("load returned null"); }
        byte[] saved = ` + pkg + `.MatchResultVariable.save(v);
        StringBuilder sb = new StringBuilder();
        for (byte b : saved) {
            int x = b & 0xff;
            if (x < 16) { sb.append('0'); }
            sb.append(Integer.toHexString(x));
        }
        System.out.println("saveHex=" + sb + " kindMismatch=" + report.kindMismatch + " malformed=" + report.malformed);
    }
}
`
}
