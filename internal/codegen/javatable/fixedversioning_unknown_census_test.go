package javatable

// `unknown_census`, §5.8 row 11 of docs/FIXED-FORM-VERSIONING-TESTS.md, on the
// java leg. `VOLD_/VNEW_unknown_census`: OLD `Item { a, drop }`, NEW the same with
// `Item.drop` REMOVED, inside `Census { lead, items [4]Item, trail }`. The pair is
// deliberately UNLAWFUL — §5.1 refuses a removal — so the lineage entry is HANDED
// IN by the probe and never read from a lock.
//
// A lawful older writer names only fields the reader retains, so a writer field
// the reader names nowhere makes the handed-in entry UNLAWFUL: COMPILE lays no
// plan and the load refuses `layoutMalformed` with every counter at zero —
// `unknown` included, because there is no compile census
// (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH, §5.4, §5.9 #30). The removed field
// sits in every element of an array of tables, so this is the rule in that slot.
// After the refusal every row handed in, POISONED first, holds its declared
// defaults (SPEC-TABLES §6.5), and a second load (fresh report) and a third (the
// first report handed back) refuse the same way.
import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// javaUnlawfulCensusPoison and javaUnlawfulCensusDefaults are the Census
// root's poison and its declared defaults (lead 1, trail 2, every items[i].a 0).
const (
	javaUnlawfulCensusPoison   = `v.lead = 99; v.trail = 88; for (int i = 0; i < 4; i++) { v.items[i].a = 5 + i; }`
	javaUnlawfulCensusDefaults = `v.lead == 1 && v.trail == 2 && v.items[0].a == 0 && v.items[1].a == 0 && v.items[2].a == 0 && v.items[3].a == 0`
)

// javaUnlawfulRequired is the loud half of the gate for the unlawful-entry
// rows: once slowtest.Gate let the test run, SCHEMA_SLOW or
// SCHEMA_REQUIRE_CORPUS promised the toolchain and the corpus, so a missing
// JDK or corpus file FAILS by name, never a quiet skip. It answers the file.
func javaUnlawfulRequired(t *testing.T, name string) string {
	t.Helper()
	if _, _, ok := javaToolsLookup(t); !ok {
		t.Fatal("no working javac/java was found: set JAVAC and JAVA")
	}
	dir := os.Getenv("SCHEMA_FIXEDFORM_CORPUS")
	if dir == "" {
		dir = filepath.Join("..", "..", "..", "build", "fixedform-corpus")
	}
	file := filepath.Join(dir, name)
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("SCHEMA_SLOW or SCHEMA_REQUIRE_CORPUS promised the fixed-form corpus and %s is not there (%v): run `make tables-fixedform-corpus`", file, err)
	}
	return file
}

// javaUnlawfulRun builds the row's NEW schema with the OLD one HANDED IN as a
// lineage entry, poisons every row with `poison` (Java statements on `v`), and
// loads `file` three times: a fresh report, a second fresh report, and the
// first report handed back. Each load must answer -1, refused by
// layoutMalformed, unknown and widened zero, and every one of the
// eight rows at its declared defaults (`defaults`, a Java boolean on `v`). The
// probe first proves its own oracle: a fresh value IS at defaults and a
// poisoned one is NOT.
func javaUnlawfulRun(t *testing.T, row, file, poison, defaults string) {
	t.Helper()
	javaUnlawfulRunPair(t, row, "VOLD_"+row, "VNEW_"+row, file, poison, defaults)
}

// javaUnlawfulRunPair is javaUnlawfulRun for a pair named outright: older and
// newer are the OLD and NEW schemas' base names under test/tables.
func javaUnlawfulRunPair(t *testing.T, row, older, newer, file, poison, defaults string) {
	t.Helper()
	dir := t.TempDir()
	builds, classes := buildRow(t, dir, row, []sideSpec{
		{key: "reads", schema: newer + ".schema", older: []string{older + ".schema"}},
	})
	b := builds["reads"]

	const class = "Probe_unlawful"
	src := fmt.Sprintf(`import java.nio.file.*;

public final class %[1]s {
    static boolean atDefaults(%[2]s.%[3]sFixed.Value v) { return %[5]s; }

    static void poison(%[2]s.%[3]sFixed.Value v) { %[4]s }

    public static void main(String[] args) throws Exception {
        byte[] data = Files.readAllBytes(Paths.get(args[0]));
        %[2]s.%[3]sFixed.Value[] vals = new %[2]s.%[3]sFixed.Value[8];
        for (int i = 0; i < vals.length; i++) { vals[i] = new %[2]s.%[3]sFixed.Value(); }
        System.out.println("FRESH defaults=" + atDefaults(vals[0]));
        poison(vals[0]);
        System.out.println("POISONED defaults=" + atDefaults(vals[0]));
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
            System.out.println("LOAD" + pass + " n=" + n
                + " refused=" + r.refused + " reason=" + r.reason
                + " unknown=" + r.unknown + " widened=" + r.widened + " atDefaults=" + at);
        }
    }
}
`, class, b.pkg, b.root, poison, defaults)

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
	got := string(out)
	t.Logf("probe said:\n%s", got)

	lines := map[string]map[string]string{}
	for line := range strings.SplitSeq(got, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		kv := map[string]string{}
		for _, tok := range fields[1:] {
			if k, v, ok := strings.Cut(tok, "="); ok {
				kv[k] = v
			}
		}
		lines[fields[0]] = kv
	}
	if lines["FRESH"]["defaults"] != "true" || lines["POISONED"]["defaults"] != "false" {
		t.Fatalf("%s: the probe's oracle is wrong: a fresh value at defaults=%s, a poisoned one=%s (want true, false)",
			row, lines["FRESH"]["defaults"], lines["POISONED"]["defaults"])
	}
	for _, tag := range []string{"LOAD1", "LOAD2", "LOAD3"} {
		kv, ok := lines[tag]
		if !ok {
			t.Fatalf("%s: the probe printed no %s:\n%s", row, tag, got)
		}
		num := func(key string) int {
			n, err := strconv.Atoi(kv[key])
			if err != nil {
				t.Fatalf("%s %s: %s=%q is not a number", row, tag, key, kv[key])
			}
			return n
		}
		if n := num("n"); n != -1 {
			t.Errorf("%s %s: the UNLAWFUL entry LOADED n=%d, want -1 (no plan is laid, §5.4)", row, tag, n)
		}
		if kv["refused"] != "true" || kv["reason"] != "layoutMalformed" {
			t.Errorf("%s %s: refused=%s reason=%s, want refused by layoutMalformed",
				row, tag, kv["refused"], kv["reason"])
		}
		if u, w := num("unknown"), num("widened"); u != 0 || w != 0 {
			t.Errorf("%s %s: a refusal moved a counter: unknown=%d widened=%d, want 0 and 0 (there is no compile census, §5.9 #30)",
				row, tag, u, w)
		}
		if at := num("atDefaults"); at != 8 {
			t.Errorf("%s %s: %d of 8 poisoned rows are at their declared defaults after the refusal, want 8 (SPEC-TABLES §6.5)",
				row, tag, at)
		}
	}
}

func TestFixedVersioningUnknownCensus(t *testing.T) {
	file := javaUnlawfulRequired(t, "old_unknown_census.bin")
	javaUnlawfulRun(t, "unknown_census", file, javaUnlawfulCensusPoison, javaUnlawfulCensusDefaults)
}
