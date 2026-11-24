package javatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED
// (docs/SPEC-TABLES.md §3.4): a load's result never depends on what the value
// held before. test/tables/FixedSlack.schema's header is the recipe: load file
// A (every counted array at its bound, every slot 77) into a value, then file B
// (cells count 1, cells[0].subs count 1, nums count 1) into THE SAME value; the
// value must read exactly as a fresh value loaded from B. Every element default
// in the fixture is non-zero, so a slot left holding the 77 (or holding a zero
// where the declared default is 7 or 3) is told apart. Both plans are run: the
// identity reader, and a newer reader that reads the same files through its
// lineage plan. The refused load into a value that held A must still leave it
// equal to a fresh value (ruling R4, docs/SPEC-TABLES.md §6.5).
func TestJavaFixedSlackPastCountHoldsDefaults(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac and java (it runs the generated reader over the FixedSlack recipe)")
	javac, javaBin := javaToolsRequired(t)

	u := versioningSchemaUnit(t, "FixedSlack.schema")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	pkg := javaPackageOf(u)
	identity := buildSlackProbe(t, javac, pkg, files)

	src, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "FixedSlack.schema"))
	if err != nil {
		t.Fatal(err)
	}
	newerSrc := strings.Replace(string(src), "    nums  [..4]int32\n}", "    nums  [..4]int32\n    extra int32 = 0\n}", 1)
	if newerSrc == string(src) {
		t.Fatal("could not append the newer reader's field to SlackRoot")
	}
	oldEntry, ok := FixedLineageOf(u, "SlackRoot")
	if !ok {
		t.Fatal("no fixed SlackRoot writer layout")
	}
	lineageFiles, err := GenerateLineage(unitFrom(t, newerSrc), map[string][]FixedLineageEntry{"SlackRoot": {oldEntry}})
	if err != nil {
		t.Fatal(err)
	}
	lineage := buildSlackProbe(t, javac, pkg, lineageFiles)

	// The files are written once, by the writer (the identity classes).
	dir := t.TempDir()
	if out, err := testproc.Command(t, javaBin, "-ea", "-cp", identity, "SlackProbe", "write", dir).CombinedOutput(); err != nil {
		t.Fatalf("java SlackProbe write: %v\n%s", err, out)
	}

	for _, reader := range []struct{ name, classes string }{
		{"identity", identity},
		{"lineage", lineage},
	} {
		t.Run(reader.name, func(t *testing.T) {
			t.Parallel()
			out, err := testproc.Command(t, javaBin, "-ea", "-cp", reader.classes, "SlackProbe", "read", dir).CombinedOutput()
			if err != nil {
				t.Fatalf("java SlackProbe read: %v\n%s", err, out)
			}
			dumps := map[string]map[string]string{}
			for line := range strings.SplitSeq(string(out), "\n") {
				tag, rest, ok := strings.Cut(line, " ")
				if !ok {
					continue
				}
				k, v, ok := strings.Cut(rest, "=")
				if !ok {
					continue
				}
				if dumps[tag] == nil {
					dumps[tag] = map[string]string{}
				}
				dumps[tag][k] = v
			}
			for _, tag := range []string{"LOAD", "HELD", "REUSED", "FRESH", "REFUSED", "NEW"} {
				if len(dumps[tag]) == 0 {
					t.Fatalf("probe printed no %s:\n%s", tag, out)
				}
			}
			if got := dumps["LOAD"]; got["a"] != "1" || got["b"] != "1" || got["fresh"] != "1" || got["bad"] != "-1" {
				t.Fatalf("load results a=%s b=%s fresh=%s bad=%s, want 1 1 1 -1", got["a"], got["b"], got["fresh"], got["bad"])
			}
			// the value really held A's 77s past B's counts before B landed
			for _, k := range []string{"cells[2].id", "cells[0].subs[1].v", "nums[3]"} {
				if got := dumps["HELD"][k]; got != "77" {
					t.Fatalf("after loading A, %s=%q, want 77: the recipe did not set the value up", k, got)
				}
			}
			want := map[string]string{
				"cellsCount":         "1",
				"cells[0].id":        "77",
				"cells[0].subsCount": "1",
				"cells[0].subs[0].v": "77",
				"cells[0].subs[1].v": "3",
				"cells[1].id":        "7",
				"cells[1].subsCount": "0",
				"cells[1].subs[0].v": "3",
				"cells[1].subs[1].v": "3",
				"cells[2].id":        "7",
				"cells[2].subsCount": "0",
				"cells[2].subs[0].v": "3",
				"cells[2].subs[1].v": "3",
				"numsCount":          "1",
				"nums[0]":            "77",
				"nums[1]":            "0",
				"nums[2]":            "0",
				"nums[3]":            "0",
			}
			for k, w := range want {
				if got := dumps["REUSED"][k]; got != w {
					t.Errorf("%s reader, lawful load of B into a value that held A: %s=%q, want %q (docs/SPEC-TABLES.md §3.4, THE SLOTS PAST A COUNT AFTER ANY LOAD)", reader.name, k, got, w)
				}
				if got := dumps["FRESH"][k]; got != w {
					t.Errorf("%s reader, B into a fresh value: %s=%q, want %q", reader.name, k, got, w)
				}
			}
			if d := slackDiff(dumps["FRESH"], dumps["REUSED"]); d != "" {
				t.Errorf("%s reader: a reused value does not read as a fresh one: %s", reader.name, d)
			}
			if d := slackDiff(dumps["NEW"], dumps["REFUSED"]); d != "" {
				t.Errorf("%s reader: a refused load left the value off its declared defaults (R4): %s", reader.name, d)
			}
		})
	}
}

