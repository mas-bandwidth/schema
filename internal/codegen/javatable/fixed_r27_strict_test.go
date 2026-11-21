package javatable

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestJavaFixedR27OracleHostile is the functional test for cell java/R27
// (FIXED-FORM-ALGORITHM §5.9 #32 and #37, SPEC-TABLES §3.4): it pins the five
// range-widening corpus vectors (hostile_range_widen.bin,
// hostile_range_widen_array.bin, hostile_range_widen_u64.bin,
// hostile_cfloat_range_widen.bin, past_cfloat_range_widen.bin, each refusing
// past the WRITER's range, plus old_cfloat_range_widen.bin landing 0.5 as the
// control; docs/rulings.json strict-only-read, ALGORITHM §4.6) under both the
// manifest bound oracle (t18Bound + t18WriterBound) and the live JDK execution
// of the generated Java fixed-form readers.
//
// TestJavaFixedR27OracleHostileNegativeControl plants faults into the real
// emitted TableFixed.java and <Root>Fixed.java artifacts and into the writer
// bound check, requiring each check to go red.

func checkJavaFixedR27HostileCorpus(t *testing.T, skipWriterBound bool, mutate func(pkg, name, src string) string) error {
	t.Helper()
	slowtest.Gate(t, "the JDK (javac and java)")
	t18RequireJava(t)
	corpus := corpusDir(t)
	byFile := map[string]t18Row{}
	for _, r := range t18Rows(t, corpus) {
		byFile[r.m.File] = r
	}
	// THE CONTROL IS A CROSS-VERSION READ: the manifest's old_ row is the old
	// reader's own file, so here the NEW reader reads it through its lineage,
	// as it reads hostile_ and past_. The writer's float range is narrower
	// than the reader's, and the 0.5 it wrote lands whole.
	if r, ok := byFile["old_cfloat_range_widen.bin"]; ok {
		r.reader = "test/tables/VNEW_" + r.m.Row + ".schema"
		r.older = []string{"VOLD_" + r.m.Row + ".schema"}
		byFile[r.m.File] = r
	}

	wantRows := []struct {
		file       string
		wantRefuse bool
		wantAim    uint32
	}{
		{"hostile_range_widen.bin", true, 0},
		{"hostile_range_widen_array.bin", true, 0},
		{"hostile_range_widen_u64.bin", true, 0},
		// the control: the old writer's 0.5 lands whole through the writer's
		// narrower float range
		{"old_cfloat_range_widen.bin", false, math.Float32bits(0.5)},
		// past the WRITER's max 1.0, inside the reader's 2.0: refuses
		// (docs/rulings.json strict-only-read, ALGORITHM §4.6)
		{"hostile_cfloat_range_widen.bin", true, 0},
		{"past_cfloat_range_widen.bin", true, 0},
	}

	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	classesDir := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classesDir, 0o755); err != nil {
		return err
	}

	emittedPkg := map[string]string{}
	probeClasses := make([]string, len(wantRows))
	for i, tc := range wantRows {
		r, ok := byFile[tc.file]
		if !ok {
			return fmt.Errorf("missing manifest row %s", tc.file)
		}
		u := t18LoadUnit(t, r.reader)
		st, _, ok := t18Root(u, r.m.Root)
		if !ok {
			return fmt.Errorf("%s: no fixed root %s", tc.file, r.m.Root)
		}
		if cls, ok := emittedPkg[u.Package]; ok {
			probeClasses[i] = cls
			continue
		}
		files, err := GenerateLineage(u, lineageOf(t, r.older, 0))
		if err != nil {
			return fmt.Errorf("%s: GenerateLineage: %w", tc.file, err)
		}
		if mutate != nil {
			for name, b := range files {
				files[name] = []byte(mutate(u.Package, name, string(b)))
			}
		}
		writeFixedJava(t, srcDir, u.Package, files)
		className := fmt.Sprintf("R27Probe_%d", i)
		probe := strings.NewReplacer("@CLASS@", className, "@PKG@", u.Package, "@ROOT@", fixedClass(st.Name)).Replace(t18LoadProbe)
		if err := os.WriteFile(filepath.Join(srcDir, className+".java"), []byte(probe), 0o644); err != nil {
			return err
		}
		emittedPkg[u.Package] = className
		probeClasses[i] = className
	}

	javac, javaBin := javaTools(t)
	sources, _ := filepath.Glob(filepath.Join(srcDir, "*", "*.java"))
	probes, _ := filepath.Glob(filepath.Join(srcDir, "*.java"))
	args := append([]string{"--release", "17", "-nowarn", "-d", classesDir}, sources...)
	args = append(args, probes...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("javac: %w\n%s", err, out)
	}

	for i, tc := range wantRows {
		r := byFile[tc.file]
		m := r.m
		want := t18Expect(m)
		reader := t18LoadUnit(t, r.reader)
		writer := t18LoadUnit(t, r.writer)
		keys := make([]string, 0, len(want))
		for k := range want {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pastBound := 0
		for _, k := range keys {
			_, past := t18Bound(reader, m.Root, k, want[k])
			if !past && !skipWriterBound {
				_, past = t18WriterBound(writer, m.Root, k, want[k])
			}
			if past {
				pastBound++
			}
		}
		if (pastBound > 0) != tc.wantRefuse {
			return fmt.Errorf("%s: manifest bound decision pastBound=%d, wantRefuse=%v", tc.file, pastBound, tc.wantRefuse)
		}

		out, err := exec.Command(javaBin, "-ea", "-cp", classesDir, probeClasses[i], filepath.Join(corpus, tc.file)).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: java: %w\n%s", tc.file, err, out)
		}
		res := t18Parse(t, string(out))
		if tc.wantRefuse {
			if res.n != -1 || !res.refused || res.damaged || res.reason != "outOfRange" {
				return fmt.Errorf("%s: got n=%d refused=%v reason=%s damaged=%v, want n=-1 refused=true reason=outOfRange damaged=false",
					tc.file, res.n, res.refused, res.reason, res.damaged)
			}
		} else {
			if res.n != 1 || res.refused || res.damaged {
				return fmt.Errorf("%s: got n=%d refused=%v reason=%s damaged=%v, want n=1 refused=false damaged=false",
					tc.file, res.n, res.refused, res.reason, res.damaged)
			}
			leaf, found := res.leaves[t18Norm("r0.aim")]
			if !found || uint32(leaf.bits) != tc.wantAim {
				return fmt.Errorf("%s: r0.aim got bits=%#x (found=%v), want %#x", tc.file, uint32(leaf.bits), found, tc.wantAim)
			}
		}
	}
	return nil
}

