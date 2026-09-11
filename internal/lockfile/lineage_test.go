// THE LINEAGE: every layout a fixed table has ever had, oldest first
// (docs/FIXED-FORM-BILL-READS-BACKWARD.md §6b, §11.6, §11.7, §11.8;
// docs/FIXED-FORM-ALGORITHM.md §5.2's table of what the lock must provide to
// COMPILE).
//
// The lock's field entries are the LAW's home — what a widening may do. The
// lineage is the RECORD's home — what was actually shipped, in the form
// COMPILE reads: the wire hash a file carries, the layout bytes LOAD compares
// a file against, the definitions digest the hash binds, and the record size
// taken from the lock and never from the file.
package lockfile_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wireHashOf is the number a file's header and every record carry, computed
// from the declaration the way every backend computes it (ir.TableFixedLayoutHash
// over ir.TableFixedLayoutBytes). The lineage entry must agree with it exactly:
// that agreement is the whole point of recording the WIRE hash rather than the
// lock's own text projection.
func wireHashOf(t *testing.T, paths []string, table string) (uint64, []byte, int64) {
	t.Helper()
	u := load(t, paths)
	st := u.Tables[table]
	if st == nil {
		t.Fatalf("the fixture declares %s", table)
	}
	entries := ir.TableFixedWalkRoot(st)
	layout := ir.TableFixedLayoutBytes(entries)
	return ir.TableFixedLayoutHash(layout, st), layout, ir.TableFixedTypeBytes(st)
}

// lineageOf is the accessor COMPILE uses, read back off the committed file.
func lineageOf(t *testing.T, path, table string) []lockfile.LineageEntry {
	t.Helper()
	return lockfile.Lineage(readLock(t, path), table)
}

// TestLockLineageFirstEntryIsTheFirstLayout: a unit's first lock records one
// lineage entry, and it is the declaration's own layout (bill §11.9 — "a fixed
// table's layout is locked before its first file ships; the first lineage entry
// is the first layout").
func TestLockLineageFirstEntryIsTheFirstLayout(t *testing.T) {
	dir, paths := fixture(t, rowTable("    a int32\n    b int32"))
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	want, layout, record := wireHashOf(t, paths, "Row")
	got := lineageOf(t, filepath.Join(dir, lockfile.FileName), "Row")
	if len(got) != 1 {
		t.Fatalf("the first lock holds one lineage entry, got %d", len(got))
	}
	if got[0].Wire != want {
		t.Errorf("the entry carries the WIRE hash: lock 0x%016x, the file header 0x%016x", got[0].Wire, want)
	}
	if string(got[0].Layout) != string(layout) {
		t.Errorf("the entry carries the layout bytes verbatim:\n  lock %x\n  wire %x", got[0].Layout, layout)
	}
	if got[0].Record != record {
		t.Errorf("the entry carries the record body size: lock %d, declaration %d", got[0].Record, record)
	}
	if got[0].Retired {
		t.Error("a fresh entry is not retired")
	}
}

