// The SCHEMA LOCK, both ways (docs/SPEC-TABLES.md §2.10): every edit the rule
// allows moves the file and passes ONCE THE LOCK IS WRITTEN, and every edit it
// forbids is refused with the entry named — by the check and by `schema lock`
// alike.
//
// The fixture is one package with a lock, edited in a temp directory, so each
// case is the whole path a user takes — schema on disk, lock beside it, the
// driver's check on load, `schema lock` to move it.
package lockfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// base is the fixture package: two FIXED tables, one of them holding a
// deprecated field already, and a VARIABLE-LENGTH one beside them that the
// lock must not carry.
const base = `package lockdemo

fixed table ShipConfig
{
    name    string(32)
    speed   float32
    armor   uint8
}

fixed table Slot
{
    index uint16
    live  bool
}

table Chain
{
    next *Chain
}
`

// fixture writes a schema into a fresh directory and returns the directory and
// the unit's paths.
func fixture(t *testing.T, src string) (dir string, paths []string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	return dir, paths
}

// load compiles the fixture with no lock check, which is what `schema lock`
// itself does.
func load(t *testing.T, paths []string) *ir.Unit {
	t.Helper()
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("the fixture does not compile: %v", err)
	}
	return u
}

// locked writes the fixture, locks it, then rewrites the schema to after and
// returns the refusals the check produces over the committed lock.
func locked(t *testing.T, after string) (dir string, paths []string, errs []error) {
	t.Helper()
	dir, paths = fixture(t, base)
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatalf("locking the fixture: %v", err)
	}
	if after != "" {
		if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(after), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, paths, lockfile.Check(load(t, paths), paths)
}

// readLock reads and parses a committed lock.
func readLock(t *testing.T, path string) *lockfile.Unit {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lk, err := lockfile.Parse(path, data)
	if err != nil {
		t.Fatal(err)
	}
	return lk
}

// refuses is the shape every negative case takes: exactly one refusal, naming
// the table, the entry and the field.
func refuses(t *testing.T, errs []error, want ...string) {
	t.Helper()
	if len(errs) != 1 {
		t.Fatalf("want one refusal, got %d: %v", len(errs), errs)
	}
	got := errs[0].Error()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("the refusal must name %q:\n%s", w, got)
		}
	}
	if !strings.Contains(got, "docs/SPEC-TABLES.md §2.10") {
		t.Errorf("every refusal cites the spec:\n%s", got)
	}
}

// ---- what the lock holds ----

// TestLockHoldsFixedTablesOnly is the set the file covers: the fixed tables
// and not the variable-length one beside them, whose evolution the id-table
// wire handles on its own (§2.2, §4).
func TestLockHoldsFixedTablesOnly(t *testing.T) {
	_, paths := fixture(t, base)
	lk := lockfile.Render(load(t, paths))
	if lk.Package != "lockdemo" {
		t.Errorf("package = %q", lk.Package)
	}
	var names []string
	for _, tb := range lk.Tables {
		names = append(names, tb.Name)
	}
	if strings.Join(names, ",") != "ShipConfig,Slot" {
		t.Errorf("the lock holds the FIXED tables in name order, got %v", names)
	}
	ship := lk.Table("ShipConfig")
	if len(ship.Entries) != 3 {
		t.Fatalf("ShipConfig has three entries, got %d", len(ship.Entries))
	}
	if ship.Entries[0].Name != "name" || ship.Entries[0].Width != 40 {
		// a string(32) is its whole storage — the character buffer AND the
		// int32 used length beside it, padding included (§19.3)
		t.Errorf("entry 1 is the whole storage of name, got %+v", ship.Entries[0])
	}
	if ship.Layout != lockfile.LayoutHash(ship.Entries) {
		t.Errorf("the layout hash is the hash of the entries")
	}
}

