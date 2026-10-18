// The integrity check (docs/SPEC-TABLES.md §2.14): a table that declares
// `integrity = "crc32c"` saves its variable-form file with a CRC-32C trailer,
// and every read checks it before decoding any field, so no single-bit flip of
// any byte of a saved file decodes as a value. A table without the key writes
// the bytes it wrote before the key existed.
package gotable

import (
	"sort"
	"strings"
	"testing"
)

const integritySchema = `package probe
table Ledger | integrity = "crc32c"
{
    n int64 | min = 0, max = 9007199254740993
    label string(16)
    count uint16
}
table Plain
{
    n int64 | min = 0, max = 9007199254740993
    label string(16)
    count uint16
}
table Big | integrity = "crc32c"
{
    n int64 | min = 0, max = 9007199254740993
}
`

// integrityPlainGolden is PlainSave of {n 1000, label "ok", count 7} at the
// base this card started from (sprint/schema-go @ caea41783528, hetzner2):
// the keyless table's file does not move.
const integrityPlainGolden = "010105e803000000000000020c026f6b030707000071f801864ce363af3d62cb8fecfcf73974a279448ee2e5b10300000000000000"

// integrityLedgerGolden is the same value under the declaring table: the
// keyless file, then 0x1d23b551, the CRC-32C of those 53 bytes, little-endian.
const integrityLedgerGolden = integrityPlainGolden + "51b5231d"

