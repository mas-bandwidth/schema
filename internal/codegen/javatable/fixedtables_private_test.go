package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestJavaFixedTablesPrivate(t *testing.T) {
	t.Parallel()
	const schema = `package probe

fixed table Root
{
    x int32 = 10 | min = 0, max = 100
    y int32 = 7
}
`
	u := unitFrom(t, schema)
	st := findTable(t, u, "Root")
	walk := ir.TableFixedWalkRoot(st)
	lay := ir.TableFixedLayoutBytes(walk)
	hash := ir.TableFixedLayoutHash(lay, st)
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{
		"Root": {
			{
				Wire:   hash ^ 0x1111,
				Layout: lay,
				Record: ir.TableFixedTypeBytes(st),
				Ranges: []FixedRangeSpec{{Name: "x", Lo: 0, Hi: 50}},
			},
		},
	})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}

	fixedSrc := string(files["RootFixed.java"])
	blockSrc := string(files["RootBlock.java"])
	if fixedSrc == "" || blockSrc == "" {
		t.Fatalf("expected RootFixed.java and RootBlock.java to be generated")
	}

	nonPrivateArrayRe := regexp.MustCompile(`(?m)^\s*(?:public\s+)?static\s+final\s+[A-Za-z0-9_.\[\]]+\[\]\s+(layout|defaults|dest|known|known\d+Layout|known\d+WriterBounds|projectionOffsets|plan|plans|bounds)\b`)
	for _, m := range nonPrivateArrayRe.FindAllString(fixedSrc, -1) {
		t.Errorf("RootFixed.java exposes non-private static final array: %s", strings.TrimSpace(m))
	}
	for _, m := range nonPrivateArrayRe.FindAllString(blockSrc, -1) {
		t.Errorf("RootBlock.java exposes non-private static final array: %s", strings.TrimSpace(m))
	}

	for _, want := range []string{
		"private static final byte[] layout =",
		"public static byte[] layout() { return layout.clone(); }",
		"private static final byte[] defaults =",
		"public static byte[] defaults() { return defaults.clone(); }",
		"private static final int[] dest =",
		"public static int[] dest() { return dest.clone(); }",
		"private static final TableFixed.KnownLayout[] known =",
		"public static TableFixed.KnownLayout[] known()",
		"private static final long[] known0WriterBounds =",
	} {
		if !javaCarries(fixedSrc, want) {
			t.Errorf("RootFixed.java missing %q", want)
		}
	}
	for _, want := range []string{
		"private static final int[] projectionOffsets =",
		"public static int[] projectionOffsets() { return projectionOffsets.clone(); }",
	} {
		if !javaCarries(blockSrc, want) {
			t.Errorf("RootBlock.java missing %q", want)
		}
	}

	javac, err := exec.LookPath("javac")
	if err != nil {
		return
	}
	javaBin, err := exec.LookPath("java")
	if err != nil {
		return
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	pkgDir := filepath.Join(src, "probe")
	otherDir := filepath.Join(src, "other")
	classes := filepath.Join(dir, "classes")
	for _, d := range []string{pkgDir, otherDir, classes} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var pkgSources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(pkgDir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		pkgSources = append(pkgSources, p)
	}

	// 1. Negative probe in another package: direct access to probe.RootFixed.layout[0] = 0 must fail to compile.
	negPath := filepath.Join(otherDir, "NegProbe.java")
	negSrc := `package other;
public final class NegProbe {
    public static void main(String[] args) {
        probe.RootFixed.layout[0] = 0;
    }
}
`
	if err := os.WriteFile(negPath, []byte(negSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	negArgs := append([]string{"--release", "17", "-d", classes}, pkgSources...)
	negArgs = append(negArgs, negPath)
	if out, err := exec.Command(javac, negArgs...).CombinedOutput(); err == nil {
		t.Fatalf("negative probe unexpectedly compiled; expected compile error on probe.RootFixed.layout[0] = 0:\n%s", out)
	}

	// 2. Positive probe in another package: mutating arrays returned by layout(), defaults(), dest(), known(), projectionOffsets() leaves subsequent decodes unchanged.
	posPath := filepath.Join(otherDir, "PosProbe.java")
	posSrc := `package other;
import java.util.Arrays;
import probe.RootBlock;
import probe.RootFixed;
import probe.TableFixed;

public final class PosProbe {
    public static void main(String[] args) {
        RootFixed.Value v = new RootFixed.Value();
        v.x = 42;
        v.y = 19;
        byte[] wire = new byte[RootFixed.measure(1)];
        if (RootFixed.save(new RootFixed.Value[] { v }, 1, wire) != wire.length) {
            throw new AssertionError("save failed");
        }

        RootFixed.Value out1 = new RootFixed.Value();
        TableFixed.Report r1 = new TableFixed.Report();
        int n1 = RootFixed.load(new RootFixed.Value[] { out1 }, 1, wire,
                TableFixed.plan(64), new short[64], RootFixed.image(), r1);
        if (n1 != 1 || r1.refused || r1.malformed || out1.x != 42 || out1.y != 19) {
            throw new AssertionError("first decode failed");
        }

        byte[] origLayout = RootFixed.layout();
        byte[] origDefaults = RootFixed.defaults();
        int[] origDest = RootFixed.dest();
        int[] origProj = RootBlock.projectionOffsets();

        byte[] mutLayout = RootFixed.layout();
        Arrays.fill(mutLayout, (byte) 0x7F);
        byte[] mutDefaults = RootFixed.defaults();
        Arrays.fill(mutDefaults, (byte) 0x7F);
        int[] mutDest = RootFixed.dest();
        Arrays.fill(mutDest, -999);
        TableFixed.KnownLayout[] mutKnown = RootFixed.known();
        Arrays.fill(mutKnown[0].layout, (byte) 0x7F);
        mutKnown[0] = null;
        int[] mutProj = RootBlock.projectionOffsets();
        Arrays.fill(mutProj, -999);

        if (!Arrays.equals(origLayout, RootFixed.layout())) {
            throw new AssertionError("layout() mutation leaked");
        }
        if (!Arrays.equals(origDefaults, RootFixed.defaults())) {
            throw new AssertionError("defaults() mutation leaked");
        }
        if (!Arrays.equals(origDest, RootFixed.dest())) {
            throw new AssertionError("dest() mutation leaked");
        }
        if (RootFixed.known()[0] == null || !Arrays.equals(origLayout, RootFixed.known()[RootFixed.known().length - 1].layout)) {
            throw new AssertionError("known() mutation leaked");
        }
        if (!Arrays.equals(origProj, RootBlock.projectionOffsets())) {
            throw new AssertionError("projectionOffsets() mutation leaked");
        }

        RootFixed.Value out2 = new RootFixed.Value();
        TableFixed.Report r2 = new TableFixed.Report();
        int n2 = RootFixed.load(new RootFixed.Value[] { out2 }, 1, wire,
                TableFixed.plan(64), new short[64], RootFixed.image(), r2);
        if (n2 != 1 || r2.refused || r2.malformed || out2.x != 42 || out2.y != 19) {
            throw new AssertionError("second decode changed after mutating cloned tables");
        }
        System.out.println("POS_PROBE_OK");
    }
}
`
	if err := os.WriteFile(posPath, []byte(posSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	posArgs := append([]string{"--release", "17", "-d", classes}, pkgSources...)
	posArgs = append(posArgs, posPath)
	if out, err := exec.Command(javac, posArgs...).CombinedOutput(); err != nil {
		t.Fatalf("positive probe javac failed: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "other.PosProbe").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "POS_PROBE_OK") {
		t.Fatalf("positive probe java failed: %v\n%s", err, out)
	}
}
