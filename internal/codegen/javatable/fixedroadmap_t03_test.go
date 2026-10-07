package javatable

// fixedroadmap_t03 — the java leg's retirement, retired-runtime,
// numeric-evolution and optional-values rows of docs/roadmap.sexp's node
// `fixed-tables` (ROADMAP.md "NEW Fixed Tables"), the assertions no other
// test of this leg makes. Law: docs/FIXED-FORM-ALGORITHM.md §4.6 (the
// bounds pass), §5.3 (LOAD), §5.6 (what is retired), §5.9 #23 (the retirement
// print), docs/FIXED-FORM-VERSIONING-TESTS.md ("The floor and the hash", the
// divergence rows), docs/SPEC-TABLES.md §3.4, §21.2.
//
// A clause already held by an existing test of this leg is named by that test
// in the card's verdicts and is not repeated here; this file carries the
// direct assertion of every clause that had none. [verify] tasks prove the
// holder RUNS and BITES (the bite is a planted break in the card's report);
// [owed] tasks are asserted here red-first and implemented in the emitter or
// runtime; [capability] tasks write and read the construct with non-default
// and boundary values through the leg's own generated save and load.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// t03Flat is the smallest declared fixed table with a lineage: two adjacent
// scalars, handed one older entry, so an emitted build has both lanes.
const t03Flat = `package probe

fixed table T
{
    x int32
    y int32
}
`

// t03CorpusFile is one file of the C++ reference's corpus, refused loudly
// when missing so this harness never silently skips a row it names.
func t03CorpusFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(corpusDir(t), name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the corpus has no %s: run `make tables-fixedform-corpus`", name)
	}
	return path
}

// t03Method is one emitted method's body, or "" when the class has none.
func t03Method(src, sig string) string {
	i := strings.Index(src, sig)
	if i < 0 {
		return ""
	}
	rest := src[i:]
	if j := strings.Index(rest, "\n    }\n"); j >= 0 {
		return rest[:j]
	}
	return rest
}

func TestFixedRoadmapT03Versions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"java/R3", t03R3},
		{"java/R32", t03R32},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t03R3: R3 "the floor is 1 + the highest retired index (0 when none); below
// the floor is layout_unsupported, reporting the file's hash". The
// behavioural column is TestFixedVersioningFloor's (floor_at, floor_below
// with the file's hash, floor_raise_live); this subtest pins the FLOOR VALUE
// itself off the emitted constant, so a floor that disagreed with the entries
// it was derived from is red by name.
func t03R3(t *testing.T) {
	older := []string{"VOLD_floor.schema", "VMID_floor.schema"}
	for _, tc := range []struct {
		retire int
		want   int
	}{
		{0, 0}, // none retired: the floor is 0
		{1, 1}, // entry 0 retired: the floor is 1
		{2, 2}, // entries 0 and 1 retired: 1 + the HIGHEST retired index
	} {
		u := versioningSchemaUnit(t, "VNEW_floor.schema")
		files, err := GenerateLineage(u, lineageOf(t, older, tc.retire))
		if err != nil {
			t.Fatalf("R3: retire=%d: GenerateLineage: %v", tc.retire, err)
		}
		src := string(files["FlooredFixed.java"])
		want := fmt.Sprintf("public static final int floor = %d;", tc.want)
		if !strings.Contains(src, want) {
			t.Errorf("R3: retire=%d: the emitted floor is not %q — the floor is 1 + the highest retired index, 0 when none is", tc.retire, want)
		}
	}
}

