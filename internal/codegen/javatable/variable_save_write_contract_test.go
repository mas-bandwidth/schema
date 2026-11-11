package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
)

// TestJavaVariableSaveWriteContract holds the variable-form save to the Java
// row of the write contract (docs/SPEC.md §5.1): rows 1-5 and 8 are a debug
// assert, so under -ea the save throws AssertionError and without it the
// save does not throw (an assert leg's release build is not required to
// check a write); rows 6, 7 and 10 are refused (null) in both builds, row 10
// on a required field. Every refused case has a lawful twin at the same boundary that
// saves and loads back equal in both builds, so a save that refused
// everything fails. Each row is held on every field shape the variable writer
// handles: a scalar, an array element, a map key and value, a union arm's
// payload, an inline table, and a table reached through a pointer.
func TestJavaVariableSaveWriteContract(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)
	u := unitFrom(t, variableSaveWriteContractSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
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
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	driver := filepath.Join(dir, "WriteContract.java")
	if err := os.WriteFile(driver, []byte(variableSaveWriteContractDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driver)
	sort.Strings(sources)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	// the verdict each case owes, per build: debug (-ea) and release
	type owed struct{ debug, release string }
	refusedDebug := owed{"assert", "no-throw"} // rows 1-5 and 8
	refusedAlways := owed{"null", "null"}      // rows 6, 7 and 10, as today
	lawful := owed{"equal", "equal"}           // every twin
	want := map[string]owed{
		// row 1: an integer off its kind's range (int8 in an int) and off a declared range
		"r1-scalar-int8-over": refusedDebug, "r1-scalar-int8-edge": lawful,
		"r1-scalar-int8-under": refusedDebug, "r1-scalar-int8-low": lawful,
		"r1-scalar-ranged-over": refusedDebug, "r1-scalar-ranged-edge": lawful,
		"r1-scalar-ranged-under": refusedDebug, "r1-scalar-ranged-low": lawful,
		"r1-element-over": refusedDebug, "r1-element-edge": lawful,
		"r1-map-value-over": refusedDebug, "r1-map-value-edge": lawful,
		"r1-arm-over": refusedDebug, "r1-arm-edge": lawful,
		"r1-inline-over": refusedDebug, "r1-inline-edge": lawful,
		"r1-pointer-over": refusedDebug, "r1-pointer-edge": lawful,
		// row 2: bits(4) past its width
		"r2-bits-over": refusedDebug, "r2-bits-edge": lawful,
		// row 3: a counted array's count past its Max
		"r3-count-over": refusedDebug, "r3-count-edge": lawful,
		// row 4: a length past N
		"r4-string-over": refusedDebug, "r4-string-edge": lawful,
		"r4-bytes-over": refusedDebug, "r4-bytes-edge": lawful,
		// a text list's element past N is refused in every build today, by the
		// graph the save builds first; it keeps that answer
		"r4-element-over": refusedAlways, "r4-element-edge": lawful,
		"r4-map-key-over": refusedDebug, "r4-map-key-edge": lawful,
		"r4-arm-over": refusedDebug, "r4-arm-edge": lawful,
		"r4-pointer-over": refusedDebug, "r4-pointer-edge": lawful,
		// row 5: an enum ordinal outside its variant set
		"r5-enum-over": refusedDebug, "r5-enum-far": refusedDebug, "r5-enum-last": lawful,
		"r5-enum-negative": refusedDebug,
		// row 8: ill-formed text
		"r8-lone-surrogate": refusedDebug, "r8-surrogate-pair": lawful,
		"r8-zero": refusedDebug, "r8-no-zero": lawful,
		"r8-element-zero": refusedDebug, "r8-map-key-surrogate": refusedDebug,
		"r8-arm-zero": refusedDebug, "r8-inline-zero": refusedDebug,
		// row 6: a union tag outside its arm set; row 7: a NaN or an infinity;
		// a cycle through pointers is refused with them
		"r6-tag-over": refusedAlways, "r6-tag-last": lawful,
		"r7-nan": refusedAlways, "r7-infinity": refusedAlways, "r7-finite": lawful,
		// row 10: a compressed float off its range, on a required field
		"r10-over": refusedAlways, "r10-under": refusedAlways, "r10-edge": lawful,
		"cycle": refusedAlways,
	}
	for _, build := range []struct {
		name string
		args []string
	}{
		{"debug", []string{"-ea"}},
		{"release", nil},
	} {
		run := append(append([]string{}, build.args...), "-cp", classes, "WriteContract")
		out, err := exec.Command(javaBin, run...).CombinedOutput()
		if err != nil {
			t.Fatalf("java (%s): %v\n%s", build.name, err, out)
		}
		got := map[string]string{}
		for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
			name, verdict, ok := strings.Cut(line, " ")
			if !ok {
				t.Fatalf("%s: driver line %q", build.name, line)
			}
			got[name] = verdict
		}
		for name, w := range want {
			expect := w.debug
			if build.name == "release" {
				expect = w.release
			}
			verdict, ok := got[name]
			switch {
			case !ok:
				t.Errorf("%s %s: the driver ran no such case", build.name, name)
			case expect == "no-throw" && verdict == "assert":
				t.Errorf("%s %s: %s, want no throw: the check is a debug assert", build.name, name, verdict)
			case expect != "no-throw" && verdict != expect:
				t.Errorf("%s %s: %s, want %s", build.name, name, verdict, expect)
			}
		}
		if len(got) != len(want) {
			t.Errorf("%s: the driver ran %d cases, the test names %d", build.name, len(got), len(want))
		}
	}
}