// TestTextRoundTrips is the file the compiler writes read back as what it
// wrote: the parse and the render are one format, not two.
func TestTextRoundTrips(t *testing.T) {
	_, paths := fixture(t, base)
	want := lockfile.Render(load(t, paths))
	got, err := lockfile.Parse("schema.lock", []byte(want.Text()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Text() != want.Text() {
		t.Errorf("round trip:\n--- wrote ---\n%s\n--- read back ---\n%s", want.Text(), got.Text())
	}
}

// ---- the edits the rule allows, and the lock that has to catch up ----
//
// Each of these is an append in the rule's sense, so `schema lock` writes it —
// and each of them is a CHANGE, so the compile before that write is refused.
// The lock in the tree is the record, and a record is current or it is
// nothing.

// TestAppendRefusedUntilLocked is the whole point of the rule and the STRICT
// reading of it in one test: a field at the BOTTOM is the change the rule
// allows, so `schema lock` extends the file by exactly that entry — and until
// it does, the compile is refused, naming the entry the lock lacks and the
// command that writes it.
func TestAppendRefusedUntilLocked(t *testing.T) {
	after := strings.Replace(base, "    armor   uint8\n", "    armor   uint8\n    shields uint8\n", 1)
	dir, paths, errs := locked(t, after)
	refuses(t, errs, "fixed table ShipConfig", "entry 4", "field shields",
		"in the declaration and not in the lock", "write it with `schema lock`")
	before := readLock(t, filepath.Join(dir, lockfile.FileName))
	path, rewrote, err := lockfile.Update(load(t, paths), paths)
	if err != nil {
		t.Fatalf("schema lock appends: %v", err)
	}
	if !rewrote {
		t.Fatalf("the lock moved and %s was not rewritten", path)
	}
	after2 := readLock(t, path)
	// the old entries are a PREFIX of the new ones, entry for entry: that is
	// the whole rule, and it is what `schema lock` is allowed to write
	old, now := before.Table("ShipConfig").Entries, after2.Table("ShipConfig").Entries
	if len(now) != len(old)+1 {
		t.Fatalf("the lock grew by one entry: %d then %d", len(old), len(now))
	}
	for i := range old {
		if old[i] != now[i] {
			t.Errorf("entry %d moved: %+v then %+v", i+1, old[i], now[i])
		}
	}
	if now[len(old)].Name != "shields" {
		t.Errorf("the appended field is the last entry: %+v", now[len(old)])
	}
	// and with the lock written, the same compile passes
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Errorf("the written lock checks clean: %v", errs)
	}
	// and the extended lock is idempotent
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || rewrote {
		t.Errorf("a current lock is left alone: rewrote=%v err=%v", rewrote, err)
	}
}

// TestNewTableRefusedUntilLocked: a table the lock does not carry has promised
// nothing, so `schema lock` adds it — and until it has, the lock is behind the
// unit and the compile is refused at the table's first entry.
func TestNewTableRefusedUntilLocked(t *testing.T) {
	after := base + "\nfixed table Beacon\n{\n    hz uint8\n}\n"
	dir, paths, errs := locked(t, after)
	refuses(t, errs, "fixed table Beacon", "entry 1", "field hz",
		"in the declaration and not in the lock", "write it with `schema lock`")
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "fixed table Beacon ") {
		t.Errorf("the new table is in the lock:\n%s", got)
	}
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Errorf("the written lock checks clean: %v", errs)
	}
}

// A fieldless table still has a named stale-lock finding, but cannot be
// appended: the emitted fixed root violates the nonzero rule in §1.1.
func TestNewFieldlessTableCannotBeLocked(t *testing.T) {
	after := base + "\nfixed table Marker\n{\n}\n"
	dir, paths, errs := locked(t, after)
	refuses(t, errs, "fixed table Marker is in the declaration and not in the lock",
		"write it with `schema lock`")
	if strings.Contains(errs[0].Error(), "entry ") {
		t.Errorf("there is no entry to name: %v", errs[0])
	}
	file := filepath.Join(dir, lockfile.FileName)
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err == nil || rewrote || !strings.Contains(err.Error(), "layout_record_too_large") {
		t.Fatalf("emitted zero root must refuse before append: rewrote=%v err=%v", rewrote, err)
	}
	afterBytes, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterBytes) != string(before) {
		t.Fatal("refused append changed the existing lock")
	}
}