// t03R32: R32 "retire for real: a retired version is refused by name, once,
// idempotently". The by-name half is TestFixedVersioningFloor's floor_below;
// this subtest is the ONCE and the IDEMPOTENTLY: the same retired file read
// twice in one process refuses layoutUnsupported both times, with the file's
// hash both times, every counter zero both times, and the destination still
// every field of a fresh value after each.
func t03R32(t *testing.T) {
	oldFile := t03CorpusFile(t, "old_floor.bin")
	newFile := t03CorpusFile(t, "new_floor.bin")
	dir := t.TempDir()
	classes := t03Build(t, dir, "floor", []sideSpec{
		{key: "reads", schema: "VNEW_floor.schema", older: []string{"VOLD_floor.schema", "VMID_floor.schema"}, retire: 1},
	}, map[string]string{
		"Probe_retired.java": t03TwiceProbe("Probe_retired", "vnew_floor", "Floored"),
	})

	out := t03RunTwice(t, classes, "Probe_retired", oldFile)
	wantHash := fileHash(t, oldFile)
	for _, half := range []string{"FIRST", "SECOND"} {
		r := t03ParseTwice(t, out, half)
		if r.n != -1 || !r.refused || r.reason != "layoutUnsupported" {
			t.Errorf("R32: %s read of a retired file: n=%d refused=%v reason=%s, want -1 refused layoutUnsupported by name", half, r.n, r.refused, r.reason)
		}
		if r.layoutHash != wantHash {
			t.Errorf("R32: %s read: layoutHash=%s, want the file's own hash %s (§5.9 #7)", half, r.layoutHash, wantHash)
		}
		if r.malformed || r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 || r.clamped != 0 {
			t.Errorf("R32: %s read moved a counter or set malformed: %+v; a refusal is one answer, once", half, r)
		}
		for k, want := range r.fresh {
			if have, ok := r.value[k]; ok && have != want {
				t.Errorf("R32: %s read wrote the destination: %s = %s, a fresh value holds %s", half, k, have, want)
			}
		}
	}
	// The reader's own file still reads between the two refusals, which is
	// what makes the refusal the entry's and not the build's.
	if r := runProbe(t, classes, "Probe_reads", newFile); r.n < 1 {
		t.Errorf("R32: the reader's own file must read beside the retired refusal: %+v", r)
	}
}

// t03Build is buildRow with room for the harness's own probes: every file of
// `extra` (name → source) is written into the same src tree and compiled by
// the same javac, so a probe sees every generated class.
func t03Build(t *testing.T, dir, row string, sides []sideSpec, extra map[string]string) string {
	t.Helper()
	if len(extra) == 0 {
		_, classes := buildRow(t, dir, row, sides)
		return classes
	}
	// write the extra probes first, then let buildRow's glob pick them up:
	// buildRow compiles every *.java at the src root beside its own probes.
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range extra {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, classes := buildRow(t, dir, row, sides)
	return classes
}

// t03TwiceProbe reads ONE file twice in one process, with the same report and
// the same destination, and dumps both halves: "refused by name, once,
// idempotently" needs the second read to answer exactly as the first did. A
// FRESH value is dumped first, so a refusal that wrote the destination is
// red by name against it.
func t03TwiceProbe(class, pkg, root string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `import java.lang.reflect.*;
import java.nio.file.*;

// GENERATED BY internal/codegen/javatable/fixedroadmap_t03_test.go. The
// idempotence probe: one file, two loads, both answers, and the fresh value
// they must both leave the destination equal to.
public final class %s {
    static final StringBuilder out = new StringBuilder();

    public static void main(String[] args) throws Exception {
        final byte[] data = Files.readAllBytes(Paths.get(args[0]));
        %s.%sFixed.Value[] vals = new %s.%sFixed.Value[4];
        for (int i = 0; i < vals.length; i++) { vals[i] = new %s.%sFixed.Value(); }
        %s.TableFixed.Report report = new %s.TableFixed.Report();
        %s.TableFixed.Entry[] plan = %s.TableFixed.plan(1 << 14);
        short[] remap = new short[1 << 14];
        byte[] image = %s.%sFixed.image();
        dump("FRESH", vals[0]);

        int n1 = %s.%sFixed.load(vals, vals.length, data, plan, remap, image, report);
        out.append("FIRST n=").append(n1)
           .append(" refused=").append(report.refused)
           .append(" reason=").append(report.reason)
           .append(" malformed=").append(report.malformed)
           .append(" unknown=").append(report.unknown)
           .append(" kindMismatch=").append(report.kindMismatch)
           .append(" widened=").append(report.widened)
           .append(" clamped=").append(report.clamped)
           .append(" layoutHash=").append("0x" + Long.toHexString(report.layoutHash))
           .append('\n');
        dump("FIRSTFIELD", vals[0]);

        int n2 = %s.%sFixed.load(vals, vals.length, data, plan, remap, image, report);
        out.append("SECOND n=").append(n2)
           .append(" refused=").append(report.refused)
           .append(" reason=").append(report.reason)
           .append(" malformed=").append(report.malformed)
           .append(" unknown=").append(report.unknown)
           .append(" kindMismatch=").append(report.kindMismatch)
           .append(" widened=").append(report.widened)
           .append(" clamped=").append(report.clamped)
           .append(" layoutHash=").append("0x" + Long.toHexString(report.layoutHash))
           .append('\n');
        dump("SECONDFIELD", vals[0]);
        System.out.print(out);
    }

    static void dump(String tag, Object v) throws Exception {
        for (Field f : v.getClass().getFields()) {
            if (Modifier.isStatic(f.getModifiers())) { continue; }
            out.append(tag).append(' ').append(f.getName()).append('=').append(String.valueOf(f.get(v))).append('\n');
        }
    }
}
`, class, pkg, root, pkg, root, pkg, root, pkg, pkg, pkg, pkg, pkg, root, pkg, root, pkg, root)
	return b.String()
}

