package javatable

// THE KEYED ACCESSOR (docs/SPEC-TABLES.md §2.4, docs/PORTING.md M5), Java side.
//
// An enum-keyed array holds one slot per NAMED variant: key k lives at slot
// k-1, None (key 0) names no slot, and neither does a key past E.Max. The
// accessor takes the KEY, refuses both ends by name in every build (one
// unsigned compare, never `assert`, which a -da run drops), and is the only
// place the shift is written. The JVM's own ArrayIndexOutOfBounds is not the
// refusal: it names an index rather than the key, and a row read over a wider
// byte[] does not throw at all.
//
// Two tests hold it:
//
//   - TestKeyedArrayIndexingJava is FUNCTIONAL: it generates a fixed table with
//     a three-variant key, compiles it under the consumer's -Xlint:all -Werror
//     and runs a driver under -da and -ea. Every check names its END (None or
//     Max+1), so the negative control in make/java.mk can require a sabotaged
//     bound to red its own end and only its own.
//   - TestKeyedFormLeavesWireCodeByteIdentical is a UNIT test: the accessor is
//     ADDITIVE, so every generated file of every table unit, with the key form
//     taken out, is byte-identical to the BASE the key form was added on — the
//     slot arrays, Write, Read and scatter bodies above all.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const keyedProbeSchema = `package probe

enum Team { Red, Blue, Green }

fixed table TeamConfig
{
    spawn_count int32 = 4
}

fixed table Scores
{
    per_team [Team]int32
    teams    [Team]TeamConfig
    tag      int32
}
`

// keyedDriver prints one CHECK line per probe. A name's prefix is the END it
// holds: none/ is the lower bound (None, and a negative key below it), max/ is
// the upper (Max+1), and reach/ and place/ are the in-range half.
const keyedDriver = `package probe;

public final class Driver {
    private Driver() {}

    static int failed;

    static void check(String name, String why) {
        if (why == null) {
            System.out.println("CHECK " + name + " ok");
        } else {
            System.out.println("CHECK " + name + " FAIL: " + why);
            failed++;
        }
    }

    static String want(boolean ok, String why) { return ok ? null : why; }

    interface Access { Object run(); }

    // null when the access threw IllegalArgumentException naming the key, the
    // enum and its Max; otherwise what it did instead.
    static String refusal(Access a, int key) {
        try {
            Object got = a.run();
            return "key " + key + " was not refused: the access answered " + got;
        } catch (IllegalArgumentException e) {
            String m = String.valueOf(e.getMessage());
            for (String w : new String[] { "key " + key, "[Team]", "Team.Max", "3" }) {
                if (!m.contains(w)) {
                    return "the refusal of key " + key + " does not name " + w + ": " + m;
                }
            }
            return null;
        } catch (RuntimeException e) {
            return "key " + key + " was refused by " + e.getClass().getName() + " (" + e.getMessage() + "), which names an index and not the key";
        }
    }

