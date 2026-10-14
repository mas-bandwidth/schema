package gotable

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE ROADMAP'S SHARED-BYTE-ORACLE AND HOSTILE-INPUT ROWS ON THE GO LEG
// (docs/roadmap.sexp node `fixed-tables`, ROADMAP.md "NEW Fixed Tables").
// Law: docs/FIXED-FORM-ALGORITHM.md §1 (the layout and its hash), §3 (a record)
// and §5.7 step 1 (the manifest is the value oracle); docs/SPEC-TABLES.md §3.4.
// One subtest per leaf, each t.Parallel() first. The bytes are the C++
// reference's own corpus, `make tables-fixedform-corpus` -> build/fixedform-corpus,
// opened through fixedCorpus(t) (fixedversioning_test.go:794), the same gate the
// rest of this package's §5 suite uses: a missing corpus skips by name, and
// under SCHEMA_REQUIRE_CORPUS=1 (which `make tables-go-versioning` sets) it
// fails by name instead.
//
// W13 and P2 and R27 read the reference's files through the leg's OWN emitted
// loader; P4 reads a reference file through the plan compiled for a DIFFERENT
// row. None of the four touches an emitter; each is a value assertion against an
// independent oracle.
func TestFixedRoadmapGoT12ConformanceHostile(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	tests := []struct {
		id  string
		run func(t *testing.T, corpus string)
	}{
		{"go/W13", t12W13},
		{"go/P2", t12P2},
		{"go/R27", t12R27},
		{"go/P4", t12P4},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t, corpus)
		})
	}
}

// t12RefFile is the reference's file for one row, quoted for a generated probe.
func t12RefFile(corpus, file string) string {
	return `"` + filepath.Join(corpus, file) + `"`
}

// ---- go/W13: layout + hash == the C++ reference ----------------------------

// t12W13 holds W13's clause: each corpus row's LAYOUT and LAYOUT HASH, as this
// leg's emitter lays them down (the generated <Root>FixedLayout and
// <Root>FixedHash, §1), equal the reference's — which are the layout behind the
// file's u32 length at offset 16 (§5.7 step 1's file head: form byte, seven
// reserved zeros, the hash at 8, the length at 16, the layout at 20) and the
// u64 hash at offset 8. The manifest names the row's root so the two sides are
// the same type and the comparison has no wiggle room.
func t12W13(t *testing.T, corpus string) {
	for _, r := range versionRows {
		r := r
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			newer := readSchema(t, "VNEW_"+r.row)
			root := fixedRootName(t, newer)
			src := fmt.Sprintf(`package probe
import ("encoding/binary"; "os"; "testing")

func TestW13(t *testing.T) {
	data, err := os.ReadFile(%[1]s)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 20 {
		t.Fatalf("the reference file is shorter than its own head: %%d", len(data))
	}
	n := int(binary.LittleEndian.Uint32(data[16:20]))
	if 20+n > len(data) {
		t.Fatalf("the file's stated layout length %%d runs past the file %%d", n, len(data))
	}
	if got := string(data[20 : 20+n]); got != string(%[2]sFixedLayout) {
		t.Fatalf("W13: the emitted layout differs from the reference's %%d-byte layout", n)
	}
	if got := binary.LittleEndian.Uint64(data[8:16]); got != %[2]sFixedHash {
		t.Fatalf("W13: the emitted hash %%#016x differs from the reference's %%#016x", %[2]sFixedHash, got)
	}
}
`, t12RefFile(corpus, "new_"+r.row+".bin"), root)
			if out, err := runVersionProbe(t, newer, nil, src); err != nil {
				t.Fatalf("W13 %s: %v\n%s", r.row, err, out)
			}
		})
	}
}

// ---- go/P2: write, read, write is byte-identical ---------------------------

// t12P2 holds P2's clause: for every corpus row, WRITE-read-WRITE is
// byte-identical. The reference's file is READ through this leg's own
// <Root>FixedLoad and then WRITTEN back through <Root>FixedSave; the two byte
// strings must be equal. A leg whose write moved one byte from the write it
// reads is a leg that cannot round-trip a sprint card written in one language
// and read in another.
func t12P2(t *testing.T, corpus string) {
	for _, r := range versionRows {
		r := r
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			newer := readSchema(t, "VNEW_"+r.row)
			root := fixedRootName(t, newer)
			src := fmt.Sprintf(`package probe
import ("bytes"; "os"; "testing")

func TestP2(t *testing.T) {
	data, err := os.ReadFile(%[1]s)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]%[2]s, 64)
	var rep TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &rep)
	if n < 1 {
		t.Fatalf("P2: the leg refused the reference's own file: n=%%d %%+v", n, rep)
	}
	buf := make([]byte, %[2]sFixedMeasure(int64(n)))
	if got := %[2]sFixedSave(back[:n], buf); got != int64(len(buf)) {
		t.Fatalf("P2: save wrote %%d of %%d", got, len(buf))
	}
	if !bytes.Equal(buf, data) {
		t.Fatalf("P2: WRITE-READ-WRITE is not byte-identical: %%d bytes in, %%d bytes out", len(data), len(buf))
	}
}
`, t12RefFile(corpus, "new_"+r.row+".bin"), root)
			if out, err := runVersionProbe(t, newer, nil, src); err != nil {
				t.Fatalf("P2 %s: %v\n%s", r.row, err, out)
			}
		})
	}
}

