package gotable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	if got := binary.LittleEndian.Uint64(data[8:16]); got != uint64(%[2]sFixedHash) {
		t.Fatalf("W13: the emitted hash %%#016x differs from the reference's %%#016x", uint64(%[2]sFixedHash), got)
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

// t12P2Clamped names the corpus rows whose NEW file carries a value PAST the
// reader's declared bound: the reference writes v = 1000 raw (62.5) into
// fixed(12,4) and fixed(28,4) slots declared `max = 7` (test/tables/
// fixedform_dump.cpp, vnew_fixed_i_grow and vnew_fixed_i_grow_element). A
// conforming reader clamps it on read and counts the clamp
// (docs/FIXED-FORM-ALGORITHM.md §3.4), so its write cannot be the reference's
// bytes for that one value. The list is exact: a row that clamps and is not
// named here fails, and a row named here that does not clamp fails.
var t12P2Clamped = map[string]bool{
	"fixed_I_grow":         true,
	"fixed_I_grow_element": true,
}

// t12P2 holds P2's clause (docs/FIXED-FORM-ALGORITHM.md §7 item 1, "read a
// file and save it back; the bytes must be identical"): for every corpus row,
// the reference's WRITE, read through this leg's own <Root>FixedLoad and
// WRITTEN back through <Root>FixedSave, is byte-identical. A leg whose write
// moved one byte from the write it reads cannot round-trip a sprint card
// written in one language and read in another.
//
// The two t12P2Clamped rows hold the clause as far as the contract lets them:
// the clamp is reported, every byte of the file head (form, hash, layout) is
// the reference's, and the clamped write is a fixpoint — it reads back clean
// and writes back byte-identical.
func t12P2(t *testing.T, corpus string) {
	for _, r := range versionRows {
		r := r
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			newer := readSchema(t, "VNEW_"+r.row)
			root := fixedRootName(t, newer)
			src := fmt.Sprintf(`package probe
import ("bytes"; "encoding/binary"; "os"; "testing")

func t12Diff(t *testing.T, what string, in, out []byte) {
	at := 0
	for at < len(out) && at < len(in) && out[at] == in[at] {
		at++
	}
	t.Fatalf("P2: %%s: %%d bytes in, %%d bytes out, first difference at byte %%d\nin:  %% x\nout: %% x", what, len(in), len(out), at, in, out)
}

func TestP2(t *testing.T) {
	data, err := os.ReadFile(%[1]s)
	if err != nil {
		t.Fatal(err)
	}
	clamped := %[3]t
	back := make([]%[2]s, 64)
	var rep TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &rep)
	if n < 1 || rep.Reason != "" || rep.Malformed {
		t.Fatalf("P2: the leg refused the reference's own file: n=%%d %%+v", n, rep)
	}
	buf := make([]byte, %[2]sFixedMeasure(int64(n)))
	if got := %[2]sFixedSave(back[:n], buf); got != int64(len(buf)) {
		t.Fatalf("P2: save wrote %%d of %%d", got, len(buf))
	}
	if !clamped {
		if rep.Clamped != 0 {
			t.Fatalf("P2: the read clamped %%d value(s) but the row is not in t12P2Clamped: %%+v", rep.Clamped, back[:n])
		}
		if !bytes.Equal(buf, data) {
			t12Diff(t, "WRITE-READ-WRITE is not byte-identical", data, buf)
		}
		return
	}
	if rep.Clamped == 0 {
		t.Fatalf("P2: the row is in t12P2Clamped but the read clamped nothing: tighten the list: %%+v", back[:n])
	}
	if len(buf) != len(data) {
		t12Diff(t, "the clamped write changed the file's length", data, buf)
	}
	head := 20 + int(binary.LittleEndian.Uint32(data[16:20]))
	if !bytes.Equal(buf[:head], data[:head]) {
		t12Diff(t, "the clamped write moved a byte of the file head", data[:head], buf[:head])
	}
	again := make([]%[2]s, 64)
	var rep2 TableReport
	plan2 := make([]TableFixedEntry, 4096)
	if n2 := %[2]sFixedLoad(again, buf, plan2, &rep2); n2 != n || rep2.Reason != "" || rep2.Malformed || rep2.Clamped != 0 {
		t.Fatalf("P2: the clamped write does not read back clean: n=%%d of %%d %%+v", n2, n, rep2)
	}
	buf2 := make([]byte, len(buf))
	%[2]sFixedSave(again[:n], buf2)
	if !bytes.Equal(buf2, buf) {
		t12Diff(t, "the clamped write is not a fixpoint of WRITE-READ-WRITE", buf, buf2)
	}
	t.Logf("P2: %%d value(s) past the declared bound clamped on read (§3.4); the head is the reference's and the write is a fixpoint", rep.Clamped)
}
`, t12RefFile(corpus, "new_"+r.row+".bin"), root, t12P2Clamped[r.row])
			if out, err := runVersionProbe(t, newer, nil, src); err != nil {
				t.Fatalf("P2 %s: %v\n%s", r.row, err, out)
			}
		})
	}
	for row := range t12P2Clamped {
		found := false
		for _, r := range versionRows {
			found = found || r.row == row
		}
		if !found {
			t.Errorf("P2: t12P2Clamped names %s, which is not a corpus row", row)
		}
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

// t12P4 holds P4's clause: a record read through a deliberately WRONG plan
// must differ from the right read or be refused by name
// (docs/FIXED-FORM-ALGORITHM.md §1, the layout hash a plan is chosen by, and
// §5.7 step 1, the manifest as value oracle).
//
// The file is uint_widen's NEW file. THE RIGHT READ is that file through its own
// loader (UintWidenFixedLoad, generated from VNEW_uint_widen), asserted value for
// value against the manifest — so the control is live: the bytes are good and
// the right plan lands exactly the reference's values. THE WRONG READ is the
// same file through the plan compiled for ANOTHER corpus row (IntWidenFixedLoad,
// generated from VNEW_int_widen). It must be refused by name, or, if it lands,
// at least one landed value must differ from the right read. A wrong plan that
// lands the right read's values, or lands while reporting nothing, fails here.
// Each side is its own generated package, so neither plan is compiled with the
// other's knowledge.
func t12P4(t *testing.T, corpus string) {
	const rightRow, wrongRow = "uint_widen", "int_widen"
	file := "new_" + rightRow + ".bin"
	want := t12ManifestRecords(t, corpus, file, []string{"lead", "v", "trail"})
	lit := "[][3]uint64{"
	for _, r := range want {
		lit += fmt.Sprintf("{%d, %d, %d},", r[0], r[1], r[2])
	}
	lit += "}"

	rightSchema := readSchema(t, "VNEW_"+rightRow)
	rightRoot := fixedRootName(t, rightSchema)
	right := fmt.Sprintf(`package probe
import ("os"; "testing")

func TestP4Right(t *testing.T) {
	data, err := os.ReadFile(%[1]s)
	if err != nil {
		t.Fatal(err)
	}
	want := %[3]s
	back := make([]%[2]s, 64)
	var rep TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &rep)
	if n != int64(len(want)) || rep.Reason != "" || rep.Malformed {
		t.Fatalf("P4 CONTROL: the right plan did not read the file clean, so the wrong-plan control proves nothing: n=%%d want %%d %%+v", n, len(want), rep)
	}
	for k := range want {
		if uint64(back[k].Lead) != want[k][0] || uint64(back[k].V) != want[k][1] || uint64(back[k].Trail) != want[k][2] {
			t.Fatalf("P4 CONTROL: record %%d through the right plan is not the manifest's: %%+v want %%v", k, back[k], want[k])
		}
	}
}
`, t12RefFile(corpus, file), rightRoot, lit)
	if out, err := runVersionProbe(t, rightSchema, nil, right); err != nil {
		t.Fatalf("P4 control (right plan, %s): %v\n%s", rightRoot, err, out)
	}

	wrongSchema := readSchema(t, "VNEW_"+wrongRow)
	wrongRoot := fixedRootName(t, wrongSchema)
	if wrongRoot == rightRoot {
		t.Fatalf("P4: the wrong plan's root %s is the right plan's: pick another row", wrongRoot)
	}
	wrong := fmt.Sprintf(`package probe
import ("os"; "testing")

func TestP4Wrong(t *testing.T) {
	data, err := os.ReadFile(%[1]s)
	if err != nil {
		t.Fatal(err)
	}
	want := %[3]s
	back := make([]%[2]s, 64)
	var rep TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &rep)
	if n < 0 {
		if rep.Verdict != TableOpenRefused || rep.Reason == "" {
			t.Fatalf("P4: the wrong plan's read is negative but NOT a named refusal: n=%%d %%+v", n, rep)
		}
		t.Logf("P4: the wrong plan (%[2]s) is refused by name: %%s", rep.Reason)
		return
	}
	same := n == int64(len(want))
	for k := int64(0); same && k < n; k++ {
		if uint64(back[k].Lead) != want[k][0] || int64(back[k].V) != int64(want[k][1]) || uint64(back[k].Trail) != want[k][2] {
			same = false
		}
	}
	if same {
		t.Fatalf("P4: a record read through the plan compiled for another row LANDED the right read's values (n=%%d %%+v): the wrong plan is indistinguishable", n, rep)
	}
	t.Logf("P4: the wrong plan (%[2]s) landed values that differ from the right read: n=%%d", n)
}
`, t12RefFile(corpus, file), wrongRoot, lit)
	out, err := runVersionProbe(t, wrongSchema, nil, wrong)
	if err != nil {
		t.Fatalf("P4 (wrong plan %s over %s): %v\n%s", wrongRoot, file, err, out)
	}
}

// t12ManifestRecords is the right read's oracle for P4: the manifest's
// `values=` for one file (§5.7 step 1), as one [lead, v, trail] triple per
// record, read through manifestRow (fixedversioning_test.go). A manifest that
// is absent, lacks the file, or lacks a field of a record fails by name.
func t12ManifestRecords(t *testing.T, corpus, file string, fields []string) [][3]uint64 {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatalf("the corpus manifest is not readable, so P4 has no oracle: %v", err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		values, ok := manifestRow(line, file)
		if !ok {
			continue
		}
		var out [][3]uint64
		for k := 0; ; k++ {
			var rec [3]uint64
			found := 0
			for i, f := range fields {
				if v, ok := values[fmt.Sprintf("r%d.%s", k, f)]; ok {
					rec[i] = v
					found++
				}
			}
			if found == 0 {
				break
			}
			if found != len(fields) {
				t.Fatalf("the manifest's record %d of %s lacks one of %v: %v", k, file, fields, values)
			}
			out = append(out, rec)
		}
		if len(out) == 0 {
			t.Fatalf("the manifest's row for %s carries no %v values", file, fields)
		}
		return out
	}
	t.Fatalf("the manifest carries no row for %s", file)
	return nil
}