    public static void main(String[] args) {
        final ScoresFixed.Value v = new ScoresFixed.Value();

        // the FIXED RECORD's accessors: get, set, and the element instance
        for (int key : new int[] { 0, -1 }) {
            check("none/fixed-get/" + key, refusal(() -> v.perTeam(key), key));
            check("none/fixed-set/" + key, refusal(() -> { v.perTeam(key, 7); return "stored"; }, key));
            check("none/fixed-element/" + key, refusal(() -> v.teams(key), key));
        }
        check("max/fixed-get", refusal(() -> v.perTeam(4), 4));
        check("max/fixed-set", refusal(() -> { v.perTeam(4, 7); return "stored"; }, 4));
        check("max/fixed-element", refusal(() -> v.teams(4), 4));

        v.perTeam(3, 33);
        check("reach/fixed-max", want(v.perTeam(3) == 33 && v.perTeam[2] == 33,
            "key Max wrote slot " + java.util.Arrays.toString(v.perTeam)));

        for (int k = 1; k <= 3; k++) {
            v.perTeam(k, 100 + k);
            v.teams(k).spawnCount = 10 + k;
        }
        for (int k = 1; k <= 3; k++) {
            check("place/fixed-slot/" + k, want(v.perTeam[k - 1] == 100 + k && v.perTeam(k) == 100 + k,
                "key " + k + " is not at slot " + (k - 1) + ": " + java.util.Arrays.toString(v.perTeam)));
            check("place/fixed-element/" + k, want(v.teams(k) == v.teams[k - 1],
                "key " + k + "'s element is not slot " + (k - 1) + "'s instance"));
        }

        // and the SAVED WIRE: slot k-1's bytes carry key k's value
        final byte[] buf = new byte[ScoresFixed.measure(1)];
        ScoresFixed.save(new ScoresFixed.Value[] { v }, 1, buf);
        final int body = ScoresFixed.headerBytes + 8;
        for (int k = 1; k <= 3; k++) {
            int scalar = TableFixed.get32(buf, body + (k - 1) * 4);
            check("place/wire-scalar/" + k, want(scalar == 100 + k,
                "the saved slot " + (k - 1) + " holds " + scalar + ", want key " + k + "'s " + (100 + k)));
            int nested = TableFixed.get32(buf, body + 3 * 4 + (k - 1) * TeamConfigFixed.bodyBytes);
            check("place/wire-element/" + k, want(nested == 10 + k,
                "the saved element slot " + (k - 1) + " holds " + nested + ", want key " + k + "'s " + (10 + k)));
        }

        // the ROW accessors (block and cook share them): the key form beside
        // the slot form, over a byte[] wider than the row so a missing bound
        // reads silently rather than throwing
        final int at = 8;
        final byte[] row = new byte[at + ScoresRow.size + 8];
        for (int k = 1; k <= 3; k++) {
            int off = at + ScoresRow.offsets[0] + (k - 1) * 4;
            java.nio.ByteBuffer.wrap(row).order(java.nio.ByteOrder.LITTLE_ENDIAN).putInt(off, 200 + k);
        }
        for (int key : new int[] { 0, -1 }) {
            check("none/row-get/" + key, refusal(() -> ScoresRow.perTeamByKey(row, at, key), key));
            check("none/row-element/" + key, refusal(() -> ScoresRow.teamsAtKey(at, key), key));
        }
        check("max/row-get", refusal(() -> ScoresRow.perTeamByKey(row, at, 4), 4));
        check("max/row-element", refusal(() -> ScoresRow.teamsAtKey(at, 4), 4));
        check("reach/row-max", want(ScoresRow.perTeamByKey(row, at, 3) == 203,
            "key Max read " + ScoresRow.perTeamByKey(row, at, 3)));
        for (int k = 1; k <= 3; k++) {
            check("place/row-slot/" + k, want(ScoresRow.perTeamByKey(row, at, k) == ScoresRow.perTeam(row, at, k - 1),
                "key " + k + " does not read slot " + (k - 1)));
            check("place/row-element/" + k, want(ScoresRow.teamsAtKey(at, k) == ScoresRow.teamsAt(at, k - 1),
                "key " + k + "'s element offset is not slot " + (k - 1) + "'s"));
        }

        System.out.println(failed == 0 ? "keyed: ok" : "keyed: " + failed + " failed");
        System.exit(failed == 0 ? 0 : 1);
    }
}
`

// TestKeyedArrayIndexingJava holds §2.4's accessor at both ends, in the
// release shape (-da) and the checked one (-ea).
func TestKeyedArrayIndexingJava(t *testing.T) {
	t.Parallel()
	var javac, javaBin string
	t.Run("jdk", func(t *testing.T) { javac, javaBin = javaTools(t) })
	if javac == "" {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatal("SCHEMA_REQUIRE_CORPUS is set and TestKeyedArrayIndexingJava found no JDK: set JAVAC and JAVA (make tables-java-keyed passes them)")
		}
		t.Skip("TestKeyedArrayIndexingJava: no JDK (set JAVAC/JAVA)")
	}
	files, err := Generate(unitFrom(t, keyedProbeSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "probe")
	classes := filepath.Join(dir, "classes")
	for _, d := range []string{src, classes} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"--release", "17", "-Xlint:all", "-Werror", "-d", classes}
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(src, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		args = append(args, p)
	}
	driver := filepath.Join(src, "Driver.java")
	if err := os.WriteFile(driver, []byte(keyedDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	args = append(args, driver)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: the generated keyed accessor does not compile beside the driver: %v\n%s", err, out)
	}
	for _, mode := range []string{"-da", "-ea"} {
		out, err := exec.Command(javaBin, mode, "-cp", classes, "probe.Driver").CombinedOutput()
		checks := 0
		sc := bufio.NewScanner(strings.NewReader(string(out)))
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "CHECK ") {
				continue
			}
			checks++
			name, rest, _ := strings.Cut(strings.TrimPrefix(line, "CHECK "), " ")
			if rest == "ok" {
				continue
			}
			end := "in-range"
			switch {
			case strings.HasPrefix(name, "none/"):
				end = "None end"
			case strings.HasPrefix(name, "max/"):
				end = "Max+1 end"
			}
			t.Errorf("java %s: keyed %s: %s: %s", mode, end, name, strings.TrimPrefix(rest, "FAIL: "))
		}
		if checks == 0 || (err != nil && !t.Failed()) {
			t.Fatalf("java %s: the driver ran %d checks and exited %v\n%s", mode, checks, err, out)
		}
	}
}

// keyedGolden is every table unit's generated Java at the BASE the key form was
// added on, one sha256 per file. Regenerate with SCHEMA_UPDATE_KEYED_GOLDEN=1
// only when a change MEANS to move the wire code, and say so in its review.
const keyedGolden = "testdata/keyedindex_base.sha256"

// stripKeyedForm takes out every member opened by keyedFormMarker (fixedform.go),
// from its javadoc line through the brace that closes it at the javadoc's own
// indent.
func stripKeyedForm(text string) string {
	var out strings.Builder
	lines := strings.SplitAfter(text, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if !strings.Contains(line, keyedFormMarker) {
			out.WriteString(line)
			continue
		}
		indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
		for i++; i < len(lines) && strings.TrimRight(lines[i], "\n") != indent+"}"; i++ {
		}
	}
	return out.String()
}

// keyedUnits is every directory under tables/ that holds a .schema unit.
func keyedUnits(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..", "..", "tables")
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".schema") {
			seen[filepath.Dir(p)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for d := range seen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs
}

func TestKeyedFormLeavesWireCodeByteIdentical(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	var got []string
	for _, dir := range keyedUnits(t) {
		rel, _ := filepath.Rel(root, dir)
		files, err := Generate(unitFromDir(t, dir))
		if err != nil {
			sum := sha256.Sum256([]byte(err.Error()))
			got = append(got, fmt.Sprintf("%s ERROR %s", filepath.ToSlash(rel), hex.EncodeToString(sum[:8])))
			continue
		}
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			sum := sha256.Sum256([]byte(stripKeyedForm(string(files[name]))))
			got = append(got, fmt.Sprintf("%s %s %s", filepath.ToSlash(rel), name, hex.EncodeToString(sum[:])))
		}
	}
	text := strings.Join(got, "\n") + "\n"
	if os.Getenv("SCHEMA_UPDATE_KEYED_GOLDEN") != "" {
		if err := os.WriteFile(keyedGolden, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(keyedGolden)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) == text {
		return
	}
	wantSet := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSpace(string(want)), "\n") {
		wantSet[l] = true
	}
	for _, l := range got {
		if !wantSet[l] {
			t.Errorf("generated %s moved against BASE with the key form taken out: the keyed accessor must be additive (docs/SPEC-TABLES.md §2.4)", l)
		}
	}
	if !t.Failed() {
		t.Errorf("the generated file set shrank against BASE (%s)", keyedGolden)
	}
}

// TestStripKeyedFormTakesOnlyTheKeyForm is the stripper's own control: a
// member opened by the marker goes, through its closing brace, and the lines
// either side of it stay exactly.
func TestStripKeyedFormTakesOnlyTheKeyForm(t *testing.T) {
	t.Parallel()
	in := "        public final int[] a = new int[3];\n" +
		"        /** [E]int32 BY KEY (docs/SPEC-TABLES.md §2.4): x */\n" +
		"        public int a(int key) {\n" +
		"            if (x) {\n" +
		"            }\n" +
		"            return a[key - 1];\n" +
		"        }\n" +
		"        public int b;\n"
	want := "        public final int[] a = new int[3];\n        public int b;\n"
	if got := stripKeyedForm(in); got != want {
		t.Fatalf("stripKeyedForm = %q, want %q", got, want)
	}
}
