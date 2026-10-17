// The integrity check on the fixed form (docs/SPEC-TABLES.md §2.14, §3.4 THE
// FRAMING): a `fixed table` that declares `integrity = "crc32c"` saves its
// form-3 file with one CRC-32C trailer after the last record, and FixedLoad
// checks it after the form byte and before the layout or any record, so no
// single-bit flip of any byte of a saved file lands a value. A fixed table
// without the key writes the bytes it wrote before the key existed. It
// mirrors integrity_test.go, the variable form's.
package gotable

import (
	"strings"
	"testing"
)

const integrityFixedSchema = `package probe
fixed table Reading | integrity = "crc32c"
{
    x int32
    y int16
    label string(8)
}
fixed table PlainReading
{
    x int32
    y int16
    label string(8)
}
`

// integrityFixedPlainGolden is PlainReadingFixedSave of the three records of
// fixedValues at this card's BASE (sprint/schema-go @ f820a0c1f, amd-1): the
// keyless table's file does not move.
const integrityFixedPlainGolden = "030000000000000064f3fa8daeaf844448000000040000000deeaf527dc1c0970d1200000003000000071702864cf563af040400000000000000541502864cf463af0302000000000000003d62cb8fecfcf7390c0c0000000000000064f3fa8daeaf8444e80300000000020000006f6b00000000000064f3fa8daeaf8444e9030000f9ff00000000000000000000000064f3fa8daeaf8444ea030000f2ff080000006569676874206368"

// integrityFixedGolden is ReadingFixedSave of the same three records: the
// form-3 file, then the CRC-32C of every byte before it, little-endian.
const integrityFixedGolden = "03000000000000006938893b0968270148000000040000007b8c6cd3fc19b9b80d1200000003000000071702864cf563af040400000000000000541502864cf463af0302000000000000003d62cb8fecfcf7390c0c000000000000006938893b09682701e80300000000020000006f6b0000000000006938893b09682701e9030000f9ff0000000000000000000000006938893b09682701ea030000f2ff0800000065696768742063682854cdb3"