// The fast pin: no toolchain. The generated scatter of every type in
// FixedSlack.schema resets each counted array's slots past the count: object
// slots in place, primitive slots filled with the element's zero (an array
// element takes no declared default).
func TestJavaFixedSlackScatterResetsPastCount(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "FixedSlack.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files := generate(t, string(src))
	for _, c := range []struct{ file, slot string }{
		{"SlackRootFixed.java", "cells"},
		{"SlackCellFixed.java", "subs"},
	} {
		scatter := methodBody(string(files[c.file]), "boolean live, boolean checkDead)")
		if scatter == "" {
			t.Fatalf("no scatter in %s", c.file)
		}
		flat := strings.Join(strings.Fields(scatter), " ")
		loop := fmt.Sprintf("= v.%sCount; slackI", c.slot)
		if !strings.Contains(flat, loop) || !strings.Contains(flat, "< v."+c.slot+".length; slackI") ||
			!strings.Contains(flat, "v."+c.slot+"[slackI") || !strings.Contains(flat, "].reset();") {
			t.Fatalf("%s: the %s slots past the count are not reset in place:\n%s", c.file, c.slot, scatter)
		}
	}
	root := strings.Join(strings.Fields(methodBody(string(files["SlackRootFixed.java"]), "boolean live, boolean checkDead)")), " ")
	if !strings.Contains(root, "java.util.Arrays.fill(v.nums, v.numsCount, v.nums.length, 0)") &&
		!strings.Contains(root, "java.util.Arrays.fill( v.nums, v.numsCount, v.nums.length, 0)") {
		t.Fatalf("the nums slots past the count are not filled with the element default:\n%s", root)
	}
}

func slackDiff(want, got map[string]string) string {
	var diffs []string
	for k, w := range want {
		if got[k] != w {
			diffs = append(diffs, fmt.Sprintf("%s=%q want %q", k, got[k], w))
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			diffs = append(diffs, fmt.Sprintf("%s unexpected", k))
		}
	}
	return strings.Join(diffs, "; ")
}

