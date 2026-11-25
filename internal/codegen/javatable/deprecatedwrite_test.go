package javatable

// A DEPRECATED VARIANT IS NEVER WRITTEN, BY ANY JAVA WRITER (docs/SPEC-TABLES.md
// §2.15 WRITE). Java's writer checks are debug asserts (SPEC.md §5.1), so the
// probe runs under -ea and wants each writer to throw AssertionError on the
// deprecated variant `Old` (ordinal 2) and to write the live variant `Fierce`
// (ordinal 3): the variable save, the fixed save and the message save, on the
// scalar field MoodField.mood and the array element Moods.row[0]
// (tables/examples). Until 2026-11-24 only the JSON writer held the rule; the
// other three wrote `Old`, which every reader reads back as the default, and
// only TestKeyedFormWireCodeMatchesUnkeyedInTree's Go oracle noticed (its
// corpus took the value the writers accepted, and the Go codec re-encoded
// the default). Each writer is one CHECK line, so a writer that loses the
// assert reds its own line.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const deprecatedWriteDriver = `package tabledemo;

public final class DeprecatedWriteDriver {
    interface Write { Object run() throws Exception; }

    // the writer must throw AssertionError on the deprecated variant (-ea)
    static void refuses(String label, Write w) {
        String got;
        try {
            Object r = w.run();
            got = "wrote (" + r + ")";
        } catch (AssertionError e) {
            System.out.println("CHECK " + label + " ok");
            return;
        } catch (Throwable e) {
            got = "threw " + e;
        }
        System.out.println("CHECK " + label + " FAIL " + got);
    }

    // and write the live one
    static void writes(String label, Write w) {
        try {
            Object r = w.run();
            boolean ok = r instanceof byte[] ? ((byte[]) r).length > 0 : r instanceof Integer && ((Integer) r) > 0;
            System.out.println("CHECK " + label + (ok ? " ok" : " FAIL refused (" + r + ")"));
        } catch (Throwable e) {
            System.out.println("CHECK " + label + " FAIL threw " + e);
        }
    }

    static MoodFieldVariable.Value scalarVar(int m) { MoodFieldVariable.Value v = new MoodFieldVariable.Value(); v.mood = m; return v; }
    static MoodsVariable.Value elemVar(int m) { MoodsVariable.Value v = new MoodsVariable.Value(); v.row.add(m); return v; }
    static MoodFieldFixed.Value scalarFixed(int m) { MoodFieldFixed.Value v = new MoodFieldFixed.Value(); v.mood = (byte) m; return v; }
    static MoodsFixed.Value elemFixed(int m) { MoodsFixed.Value v = new MoodsFixed.Value(); v.row[0] = (byte) m; v.rowCount = 1; return v; }