// TestLockLineageGainsAnEntryOnAWidening is §11.6: a change of layout hash AT
// COMMIT earns one entry, the old one stays, and the order is OLDEST FIRST.
func TestLockLineageGainsAnEntryOnAWidening(t *testing.T) {
	dir, paths := fixture(t, rowTable("    a int32\n    b int32"))
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, lockfile.FileName)
	first, firstBytes, _ := wireHashOf(t, paths, "Row")

	// the widening: one appended field, which moves the layout's entry count
	// and so the hash
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(rowTable("    a int32\n    b int32\n    c int32")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || !rewrote {
		t.Fatalf("`schema lock` takes the widening: rewrote=%v err=%v", rewrote, err)
	}
	second, secondBytes, secondRecord := wireHashOf(t, paths, "Row")
	if first == second {
		t.Fatal("an appended field moves the layout hash; the fixture proves nothing")
	}

	got := lineageOf(t, path, "Row")
	if len(got) != 2 {
		t.Fatalf("a widening appends one lineage entry: want 2, got %d", len(got))
	}
	if got[0].Wire != first || string(got[0].Layout) != string(firstBytes) {
		t.Errorf("OLDEST FIRST, bytes unchanged: got 0x%016x, want 0x%016x", got[0].Wire, first)
	}
	if got[1].Wire != second || string(got[1].Layout) != string(secondBytes) || got[1].Record != secondRecord {
		t.Errorf("the CURRENT layout is the last entry: got 0x%016x, want 0x%016x", got[1].Wire, second)
	}

	// and a second widening appends a third, keeping both older ones
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(rowTable("    a int32\n    b int32\n    c int32\n    d int32")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	got = lineageOf(t, path, "Row")
	if len(got) != 3 || got[0].Wire != first || got[1].Wire != second {
		t.Fatalf("the lineage only grows, oldest first: %d entries", len(got))
	}
}

// TestLockLineageUnchangedHashAddsNothing: the lineage is one entry per LAYOUT,
// not one per commit. Deprecating a field rewrites the lock — the entry line
// gains `deprecated` — and moves no byte of the layout, so the lineage does not
// move either (bill §11.6, §12.3: the slot stays and is read on every plan).
func TestLockLineageUnchangedHashAddsNothing(t *testing.T) {
	dir, paths := fixture(t, rowTable("    a int32\n    b int32"))
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, lockfile.FileName)
	want, _, _ := wireHashOf(t, paths, "Row")

	// re-locking an unchanged unit writes nothing at all
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || rewrote {
		t.Fatalf("`schema lock` is idempotent: rewrote=%v err=%v", rewrote, err)
	}
	if got := lineageOf(t, path, "Row"); len(got) != 1 {
		t.Fatalf("re-locking an unchanged unit adds no entry: %d", len(got))
	}

	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(rowTable("    a int32\n    b int32 | deprecated")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || !rewrote {
		t.Fatalf("`schema lock` writes the deprecation: rewrote=%v err=%v", rewrote, err)
	}
	after, _, _ := wireHashOf(t, paths, "Row")
	if after != want {
		t.Fatal("a deprecation moves no byte of the layout; the fixture proves nothing")
	}
	got := lineageOf(t, path, "Row")
	if len(got) != 1 {
		t.Fatalf("an unchanged layout hash adds no lineage entry: %d entries", len(got))
	}
	if got[0].Wire != want {
		t.Errorf("the one entry is still the one layout: got 0x%016x, want 0x%016x", got[0].Wire, want)
	}
}

// TestLockV6SalvagesIntoV7WithOneEntry is §11.8: "a rendering-version bump
// SALVAGES, never deletes." A committed v6 lock holds no lineage, so the one
// layout it holds becomes the first lineage entry, and the field lines it holds
// are carried forward unchanged rather than wiped fleet-wide.
func TestLockV6SalvagesIntoV7WithOneEntry(t *testing.T) {
	dir, paths := fixture(t, rowTable("    a int32\n    b int32"))
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, lockfile.FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// the v6 file this lock would have been one rendering version ago: the
	// same lines, without the lineage the version added
	var v6 []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "lineage ") {
			continue
		}
		v6 = append(v6, line)
	}
	old := strings.Replace(strings.Join(v6, "\n"), "schema-lock 7", "schema-lock 6", 1)
	if old == strings.Join(v6, "\n") {
		t.Fatal("this test writes the PREVIOUS rendering version's file")
	}
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	// it READS: a v6 lock is salvage, not a delete, so the law still holds
	// against it and the check is silent
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Fatalf("a v6 lock salvages rather than refusing: %v", errs)
	}
	// and `schema lock` rewrites it as v7 with ONE lineage entry, the single
	// layout it held
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || !rewrote {
		t.Fatalf("`schema lock` salvages the old rendering: rewrote=%v err=%v", rewrote, err)
	}
	lk := readLock(t, path)
	if lk.Version != lockfile.Version {
		t.Errorf("the salvage writes the current rendering version: %d", lk.Version)
	}
	want, _, _ := wireHashOf(t, paths, "Row")
	got := lockfile.Lineage(lk, "Row")
	if len(got) != 1 || got[0].Wire != want {
		t.Fatalf("the v6 lock's single layout is the first lineage entry: %d entries", len(got))
	}
	// the field lines came through unchanged: the law is the same law
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Fatalf("the salvaged lock is current: %v", errs)
	}
}