func buildSlackProbe(t *testing.T, javac, pkg string, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixedJava(t, src, pkg, files)
	probe := filepath.Join(src, "SlackProbe.java")
	if err := os.WriteFile(probe, []byte(strings.ReplaceAll(slackProbeJava, "PKG", pkg)), 0o644); err != nil {
		t.Fatal(err)
	}
	sources, err := filepath.Glob(filepath.Join(src, "*", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	args = append(args, probe)
	if out, err := testproc.Command(t, javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	return classes
}

// write <dir>: A.bin (every count at its bound, every slot 77), B.bin (every
// count 1, live slots 77), bad.bin (B with the cells count past its bound).
// read <dir>: the recipe, dumped slot by slot.
const slackProbeJava = `import java.nio.file.*;
import PKG.*;

public final class SlackProbe {
    static final StringBuilder out = new StringBuilder();

    static SlackRootFixed.Value make(int cells, int subs, int nums) {
        SlackRootFixed.Value v = new SlackRootFixed.Value();
        v.cellsCount = cells;
        for (int i = 0; i < cells; i++) {
            v.cells[i].id = 77;
            v.cells[i].subsCount = subs;
            for (int j = 0; j < subs; j++) { v.cells[i].subs[j].v = 77; }
        }
        v.numsCount = nums;
        for (int i = 0; i < nums; i++) { v.nums[i] = 77; }
        return v;
    }

    static byte[] save(SlackRootFixed.Value v) {
        SlackRootFixed.Value[] vals = { v };
        byte[] b = new byte[SlackRootFixed.measure(1)];
        int w = SlackRootFixed.save(vals, 1, b);
        if (w != b.length) { throw new IllegalStateException("save wrote " + w + " of " + b.length); }
        return b;
    }

    static int load(SlackRootFixed.Value v, byte[] data) {
        SlackRootFixed.Value[] vals = { v };
        return SlackRootFixed.load(vals, 1, data, TableFixed.plan(1 << 14), new short[1 << 14],
            SlackRootFixed.image(), new TableFixed.Report());
    }

    static void dump(String tag, SlackRootFixed.Value v) {
        out.append(tag).append(" cellsCount=").append(v.cellsCount).append('\n');
        for (int i = 0; i < v.cells.length; i++) {
            SlackCellFixed.Value c = v.cells[i];
            out.append(tag).append(" cells[").append(i).append("].id=").append(c.id).append('\n');
            out.append(tag).append(" cells[").append(i).append("].subsCount=").append(c.subsCount).append('\n');
            for (int j = 0; j < c.subs.length; j++) {
                out.append(tag).append(" cells[").append(i).append("].subs[").append(j).append("].v=").append(c.subs[j].v).append('\n');
            }
        }
        out.append(tag).append(" numsCount=").append(v.numsCount).append('\n');
        for (int i = 0; i < v.nums.length; i++) {
            out.append(tag).append(" nums[").append(i).append("]=").append(v.nums[i]).append('\n');
        }
    }

    public static void main(String[] args) throws Exception {
        Path dir = Paths.get(args[1]);
        if (args[0].equals("write")) {
            Files.write(dir.resolve("A.bin"), save(make(3, 2, 4)));
            byte[] b = save(make(1, 1, 1));
            Files.write(dir.resolve("B.bin"), b);
            // the cells count is the first four bytes of the body, after the
            // record's eight-byte hash: 4 is past the bound of 3
            byte[] bad = b.clone();
            int at = SlackRootFixed.headerBytes + 8;
            if (bad[at] != 1) { throw new IllegalStateException("cells count not at " + at); }
            bad[at] = 4;
            Files.write(dir.resolve("bad.bin"), bad);
            return;
        }
        byte[] a = Files.readAllBytes(dir.resolve("A.bin"));
        byte[] b = Files.readAllBytes(dir.resolve("B.bin"));
        byte[] bad = Files.readAllBytes(dir.resolve("bad.bin"));

        SlackRootFixed.Value reused = new SlackRootFixed.Value();
        int na = load(reused, a);
        dump("HELD", reused);
        int nb = load(reused, b);
        dump("REUSED", reused);

        SlackRootFixed.Value fresh = new SlackRootFixed.Value();
        int nf = load(fresh, b);
        dump("FRESH", fresh);

        SlackRootFixed.Value refused = new SlackRootFixed.Value();
        load(refused, a);
        int nbad = load(refused, bad);
        dump("REFUSED", refused);
        dump("NEW", new SlackRootFixed.Value());

        out.append("LOAD a=").append(na).append('\n');
        out.append("LOAD b=").append(nb).append('\n');
        out.append("LOAD fresh=").append(nf).append('\n');
        out.append("LOAD bad=").append(nbad).append('\n');
        System.out.print(out);
    }
}
`
