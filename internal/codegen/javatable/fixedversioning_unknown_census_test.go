package javatable

// `unknown_census`, §5.8 row 11 of docs/FIXED-FORM-VERSIONING-TESTS.md, on the
// java leg. `VOLD_/VNEW_unknown_census`: OLD `Item { a, drop }`, NEW the same with
// `Item.drop` REMOVED, inside `Census { lead, items [4]Item, trail }`. The pair is
// deliberately UNLAWFUL — §5.1 refuses a removal — so the lineage entry is HANDED
// IN by the probe and never read from a lock.
//
// `unknown` is asserted EXACTLY == 1: `Item.drop` is ONE field of ONE peer however
// many of the four elements carry it. A leg that counts once per ELEMENT lands 4,
// and that 4 is the whole reason this row exists. The census lands ONCE, after the
// record loop, on a read that RETURNS (§5.9 #6) — so a SECOND read of the same
// peer owes the same number again, which is the second half of the row.
import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestFixedVersioningUnknownCensus(t *testing.T) {
	corpus := corpusDir(t)
	file := filepath.Join(corpus, "old_unknown_census.bin")
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the corpus has no old_unknown_census.bin: run `make tables-fixedform-corpus`")
	}

	dir := t.TempDir()
	builds, classes := buildRow(t, dir, "unknown_census", []sideSpec{
		{key: "reads", schema: "VNEW_unknown_census.schema", older: []string{"VOLD_unknown_census.schema"}},
	})
	b := builds["reads"]

	// A PROBE OF THIS ROW'S OWN, because the shared one reads once and the row
	// is about the SECOND read. Each read is handed a FRESH report, exactly as
	// the Go reference's probe does (fixedversioning_unknown_census_test.go in
	// internal/codegen/gotable): the census is a per-read number, so two reads
	// of one peer owe the same one.
	const class = "Probe_census"
	src := fmt.Sprintf(`import java.nio.file.*;

public final class %[1]s {
    static int read(String path, %[2]s.TableFixed.Report report) throws Exception {
        byte[] data = Files.readAllBytes(Paths.get(path));
        %[2]s.%[3]sFixed.Value[] vals = new %[2]s.%[3]sFixed.Value[8];
        for (int i = 0; i < vals.length; i++) { vals[i] = new %[2]s.%[3]sFixed.Value(); }
        %[2]s.TableFixed.Entry[] plan = %[2]s.TableFixed.plan(1 << 14);
        short[] remap = new short[1 << 14];
        byte[] image = %[2]s.%[3]sFixed.image();
        int n = %[2]s.%[3]sFixed.load(vals, vals.length, data, plan, remap, image, report);
        last = vals[0];
        return n;
    }

    static %[2]s.%[3]sFixed.Value last;

    static void say(String tag, int n, %[2]s.TableFixed.Report r) {
        System.out.println(tag + " n=" + n
            + " unknown=" + r.unknown + " kindMismatch=" + r.kindMismatch
            + " widened=" + r.widened + " clamped=" + r.clamped
            + " refused=" + r.refused + " malformed=" + r.malformed
            + " reason=" + r.reason);
    }

    public static void main(String[] args) throws Exception {
        %[2]s.TableFixed.Report r1 = new %[2]s.TableFixed.Report();
        int n1 = read(args[0], r1);
        say("READ1", n1, r1);
        System.out.println("VALUES lead=" + last.lead + " trail=" + last.trail
            + " a0=" + last.items[0].a + " a1=" + last.items[1].a
            + " a2=" + last.items[2].a + " a3=" + last.items[3].a);

        // THE SECOND READ, its own report, as the reference's probe does.
        %[2]s.TableFixed.Report r2 = new %[2]s.TableFixed.Report();
        int n2 = read(args[0], r2);
        say("READ2", n2, r2);

        // And the same report handed back for a third read, printed only so a
        // red says which of the two shapes the leg is in.
        int n3 = read(args[0], r1);
        say("READ3REUSED", n3, r1);
    }
}
`, class, b.pkg, b.root)

	path := filepath.Join(dir, class+".java")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	javac, javaBin := javaTools(t)
	if out, err := exec.Command(javac, "--release", "17", "-nowarn", "-cp", classes, "-d", classes, path).CombinedOutput(); err != nil {
		t.Fatalf("javac %s: %v\n%s", class, err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, class, file).CombinedOutput()
	if err != nil {
		t.Fatalf("java %s: %v\n%s", class, err, out)
	}
	got := string(out)
	t.Logf("probe said:\n%s", got)

	reads := map[string]map[string]string{}
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
		reads[fields[0]] = kv
	}
	num := func(tag, key string) int {
		v, ok := reads[tag][key]
		if !ok {
			t.Fatalf("the probe printed no %s on %s:\n%s", key, tag, got)
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("%s %s=%q is not a number", tag, key, v)
		}
		return n
	}

	for _, tag := range []string{"READ1", "READ2"} {
		if n := num(tag, "n"); n != 1 {
			t.Errorf("%s: n=%d, want 1", tag, n)
		}
		if reads[tag]["refused"] != "false" || reads[tag]["malformed"] != "false" || reads[tag]["reason"] != "none" {
			t.Errorf("%s: a clean NEW-READS-OLD is not a refusal: %v", tag, reads[tag])
		}
		if num(tag, "kindMismatch") != 0 || num(tag, "widened") != 0 || num(tag, "clamped") != 0 {
			t.Errorf("%s: a counter that must not move did: %v", tag, reads[tag])
		}
		if u := num(tag, "unknown"); u != 1 {
			t.Errorf("%s: unknown=%d, want 1 — Item.drop is ONE field of ONE peer, not one per element",
				tag, u)
		}
	}

	v := reads["VALUES"]
	if v["lead"] != "1" || v["trail"] != "2" {
		t.Errorf("the bracketing fields moved: lead=%s trail=%s, want 1 and 2", v["lead"], v["trail"])
	}
	for i, want := range []string{"10", "11", "12", "13"} {
		key := fmt.Sprintf("a%d", i)
		if v[key] != want {
			t.Errorf("items[%d].a=%s, want %s", i, v[key], want)
		}
	}
}
