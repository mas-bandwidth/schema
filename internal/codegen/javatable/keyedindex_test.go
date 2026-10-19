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
//   - TestKeyedFormWireCodeMatchesUnkeyedInTree is a UNIT test: the accessor is
//     ADDITIVE, so every generated file of every table unit, with the key form
//     taken out, is byte-identical to the unkeyed form of the same table,
//     generated in the same tree at test time — the slot arrays, Write, Read
//     and scatter bodies above all, and the keyed message wire against the
//     writer's own IR (docs/SPEC-TABLES.md §2.4).

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
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

// stripKeyedForm takes out every member opened by keyedFormMarker (fixedform.go),
// from its javadoc line through the brace that closes it at the javadoc's own
// indent. The marker can ride on the javadoc's FIRST line (a row's one-liner) or
// on a later line after the formatter has reflowed a fixed record's javadoc, so
// the opening `/**` and its indent are what the closing brace is matched
// against, never the marker line's own leading spaces.
func stripKeyedForm(text string) string {
	var out strings.Builder
	lines := strings.SplitAfter(text, "\n")
	for i := 0; i < len(lines); i++ {
		if !strings.Contains(lines[i], keyedFormMarker) {
			out.WriteString(lines[i])
			continue
		}
		start, indent := i, leadingSpaces(lines[i])
		for j := i; j >= 0; j-- {
			trimmed := strings.TrimLeft(lines[j], " ")
			if strings.HasPrefix(trimmed, "/**") {
				start, indent = j, leadingSpaces(lines[j])
				break
			}
			if !strings.HasPrefix(trimmed, "*") {
				break
			}
		}
		for i = start; i < len(lines); i++ {
			if leadingSpaces(lines[i]) <= indent && strings.TrimSpace(lines[i]) == "}" {
				break
			}
		}
	}
	return out.String()
}

func leadingSpaces(s string) int {
	return len(s) - len(strings.TrimLeft(s, " "))
}

// lowerKeyedFields spells every enum-keyed field as the positional `[E.Max]T`
// array the type wire says it already is (§2.4), on a SECOND parse of the same
// unit. It is how this test generates the UNKEYED form in the same tree: the
// spelling drops the key form, so the fixed record's slot arrays, Write, Read
// and scatter bodies are generated without it. It reports whether the unit had
// any keyed field at all.
func lowerKeyedFields(u *ir.Unit) bool {
	seen := map[*ir.Struct]bool{}
	lowered := false
	var walk func(*ir.Struct)
	walk = func(st *ir.Struct) {
		if st == nil || seen[st] {
			return
		}
		seen[st] = true
		for _, f := range st.Fields {
			if f.KeyEnum != "" && f.KeyEnumRef != nil {
				f.Array = ir.ArrayFixed
				f.ArrayBound = f.KeyEnumRef.Max
				f.KeyEnum = ""
				f.KeyEnumRef = nil
				lowered = true
			}
			if f.MapEntry != nil {
				walk(f.MapEntry)
			}
			if s, ok := f.Type.Ref.(*ir.Struct); ok {
				walk(s)
			}
		}
	}
	for _, st := range u.Tables {
		walk(st)
	}
	for _, st := range u.Structs {
		walk(st)
	}
	return lowered
}

// fixedWireSurface is a fixed record's WIRE CODE: the Value's storage fields
// (slot arrays, defaults) and the body's constant size, Write, Read and scatter
// bodies. The layout hash and byte table below them are deliberately not
// compared: an enum-keyed field's layout tag is keyed while its positional
// lowering's is an array (§2.4), so those bytes differ by design and are not
// what the key form must leave alone.
func fixedWireSurface(text string) string {
	value := braceBlock(text, "public static final class Value {")
	start := strings.Index(text, "public static final int bodyBytes")
	if start < 0 {
		return value
	}
	end := len(text)
	for _, marker := range []string{"public static final long hash", "public static final byte[] layout"} {
		if j := strings.Index(text[start:], marker); j >= 0 && start+j < end {
			end = start + j
		}
	}
	return value + "\n" + text[start:end]
}

