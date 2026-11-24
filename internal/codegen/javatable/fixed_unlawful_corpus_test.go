package javatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedunlawful"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// javaUnlawfulAnswer is the java leg's answer to one case of the shared
// unlawful-lineage corpus (internal/fixedunlawful): Java statements on `v`, a
// row of the NEW root, that set it to values that are not its declared
// defaults, and a Java boolean on `v` that holds exactly when the row is at
// its declared defaults (lead 1, trail 2, every unlisted field its type's
// default; read off the VNEW_ schema).
type javaUnlawfulAnswer struct {
	poison   string
	defaults string
	// values, for the LAWFUL control only, is a Java String expression over
	// `v`, row 0 after the load, that formats it as the case's Values line.
	values string
}

var javaUnlawfulAnswers = map[string]javaUnlawfulAnswer{
	"unknown_census":      {poison: javaUnlawfulCensusPoison, defaults: javaUnlawfulCensusDefaults},
	"unknown_census_many": {poison: javaUnlawfulCensusPoison, defaults: javaUnlawfulCensusDefaults},
	"kind_moved": {
		poison:   `v.lead = 99; v.v = 5.5f; v.trail = 88;`,
		defaults: `v.lead == 1 && v.v == 0.0f && v.trail == 2`,
	},
	"arm_removed": {
		// gamma is the arm the file takes and the reader keeps.
		poison:   `v.lead = 99; v.pick.type = 2; v.pick.gamma.p = 77; v.trail = 88;`,
		defaults: `v.lead == 1 && v.pick.type == 0 && v.pick.alpha.m == 0 && v.pick.gamma.p == 0 && v.trail == 2`,
	},
	"variant_removed": {
		// Gold, the variant the file holds and the reader keeps, at a new id.
		poison:   `v.lead = 99; v.tier = (byte) 2; v.trail = 88;`,
		defaults: `v.lead == 1 && v.tier == 0 && v.trail == 2`,
	},
	"key_removed": {
		poison:   `v.lead = 99; v.slots[0] = 70; v.slots[1] = 71; v.trail = 88;`,
		defaults: `v.lead == 1 && v.slots[0] == 0 && v.slots[1] == 0 && v.trail == 2`,
	},
	"array_shrunk": {
		poison:   `v.lead = 99; v.vals[0] = 70; v.vals[1] = 71; v.trail = 88;`,
		defaults: `v.lead == 1 && v.vals[0] == 0 && v.vals[1] == 0 && v.trail == 2`,
	},
	"counted_shrunk": {
		poison:   `v.lead = 99; v.valsCount = 2; v.vals[0] = 70; v.vals[1] = 71; v.trail = 88;`,
		defaults: `v.lead == 1 && v.valsCount == 0 && v.vals[0] == 0 && v.vals[1] == 0 && v.trail == 2`,
	},
	"text_narrowed": {
		poison:   `v.lead = 99; v.text[0] = (byte) 'z'; v.text[1] = (byte) 'z'; v.textLength = 2; v.trail = 88;`,
		defaults: `v.lead == 1 && v.textLength == 0 && java.util.Arrays.equals(v.text, new byte[4]) && v.trail == 2`,
	},
	"bytes_narrowed": {
		poison:   `v.lead = 99; v.blob[0] = (byte) 0x5A; v.blobLength = 1; v.trail = 88;`,
		defaults: `v.lead == 1 && v.blobLength == 0 && java.util.Arrays.equals(v.blob, new byte[4]) && v.trail == 2`,
	},
	"lawful_control": {
		poison: `v.lead = 99; v.v = 5; v.trail = 88;`,
		// Java has no unsigned integer: print each field's bits as unsigned.
		values: `"lead=" + Integer.toUnsignedString((int) v.lead) + " v=" + Integer.toUnsignedString((int) v.v) + " trail=" + Integer.toUnsignedString((int) v.trail)`,
	},
}

// TestJavaFixedUnlawfulCorpus is the java leg's driver for the shared
// unlawful-lineage corpus. Per case the NEW build, handed the OLD layout as a
// lineage entry, loads the OLD file. The Java probe prints the verdict, and the
// driver answers what the probe printed. The LAWFUL control must load one
// record, and the probe prints the values row 0 landed. Per UNLAWFUL case the
// load is made three times (a fresh report, a second fresh one, the first
// handed back): each refuses before any record (-1), by layoutMalformed (not
// damaged: the java leg's own refusal name, pinned here and never compared
// across legs, refusal-verdict-only), every counter zero, every poisoned row
// back at its declared defaults (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH/EMIT,
// §5.4; SPEC-TABLES §6.5). The probe prints "refused" only after all of those
// held.
func TestJavaFixedUnlawfulCorpus(t *testing.T) {
	fixedunlawful.Run(t, "javatable", javaUnlawfulAnswers, func(t *testing.T, c fixedunlawful.Case, a javaUnlawfulAnswer) string {
		t.Parallel()
		file := javaUnlawfulRequired(t, c.File)
		return javaUnlawfulVerdict(t, c, file, a)
	})
}