// TestLockLineageHandEditRefused: the wire hash is fnv1a64 over the layout
// bytes and the digest, so the three facts on a lineage line are one statement
// written twice and a hand that moves one of them is caught — the same way the
// layout= hash catches a hand-edited entry line.
func TestLockLineageHandEditRefused(t *testing.T) {
	dir, paths := fixture(t, rowTable("    a int32\n    b int32"))
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, lockfile.FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lk := readLock(t, path)
	entry := lockfile.Lineage(lk, "Row")[0]
	hex := make([]byte, 0, len(entry.Layout))
	for _, b := range entry.Layout {
		hex = append(hex, "0123456789abcdef"[b>>4], "0123456789abcdef"[b&0xf])
	}
	lie := strings.Replace(string(data), "bytes="+string(hex), "bytes="+string(hex[:len(hex)-2])+"ff", 1)
	if lie == string(data) {
		t.Fatal("the lineage line carries the layout bytes as hex")
	}
	if err := os.WriteFile(path, []byte(lie), 0o644); err != nil {
		t.Fatal(err)
	}
	errs := lockfile.Check(load(t, paths), paths)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "schema lock") {
		t.Fatalf("a hand-edited lineage entry is caught and names the remedy: %v", errs)
	}
}

// TestLockLineageWarnsPastTheAdvisoryBound is §11.6's warning: "a warning names
// a lineage past thirty-two entries per table; the remedies are the floor and
// the new table." It is a warning and not a refusal — every layout in the list
// is real — and it is said where the person who moved the file sees it.
func TestLockLineageWarnsPastTheAdvisoryBound(t *testing.T) {
	dir, paths := fixture(t, rowTable("    f0 int32"))
	fields := "    f0 int32"
	for i := 1; i <= 33; i++ {
		if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
			t.Fatalf("widening %d: %v", i, err)
		}
		if got := lockfile.Warnings(paths); (len(got) != 0) != (i > 32) {
			t.Fatalf("%d layouts: %d warnings", i, len(got))
		}
		fields += fmt.Sprintf("\n    f%d int32", i)
		if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(rowTable(fields)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	got := lockfile.Warnings(paths)
	if len(got) != 1 || !strings.Contains(got[0], "carries 34 layouts") || !strings.Contains(got[0], "--retire Row@<hash>") {
		t.Fatalf("the warning names the table, the count and both remedies: %v", got)
	}
	// and the lock still READS: a long lineage is never a refusal
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Fatalf("a long lineage is a warning, not a break: %v", errs)
	}
	if n := len(lineageOf(t, filepath.Join(dir, lockfile.FileName), "Row")); n != 34 {
		t.Errorf("every layout stays: %d", n)
	}
}

// ---- THE RETIRE VERBS: the one non-append edit this file permits ----

// TestLockRetireEntry is §11.4: "`schema lock --retire Table@<hash>` marks a
// lineage entry retired (a permitted non-append edit, recorded with a reason);
// COMPILE emits the retired hashes beside the supported ones so LOAD can say
// `layout_unsupported` rather than `layout_newer`; a retired entry stays in the
// lineage forever."
func TestLockRetireEntry(t *testing.T) {
	dir, paths := fixture(t, rowTable("    a int32"))
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, lockfile.FileName)
	first, _, _ := wireHashOf(t, paths, "Row")
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(rowTable("    a int32\n    b int32")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	second, _, _ := wireHashOf(t, paths, "Row")

	reason := "the 0.3 client is gone; nothing live writes this layout"
	if _, rewrote, err := lockfile.Retire(load(t, paths), paths, fmt.Sprintf("Row@0x%016x", first), reason); err != nil || !rewrote {
		t.Fatalf("--retire takes one lineage entry: rewrote=%v err=%v", rewrote, err)
	}
	got := lineageOf(t, path, "Row")
	if len(got) != 2 {
		t.Fatalf("a retired entry STAYS in the lineage: %d entries", len(got))
	}
	if !got[0].Retired || got[0].Reason != reason || got[0].Wire != first {
		t.Errorf("the named entry is retired, with its reason: %+v", got[0])
	}
	if got[1].Retired || got[1].Wire != second {
		t.Errorf("no other entry moves: %+v", got[1])
	}
	if f := lockfile.Floor(readLock(t, path), "Row"); f != 1 {
		t.Errorf("the floor is one past the highest retired index: %d", f)
	}
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Fatalf("a retirement is not a break: %v", errs)
	}
	// the current layout cannot be retired: a reader built from this lock reads
	// its own records
	if _, _, err := lockfile.Retire(load(t, paths), paths, fmt.Sprintf("Row@0x%016x", second), "no"); err == nil {
		t.Error("retiring the CURRENT layout is refused")
	}
	// and a hash nothing in the lineage carries is a refusal that says so
	if _, _, err := lockfile.Retire(load(t, paths), paths, "Row@0x0000000000000001", "no"); err == nil {
		t.Error("a hash the lineage does not carry is refused")
	}
	// a retirement is a declaration, so it wants a sentence
	if _, _, err := lockfile.Retire(load(t, paths), paths, fmt.Sprintf("Row@0x%016x", first), ""); err == nil {
		t.Error("--retire wants --reason")
	}
}