// TestWasRenameIsNotAChange: `was` keeps the wire id, and the lock records
// wire names — so a rename moves not one line of the file (§5).
func TestWasRenameIsNotAChange(t *testing.T) {
	after := strings.Replace(base, "    speed   float32\n", "    velocity float32 | was = \"speed\"\n", 1)
	dir, paths, errs := locked(t, after)
	if len(errs) != 0 {
		t.Fatalf("a was rename is not a change: %v", errs)
	}
	before, err := os.ReadFile(filepath.Join(dir, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || rewrote {
		t.Errorf("a rename moves no line: rewrote=%v err=%v", rewrote, err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, lockfile.FileName))
	if string(got) != string(before) {
		t.Errorf("the file moved:\n--- was ---\n%s\n--- now ---\n%s", before, got)
	}
	if !strings.Contains(string(got), "field speed ") {
		t.Errorf("the lock records the WIRE name:\n%s", got)
	}
}

// TestDeprecateIsAllowedOnceLocked: the marker turns on and `schema lock`
// flips the flag in place — the entry keeps its position, its id and its
// width, because deprecation is about meaning and never about layout. It is a
// change all the same, so the compile before the flip is written is refused.
func TestDeprecateIsAllowedOnceLocked(t *testing.T) {
	after := strings.Replace(base, "    armor   uint8\n", "    armor   uint8 | deprecated\n", 1)
	dir, paths, errs := locked(t, after)
	refuses(t, errs, "fixed table ShipConfig", "entry 3", "field armor",
		"deprecated in the declaration and live in the lock", "write it with `schema lock`")
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || !rewrote {
		t.Fatalf("the lock flips the flag: rewrote=%v err=%v", rewrote, err)
	}
	got, err := os.ReadFile(filepath.Join(dir, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	lk, err := lockfile.Parse("schema.lock", got)
	if err != nil {
		t.Fatal(err)
	}
	armor := lk.Table("ShipConfig").Entries[2]
	if armor.Name != "armor" || !armor.Deprecated {
		t.Errorf("entry 3 is armor, deprecated: %+v", armor)
	}
	if armor.Width != 1 || armor.Kind == 0 {
		t.Errorf("deprecation moves no byte and changes no kind: %+v", armor)
	}
	// and the lock still checks clean against itself
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Errorf("the flipped lock checks clean: %v", errs)
	}
}

// TestDeprecatedFieldStillHasItsSlot is the layout claim the marker makes,
// measured rather than asserted: the record's size and every offset in it are
// what they were before the marker.
func TestDeprecatedFieldStillHasItsSlot(t *testing.T) {
	_, livePaths := fixture(t, base)
	live := ir.RecordLayout(load(t, livePaths), load(t, livePaths).Tables["ShipConfig"])
	_, depPaths := fixture(t, strings.Replace(base, "    armor   uint8\n", "    armor   uint8 | deprecated\n", 1))
	du := load(t, depPaths)
	dep := ir.RecordLayout(du, du.Tables["ShipConfig"])
	if live.Size != dep.Size || len(live.Fields) != len(dep.Fields) {
		t.Fatalf("deprecation moved the record: %d/%d fields, %d/%d bytes", len(live.Fields), len(dep.Fields), live.Size, dep.Size)
	}
	for i := range live.Fields {
		if live.Fields[i].Offset != dep.Fields[i].Offset || live.Fields[i].Size != dep.Fields[i].Size {
			t.Errorf("field %s moved: %+v then %+v", live.Fields[i].Field.Name, live.Fields[i], dep.Fields[i])
		}
	}
	if !du.Tables["ShipConfig"].Fields[2].Deprecated {
		t.Errorf("the marker reaches the IR")
	}
}

// ---- the edits the rule refuses ----

// reordered is the fixture with speed and armor swapped — the break both
// verbs refuse.
const reordered = `package lockdemo

fixed table ShipConfig
{
    name    string(32)
    armor   uint8
    speed   float32
}

fixed table Slot
{
    index uint16
    live  bool
}

table Chain
{
    next *Chain
}
`

func TestReorderRefused(t *testing.T) {
	_, _, errs := locked(t, reordered)
	refuses(t, errs, "fixed table ShipConfig", "entry 2", "field speed", "APPEND-ONLY", "never inserted, moved or removed")
}

// TestRemovalRefused: a field taken out of the MIDDLE is caught at its own
// entry, where the declaration now holds the field that followed it — which is
// exactly what a reader at that offset would find.
func TestRemovalRefused(t *testing.T) {
	after := strings.Replace(base, "    speed   float32\n", "", 1)
	_, _, errs := locked(t, after)
	refuses(t, errs, "fixed table ShipConfig", "entry 2", "field speed", "is field armor", "never inserted, moved or removed")
}

func TestRemovalOfTheLastFieldRefused(t *testing.T) {
	after := strings.Replace(base, "    armor   uint8\n", "", 1)
	_, _, errs := locked(t, after)
	refuses(t, errs, "fixed table ShipConfig", "entry 3", "field armor", "gone from the declaration")
}

// TestWidenTakenAsAChangeToRecord grows a capacity: under the monotone law
// (docs/FIXED-FORM-BILL-READS-BACKWARD.md §2, Glenn's ruling that the lock is
// the law's home) a widening is not a break — every value an older writer
// could write still lands — so it is a change the RECORD must carry, which is
// the one refusal that names `schema lock`, and that command writes it.
func TestWidenTakenAsAChangeToRecord(t *testing.T) {
	after := strings.Replace(base, "    name    string(32)\n", "    name    string(64)\n", 1)
	_, paths, errs := locked(t, after)
	refuses(t, errs, "fixed table ShipConfig", "entry 1", "field name", "write it with `schema lock`")
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || !rewrote {
		t.Fatalf("`schema lock` writes a widening: rewrote=%v err=%v", rewrote, err)
	}
}

// TestNarrowRefused is the other half of the same fact: a capacity cut is a
// break, and the refusal names the rule and BOTH VALUES.
func TestNarrowRefused(t *testing.T) {
	after := strings.Replace(base, "    name    string(32)\n", "    name    string(16)\n", 1)
	_, _, errs := locked(t, after)
	refuses(t, errs, "fixed table ShipConfig", "entry 1", "field name", "capacity narrowed (32 -> 16)")
}

// TestWidenScalarTakenAsAChangeToRecord is the same rule over a scalar swap:
// `uint8` to `uint32` is a read (the reader zero-extends), so the lock records
// it rather than refusing it.
func TestWidenScalarTakenAsAChangeToRecord(t *testing.T) {
	after := strings.Replace(base, "    armor   uint8\n", "    armor   uint32\n", 1)
	_, paths, errs := locked(t, after)
	refuses(t, errs, "fixed table ShipConfig", "entry 3", "field armor", "write it with `schema lock`")
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || !rewrote {
		t.Fatalf("`schema lock` writes a widening: rewrote=%v err=%v", rewrote, err)
	}
}

// TestNarrowScalarRefused: the same swap the other way is refused by name.
func TestNarrowScalarRefused(t *testing.T) {
	before := strings.Replace(base, "    armor   uint8\n", "    armor   uint32\n", 1)
	_, _, errs := lockedFrom(t, before, base)
	refuses(t, errs, "fixed table ShipConfig", "entry 3", "field armor", "narrowed (uint32 -> uint8)")
}

// TestKindChangeRefused is the edit that moves no byte and still lies: float32
// and int32 are four bytes each, so only the KIND says the stored bits are
// being read as something else.
func TestKindChangeRefused(t *testing.T) {
	after := strings.Replace(base, "    speed   float32\n", "    speed   int32\n", 1)
	_, _, errs := locked(t, after)
	refuses(t, errs, "fixed table ShipConfig", "entry 2", "field speed", "in the lock and kind", "keeps its type")
}

func TestInsertInTheMiddleRefused(t *testing.T) {
	after := strings.Replace(base, "    speed   float32\n", "    hull    uint8\n    speed   float32\n", 1)
	_, _, errs := locked(t, after)
	refuses(t, errs, "fixed table ShipConfig", "entry 2", "field speed", "is field hull", "added at the BOTTOM")
}

// TestUnDeprecateRefused: DEPRECATION IS ONE WAY
// (docs/FIXED-FORM-BILL-READS-BACKWARD.md §12.3, which restores this test's
// original answer). Every writer that ran while the field was deprecated left
// the DEFAULT in the slot, so "what would come back is not data" — and
// `schema lock` will not write the flag off either, because the lock is not a
// way to make the refusal go away.
func TestUnDeprecateRefused(t *testing.T) {
	dir, paths := fixture(t, strings.Replace(base, "    armor   uint8\n", "    armor   uint8 | deprecated\n", 1))
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	refuses(t, lockfile.Check(load(t, paths), paths),
		"fixed table ShipConfig", "entry 3", "field armor",
		"undeprecated", "deprecation is ONE WAY")
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err == nil || rewrote {
		t.Fatalf("`schema lock` does not write the flag off: rewrote=%v err=%v", rewrote, err)
	}
}

// TestTableGoneRefused: a fixed table's layout is a promise, and a promise is
// not withdrawn — compaction is a new table under a new name.
func TestTableGoneRefused(t *testing.T) {
	after := strings.Replace(base, "fixed table Slot\n{\n    index uint16\n    live  bool\n}\n", "", 1)
	_, _, errs := locked(t, after)
	refuses(t, errs, "fixed table Slot", "no longer declares it as a fixed table", "NEW table under a NEW name")
}

// TestHandEditedLockRefused is the file caught lying: the entries and the
// layout hash beside them are one statement made twice, so a hand that edits
// one half and not the other is visible.
func TestHandEditedLockRefused(t *testing.T) {
	dir, paths := fixture(t, base)
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, lockfile.FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// the lie a widening tempts: edit the width in the lock to match the
	// schema you are about to write
	lines := strings.Split(string(data), "\n")
	edited := false
	for i, line := range lines {
		if strings.Contains(line, "field armor ") {
			lines[i] = strings.Replace(line, "width=1", "width=4", 1)
			edited = lines[i] != line
		}
	}
	if !edited {
		t.Fatal("the fixture's armor entry is not what this test edits")
	}
	lie := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(lie), 0o644); err != nil {
		t.Fatal(err)
	}
	refuses(t, lockfile.Check(load(t, paths), paths),
		"fixed table ShipConfig", "entries that hash to", "a hand-edit does not hold", "schema lock")
}

// ---- the command's own refusal ----

// TestLockRefusesToWriteABreak: `schema lock` is not the way to make a refusal
// go away. It runs the check's comparison first and leaves the file alone.
func TestLockRefusesToWriteABreak(t *testing.T) {
	dir, paths := fixture(t, base)
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, lockfile.FileName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// a NARROWING, since the monotone law took the widening
	// (docs/FIXED-FORM-BILL-READS-BACKWARD.md §2): `armor uint8` grown to
	// uint32 is a read, and a string capacity cut in half is not.
	after := strings.Replace(base, "    name    string(32)\n", "    name    string(16)\n", 1)
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}
	_, rewrote, err := lockfile.Update(load(t, paths), paths)
	if err == nil {
		t.Fatal("schema lock appends, and a narrowing is not an append")
	}
	if rewrote {
		t.Error("a refused lock writes nothing")
	}
	if !strings.Contains(err.Error(), "not an append") {
		t.Errorf("the refusal says why: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(before) {
		t.Errorf("the file was left alone:\n--- was ---\n%s\n--- now ---\n%s", before, got)
	}
}

// TestLockRefusesWhatTheCheckRefuses: the two verbs part company on ONE
// reading — a declaration the lock has not caught up to, which is the whole
// reason this command exists — and on nothing else. A reorder, a removal and
// a kind change are refused by both, and none of them moves the file.
func TestLockRefusesWhatTheCheckRefuses(t *testing.T) {
	for _, tc := range []struct{ name, after, want string }{
		{"reorder", reordered, "never inserted, moved or removed"},
		{"removal from the end", strings.Replace(base, "    armor   uint8\n", "", 1), "gone from the declaration"},
		{"kind change", strings.Replace(base, "    speed   float32\n", "    speed   int32\n", 1), "keeps its type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, paths := fixture(t, base)
			if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, lockfile.FileName)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(tc.after), 0o644); err != nil {
				t.Fatal(err)
			}
			// the check refuses it
			refuses(t, lockfile.Check(load(t, paths), paths), tc.want)
			// and so does the one writer, with the file left alone
			_, rewrote, err := lockfile.Update(load(t, paths), paths)
			if err == nil || rewrote {
				t.Fatalf("`schema lock` appends, and this is not an append: rewrote=%v err=%v", rewrote, err)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "not an append") {
				t.Errorf("the refusal says which break and why: %v", err)
			}
			if got, _ := os.ReadFile(path); string(got) != string(before) {
				t.Errorf("a refused lock writes nothing:\n--- was ---\n%s\n--- now ---\n%s", before, got)
			}
		})
	}
}

