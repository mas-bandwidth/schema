package tablewire_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The old writer's range is narrower than the reader's, including in array,
// union and nested positions. A forged value of 1001 remains representable
// and inside the new range; the checked historical writer must reject it.
const historicalBoundsOld = `package historical
union Choice { number int16 | min = -1000, max = 1000 }
type Child { n int16 | min = -1000, max = 1000 }
fixed table Cfg {
 scalar int16 | min = -1000, max = 1000
 values [..2]int16 | min = -1000, max = 1000
 choice Choice
 child Child
}
`

func TestFixedLineageValidatesHistoricalWriterBounds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "Lin.schema")
	old := lineageUnit(t, dir, historicalBoundsOld)
	if _, _, err := lockfile.Update(old.Unit, []string{path}); err != nil {
		t.Fatal(err)
	}
	newer := strings.ReplaceAll(historicalBoundsOld, "int16", "int32")
	newer = strings.Replace(newer, "union Choice { number int32", "union Choice { number int16", 1)
	newer = strings.ReplaceAll(newer, "-1000", "-10000")
	newer = strings.ReplaceAll(newer, "max = 1000", "max = 10000")
	now := lineageUnit(t, dir, newer)
	if _, _, err := lockfile.Update(now.Unit, []string{path}); err != nil {
		t.Fatal(err)
	}
	old = lineageUnit(t, t.TempDir(), historicalBoundsOld)
	wire := lineageWrite(t, old, `{"scalar": 1000, "values": [1000], "choice": {"number": 1000}, "child": {"n": 1000}}`)
	st := old.Lookup("Cfg")
	body := len(wire) - int(ir.TableFixedTypeBytes(st))
	cases := []struct {
		name   string
		offset int
	}{
		{"scalar", deadFieldAt(t, st, "scalar")},
		{"array element", deadFieldAt(t, st, "values") + 4},
		{"union arm", deadFieldAt(t, st, "choice") + 1},
		{"nested field", deadFieldAt(t, st, "child")},
	}
	decode := func(data []byte) (bool, tabletext.Report, error) {
		var report tabletext.Report
		ok, err := tablewire.Decode(now, now.New(now.Lookup("Cfg")), data, &report)
		return ok, report, err
	}
	if ok, report, err := decode(wire); !ok || err != nil || report.Refused {
		t.Fatalf("lawful old maxima: ok=%v err=%v report=%+v", ok, err, report)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forged := append([]byte(nil), wire...)
			binary.LittleEndian.PutUint16(forged[body+tc.offset:], 1001)
			ok, report, err := decode(forged)
			var refusal *tablewire.FixedRefusal
			if ok || !errors.As(err, &refusal) || refusal.Reason != tablewire.FixedReasonOutOfRange || !report.Refused {
				t.Fatalf("old out-of-range but inside new range: ok=%v err=%v report=%+v", ok, err, report)
			}
		})
	}
}

// A missing snapshot is an API/setup error, not a claim about this wire.
// Explicitly importing the archived source restores historical validation.
func TestFixedLineageRequiresTrustedHistoricalWriter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "Lin.schema")
	old := lineageUnit(t, dir, lineageOld)
	if _, _, err := lockfile.Update(old.Unit, []string{path}); err != nil {
		t.Fatal(err)
	}
	now := lineageUnit(t, dir, lineageNew)
	if _, _, err := lockfile.Update(now.Unit, []string{path}); err != nil {
		t.Fatal(err)
	}
	writer := lineageUnit(t, t.TempDir(), lineageOld)
	wire := lineageWrite(t, writer, `{"speed": 1000}`)
	lock, ok, err := lockfile.Open([]string{path})
	if err != nil || !ok {
		t.Fatalf("open lock: ok=%v err=%v", ok, err)
	}
	entry := &lock.Table("Cfg").Lineage[0]
	key := entry.Wire
	entry.Writer = nil
	if err := os.WriteFile(filepath.Join(dir, lockfile.FileName), []byte(lock.Text()), 0600); err != nil {
		t.Fatal(err)
	}
	var report tabletext.Report
	ok, err = tablewire.Decode(now, now.New(now.Lookup("Cfg")), wire, &report)
	var refusal *tablewire.FixedRefusal
	if ok || err == nil || errors.As(err, &refusal) || report.Refused || !strings.Contains(err.Error(), "Cfg") || !strings.Contains(err.Error(), fmt.Sprintf("%016x", key)) || !strings.Contains(err.Error(), "--writer") {
		t.Fatalf("missing metadata must be setup error with remedy, not wire refusal: ok=%v err=%v report=%+v", ok, err, report)
	}
	target := fmt.Sprintf("Cfg@0x%016x", key)
	if _, _, err := lockfile.ImportWriter(now.Unit, []string{path}, target, writer.Unit); err != nil {
		t.Fatal(err)
	}
	// A new model avoids the per-unit lock cache after the explicit import.
	imported := lineageUnit(t, dir, lineageNew)
	report = tabletext.Report{}
	ok, err = tablewire.Decode(imported, imported.New(imported.Lookup("Cfg")), wire, &report)
	if !ok || err != nil || report.Refused {
		t.Fatalf("trusted import should restore lawful old read: ok=%v err=%v report=%+v", ok, err, report)
	}
}

// The old v1 key really collided for two historical bounds. Once a caller
// selects the trusted small-writer snapshot, identical forged-small and lawful
// large-writer bytes cannot be told apart; both are held to that snapshot.
func TestFixedLineageHistoricalV1WideCollision(t *testing.T) {
	t.Parallel()
	fixture := filepath.Join("..", "lockfile", "testdata", "writer-v1", "wide")
	read := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	oldSource := read("Writer.schema")
	dir := t.TempDir()
	path := filepath.Join(dir, "Lin.schema")
	if err := os.WriteFile(filepath.Join(dir, lockfile.FileName), read("schema.lock"), 0600); err != nil {
		t.Fatal(err)
	}
	newSource := strings.Replace(string(oldSource), "1208925819614629174706176", "2417851639229258349412352", 1)
	now := lineageUnit(t, dir, newSource)
	if _, _, err := lockfile.Update(now.Unit, []string{path}); err != nil {
		t.Fatal(err)
	}
	forged, other := read("forged-small.bin"), read("lawful-colliding-large.bin")
	if !bytes.Equal(forged, other) {
		t.Fatal("historical collision bytes changed")
	}
	for _, tc := range []struct {
		name   string
		wire   []byte
		accept bool
	}{
		{"small writer maximum", read("good.bin"), true},
		{"forged small writer", forged, false},
		{"identical lawful other writer", other, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var report tabletext.Report
			ok, err := tablewire.Decode(now, now.New(now.Lookup("Row")), tc.wire, &report)
			if tc.accept {
				if !ok || err != nil || report.Refused {
					t.Fatalf("old maximum: ok=%v err=%v report=%+v", ok, err, report)
				}
			} else {
				var refusal *tablewire.FixedRefusal
				if ok || !errors.As(err, &refusal) || refusal.Reason != tablewire.FixedReasonOutOfRange {
					t.Fatalf("trusted small-writer bound: ok=%v err=%v report=%+v", ok, err, report)
				}
			}
		})
	}
}