func TestJavaFixedR27OracleHostile(t *testing.T) {
	t.Parallel()
	if err := checkJavaFixedR27HostileCorpus(t, false, nil); err != nil {
		t.Fatal(err)
	}
}

func TestJavaFixedR27OracleHostileNegativeControl(t *testing.T) {
	t.Parallel()
	t.Run("oracle-skips-writer-bound", func(t *testing.T) {
		t.Parallel()
		if err := checkJavaFixedR27HostileCorpus(t, true, nil); err == nil {
			t.Fatal("NEGATIVE CONTROL FAILED: skipping t18WriterBound left checkJavaFixedR27HostileCorpus green")
		}
	})

	controls := []struct {
		name, file, from, to string
	}{
		{
			name: "writer-bounds-bypassed",
			file: "TableFixed.java",
			from: "if (spec == null || spec.length == 0 || image == null) {",
			to:   "if (true || spec == null || spec.length == 0 || image == null) {",
		},
		// The writer's FLOAT bounds pass skipped: hostile_ (1.5) lands
		// through the reader's wider 2.0 and the check goes red. (The reader's
		// own float pass is not separately observable on this row's compiled
		// lane: the writer's range sits inside the reader's, so the writer's
		// pass refuses every value the reader's would.)
		{
			name: "writer-cfloat-bounds-bypassed",
			file: "TableFixed.java",
			from: "for (int i = 0; i + 3 < spec.length; i += 4) {",
			to:   "for (int i = 0; false; i += 4) {",
		},
		// The writer's float max narrowed from 1.0 to 0.25 in the emitted
		// spec: the old_ control (0.5) refuses and the check goes red.
		{
			name: "writer-cfloat-bound-narrowed",
			file: "CfloatRangeWidenFixed.java",
			from: "Double.longBitsToDouble(0x3ff0000000000000L)",
			to:   "Double.longBitsToDouble(0x3fd0000000000000L)",
		},
	}
	for _, c := range controls {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			planted := 0
			err := checkJavaFixedR27HostileCorpus(t, false, func(pkg, name, src string) string {
				if name != c.file || !strings.Contains(src, c.from) {
					return src
				}
				planted++
				return strings.ReplaceAll(src, c.from, c.to)
			})
			if planted == 0 {
				t.Fatalf("NEGATIVE CONTROL FAILED: %s anchor %q was not found in %s", c.name, c.from, c.file)
			}
			if err == nil {
				t.Fatalf("NEGATIVE CONTROL FAILED: %s plant left checkJavaFixedR27HostileCorpus green", c.name)
			}
		})
	}
}