const integrityFixedProbe = `package probe
import ("encoding/binary"; "encoding/hex"; "errors"; "hash/crc32"; "testing")

func fixedValues() ([]Reading, []PlainReading) {
	var r []Reading
	var p []PlainReading
	for k, s := range []string{"ok", "", "eight ch"} {
		var a Reading
		a.X = int32(1000 + k); a.Y = int16(-7 * k); copy(a.Label[:], s); a.LabelLength = int32(len(s))
		r = append(r, a)
		var b PlainReading
		b.X = a.X; b.Y = a.Y; copy(b.Label[:], s); b.LabelLength = int32(len(s))
		p = append(p, b)
	}
	return r, p
}

var sentinel = Reading{X: -1, Y: -1}

func load(data []byte) ([]Reading, int64, TableReport) {
	out := []Reading{sentinel, sentinel, sentinel, sentinel}
	var r TableReport
	n := ReadingFixedLoad(out, data, make([]TableFixedEntry, ReadingFixedPlanMax), &r)
	return out, n, r
}

func TestIntegrityFixed(t *testing.T) {
	t.Parallel()
	rv, pv := fixedValues()
	// THE WRITE (§2.14): the measure counts the four trailer bytes and the save
	// writes exactly that many; one byte short is refused.
	if ReadingFixedMeasure(3) != PlainReadingFixedMeasure(3)+4 { t.Fatalf("measure %d, keyless %d: want four more", ReadingFixedMeasure(3), PlainReadingFixedMeasure(3)) }
	kw := make([]byte, ReadingFixedMeasure(3))
	if n := ReadingFixedSave(rv, kw); n != int64(len(kw)) { t.Fatalf("save wrote %d, measure said %d", n, len(kw)) }
	if n := ReadingFixedSave(rv, make([]byte, len(kw)-1)); n >= 0 { t.Fatal("save into a buffer one byte short did not refuse") }
	if _, err := ReadingFixedSaveReason(rv, make([]byte, len(kw)-1)); !errors.Is(err, TableRefuseTruncated) { t.Fatalf("short buffer reason %v", err) }
	pw := make([]byte, PlainReadingFixedMeasure(3))
	if n := PlainReadingFixedSave(pv, pw); n != int64(len(pw)) { t.Fatalf("keyless save wrote %d", n) }
	// THE GOLDENS: the keyless file is BASE's, byte for byte; the declaring
	// file's last four bytes are the CRC-32C of the bytes before them.
	if got := hex.EncodeToString(pw); got != "` + integrityFixedPlainGolden + `" { t.Fatalf("PlainReading moved from BASE: %s", got) }
	if got := hex.EncodeToString(kw); got != "` + integrityFixedGolden + `" { t.Fatalf("Reading golden: %s", got) }
	body := kw[:len(kw)-4]
	if binary.LittleEndian.Uint32(kw[len(body):]) != crc32.Checksum(body, crc32.MakeTable(crc32.Castagnoli)) { t.Fatalf("trailer is not the CRC-32C of the bytes before it") }
	// THE CLEAN ROUND TRIP, and the empty file of zero records too.
	got, n, r := load(kw)
	if n != 3 || r != (TableReport{}) || got[0] != rv[0] || got[1] != rv[1] || got[2] != rv[2] || got[3] != sentinel { t.Fatalf("round trip: n=%d report=%+v got=%+v", n, r, got) }
	empty := make([]byte, ReadingFixedMeasure(0))
	if ReadingFixedSave(nil, empty) != int64(len(empty)) { t.Fatal("empty save") }
	if _, n, r := load(empty); n != 0 || r != (TableReport{}) { t.Fatalf("empty round trip: n=%d %+v", n, r) }
	// EVERY SINGLE-BIT FLIP AND EVERY 0xff OF EVERY BYTE IS REFUSED BY NAME:
	// the form byte keeps its §3.4 name, and every other byte, the trailer's
	// own included, is integrity_mismatch with nothing landed (§2.14).
	silent, flips := 0, 0
	for i := range kw {
		for _, x := range []byte{0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80, 0xff} {
			flips++
			bad := append([]byte(nil), kw...)
			bad[i] ^= x
			got, n, r := load(bad)
			if n >= 0 { silent++; t.Errorf("byte %d ^ 0x%02x read clean: n=%d %+v", i, x, n, got); continue }
			for k := range got { if got[k] != sentinel { t.Errorf("byte %d ^ 0x%02x landed record %d: %+v", i, x, k, got[k]) } }
			if i == 0 {
				want := map[byte]string{1: "previous_form", 2: "message_form_as_file"}[bad[0]]
				if want == "" { want = "newer_form" }
				if r != (TableReport{Verdict: TableOpenRefused, Reason: want}) { t.Errorf("form byte ^ 0x%02x: %+v, want %s", x, r, want) }
				continue
			}
			if r != (TableReport{Verdict: TableOpenDamaged, Malformed: true, Reason: "integrity_mismatch"}) { t.Errorf("byte %d ^ 0x%02x: report %+v, want integrity_mismatch", i, x, r) }
			if !errors.Is(r.Err(), TableRefuseIntegrityMismatch) { t.Errorf("byte %d ^ 0x%02x: Err() = %v", i, x, r.Err()) }
		}
	}
	t.Logf("Reading: %d bytes, %d flips, %d silent wrong decodes", len(kw), flips, silent)
	if silent != 0 { t.Fatalf("%d silent wrong decodes", silent) }
	// A FILE TOO SHORT TO HOLD THE TRAILER is integrity_mismatch; no first byte is truncated.
	for k := 1; k < 5; k++ {
		if _, n, r := load(kw[:k]); n != -1 || r.Reason != "integrity_mismatch" { t.Errorf("%d-byte file: n=%d %+v", k, n, r) }
	}
	if _, n, r := load(nil); n != -1 || r != (TableReport{Malformed: true, Reason: "truncated"}) { t.Errorf("empty file: n=%d %+v", n, r) }
	// THE TWO MISMATCHED READERS (§2.14): neither reads as data. A keyless
	// reader given a trailered file has four bytes past its last record.
	sealed := append(append([]byte(nil), pw...), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(sealed[len(pw):], crc32.Checksum(pw, crc32.MakeTable(crc32.Castagnoli)))
	var pr TableReport
	if n := PlainReadingFixedLoad(make([]PlainReading, 4), sealed, make([]TableFixedEntry, PlainReadingFixedPlanMax), &pr); n != -1 || pr != (TableReport{Malformed: true, Reason: "truncated"}) { t.Errorf("trailered file under the keyless reader: n=%d %+v", n, pr) }
	// and a keyed reader given the file without its trailer refuses it
	if _, n, r := load(body); n != -1 || r.Reason != "integrity_mismatch" { t.Errorf("trailerless file under the keyed reader: n=%d %+v", n, r) }
}
`

func TestGoWireIntegrityFixedForm(t *testing.T) {
	t.Parallel()
	t.Run("emitted", func(t *testing.T) {
		t.Parallel()
		src := strings.Join(sortedSources(generate(t, integrityFixedSchema)), "")
		load := funcSource(src, "func ReadingFixedLoad(")
		if !strings.Contains(load, "tableIntegrityHolds(data)") || !strings.Contains(load, `"integrity_mismatch"`) {
			t.Errorf("ReadingFixedLoad does not check the trailer:\n%s", load)
		}
		// the check runs after the form byte and before the layout is read
		if c, f, l := strings.Index(load, "tableIntegrityHolds(data)"), strings.Index(load, "data[0] != TableFixedForm"), strings.Index(load, "layoutBytes :="); c < f || c > l {
			t.Errorf("the trailer is not checked between the form byte (%d) and the layout (%d): %d", f, l, c)
		}
		if !strings.Contains(funcSource(src, "func ReadingFixedSave("), "tableIntegritySeal") {
			t.Error("ReadingFixedSave does not seal the trailer")
		}
		plain := funcSource(src, "func PlainReadingFixedLoad(") + funcSource(src, "func PlainReadingFixedSave(") + funcSource(src, "func PlainReadingFixedMeasure(")
		if strings.Contains(plain, "tableIntegrity") {
			t.Error("a keyless fixed table's surface carries the trailer")
		}
	})
	t.Run("compiled", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, integrityFixedSchema, integrityFixedProbe)
	})
}
