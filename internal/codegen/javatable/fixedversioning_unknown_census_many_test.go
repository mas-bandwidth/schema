package javatable

// This closes the second half of docs/FIXED-FORM-ALGORITHM.md §5.4: the compile
// census lands ONCE PER PEER AND NEVER PER RECORD. The landed one-record row
// reads `old_unknown_census.bin`, which holds ONE record — and with one record
// "once per peer" and "once per record" are the SAME NUMBER, 1, so that half of
// the clause has never been under a gate. A per-record Unknown++ really exists
// in these runtimes (the union-tag path) and only a three-record file sees it.
//
// `unknown` is asserted EXACTLY == 1: `Item.drop` is ONE field of ONE peer, so
// three records of the same peer owe the same one, never 3 and never >=.
import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	corpus := corpusDir(t)
	file := filepath.Join(corpus, "many_unknown_census.bin")
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the corpus has no many_unknown_census.bin: run `make tables-fixedform-corpus`")
	}

	dir := t.TempDir()
	builds, classes := buildRow(t, dir, "unknown_census", []sideSpec{
		{key: "reads", schema: "VNEW_unknown_census.schema", older: []string{"VOLD_unknown_census.schema"}},
	})
	b := builds["reads"]

	// A PROBE OF THIS ROW'S OWN, because the shared one reads a single record.
	// The NEW reader dropped Item.drop; the one older peer is VOLD, handed in as
	// a lineage entry exactly as the landed one-record row does. The probe reads
	// THREE records of that peer and prints each record's own lead, trail and
	// four a values, so a reader that lost, duplicated or reordered a record is
	// visible, not just the census.
	const class = "Probe_census_many"
	src := fmt.Sprintf(`import java.nio.file.*;

public final class %[1]s {
    static %[2]s.%[3]sFixed.Value[] vals = new %[2]s.%[3]sFixed.Value[8];
    static { for (int i = 0; i < vals.length; i++) { vals[i] = new %[2]s.%[3]sFixed.Value(); } }

    static int read(String path, %[2]s.TableFixed.Report report) throws Exception {
        byte[] data = Files.readAllBytes(Paths.get(path));
        %[2]s.TableFixed.Entry[] plan = %[2]s.TableFixed.plan(1 << 14);
        short[] remap = new short[1 << 14];
        byte[] image = %[2]s.%[3]sFixed.image();
        int n = %[2]s.%[3]sFixed.load(vals, vals.length, data, plan, remap, image, report);
        return n;
    }

    static void say(String tag, int n, %[2]s.TableFixed.Report r) {
        System.out.println(tag + " n=" + n
            + " unknown=" + r.unknown + " kindMismatch=" + r.kindMismatch
            + " widened=" + r.widened + " clamped=" + r.clamped
            + " refused=" + r.refused + " malformed=" + r.malformed
            + " reason=" + r.reason);
    }

    public static void main(String[] args) throws Exception {
        %[2]s.TableFixed.Report r = new %[2]s.TableFixed.Report();
        int n = read(args[0], r);
        say("READ1", n, r);
        for (int k = 0; k < n; k++) {
            %[2]s.%[3]sFixed.Value v = vals[k];
            System.out.println("REC" + k + " lead=" + v.lead + " trail=" + v.trail
                + " a0=" + v.items[0].a + " a1=" + v.items[1].a
                + " a2=" + v.items[2].a + " a3=" + v.items[3].a);
        }
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

	if n := num("READ1", "n"); n != 3 {
		t.Errorf("READ1: n=%d, want 3 — a three-record file must read three records", n)
	}
	if reads["READ1"]["refused"] != "false" || reads["READ1"]["malformed"] != "false" || reads["READ1"]["reason"] != "none" {
		t.Errorf("READ1: a clean NEW-READS-OLD is not a refusal: %v", reads["READ1"])
	}
	if num("READ1", "kindMismatch") != 0 || num("READ1", "widened") != 0 || num("READ1", "clamped") != 0 {
		t.Errorf("READ1: a counter that must not move did: %v", reads["READ1"])
	}
	if u := num("READ1", "unknown"); u != 1 {
		t.Errorf("READ1: unknown=%d, want 1 — Item.drop is ONE field of ONE peer, once per peer and never per record",
			u)
	}

	wantA := [][]string{
		{"10", "11", "12", "13"},
		{"20", "21", "22", "23"},
		{"30", "31", "32", "33"},
	}
	for rec, wants := range wantA {
		tag := fmt.Sprintf("REC%d", rec)
		v, ok := reads[tag]
		if !ok {
			t.Errorf("%s: the probe printed no record %d:\n%s", tag, rec, got)
			continue
		}
		if v["lead"] != "1" || v["trail"] != "2" {
			t.Errorf("%s: the bracketing fields moved: lead=%s trail=%s, want 1 and 2", tag, v["lead"], v["trail"])
		}
		for i, want := range wants {
			key := fmt.Sprintf("a%d", i)
			if v[key] != want {
				t.Errorf("%s: items[%d].a=%s, want %s", tag, i, v[key], want)
			}
		}
	}
}