    public static void main(String[] args) {
        refuses("variable:scalar", () -> MoodFieldVariable.save(scalarVar(2)));
        writes("variable:scalar-live", () -> MoodFieldVariable.save(scalarVar(3)));
        refuses("variable:element", () -> MoodsVariable.save(elemVar(2)));
        writes("variable:element-live", () -> MoodsVariable.save(elemVar(3)));

        refuses("fixed:scalar", () -> MoodFieldFixed.save(new MoodFieldFixed.Value[] { scalarFixed(2) }, 1, new byte[4096]));
        writes("fixed:scalar-live", () -> MoodFieldFixed.save(new MoodFieldFixed.Value[] { scalarFixed(3) }, 1, new byte[4096]));
        refuses("fixed:element", () -> MoodsFixed.save(new MoodsFixed.Value[] { elemFixed(2) }, 1, new byte[4096]));
        writes("fixed:element-live", () -> MoodsFixed.save(new MoodsFixed.Value[] { elemFixed(3) }, 1, new byte[4096]));

        refuses("message:scalar", () -> MoodFieldMessage.saveMessages(new MoodFieldFixed.Value[] { scalarFixed(2) }, 1, new byte[4096], new TableVocabulary.Report()));
        writes("message:scalar-live", () -> MoodFieldMessage.saveMessages(new MoodFieldFixed.Value[] { scalarFixed(3) }, 1, new byte[4096], new TableVocabulary.Report()));
        refuses("message:element", () -> MoodsMessage.saveMessages(new MoodsFixed.Value[] { elemFixed(2) }, 1, new byte[4096], new TableVocabulary.Report()));
        writes("message:element-live", () -> MoodsMessage.saveMessages(new MoodsFixed.Value[] { elemFixed(3) }, 1, new byte[4096], new TableVocabulary.Report()));
    }
}
`

var deprecatedWriteLabels = []string{
	"variable:scalar", "variable:scalar-live", "variable:element", "variable:element-live",
	"fixed:scalar", "fixed:scalar-live", "fixed:element", "fixed:element-live",
	"message:scalar", "message:scalar-live", "message:element", "message:element-live",
}

// runDeprecatedWriteProbe generates tables/examples, applies the edits (none
// for the gate) to every generated file whose name ends with suffix, compiles
// the unit with the driver and answers each check's verdict ("" for a pass).
func runDeprecatedWriteProbe(t *testing.T, suffix string, edits [][2]string) map[string]string {
	t.Helper()
	javac, java := javaTools(t)
	u := unitFromDir(t, "../../../tables/examples")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range packet {
		if _, ok := files[name]; !ok {
			files[name] = data
		}
	}
	for _, e := range edits {
		patched := 0
		for name, data := range files {
			if strings.HasSuffix(name, suffix) && strings.Contains(string(data), e[0]) {
				files[name] = []byte(strings.ReplaceAll(string(data), e[0], e[1]))
				patched++
			}
		}
		if patched == 0 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage patched nothing: %q is in no *%s file", e[0], suffix)
		}
	}
	files["DeprecatedWriteDriver.java"] = []byte(deprecatedWriteDriver)
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	sort.Strings(sources)
	args := append([]string{"--release", "17", "-nowarn", "-d", dir}, sources...)
	if out, err := testproc.Command(t, javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := testproc.Command(t, java, "-ea", "-cp", dir, u.Package+".DeprecatedWriteDriver").CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		rest, ok := strings.CutPrefix(line, "CHECK ")
		if !ok {
			continue
		}
		label, verdict, _ := strings.Cut(rest, " ")
		got[label] = strings.TrimPrefix(strings.TrimPrefix(verdict, "ok"), "FAIL ")
	}
	for _, label := range deprecatedWriteLabels {
		if _, ok := got[label]; !ok {
			t.Fatalf("the probe made no %s check\n%s", label, out)
		}
	}
	if len(got) != len(deprecatedWriteLabels) {
		t.Fatalf("the probe made %d checks, deprecatedWriteLabels names %d\n%s", len(got), len(deprecatedWriteLabels), out)
	}
	return got
}

// TestJavaDeprecatedVariantNeverWritten is the gate: every writer refuses Old
// and writes Fierce.
func TestJavaDeprecatedVariantNeverWritten(t *testing.T) {
	t.Parallel()
	got := runDeprecatedWriteProbe(t, ".java", nil)
	for _, label := range deprecatedWriteLabels {
		if why := got[label]; why != "" {
			t.Errorf("deprecated %s: %s (docs/SPEC-TABLES.md §2.15 WRITE)", label, why)
		}
	}
}

// TestJavaDeprecatedVariantNegativeControl drops each writer's deprecated
// assert in turn and wants exactly that writer's refusal lines red: one form
// cannot stand in for another.
func TestJavaDeprecatedVariantNegativeControl(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		form, suffix string
		edits        [][2]string
	}{
		// the variable checkWrite's one assert per enum field
		{"variable", "Variable.java", [][2]string{{"assert v.mood != 2", "assert true"}, {"assert e != 2", "assert true"}}},
		// the fixed checkWrite's slot condition, scalar and element
		{"fixed", "Fixed.java", [][2]string{{"&& v.mood != 2", ""}, {"&& v.row[i] != 2", ""}}},
		// the message checkWrite's walk
		{"message", "Message.java", [][2]string{{"assert v.mood != 2;", "assert true;"}, {"assert v.row[i2] != 2;", "assert true;"}}},
	} {
		t.Run(c.form, func(t *testing.T) {
			t.Parallel()
			got := runDeprecatedWriteProbe(t, c.suffix, c.edits)
			for _, label := range deprecatedWriteLabels {
				form, which, _ := strings.Cut(label, ":")
				mustRed := form == c.form && !strings.HasSuffix(which, "-live")
				switch {
				case mustRed && got[label] == "":
					t.Errorf("NEGATIVE CONTROL FAILED: with the %s writer's deprecated assert gone, %s stayed green", c.form, label)
				case !mustRed && got[label] != "":
					t.Errorf("NEGATIVE CONTROL FAILED: the %s sabotage also redded %s: %s", c.form, label, got[label])
				}
			}
			t.Logf("negative control: with the %s writer's deprecated assert gone, its two refusal checks go red and nothing else", c.form)
		})
	}
}