const integrityProbe = `package probe
import ("encoding/base64"; "encoding/binary"; "encoding/hex"; "errors"; "hash/crc32"; "testing"; "unsafe")

func aligned(n int) []byte { raw := make([]byte, n+16); off := (-uintptr(unsafe.Pointer(&raw[0]))) & 15; return raw[off : off+uintptr(n)] }

func integrityValues() (Ledger, Plain, Big) {
	var l Ledger
	l.N = 1000; copy(l.Label[:], "ok"); l.LabelLength = 2; l.Count = 7
	var p Plain
	p.N = 1000; copy(p.Label[:], "ok"); p.LabelLength = 2; p.Count = 7
	var b Big
	b.N = 1000
	return l, p, b
}

func save[T any](t *testing.T, v *T, measure func(*T, ...TableWriteContext) int64, saveFn func(*T, []byte, ...TableWriteContext) int64) []byte {
	t.Helper()
	n := measure(v)
	if n < 0 { t.Fatal("measure refused") }
	buf := make([]byte, n)
	if got := saveFn(v, buf); got != n { t.Fatalf("save wrote %d, measure said %d", got, n) }
	if saveFn(v, make([]byte, n-1)) >= 0 { t.Fatal("save into a buffer one byte short did not refuse") }
	return buf
}

// flipAll flips every byte of wire by every single bit and by 0xff, and holds
// the strict read, the text read, the measure and the tolerant read to a
// refusal by name: unknown_form for the form byte, which is read first, and
// integrity_mismatch for every other byte. It returns the silent wrong decodes.
func flipAll[T any](t *testing.T, name string, wire []byte,
	strict func([]byte) (*T, error),
	text func(string, *TableReport) (*T, error),
	measureReason func([]byte) (int64, error),
	load func([]byte, []byte, *TableReport) *T) int {
	t.Helper()
	silent, flips := 0, 0
	region := aligned(4096)
	for i := range wire {
		for _, x := range []byte{0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80, 0xff} {
			flips++
			bad := append([]byte(nil), wire...)
			bad[i] ^= x
			want := TableRefuseIntegrityMismatch
			if i == 0 { want = TableRefuseUnknownForm }
			v, err := strict(bad)
			if err == nil { silent++; t.Errorf("%s: byte %d ^ 0x%02x read clean through ReadStrict: %+v", name, i, x, *v); continue }
			if !errors.Is(err, want) { t.Errorf("%s: byte %d ^ 0x%02x ReadStrict refused %v, want %v", name, i, x, err, want) }
			var r TableReport
			if v, err := text(base64.StdEncoding.EncodeToString(bad), &r); err == nil { silent++; t.Errorf("%s: byte %d ^ 0x%02x read clean through LoadText: %+v", name, i, x, *v) } else if !errors.Is(err, want) { t.Errorf("%s: byte %d ^ 0x%02x LoadText refused %v, want %v", name, i, x, err, want) }
			if n, err := measureReason(bad); n >= 0 || !errors.Is(err, want) { t.Errorf("%s: byte %d ^ 0x%02x LoadMeasureReason = %d, %v, want -1, %v", name, i, x, n, err, want) }
			if i == 0 { continue }
			r = TableReport{}
			if got := load(region, bad, &r); got != nil || !r.Malformed || r.Reason != "integrity_mismatch" || r.Verdict != TableOpenDamaged {
				t.Errorf("%s: byte %d ^ 0x%02x tolerant Load = %v, report %+v; want nil, malformed, integrity_mismatch", name, i, x, got != nil, r)
			}
			if !errors.Is(r.Err(), TableRefuseIntegrityMismatch) { t.Errorf("%s: byte %d ^ 0x%02x report.Err() = %v", name, i, x, r.Err()) }
		}
	}
	t.Logf("%s: %d bytes, %d flips, %d silent wrong decodes", name, len(wire), flips, silent)
	return silent
}

func TestIntegrity(t *testing.T) {
	t.Parallel()
	l, p, b := integrityValues()
	lw := save(t, &l, LedgerMeasure, LedgerSave)
	pw := save(t, &p, PlainMeasure, PlainSave)
	bw := save(t, &b, BigMeasure, BigSave)
	// the keyless file is BASE's, byte for byte, and the declaring table's is
	// that file and its trailer
	if got := hex.EncodeToString(pw); got != "` + integrityPlainGolden + `" { t.Fatalf("Plain moved from BASE: %s", got) }
	if got := hex.EncodeToString(lw); got != "` + integrityLedgerGolden + `" { t.Fatalf("Ledger golden: %s", got) }
	for _, w := range [][]byte{lw, bw} {
		if binary.LittleEndian.Uint32(w[len(w)-4:]) != crc32.Checksum(w[:len(w)-4], crc32.MakeTable(crc32.Castagnoli)) { t.Fatalf("trailer is not the CRC-32C of the bytes before it: %x", w) }
	}
	if len(bw) != 28+4 { t.Fatalf("Big file is %d bytes, want the 28 of the keyless form plus 4", len(bw)) }
	// clean round trips: strict, text, measure, tolerant
	if v, err := LedgerReadStrict(lw); err != nil || v.N != 1000 || string(v.Label[:v.LabelLength]) != "ok" || v.Count != 7 { t.Fatalf("Ledger strict round trip: %v %+v", err, v) }
	text, err := LedgerSaveText(&l)
	if err != nil || text != base64.StdEncoding.EncodeToString(lw) { t.Fatalf("LedgerSaveText is not the base64 of the file: %v %q", err, text) }
	if v, err := LedgerLoadText(text, nil); err != nil || v.N != 1000 { t.Fatalf("Ledger text round trip: %v", err) }
	if n, err := LedgerLoadMeasureReason(lw); err != nil || n <= 0 || LedgerLoadMeasure(lw) != n { t.Fatalf("Ledger LoadMeasure: %d %v", n, err) }
	var r TableReport
	if v := LedgerLoad(aligned(4096), lw, &r); v == nil || v.N != 1000 || r != (TableReport{Verdict: TableOpenOk}) { t.Fatalf("Ledger tolerant round trip: %+v", r) }
	if v, err := BigReadStrict(bw); err != nil || v.N != 1000 { t.Fatalf("Big strict round trip: %v", err) }
	// every single-bit flip and every 0xff of every byte is refused by name
	silent := flipAll(t, "Ledger", lw, LedgerReadStrict, LedgerLoadText, LedgerLoadMeasureReason, LedgerLoad)
	silent += flipAll(t, "Big", bw, BigReadStrict, BigLoadText, BigLoadMeasureReason, BigLoad)
	if silent != 0 { t.Fatalf("%d silent wrong decodes", silent) }
	// SHIFTED FRAMING, by name (§2.14): a length or a field reference changed
	// so the bytes of one field are read into the next. Ledger's file is form
	// byte, then n (ref 1, kind 5, 8 bytes, offsets 1..10), then label (ref 2
	// at 11, kind 12 at 12, length 2 at 13, "ok"), then count (ref 3 at 16).
	// The keyless reader is the control: what it makes of the same edit is
	// logged; the declaring reader refuses both by the check.
	for _, shift := range []struct{ name string; at int; to byte }{
		{"label length 2 to 4 runs into count", 13, 4},
		{"label length 2 to 1 leaves k as a reference", 13, 1},
		{"label field id 2 to 3 reads the text as count", 11, 3},
		{"n field id 1 to 3 reads the int64 as count", 1, 3},
	} {
		bad := append([]byte(nil), lw...)
		if bad[shift.at] == shift.to { t.Fatalf("%s: offset %d already holds %d", shift.name, shift.at, shift.to) }
		bad[shift.at] = shift.to
		if _, err := LedgerReadStrict(bad); !errors.Is(err, TableRefuseIntegrityMismatch) { t.Errorf("%s: ReadStrict %v, want integrity_mismatch", shift.name, err) }
		if _, err := LedgerLoadText(base64.StdEncoding.EncodeToString(bad), nil); !errors.Is(err, TableRefuseIntegrityMismatch) { t.Errorf("%s: LoadText %v, want integrity_mismatch", shift.name, err) }
		plain := append([]byte(nil), pw...)
		plain[shift.at] = shift.to
		var pr TableReport
		pv := PlainLoad(aligned(4096), plain, &pr)
		t.Logf("%s: keyless control reads %v with report %+v; declaring reader refuses integrity_mismatch", shift.name, pv != nil, pr)
	}
	// THE TWO MISMATCHED READERS (§2.14): neither reads as data
	if _, err := LedgerReadStrict(pw); !errors.Is(err, TableRefuseIntegrityMismatch) { t.Fatalf("a keyless file under the keyed reader: %v", err) }
	if _, err := PlainReadStrict(lw); err == nil { t.Fatal("a trailered file read clean under the keyless reader") }
	// a file too short to hold the trailer is integrity_mismatch, and an empty one is damage
	if _, err := LedgerReadStrict([]byte{1, 0, 0, 0}); !errors.Is(err, TableRefuseIntegrityMismatch) { t.Fatalf("short file: %v", err) }
	if _, err := LedgerReadStrict(nil); err == nil || errors.Is(err, TableRefuseIntegrityMismatch) { t.Fatalf("empty file: %v", err) }
	if TableRefuseIntegrityMismatch.Error() != "integrity_mismatch" { t.Fatal(TableRefuseIntegrityMismatch.Error()) }
}
`

func TestGoWireIntegrityCheck(t *testing.T) {
	t.Parallel()
	t.Run("emitted", func(t *testing.T) {
		t.Parallel()
		keyed := strings.Join(sortedSources(generate(t, integritySchema)), "")
		for _, m := range []string{"crc32.MakeTable(crc32.Castagnoli)", "TableRefuseIntegrityMismatch", `"integrity_mismatch"`} {
			if !strings.Contains(keyed, m) {
				t.Errorf("the declaring unit lacks %q", m)
			}
		}
		plain := strings.Join(sortedSources(generate(t, "package probe\ntable Plain\n{\n    n int64\n}\n")), "")
		if strings.Contains(plain, "crc32") || strings.Contains(plain, "tableIntegrity") {
			t.Error("a unit with no declaring table carries the integrity runtime")
		}
	})
	t.Run("compiled", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, integritySchema, integrityProbe)
	})
}

func sortedSources(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, string(files[name]))
	}
	return out
}
