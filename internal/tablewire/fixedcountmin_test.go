package tablewire_test

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A counted array's count below its Min is refused on read, and on write in
// every build (docs/SPEC.md §5.1 row 3; docs/SPEC-TABLES.md §16.2, W0). The
// writer stores two of [2..3]uint16; the count is then forged to one and the
// slot it leaves dead is zeroed, so only the low bound can decide the read.
const fixedCountMinUnit = `package minpin

fixed table Root
{
    ca [2..3]uint16
}
`

func TestFixedCountBelowMinRefuses(t *testing.T) {
	t.Parallel()
	m := listModel(t, fixedCountMinUnit)
	st := m.Lookup("Root")
	good := place(t, m, "Root", `{"ca": [7, 65535]}`)
	wire, err := tablewire.Encode(m, good)
	if err != nil {
		t.Fatal(err)
	}
	needle := []byte{2, 0, 0, 0, 7, 0, 0xff, 0xff}
	at := bytes.Index(wire, needle)
	if at < 0 {
		t.Fatal("the count of two with its two elements is not in the record")
	}

	forged := append([]byte(nil), wire...)
	binary.LittleEndian.PutUint32(forged[at:], 1)
	binary.LittleEndian.PutUint16(forged[at+6:], 0)
	var report tabletext.Report
	ok, err := tablewire.Decode(m, m.New(st), forged, &report)
	if ok || !tablewire.Refused(err) || !report.Refused {
		t.Fatalf("a count of one below [2..3]: ok=%v err=%v report=%+v, want a refusal", ok, err, report)
	}

	below := place(t, m, "Root", `{"ca": [7, 65535]}`)
	below.Fields[0].Count = 1
	if out, err := tablewire.Encode(m, below); err == nil {
		t.Fatalf("a write of a count of one below [2..3] was accepted: %x", out)
	}

	t.Run("through the lock", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "Lin.schema")
		old := lineageUnit(t, dir, fixedCountMinUnit)
		if _, _, err := lockfile.Update(old.Unit, []string{path}); err != nil {
			t.Fatal(err)
		}
		now := lineageUnit(t, dir, `package minpin

fixed table Root
{
    ca [2..4]uint16
}
`)
		if _, _, err := lockfile.Update(now.Unit, []string{path}); err != nil {
			t.Fatal(err)
		}
		writer := lineageUnit(t, t.TempDir(), fixedCountMinUnit)
		file, err := tablewire.Encode(writer, place(t, writer, "Root", `{"ca": [7, 65535]}`))
		if err != nil {
			t.Fatal(err)
		}
		at := bytes.Index(file, needle)
		if at < 0 {
			t.Fatal("the count of two with its two elements is not in the older writer's record")
		}
		binary.LittleEndian.PutUint32(file[at:], 1)
		binary.LittleEndian.PutUint16(file[at+6:], 0)
		var report tabletext.Report
		ok, err := tablewire.Decode(now, now.New(now.Lookup("Root")), file, &report)
		if ok || !tablewire.Refused(err) || !report.Refused {
			t.Fatalf("a count of one below [2..4] through the lock: ok=%v err=%v report=%+v, want a refusal", ok, err, report)
		}
	})
}