// rowWireSurface is a row's WIRE CODE: the slot accessors and count accessors,
// above this record's block and cook descriptors. The descriptors carry the
// keyed/positional kind difference by design, as fixedWireSurface's layout does.
func rowWireSurface(text string) string {
	end := len(text)
	for _, marker := range []string{
		"// this record's BLOCK descriptor",
		"// this record's COOK descriptor",
		"private static final class BlockInfoHolder",
		"private static final class CookInfoHolder",
	} {
		if j := strings.Index(text, marker); j >= 0 && j < end {
			end = j
		}
	}
	return text[:end]
}

// withoutComments drops comment and blank lines: a keyed field's type spelling
// and a positional lowering's are comments, not wire, and §2.4 lets them differ.
func withoutComments(text string) string {
	var out strings.Builder
	for _, line := range strings.SplitAfter(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") ||
			strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		out.WriteString(line)
	}
	return out.String()
}

func braceBlock(text, marker string) string {
	i := strings.Index(text, marker)
	if i < 0 {
		return ""
	}
	open := strings.Index(text[i:], "{")
	if open < 0 {
		return ""
	}
	depth := 0
	for j := i + open; j < len(text); j++ {
		switch text[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[i : j+1]
			}
		}
	}
	return ""
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

// pairCountWidthRe matches the message writer's pair-count announcement for an
// enum-keyed field: `w.put((long) pairsN, width)` (§2.4).
var pairCountWidthRe = regexp.MustCompile(`w\.put\(\(long\) pairs\d*, (\d+)\)`)

// checkKeyedMessageWire holds the keyed form's MESSAGE wire emission against the
// writer's own IR: for every enum-keyed field of every struct, the pair-write
// block must announce each variant at the name slot the standalone computes
// (nameSlotOf(TableWireId(VariantWireName(i)))), announce the pair count at the
// width the field's array shape needs, and address each variant's element by
// its own slot index (§2.4). An off-by-one in any of the three reds this,
// where a byte comparison of the two message forms cannot, because the lowered
// form writes the same elements as a positional loop.
func checkKeyedMessageWire(t *testing.T, rel string, u *ir.Unit, files map[string][]byte) {
	t.Helper()
	vocab := ir.TableVocabulary(u)
	g := &messageGen{unit: u, vocab: vocab, refBits: ir.TableMessageRefBits(len(vocab))}
	widths := map[string][]int{}
	seen := map[*ir.Struct]bool{}
	var walk func(*ir.Struct)
	walk = func(st *ir.Struct) {
		if st == nil || seen[st] {
			return
		}
		seen[st] = true
		for _, f := range st.Fields {
			if f.KeyEnum != "" && f.KeyEnumRef != nil {
				name := messageClass(st.Name) + ".java"
				txt, ok := files[name]
				if !ok {
					continue
				}
				fn := javaName(f.Name)
				for i := range f.KeyEnumRef.Variants {
					keyID := ir.TableWireId(f.KeyEnumRef.VariantWireName(i))
					slot := g.nameSlotOf(keyID)
					want := fmt.Sprintf("w.put(%dL, %d)", slot, g.refBits)
					if !strings.Contains(string(txt), want) {
						t.Errorf("%s %s: keyed pair tag for variant %d (%s) is wrong: want %q (docs/SPEC-TABLES.md §2.4)",
							rel, filepath.ToSlash(name), i, f.KeyEnumRef.VariantWireName(i), want)
					}
					if !strings.Contains(string(txt), fn+"["+strconv.Itoa(i)+"]") {
						t.Errorf("%s %s: keyed element for variant %d (%s) is not written at slot %d (docs/SPEC-TABLES.md §2.4)",
							rel, filepath.ToSlash(name), i, f.KeyEnumRef.VariantWireName(i), i)
					}
				}
				shape := ir.TableFieldShape(f)
				widths[name] = append(widths[name], ir.TableMessageBitsRequired(0, shape.Max))
			}
			if f.MapEntry != nil {
				walk(f.MapEntry)
			}
			if s, ok := f.Type.Ref.(*ir.Struct); ok {
				walk(s)
			}
			if un, ok := f.Type.Ref.(*ir.Union); ok {
				for _, v := range un.Variants {
					if v.F != nil {
						if s, ok := v.F.Type.Ref.(*ir.Struct); ok {
							walk(s)
						}
					}
				}
			}
		}
	}
	for _, st := range u.Tables {
		walk(st)
	}
	for _, st := range u.Structs {
		walk(st)
	}
	for name, want := range widths {
		txt, ok := files[name]
		if !ok {
			continue
		}
		sort.Ints(want)
		var got []int
		for _, m := range pairCountWidthRe.FindAllStringSubmatch(string(txt), -1) {
			n, _ := strconv.Atoi(m[1])
			got = append(got, n)
		}
		sort.Ints(got)
		if len(got) != len(want) {
			t.Errorf("%s %s: keyed pair-count announcements %v do not match the writer's %v (docs/SPEC-TABLES.md §2.4)",
				rel, filepath.ToSlash(name), got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s %s: keyed pair-count announcements %v do not match the writer's %v (docs/SPEC-TABLES.md §2.4)",
					rel, filepath.ToSlash(name), got, want)
				break
			}
		}
	}
}