// javaUnlawfulVerdict builds the case's NEW schema with the OLD one handed in
// as a lineage entry, runs the probe, and answers the verdict line the probe
// printed. A probe that printed none, or exited nonzero, fails by name.
func javaUnlawfulVerdict(t *testing.T, c fixedunlawful.Case, file string, a javaUnlawfulAnswer) string {
	t.Helper()
	dir := t.TempDir()
	builds, classes := buildRow(t, dir, c.Name, []sideSpec{
		{key: "reads", schema: c.New + ".schema", older: []string{c.Old + ".schema"}},
	})
	b := builds["reads"]

	const class = "Probe_unlawful"
	var src string
	if c.Lawful {
		src = fmt.Sprintf(`import java.nio.file.*;

public final class %[1]s {
    static void poison(%[2]s.%[3]sFixed.Value v) { %[4]s }

    public static void main(String[] args) throws Exception {
        byte[] data = Files.readAllBytes(Paths.get(args[0]));
        %[2]s.%[3]sFixed.Value[] vals = new %[2]s.%[3]sFixed.Value[8];
        for (int i = 0; i < vals.length; i++) { vals[i] = new %[2]s.%[3]sFixed.Value(); poison(vals[i]); }
        %[2]s.TableFixed.Report r = new %[2]s.TableFixed.Report();
        %[2]s.TableFixed.Entry[] plan = %[2]s.TableFixed.plan(1 << 14);
        short[] remap = new short[1 << 14];
        byte[] image = %[2]s.%[3]sFixed.image();
        int n = %[2]s.%[3]sFixed.load(vals, vals.length, data, plan, remap, image, r);
        if (n < 0 || r.refused || r.damaged) {
            throw new AssertionError("the LAWFUL control refused: n=" + n + " refused=" + r.refused + " damaged=" + r.damaged + " reason=" + r.reason);
        }
        %[2]s.%[3]sFixed.Value v = vals[0];
        System.out.println("VERDICT loaded " + n + ": " + %[5]s);
    }
}
`, class, b.pkg, b.root, a.poison, a.values)
	} else {
		src = fmt.Sprintf(`import java.nio.file.*;

public final class %[1]s {
    static boolean atDefaults(%[2]s.%[3]sFixed.Value v) { return %[5]s; }

    static void poison(%[2]s.%[3]sFixed.Value v) { %[4]s }

    static void check(boolean ok, String what) {
        if (!ok) { throw new AssertionError(what); }
    }

    public static void main(String[] args) throws Exception {
        byte[] data = Files.readAllBytes(Paths.get(args[0]));
        %[2]s.%[3]sFixed.Value[] vals = new %[2]s.%[3]sFixed.Value[8];
        for (int i = 0; i < vals.length; i++) { vals[i] = new %[2]s.%[3]sFixed.Value(); }
        check(atDefaults(vals[0]), "the probe's oracle is wrong: a fresh value is not at defaults");
        poison(vals[0]);
        check(!atDefaults(vals[0]), "the probe's oracle is wrong: a poisoned value is at defaults");
        %[2]s.TableFixed.Report first = new %[2]s.TableFixed.Report();
        for (int pass = 1; pass <= 3; pass++) {
            for (int k = 0; k < vals.length; k++) { poison(vals[k]); }
            %[2]s.TableFixed.Report r = (pass == 2) ? new %[2]s.TableFixed.Report() : first;
            %[2]s.TableFixed.Entry[] plan = %[2]s.TableFixed.plan(1 << 14);
            short[] remap = new short[1 << 14];
            byte[] image = %[2]s.%[3]sFixed.image();
            int n = %[2]s.%[3]sFixed.load(vals, vals.length, data, plan, remap, image, r);
            int at = 0;
            for (int k = 0; k < vals.length; k++) { if (atDefaults(vals[k])) { at++; } }
            String say = "LOAD" + pass + " n=" + n + " refused=" + r.refused + " damaged=" + r.damaged + " reason=" + r.reason
                + " unknown=" + r.unknown + " widened=" + r.widened + " atDefaults=" + at;
            System.out.println(say);
            check(n == -1, say + ": the UNLAWFUL entry LOADED, want -1 (no plan is laid, §5.4)");
            check(r.refused && !r.damaged && String.valueOf(r.reason).equals("layoutMalformed"),
                say + ": want refused by layoutMalformed and not damaged");
            check(r.unknown == 0 && r.widened == 0, say + ": a refusal moved a counter (there is no compile census, §5.9 #30)");
            check(at == 8, say + ": the poisoned rows are not at their declared defaults after the refusal (SPEC-TABLES §6.5)");
        }
        System.out.println("VERDICT refused");
    }
}
`, class, b.pkg, b.root, a.poison, a.defaults)
	}

	path := filepath.Join(dir, class+".java")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	javac, javaBin := javaTools(t)
	if out, err := testproc.Command(t, javac, "--release", "17", "-nowarn", "-cp", classes, "-d", classes, path).CombinedOutput(); err != nil {
		t.Fatalf("javac %s: %v\n%s", class, err, out)
	}
	out, err := testproc.Command(t, javaBin, "-ea", "-cp", classes, class, file).CombinedOutput()
	if err != nil {
		t.Fatalf("java %s: %v\n%s", class, err, out)
	}
	t.Logf("probe said:\n%s", out)
	for line := range strings.SplitSeq(string(out), "\n") {
		if v, ok := strings.CutPrefix(line, "VERDICT "); ok {
			return strings.TrimSpace(v)
		}
	}
	t.Fatalf("%s: the probe printed no VERDICT line:\n%s", c.Name, out)
	return ""
}
