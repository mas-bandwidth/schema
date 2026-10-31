// The message form carries the integrity trailer (docs/SPEC-TABLES.md §2.14,
// §3.3): a fixed table that declares `integrity = "crc32c"` saves a batch with
// four CRC-32C bytes after the padded bit stream, and the reader checks them
// before it decodes the first body, so no single-bit flip of a saved batch
// decodes as a value. A table without the key writes the bytes it wrote before.
package gotable

import "testing"

const integrityMessageSchema = `package probe
fixed table Ledger | integrity = "crc32c"
{
    n int32
    count uint16
}
fixed table Plain
{
    n int32
    count uint16
}
`

// integrityMessagePlainGolden is PlainSaveMessages of {{n 1000, count 7}} at
// the card's BASE: the keyless table's batch does not move. Its references
// are three bits since the announcement's tail carries two blob ids (§3.3).
const integrityMessagePlainGolden = "0200411f0000d0010000"

// integrityMessageLedgerGolden is the same batch under the declaring table:
// the keyless batch and its CRC-32C trailer.
const integrityMessageLedgerGolden = "0200411f0000d001000022c51159"

const integrityMessageProbe = `package probe
import ("encoding/binary"; "encoding/hex"; "errors"; "hash/crc32"; "testing")

func integrityMessageVocab(t *testing.T) *TableVocabulary {
	t.Helper()
	v := new(TableVocabulary)
	v.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	ann := make([]byte, AnnounceMeasure())
	if Announce(ann) != int64(len(ann)) { t.Fatal("Announce") }
	var r TableReport
	if !AnnounceRead(v, ann, &r) { t.Fatalf("AnnounceRead %+v", r) }
	return v
}

func TestIntegrityMessage(t *testing.T) {
	var l Ledger
	l.N = 1000; l.Count = 7
	var p Plain
	p.N = 1000; p.Count = 7
	lv := []*Ledger{&l}
	pv := []*Plain{&p}
	ln := LedgerMeasureMessages(lv, nil)
	pn := PlainMeasureMessages(pv, nil)
	if ln < 0 || pn < 0 { t.Fatalf("measure %d %d", ln, pn) }
	if ln != pn+4 { t.Fatalf("the declaring batch measures %d, the keyless one %d; want four more", ln, pn) }
	lb := make([]byte, ln)
	pb := make([]byte, pn)
	if got := LedgerSaveMessages(lv, lb, nil); got != ln { t.Fatalf("LedgerSaveMessages %d want %d", got, ln) }
	if got := PlainSaveMessages(pv, pb, nil); got != pn { t.Fatalf("PlainSaveMessages %d want %d", got, pn) }
	if LedgerSaveMessages(lv, make([]byte, ln-1), nil) >= 0 { t.Fatal("a buffer one byte short did not refuse") }
	if got := hex.EncodeToString(pb); got != "` + integrityMessagePlainGolden + `" { t.Fatalf("Plain batch moved from BASE: %s", got) }
	if got := hex.EncodeToString(lb); got != "` + integrityMessageLedgerGolden + `" { t.Fatalf("Ledger batch golden: %s", got) }
	if binary.LittleEndian.Uint32(lb[len(lb)-4:]) != crc32.Checksum(lb[:len(lb)-4], crc32.MakeTable(crc32.Castagnoli)) { t.Fatalf("trailer is not the CRC-32C of the batch before it: %x", lb) }
	vocab := integrityMessageVocab(t)
	var out [1]Ledger
	n, ok := LedgerLoadMessages(out[:], vocab, lb, nil)
	if !ok || n != 1 || out[0].N != 1000 || out[0].Count != 7 { t.Fatalf("Ledger clean round trip: %d %v %+v", n, ok, out[0]) }
	var pout [1]Plain
	if n, ok := PlainLoadMessages(pout[:], vocab, pb, nil); !ok || n != 1 || pout[0].N != 1000 || pout[0].Count != 7 { t.Fatalf("Plain clean round trip: %d %v %+v", n, ok, pout[0]) }
	// every single-bit flip and every 0xff of every byte is refused by name,
	// and nothing is decoded. The form byte is read first, so its flip is
	// §3.3's newer_form; every other byte is the batch trailer's
	// integrity_mismatch (docs/SPEC-TABLES.md §2.14).
	for i := range lb {
		for _, x := range []byte{1, 2, 4, 8, 16, 32, 64, 128, 255} {
			bad := append([]byte(nil), lb...)
			bad[i] ^= x
			var got [1]Ledger
			var r TableReport
			n, ok := LedgerLoadMessages(got[:], vocab, bad, &r)
			want := "integrity_mismatch"
			if i == 0 { want = "newer_form" }
			if ok || n != 0 || r.Reason != want { t.Fatalf("byte %d ^ 0x%02x: load %d %v report %+v, want 0 false %q", i, x, n, ok, r, want) }
			if i > 0 && !errors.Is(r.Err(), TableRefuseIntegrityMismatch) { t.Fatalf("byte %d ^ 0x%02x: report.Err() = %v", i, x, r.Err()) }
			if i > 0 && !r.Malformed { t.Fatalf("byte %d ^ 0x%02x: report is not malformed: %+v", i, x, r) }
		}
	}
}
`

func TestGoWireIntegrityMessageForm(t *testing.T) {
	t.Parallel()
	runGenerated(t, integrityMessageSchema, integrityMessageProbe)
}
