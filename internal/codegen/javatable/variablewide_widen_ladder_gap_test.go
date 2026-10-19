package javatable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJavaFixWidenLadderGap pins that a `flags` field (wire kind 9, u64) whose
// wire rides a NARROWER unsigned rung — the fuzzer's widen pass rewrites the
// kind byte 9 -> 6 and truncates the payload to the u8 width — answers exactly
// as the Go oracle does: one `widened`, the value kept, nothing else
// (docs/SPEC-TABLES.md §4). Before the fix the emitted field read counted a
// kind_mismatch, because its widening ladder stopped at kind 9.
func TestJavaFixWidenLadderGap(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, "package probe\nflags Perks { Shielded, Cloaked, Turbo }\ntable Root {\n    perks Perks\n}\n")
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	value.Fields[0].Cell.U = 4 // Turbo
	value.Fields[0].Present = true
	wire, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	// The single field record is [idref=1][kind=9][u64 payload]; the root body
	// ends at its zero reference, and the id table + entry count ride the tail.
	// Rewrite the kind byte to a narrower unsigned rung and truncate the payload
	// to that rung's width, exactly the widening pass's rewriteScalar
	// (test/conformance/harness/wireframe.go). All three unsigned rungs below
	// u64 must count one `widened`, like the oracle.
	kindWidth := map[int]int{6: 1, 7: 2, 8: 4}
	for _, to := range []int{6, 7, 8} {
		to := to
		t.Run(fmt.Sprintf("kind%d", to), func(t *testing.T) {
			t.Parallel()
			w := kindWidth[to]
			mutant := append([]byte{}, wire[:2]...)
			mutant = append(mutant, byte(to))
			mutant = append(mutant, wire[3:3+w]...)
			mutant = append(mutant, wire[11:]...)

			oracle := new(tabletext.Report)
			got := model.New(u.Tables["Root"])
			if ok, err := tablewire.Decode(model, got, mutant, oracle); !ok || err != nil {
				t.Fatalf("Go oracle decode: ok=%v err=%v", ok, err)
			}
			verdict := "read"
			if oracle.Refused {
				verdict = "refused"
			}
			want := fmt.Sprintf("%d,%d,%d,%d,%d,%t,%s", oracle.Unknown, oracle.KindMismatch,
				oracle.Widened, oracle.Clamped, oracle.Duplicate, oracle.Malformed, verdict)
			if oracle.Widened != 1 || oracle.KindMismatch != 0 || oracle.Malformed {
				t.Fatalf("oracle expected exactly one widen; got %s", want)
			}

			javac, javaBin := javaTools(t)
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			classes := filepath.Join(dir, "classes")
			if err := os.MkdirAll(classes, 0o755); err != nil {
				t.Fatal(err)
			}
			var sources []string
			for name, data := range files {
				if filepath.Ext(name) != ".java" {
					continue
				}
				path := filepath.Join(dir, name)
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatal(err)
				}
				sources = append(sources, path)
			}
			wirePath := filepath.Join(dir, "mutant.bin")
			if err := os.WriteFile(wirePath, mutant, 0o644); err != nil {
				t.Fatal(err)
			}
			driver := filepath.Join(dir, "Driver.java")
			if err := os.WriteFile(driver, []byte(widenLadderProbe), 0o644); err != nil {
				t.Fatal(err)
			}
			sources = append(sources, driver)
			sort.Strings(sources)
			args := append([]string{"--release", "17", "-d", classes}, sources...)
			if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
				t.Fatalf("javac: %v\n%s", err, out)
			}
			out, err := exec.Command(javaBin, "-ea", "-cp", classes, "Driver", wirePath).CombinedOutput()
			if err != nil {
				t.Fatalf("java: %v\n%s", err, out)
			}
			gotLine := ""
			for _, line := range strings.Split(string(out), "\n") {
				if line != "" && !strings.HasPrefix(line, "Picked up") {
					gotLine = line
					break
				}
			}
			if gotLine != want {
				t.Fatalf("Java %q != oracle %q\njava output:\n%s", gotLine, want, out)
			}
		})
	}
}

// widenLadderProbe loads the mutant and prints the report line in the harness's
// format: unknown,kindMismatch,widened,clamped,duplicate,malformed,verdict.
const widenLadderProbe = `import java.nio.file.Files;
import java.nio.file.Paths;

public final class Driver {
    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        probe.TableVariable.Report report = new probe.TableVariable.Report();
        probe.RootVariable.Value v = probe.RootVariable.load(wire, report);
        if (v == null) { System.out.println("null"); return; }
        System.out.println(report.unknown + "," + report.kindMismatch + "," + report.widened
                + "," + report.clamped + "," + report.duplicate + "," + report.malformed
                + "," + (report.refused ? "refused" : "read"));
    }
}
`