// TestLockRetireTableThenRemoveTheDeclaration is §11.5: "A table is RETIRED,
// never removed, until nothing live speaks it. `schema lock --retire Table`
// marks the whole table retired; its lineage stays; the schema may then drop
// the declaration, and §5.1's 'fixed removed' refusal applies only to an
// UNRETIRED table."
func TestLockRetireTableThenRemoveTheDeclaration(t *testing.T) {
	const two = "package lockrows\n\nfixed table Row\n{\n    a int32\n}\n\nfixed table Keep\n{\n    k int32\n}\n"
	dir, paths := fixture(t, two)
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, lockfile.FileName)
	was, _, _ := wireHashOf(t, paths, "Row")

	// FIRST the refusal, on the UNRETIRED table: dropping the declaration is a
	// promise withdrawn
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte("package lockrows\n\nfixed table Keep\n{\n    k int32\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refuses(t, lockfile.Check(load(t, paths), paths),
		"fixed table Row", "fixed removed", "--retire Row")
	if _, _, err := lockfile.Update(load(t, paths), paths); err == nil {
		t.Error("`schema lock` does not drop an unretired fixed table either")
	}

	// the declaration back, and the table retired
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(two), 0o644); err != nil {
		t.Fatal(err)
	}
	reason := "nothing live speaks Row: the last writer shipped 2026-01"
	if _, rewrote, err := lockfile.Retire(load(t, paths), paths, "Row", reason); err != nil || !rewrote {
		t.Fatalf("--retire takes a whole table: rewrote=%v err=%v", rewrote, err)
	}
	lk := readLock(t, path)
	if tbl := lk.Table("Row"); tbl == nil || !tbl.Retired || tbl.Reason != reason {
		t.Fatalf("the table is marked retired, with its reason: %+v", tbl)
	}
	if len(lockfile.Lineage(lk, "Row")) != 1 {
		t.Error("a retired table keeps its lineage")
	}

	// NOW the declaration may go, and the lock keeps the block and the lineage
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte("package lockrows\n\nfixed table Keep\n{\n    k int32\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Fatalf("a retired table's declaration may be dropped (§11.5): %v", errs)
	}
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	lk = readLock(t, path)
	tbl := lk.Table("Row")
	if tbl == nil || !tbl.Retired {
		t.Fatal("the retired table's block stays in the lock after the declaration goes")
	}
	if got := lockfile.Lineage(lk, "Row"); len(got) != 1 || got[0].Wire != was {
		t.Errorf("its lineage stays, byte for byte: %+v", got)
	}
	if lk.Table("Keep") == nil {
		t.Error("the live table is still locked")
	}
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Errorf("and the lock is current: %v", errs)
	}
	// a table nothing locked cannot be retired
	if _, _, err := lockfile.Retire(load(t, paths), paths, "Nope", "no"); err == nil {
		t.Error("a table the lock does not carry is refused")
	}
}

