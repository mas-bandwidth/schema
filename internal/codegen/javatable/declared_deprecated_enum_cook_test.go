package javatable

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestJavaDeclaredDeprecatedEnumCook answers the SF-26 witness's six cook
// rows (docs/SPEC-CONFORMANCE.md "Declared deprecated enum writers";
// testdata/conformance/feature-witnesses/declared-deprecated-enum) through the
// generated Java writer, run with -ea because Java is an assert leg (SPEC.md
// §5.1). Phase.Next cooks the pinned 88 bytes in both orders; Phase.Old, and
// the ordinal 9 past the variant set (SPEC.md §5.1 row 5), are refused by
// cookFrom's assertion before a byte of the 0x5a-filled destination moves.
// cookMeasure is the layout's 88 for every value (§7.6). The shared
// write-refusal-cook rows cref_enum_* hold the same rule on every leg.
func TestJavaDeclaredDeprecatedEnumCook(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaTools(t)
	fixture := filepath.Join("..", "..", "..", "testdata", "conformance", "feature-witnesses", "declared-deprecated-enum")
	cases, err := os.ReadFile(filepath.Join(fixture, "cases.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for line := range strings.SplitSeq(string(cases), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if len(f) != 2 || want[f[0]] != "" {
			t.Fatalf("invalid expected row %q", line)
		}
		want[f[0]] = f[1]
	}
	rows := []string{"sf26_cook_measure_next", "sf26_cook_measure_old", "sf26_cook_next_le", "sf26_cook_next_be", "sf26_cook_old_le", "sf26_cook_old_be"}
	var args []string
	for _, row := range rows {
		if want[row] == "" {
			t.Fatalf("cases.txt holds no %s row", row)
		}
		args = append(args, row, want[row])
	}
	dir := compileCookUnit(t, javac, cookUnitFiles(t, filepath.Join(fixture, "Probe.schema")), "depr",
		map[string]string{"DeprCook": deprCookDriver})
	out, err := testproc.Command(t, javaBin, append([]string{"-ea", "-cp", dir, "DeprCook"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	var got []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		got = append(got, line)
	}
	sort.Strings(got)
	expect := []string{"phase9_cook_be refused", "phase9_cook_le refused"}
	for _, row := range rows {
		expect = append(expect, fmt.Sprintf("%s ok", row))
	}
	sort.Strings(expect)
	if strings.Join(got, "\n") != strings.Join(expect, "\n") {
		t.Fatalf("answers:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(expect, "\n"))
	}
}

const deprCookDriver = `import java.util.Arrays;
import java.util.HexFormat;

public final class DeprCook {
    static depr.FixedRootVariable.Value value(int phase) {
        depr.FixedRootVariable.Value v = new depr.FixedRootVariable.Value();
        v.phase = phase;
        return v;
    }

    // a refusal is cookFrom's assertion, and the destination keeps every 0x5a
    static boolean refused(int phase, int order) {
        byte[] out = new byte[88];
        Arrays.fill(out, (byte) 0x5a);
        boolean asserted = false;
        try {
            if (depr.FixedRootCook.cookFrom(value(phase), out, order)) { return false; }
        } catch (AssertionError expected) {
            asserted = true;
        }
        for (byte b : out) { if (b != 0x5a) { return false; } }
        return asserted;
    }

    public static void main(String[] args) {
        for (int i = 0; i < args.length; i += 2) {
            String row = args[i], want = args[i + 1];
            int phase = row.contains("_old") ? 2 : 3;
            int order = row.endsWith("_be") ? 2 : 1;
            boolean ok;
            if (row.startsWith("sf26_cook_measure_")) {
                ok = depr.FixedRootCook.cookMeasure(value(phase)) == Long.parseLong(want);
            } else if (want.equals("refused")) {
                ok = refused(phase, order);
            } else {
                byte[] out = new byte[88];
                Arrays.fill(out, (byte) 0x5a);
                ok = depr.FixedRootCook.cookFrom(value(phase), out, order)
                        && Arrays.equals(out, HexFormat.of().parseHex(want));
            }
            System.out.println(row + (ok ? " ok" : " FAILED"));
        }
        // the ordinal 9 lies past Phase's three variants (SPEC.md §5.1 row 5)
        System.out.println("phase9_cook_le " + (refused(9, 1) ? "refused" : "WROTE"));
        System.out.println("phase9_cook_be " + (refused(9, 2) ? "refused" : "WROTE"));
    }
}
`