// TestNoLockNoCheck: a unit that has never been locked promises nothing.
func TestNoLockNoCheck(t *testing.T) {
	_, paths := fixture(t, strings.Replace(base, "    armor   uint8\n", "    armor   uint32\n", 1))
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Errorf("no file, no check: %v", errs)
	}
}

// TestDriverRefusesOnLoad is the check where a user meets it: the driver's
// policy on, the committed lock read, and the load refusing.
func TestDriverRefusesOnLoad(t *testing.T) {
	dir, paths := fixture(t, base)
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	// a NARROWING, since the monotone law took the widening
	// (docs/FIXED-FORM-BILL-READS-BACKWARD.md §2): `armor uint8` grown to
	// uint32 is a read, and a string capacity cut in half is not.
	after := strings.Replace(base, "    name    string(32)\n", "    name    string(16)\n", 1)
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	c.SchemaLock = true
	if _, err := c.Load(paths); err == nil {
		t.Fatal("the check runs on load")
	} else if !strings.Contains(err.Error(), "docs/SPEC-TABLES.md §2.10") {
		t.Errorf("the refusal reaches the caller intact: %v", err)
	}
	// and with the policy off, the same unit loads
	if _, err := compiler.New().Load(paths); err != nil {
		t.Errorf("the check is the driver's policy, not the checker's: %v", err)
	}
}

// TestGuardOnDeprecatedFieldRefused is the one NEW USE the compiler refuses
// (§2.10): a guard is the only construct that reads another field's value, and
// a retired field's value is whatever the last writer that cared left behind.
func TestGuardOnDeprecatedFieldRefused(t *testing.T) {
	src := `package lockdemo

table Body
{
    active bool | deprecated
    if active
    {
        hp uint8
    }
}
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.New().Load(paths)
	if err == nil {
		t.Fatal("a guard on a deprecated field is a new use")
	}
	for _, w := range []string{"if condition active", "| deprecated", "NEW USE", "docs/SPEC-TABLES.md §2.10"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("the refusal must name %q: %v", w, err)
		}
	}
}