// TestLineageAccessorIsWhatCompileReads is step 4 of #898: the accessor a
// BACKEND calls, holding exactly what algorithm §5.2's table of lock facts asks
// for — a lineage OLDEST FIRST, per entry the wire hash, the layout bytes, the
// definitions digest and the record size, plus the floor as one number. It
// replaces the filename convention the C++ backend reads its lineage from today
// (internal/codegen/cpptable/lineage.go, an interim named as an interim): this
// test is the contract that interim is swapped for.
func TestLineageAccessorIsWhatCompileReads(t *testing.T) {
	dir, paths := fixture(t, rowTable("    a int32"))
	var hashes []uint64
	fields := "    a int32"
	for i := 0; i < 3; i++ {
		if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
			t.Fatal(err)
		}
		h, _, _ := wireHashOf(t, paths, "Row")
		hashes = append(hashes, h)
		fields += fmt.Sprintf("\n    f%d int32", i)
		if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(rowTable(fields)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(rowTable("    a int32\n    f0 int32\n    f1 int32")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	if _, _, err := lockfile.Retire(load(t, paths), paths, fmt.Sprintf("Row@0x%016x", hashes[0]), "the first client is gone"); err != nil {
		t.Fatal(err)
	}

	// ONE CALL from a backend that holds the unit's paths
	lock, ok, err := lockfile.Open(paths)
	if err != nil || !ok {
		t.Fatalf("Open reads the unit's lock: ok=%v err=%v", ok, err)
	}
	got := lockfile.Lineage(lock, "Row")
	if len(got) != 3 {
		t.Fatalf("three layouts, oldest first: %d", len(got))
	}
	for i, e := range got {
		if e.Wire != hashes[i] {
			t.Errorf("entry %d is 0x%016x, want 0x%016x (OLDEST FIRST)", i, e.Wire, hashes[i])
		}
		// the bytes are what LOAD compares a file against, and the hash binds
		// them together with the digest
		if len(e.Layout) == 0 || ir.TableFixedLayoutHash(e.Layout, nil) != e.Wire || len(e.Digest) != 0 {
			t.Errorf("entry %d: bytes=%x digest=%x do not hash to 0x%016x", i, e.Layout, e.Digest, e.Wire)
		}
		if e.Record <= 0 {
			t.Errorf("entry %d carries the record body size, got %d", i, e.Record)
		}
	}
	if !got[0].Retired || got[1].Retired || got[2].Retired {
		t.Errorf("the retired mark is per entry: %v %v %v", got[0].Retired, got[1].Retired, got[2].Retired)
	}
	if f := lockfile.Floor(lock, "Row"); f != 1 {
		t.Errorf("the floor is one number, one past the highest retired index: %d", f)
	}
	// a nested `type` has no lineage: it has no file of its own and no hash a
	// file carries
	if lockfile.Lineage(lock, "Nope") != nil {
		t.Error("a table nothing locked has no lineage")
	}
}

// TestLineageSetIsBoundByTheRollup is the hole the roll-up closes, stated as
// the hand that opens it: a lock with two layouts in its lineage, and one
// HAND that deletes the OLDER line.
//
// Nothing in the entry lines moves, nothing in the declaration moves, and the
// lineage's last entry is still the declaration's own layout — so every check
// the file had before the `lineage=` token was silent, and `schema lock`
// reported the file already current. What was lost is a SHIPPED layout: a
// reader compiled from the truncated lock lays down no plan for it, and
// yesterday's readable file answers `layout_newer` — "ship the reader" — for a
// reader that was shipped. Bill §11.8 forbids exactly that deletion, fleet
// wide, and the `lineage=` roll-up on the `fixed table` line is what makes a
// deletion visible: it is one statement made twice, the way `layout=` binds
// the entry lines.
func TestLineageSetIsBoundByTheRollup(t *testing.T) {
	dir, paths := fixture(t, rowTable("    a int32\n    b int32"))
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(rowTable("    a int32\n    b int32\n    c int32")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || !rewrote {
		t.Fatalf("`schema lock` takes the widening: rewrote=%v err=%v", rewrote, err)
	}
	path := filepath.Join(dir, lockfile.FileName)
	if got := lineageOf(t, path, "Row"); len(got) != 2 {
		t.Fatalf("the fixture wants two layouts in the lineage, got %d", len(got))
	}
	// and the check is silent on the file as the compiler wrote it
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Fatalf("the committed lock passes its own check: %v", errs)
	}

	// THE HAND: the OLDEST lineage line, gone
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	dropped := false
	for _, line := range strings.Split(string(data), "\n") {
		if !dropped && strings.HasPrefix(strings.TrimSpace(line), "lineage ") {
			dropped = true
			continue
		}
		kept = append(kept, line)
	}
	if !dropped {
		t.Fatal("this test deletes a lineage line")
	}
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	errs := lockfile.Check(load(t, paths), paths)
	if len(errs) == 0 {
		t.Fatal("a deleted lineage line is a DELETED SHIPPED LAYOUT: the check must refuse it (docs/FIXED-FORM-BILL-READS-BACKWARD.md §11.8)")
	}
	if !strings.Contains(errs[0].Error(), "lineage=") {
		t.Errorf("the refusal names the roll-up the file carries: %v", errs[0])
	}

	// and `schema lock` must not write over it either: the command that moves
	// this file is the one that must not be a way to make the refusal go away
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err == nil || rewrote {
		t.Fatalf("`schema lock` refuses a truncated lineage rather than reporting it current: rewrote=%v err=%v", rewrote, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != strings.Join(kept, "\n") {
		t.Error("a refusal writes nothing")
	}
}