// ---- go/R27: the manifest read as wire -------------------------------------

// t12R27 holds R27's clause: each reference file READS CLEAN through the leg's
// own loader, value for value against the manifest. The manifest
// (build/fixedform-corpus/manifest.txt, §5.7 step 1) is the value oracle
// (fixedversioning_test.go:873 manifestBrackets); the row's lead/trail — where
// its root declares them — are asserted from the manifest and never from the
// bytes under test. A file that reads while a mislaid size moves a neighbour is
// caught here by the bracket.
func t12R27(t *testing.T, corpus string) {
	for _, r := range versionRows {
		r := r
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			newer := readSchema(t, "VNEW_"+r.row)
			root := fixedRootName(t, newer)
			st := fixedRoot(t, newer)
			body := ""
			if hasField(st, "lead") && hasField(st, "trail") {
				lead, trail := brackets(t, corpus, r.row, ir.TableFixedTypeBytes(st))
				body = fmt.Sprintf(`
	if back[0].Lead != 0x%08X || back[0].Trail != 0x%08X {
		t.Fatalf("R27: the manifest's brackets did not land: lead=%%#x trail=%%#x", back[0].Lead, back[0].Trail)
	}
`, lead, trail)
			}
			src := fmt.Sprintf(`package probe
import ("os"; "testing")

func TestR27(t *testing.T) {
	data, err := os.ReadFile(%[1]s)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]%[2]s, 64)
	var rep TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &rep)
	if n < 1 {
		t.Fatalf("R27: the reference file did not read clean: n=%%d %%+v", n, rep)
	}
	if rep.Reason != "" || rep.Malformed || rep.Verdict == TableOpenRefused {
		t.Fatalf("R27: a clean reference file is not a refusal: %%+v", rep)
	}
	%[3]s
}
`, t12RefFile(corpus, "new_"+r.row+".bin"), root, body)
			if out, err := runVersionProbe(t, newer, nil, src); err != nil {
				t.Fatalf("R27 %s: %v\n%s", r.row, err, out)
			}
		})
	}
}

// ---- go/P4: the wrong-plan negative control --------------------------------

// t12P4 holds P4's clause: a record read through a deliberately wrong plan must
// differ from the right read or be refused by name. The right read is the row's
// own file through its own loader (proved by W13/P2/R27 above); the wrong read
// is THAT SAME FILE through the plan compiled for another row of the corpus
// (uint_widen's file through int_widen's loader). The leg must refuse it by
// name — a wrong plan that lands a record silently is the fourth answer §7
// item 5 forbids — and the control is only meaningful because the right loader
// reads the very same file clean.
func t12P4(t *testing.T, corpus string) {
	const readerRow, fileRow = "int_widen", "uint_widen"
	newer := readSchema(t, "VNEW_"+readerRow)
	readerRoot := fixedRootName(t, newer)
	wrongRoot := fixedRootName(t, readSchema(t, "VNEW_"+fileRow))
	src := fmt.Sprintf(`package probe
import ("os"; "testing")

func TestP4(t *testing.T) {
	data, err := os.ReadFile(%[2]s)
	if err != nil {
		t.Fatal(err)
	}
	// THE RIGHT READ, in this same case: the file is good. (The wrong loader
	// below is the only change.)
	right := make([]%[3]s, 64)
	var rr TableReport
	rplan := make([]TableFixedEntry, 4096)
	if n := %[3]sFixedLoad(right, data, rplan, &rr); n < 1 {
		t.Fatalf("P4 CONTROL: the right plan refused the file, so the wrong-plan control proves nothing: n=%%d %%+v", n, rr)
	}
	// THE WRONG PLAN: the reader compiled for %[1]s, handed %[1]s's file.
	back := make([]%[1]s, 64)
	var rep TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[1]sFixedLoad(back, data, plan, &rep)
	if n >= 0 {
		t.Fatalf("P4: a record read through a deliberately wrong plan LANDED (n=%%d): the fourth answer §7 item 5 forbids", n)
	}
	if rep.Reason == "" && !rep.Malformed {
		t.Fatalf("P4: a wrong-plan read is NEITHER a named refusal NOR malformed: %%+v", rep)
	}
}
`, readerRoot, t12RefFile(corpus, "new_"+fileRow+".bin"), wrongRoot)
	if out, err := runVersionProbe(t, newer, nil, src); err != nil {
		t.Fatalf("P4: %v\n%s", err, out)
	}
}