func t03RunTwice(t *testing.T, classes, class, file string) string {
	t.Helper()
	_, javaBin := javaTools(t)
	out, err := runJava(t, javaBin, classes, class, file)
	if err != nil {
		t.Fatalf("java %s %s: %v\n%s", class, filepath.Base(file), err, out)
	}
	return out
}

// t03ParseTwice reads one half of the twice-probe's answer, with the FRESH
// dump as the destination's oracle.
func t03ParseTwice(t *testing.T, out, half string) probeRun {
	t.Helper()
	r := probeRun{fresh: map[string]string{}, value: map[string]string{}}
	fieldTag := half + "FIELD"
	for line := range strings.SplitSeq(out, "\n") {
		k, v, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		switch k {
		case "FRESH":
			name, val, _ := strings.Cut(v, "=")
			r.fresh[name] = val
		case fieldTag:
			name, val, _ := strings.Cut(v, "=")
			r.value[name] = val
		case half:
			for tok := range strings.FieldsSeq(v) {
				kk, vv, has := strings.Cut(tok, "=")
				if !has {
					continue
				}
				t03ReportField(&r, kk, vv)
			}
		}
	}
	return r
}

func t03ReportField(r *probeRun, k, v string) {
	switch k {
	case "n":
		r.n, _ = strconv.Atoi(v)
	case "refused":
		r.refused = v == "true"
	case "reason":
		r.reason = v
	case "malformed":
		r.malformed = v == "true"
	case "unknown":
		r.unknown, _ = strconv.Atoi(v)
	case "kindMismatch":
		r.kindMismatch, _ = strconv.Atoi(v)
	case "widened":
		r.widened, _ = strconv.Atoi(v)
	case "clamped":
		r.clamped, _ = strconv.Atoi(v)
	case "layoutHash":
		r.layoutHash = v
	}
}


// runJava runs one compiled probe and answers its combined output, with the
// harness's file arguments when the probe reads any.
func runJava(t *testing.T, javaBin, classes, class string, args ...string) (string, error) {
	t.Helper()
	cmdArgs := append([]string{"-ea", "-cp", classes, class}, args...)
	out, err := exec.Command(javaBin, cmdArgs...).CombinedOutput()
	return string(out), err
}
