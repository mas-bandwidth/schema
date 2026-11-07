package tablewire_test

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// AN OLDER WRITER IS READ THROUGH THE LOCK, STRICTLY (docs/SPEC-TABLES.md §3.4,
// §21): the engine matches a form-3 file's header hash against the unit's
// schema.lock lineage, lands the value (widened, the reader's defaults where
// the writer had no field, an unknown variant as None), or refuses once by
// name. A value outside the declared range refuses; nothing is clamped. A
// NEWER writer is never read backward: it is layout_unknown, and so is any
// file of a unit with no lock.

const lineageOld = `package lin

enum Mode { Walk, Run }

fixed table Cfg
{
    speed int16 | min = -1000, max = 1000
    name  string(8)
    mode  Mode
    gone  int32
}
`

const lineageNew = `package lin

enum Mode { Walk, Run, Fly }

fixed table Cfg
{
    speed int32 | min = -1000, max = 1000
    name  string(16)
    mode  Mode
    gone  int32 | deprecated
    extra int32 = 5
}
`

// lineageUnit writes source as dir/Lin.schema and loads it.
func lineageUnit(t *testing.T, dir, source string) *tabletext.Model {
	t.Helper()
	path := filepath.Join(dir, "Lin.schema")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

// lockedPair is a directory whose lock holds the old layout and then the new
// one, the way a user's lock grows as the schema evolves: the old unit locked,
// then the schema edited and locked again.
func lockedPair(t *testing.T) (old, now *tabletext.Model) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Lin.schema")
	old = lineageUnit(t, dir, lineageOld)
	if _, _, err := lockfile.Update(old.Unit, []string{path}); err != nil {
		t.Fatal(err)
	}
	now = lineageUnit(t, dir, lineageNew)
	if _, _, err := lockfile.Update(now.Unit, []string{path}); err != nil {
		t.Fatal(err)
	}
	// the old unit is read from its own directory, which holds no lock: it
	// is the build that wrote the file, and it knows only its own layout
	old = lineageUnit(t, t.TempDir(), lineageOld)
	return old, now
}

func lineageWrite(t *testing.T, m *tabletext.Model, text string) []byte {
	t.Helper()
	wire, err := tablewire.Encode(m, place(t, m, "Cfg", text))
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func TestFixedLineageReadsAnOlderWriterStrictly(t *testing.T) {
	t.Parallel()
	old, now := lockedPair(t)
	wire := lineageWrite(t, old, `{"speed": -300, "name": "ab", "mode": "Run", "gone": 9}`)

	inst := now.New(now.Lookup("Cfg"))
	var report tabletext.Report
	ok, err := tablewire.Decode(now, inst, wire, &report)
	if !ok || err != nil || report.Refused {
		t.Fatalf("an older writer the lock holds: ok=%v err=%v report=%+v", ok, err, report)
	}
	text, err := now.Write(inst)
	if err != nil {
		t.Fatal(err)
	}
	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	if got, want := flat(string(text)), `{ "speed": -300, "name": "ab", "mode": "Run", "gone": 0, "extra": 5 }`; got != want {
		t.Errorf("the value read through the lock:\n%s\nwant\n%s", got, want)
	}
	if report.Widened != 1 {
		t.Errorf("speed widened once: %+v", report)
	}
}

func TestFixedLineageOutOfRangeRefuses(t *testing.T) {
	t.Parallel()
	old, now := lockedPair(t)
	// the writer's own range is [-1000, 1000], so 5000 is a forged record: the
	// two bytes of speed sit at the body's start, behind the record's hash
	wire := lineageWrite(t, old, `{"speed": 7}`)
	rec := len(wire) - int(ir.TableFixedTypeBytes(old.Unit.Tables["Cfg"]))
	binary.LittleEndian.PutUint16(wire[rec:], 5000)
	inst := now.New(now.Lookup("Cfg"))
	var report tabletext.Report
	ok, err := tablewire.Decode(now, inst, wire, &report)
	var refusal *tablewire.FixedRefusal
	if ok || !errors.As(err, &refusal) || refusal.Reason != tablewire.FixedReasonOutOfRange {
		t.Fatalf("a forged 5000 against max = 1000: ok=%v err=%v, want the one refusal %s", ok, err, tablewire.FixedReasonOutOfRange)
	}
	if !report.Refused || report.Widened != 0 {
		t.Errorf("a refusal carries no counter: %+v", report)
	}
}

func TestFixedLineageNeverReadsForward(t *testing.T) {
	t.Parallel()
	old, now := lockedPair(t)
	wire := lineageWrite(t, now, `{"speed": 1}`)
	var report tabletext.Report
	_, err := tablewire.Decode(old, old.New(old.Lookup("Cfg")), wire, &report)
	var refusal *tablewire.FixedRefusal
	if !errors.As(err, &refusal) || refusal.Reason != tablewire.FixedReasonLayoutUnknown {
		t.Fatalf("a newer writer read by the older build: %v, want layout_unknown (§21)", err)
	}
}

func TestFixedLineageWithoutALockIsLayoutUnknown(t *testing.T) {
	t.Parallel()
	old, _ := lockedPair(t)
	wire := lineageWrite(t, old, `{"speed": 1}`)
	unlocked := lineageUnit(t, t.TempDir(), lineageNew)
	var report tabletext.Report
	_, err := tablewire.Decode(unlocked, unlocked.New(unlocked.Lookup("Cfg")), wire, &report)
	var refusal *tablewire.FixedRefusal
	if !errors.As(err, &refusal) || refusal.Reason != tablewire.FixedReasonLayoutUnknown {
		t.Fatalf("an older writer with no lock to read it through: %v, want layout_unknown", err)
	}
}