const variableSaveWriteContractSchema = `package wcheck

enum Hue { Red, Green, Blue }

table Leaf
{
    small int8
    name  string(16)
}

union Arm
{
    text  string(16)
    count int32 | min = -5, max = 5
}

table Doc
{
    small  int8
    ranged int32 | min = -5, max = 5
    nibble bits(4)
    hue    Hue
    title  string(16)
    blob   bytes(8)
    tags   [..3]int8
    names  []string(16)
    keyed  map[string(8)]int8
    arm    Arm
    inner  Leaf
    next   *Doc
    ratio  float32
    level  float32 | min = 0, max = 100, resolution = 0.01, required
}
`

const variableSaveWriteContractDriver = `import wcheck.*;
import java.util.function.Consumer;
import java.util.function.Predicate;

public final class WriteContract {
    // case runs one save and prints its verdict: "assert" (AssertionError),
    // "null" (the save refused), "equal" (written and loaded back with the
    // same value), "wrote" (written, and the load does not give it back).
    static void check(String name, Consumer<DocVariable.Value> set, Predicate<DocVariable.Value> same) {
        DocVariable.Value v = new DocVariable.Value();
        set.accept(v);
        String verdict;
        try {
            byte[] wire = DocVariable.save(v);
            if (wire == null) {
                verdict = "null";
            } else {
                TableVariable.Report report = new TableVariable.Report();
                DocVariable.Value back = DocVariable.load(wire, report);
                verdict = back != null && same.test(back) ? "equal" : "wrote";
            }
        } catch (AssertionError e) {
            verdict = "assert";
        }
        System.out.println(name + " " + verdict);
    }

    static DocKeyedEntryVariable.Value entry(String key, int value) {
        DocKeyedEntryVariable.Value e = new DocKeyedEntryVariable.Value();
        e.key = key;
        e.value = value;
        return e;
    }

    static DocNamesTextVariable.Value text(String s) {
        DocNamesTextVariable.Value e = new DocNamesTextVariable.Value();
        e.value = s;
        return e;
    }

    static String repeat(int n) { return "x".repeat(n); }

    public static void main(String[] args) {
        // row 1
        check("r1-scalar-int8-over", v -> v.small = 300, b -> false);
        check("r1-scalar-int8-edge", v -> v.small = 127, b -> b.small == 127);
        check("r1-scalar-int8-under", v -> v.small = -129, b -> false);
        check("r1-scalar-int8-low", v -> v.small = -128, b -> b.small == -128);
        check("r1-scalar-ranged-over", v -> v.ranged = 6, b -> false);
        check("r1-scalar-ranged-edge", v -> v.ranged = 5, b -> b.ranged == 5);
        check("r1-scalar-ranged-under", v -> v.ranged = -6, b -> false);
        check("r1-scalar-ranged-low", v -> v.ranged = -5, b -> b.ranged == -5);
        check("r1-element-over", v -> { v.tags.add(1); v.tags.add(300); }, b -> false);
        check("r1-element-edge", v -> { v.tags.add(1); v.tags.add(127); },
            b -> b.tags.size() == 2 && b.tags.get(1) == 127);
        check("r1-map-value-over", v -> v.keyed.add(entry("k", 300)), b -> false);
        check("r1-map-value-edge", v -> v.keyed.add(entry("k", 127)),
            b -> b.keyed.size() == 1 && b.keyed.get(0).value == 127);
        check("r1-arm-over", v -> { v.arm.tag = 2; v.arm.count = 6; }, b -> false);
        check("r1-arm-edge", v -> { v.arm.tag = 2; v.arm.count = 5; },
            b -> b.arm.tag == 2 && b.arm.count == 5);
        check("r1-inline-over", v -> v.inner.small = 300, b -> false);
        check("r1-inline-edge", v -> v.inner.small = 127, b -> b.inner.small == 127);
        check("r1-pointer-over", v -> { v.next = new DocVariable.Value(); v.next.small = 300; }, b -> false);
        check("r1-pointer-edge", v -> { v.next = new DocVariable.Value(); v.next.small = 127; },
            b -> b.next != null && b.next.small == 127);
        // row 2
        check("r2-bits-over", v -> v.nibble = 16, b -> false);
        check("r2-bits-edge", v -> v.nibble = 15, b -> b.nibble == 15);
        // row 3
        check("r3-count-over", v -> { for (int i = 0; i < 4; i++) v.tags.add(i + 1); }, b -> false);
        check("r3-count-edge", v -> { for (int i = 0; i < 3; i++) v.tags.add(i + 1); },
            b -> b.tags.size() == 3 && b.tags.get(2) == 3);
        // row 4
        check("r4-string-over", v -> v.title = repeat(17), b -> false);
        check("r4-string-edge", v -> v.title = repeat(16), b -> b.title.equals(repeat(16)));
        check("r4-bytes-over", v -> v.blob = new byte[9], b -> false);
        check("r4-bytes-edge", v -> v.blob = new byte[] {1, 2, 3, 4, 5, 6, 7, 8},
            b -> java.util.Arrays.equals(b.blob, new byte[] {1, 2, 3, 4, 5, 6, 7, 8}));
        check("r4-element-over", v -> v.names.add(text(repeat(17))), b -> false);
        check("r4-element-edge", v -> v.names.add(text(repeat(16))),
            b -> b.names.size() == 1 && b.names.get(0).value.equals(repeat(16)));
        check("r4-map-key-over", v -> v.keyed.add(entry(repeat(9), 1)), b -> false);
        check("r4-map-key-edge", v -> v.keyed.add(entry(repeat(8), 1)),
            b -> b.keyed.size() == 1 && b.keyed.get(0).key.equals(repeat(8)));
        check("r4-arm-over", v -> { v.arm.tag = 1; v.arm.text = repeat(17); }, b -> false);
        check("r4-arm-edge", v -> { v.arm.tag = 1; v.arm.text = repeat(16); },
            b -> b.arm.tag == 1 && b.arm.text.equals(repeat(16)));
        check("r4-pointer-over", v -> { v.next = new DocVariable.Value(); v.next.title = repeat(17); }, b -> false);
        check("r4-pointer-edge", v -> { v.next = new DocVariable.Value(); v.next.title = repeat(16); },
            b -> b.next != null && b.next.title.equals(repeat(16)));
        // row 5
        check("r5-enum-over", v -> v.hue = 4, b -> false);
        check("r5-enum-far", v -> v.hue = 9, b -> false);
        check("r5-enum-negative", v -> v.hue = -1, b -> false);
        check("r5-enum-last", v -> v.hue = 3, b -> b.hue == 3);
        // row 8
        check("r8-lone-surrogate", v -> v.title = "a\uD800b", b -> false);
        check("r8-surrogate-pair", v -> v.title = "a😀b", b -> b.title.equals("a😀b"));
        check("r8-zero", v -> v.title = "a\u0000b", b -> false);
        check("r8-no-zero", v -> v.title = "ab", b -> b.title.equals("ab"));
        check("r8-element-zero", v -> v.names.add(text("a\u0000b")), b -> false);
        check("r8-map-key-surrogate", v -> v.keyed.add(entry("a\uDC00", 1)), b -> false);
        check("r8-arm-zero", v -> { v.arm.tag = 1; v.arm.text = "a\u0000b"; }, b -> false);
        check("r8-inline-zero", v -> v.inner.name = "a\u0000b", b -> false);
        // row 6
        check("r6-tag-over", v -> v.arm.tag = 3, b -> false);
        check("r6-tag-last", v -> { v.arm.tag = 2; v.arm.count = -5; },
            b -> b.arm.tag == 2 && b.arm.count == -5);
        // row 7
        check("r7-nan", v -> v.ratio = Float.NaN, b -> false);
        check("r7-infinity", v -> v.ratio = Float.POSITIVE_INFINITY, b -> false);
        check("r7-finite", v -> v.ratio = 1.5f, b -> b.ratio == 1.5f);
        // row 10
        check("r10-over", v -> v.level = 100.5f, b -> false);
        check("r10-under", v -> v.level = -0.5f, b -> false);
        check("r10-edge", v -> v.level = 100f, b -> b.level == 100f);
        // a cycle through pointers
        check("cycle", v -> v.next = v, b -> false);
    }
}
`