func TestKeyedFormWireCodeMatchesUnkeyedInTree(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	for _, dir := range keyedUnits(t) {
		rel, _ := filepath.Rel(root, dir)
		keyedUnit := unitFromDir(t, dir)
		keyed, err := Generate(keyedUnit)
		if err != nil {
			t.Errorf("%s ERROR %v", filepath.ToSlash(rel), err)
			continue
		}
		// The unkeyed form is generated in the same tree from a second parse,
		// spelled as the positional [E.Max]T arrays the type wire says keyed
		// arrays already are (§2.4). No committed pin stands between them.
		unkeyedUnit := unitFromDir(t, dir)
		hadKeyed := lowerKeyedFields(unkeyedUnit)
		unkeyed, err := Generate(unkeyedUnit)
		if err != nil {
			t.Fatalf("%s: generating the unkeyed form: %v", filepath.ToSlash(rel), err)
		}
		if hadKeyed {
			checkKeyedMessageWire(t, filepath.ToSlash(rel), keyedUnit, keyed)
		}
		names := make([]string, 0, len(keyed))
		for name := range keyed {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			// The key form's members are ADDITIVE: take them out of both forms
			// and the wire code must be byte-identical.
			keyedText := stripKeyedForm(string(keyed[name]))
			unkeyedText := stripKeyedForm(string(unkeyed[name]))
			switch {
			case strings.HasSuffix(name, "Fixed.java"):
				keyedText = withoutComments(fixedWireSurface(keyedText))
				unkeyedText = withoutComments(fixedWireSurface(unkeyedText))
			case strings.HasSuffix(name, "Row.java"):
				keyedText = withoutComments(rowWireSurface(keyedText))
				unkeyedText = withoutComments(rowWireSurface(unkeyedText))
			default:
				// A keyed table field and its positional lowering ride the TABLE
				// wire differently BY DESIGN (§2.4), so only a unit with no
				// keyed field has an unkeyed spelling to compare here. A keyed
				// unit's message files are held by checkKeyedMessageWire:
				// lowering the keyed fields shifts the unit's vocabulary, so the
				// two message forms cannot be byte-compared pin-free.
				if hadKeyed {
					continue
				}
			}
			if keyedText != unkeyedText {
				t.Errorf("%s %s: keyed wire code differs from the in-tree unkeyed form (docs/SPEC-TABLES.md §2.4)",
					filepath.ToSlash(rel), filepath.ToSlash(name))
			}
		}
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
