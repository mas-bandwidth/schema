// Package cards owns a typed work card and its wire/JSON round trips.
//
// The generated table codecs (CardLoad/CardMeasure/CardSave, CardBuilder,
// CardFromJson/CardToJson) answer the wire and text forms; this facade is the
// thin owning layer that a customer imports: a Record that keeps its own
// defensive copy of the wire and its measured region, checked writes that
// refuse before publishing, and the two customer ceilings the schema
// deliberately leaves unbounded (docs/SPEC-TABLES.md §2.5, §2.9).
package cards

import (
	"bytes"
	"errors"
	"fmt"
	"unicode/utf8"
	"unsafe"
)

// MaxBodyBytes is the customer ceiling on one card's opaque body. The schema
// declares body *string with no bound (docs/SPEC-TABLES.md §2.5); this facade
// refuses a larger body before any bytes are published.
const MaxBodyBytes = 64 << 10

// MaxListEntries is the customer ceiling on each of a card's collection fields
// (paths/new/shared/needs), which the schema leaves unbounded (docs/SPEC-TABLES.md §2.9).
const MaxListEntries = 512

// Report is the generated TableReport (docs/SPEC-TABLES.md §16), not a second
// reporting system. A read reports its tolerant events in this one type.
type Report = TableReport

// Record owns a private defensive copy of the wire and the measured, aligned
// region that backs the loaded card. No generated *Card and no shallow copy of
// relative pointers escapes (docs/SPEC-TABLES.md §6.2): the region stays alive
// for the Record's whole life, and the wire field is the caller's bytes copied.
type Record struct {
	wire   []byte
	region []byte
	root   *Card
	report TableReport
}

// Load reads wire into a Record using the generated measured/aligned temporary
// region load (docs/SPEC-TABLES.md §6.2). Failures and malformed wire are
// errors; tolerant events ride in the returned Report. The ORIGINAL wire is
// copied, never rewritten, so an old input is preserved byte for byte.
func Load(wire []byte) (*Record, Report, error) {
	var report TableReport
	n := CardLoadMeasure(wire)
	if n < 0 {
		_, reason := CardLoadMeasureReason(wire)
		if reason != nil {
			report.Reason = reason.Error()
		}
		CardLoad(make([]byte, 64), wire, &report)
		return nil, report, fmt.Errorf("cards: wire is not a card: %v", reason)
	}
	region := alignedRegion(n)
	root := CardLoad(region, wire, &report)
	if root == nil {
		return nil, report, errors.New("cards: wire is malformed")
	}
	if err := checkCard(root, nil); err != nil {
		return nil, report, err
	}
	w := make([]byte, len(wire))
	copy(w, wire)
	return &Record{wire: w, region: region, root: root, report: report}, report, nil
}

// FromJSON builds a Record from JSON text through the generated CardBuilder and
// CardFromJson (docs/SPEC-TABLES.md §16), then the generated save. Unknown,
// mismatched, clamped and malformed inputs are refused, as are the customer
// bounds and any body or element that is not well-formed UTF-8 without NUL.
// Bytes are published only on complete success; a refused write leaves no
// destination and mutates nothing.
func FromJSON(text []byte) (*Record, error) {
	var b CardBuilder
	if !b.Init() {
		return nil, errors.New("cards: arena initialization failed")
	}
	defer b.Shutdown()
	var report TableReport
	if !CardFromJson(&b, text, &report) || report.Malformed {
		return nil, errors.New("cards: JSON is malformed")
	}
	if report.Unknown > 0 || report.KindMismatch > 0 || report.Clamped > 0 {
		return nil, fmt.Errorf("cards: JSON has loss events (unknown=%d kindMismatch=%d clamped=%d): refused, never truncated",
			report.Unknown, report.KindMismatch, report.Clamped)
	}
	if err := checkCard(b.GetRoot(), &b.Arena); err != nil {
		return nil, err
	}
	if !b.Lock() {
		return nil, errors.New("cards: packing failed")
	}
	root := b.AsConst()
	size := CardMeasure(root)
	if size < 0 {
		return nil, errors.New("cards: measure refused the built card")
	}
	wire := make([]byte, size)
	if CardSave(root, wire) != size {
		return nil, errors.New("cards: save did not fill the measured wire")
	}
	region := make([]byte, len(b.Region()))
	copy(region, b.Region())
	own := (*Card)(unsafe.Pointer(&region[0]))
	return &Record{wire: wire, region: region, root: own, report: report}, nil
}

// ToJSON writes the record's known-field JSON text and returns the record's
// load report. It is the known-field view, not a lossless archival rewrite:
// a value the writer cannot name refuses rather than emitting "???" (§16).
func ToJSON(record *Record) ([]byte, Report, error) {
	if record == nil || record.root == nil {
		return nil, Report{}, errors.New("cards: nil record")
	}
	size := CardToJsonMeasure(record.root)
	if size < 0 {
		return nil, record.report, errors.New("cards: the card has no JSON spelling")
	}
	buf := make([]byte, size)
	if CardToJson(record.root, buf) != size {
		return nil, record.report, errors.New("cards: JSON write did not fill the measured text")
	}
	return buf, record.report, nil
}

// Wire returns a defensive copy of the record's wire bytes.
func (r *Record) Wire() []byte {
	if r == nil {
		return nil
	}
	w := make([]byte, len(r.wire))
	copy(w, r.wire)
	return w
}

// alignedRegion returns a 64-byte-aligned slice of n bytes: the measured,
// aligned temporary storage the generated region load requires (§6.2).
func alignedRegion(n int64) []byte {
	raw := make([]byte, n+63)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	return raw[off : off+uintptr(n)]
}

// checkCard is the checked-write helper: the customer bounds and the
// well-formedness of the opaque body and every collection element, enforced
// before bytes are published. arena is non-nil for a builder's arena-backed
// root and nil for a loaded self-relative region (docs/SPEC-TABLES.md §2.5, §2.9).
func checkCard(root *Card, arena *TableArena) error {
	if root == nil {
		return errors.New("cards: nil card")
	}
	if root.Body != 0 {
		body := stringData(&root.Body, arena)
		if len(body) > MaxBodyBytes {
			return fmt.Errorf("cards: body is %d bytes, past the %d-byte ceiling", len(body), MaxBodyBytes)
		}
		if !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
			return errors.New("cards: body is not well-formed UTF-8 without NUL")
		}
	}
	if err := checkPathList(&root.Paths, "paths", CardPathsEach, arena); err != nil {
		return err
	}
	if err := checkPathList(&root.New, "_new", CardNewEach, arena); err != nil {
		return err
	}
	if err := checkPathList(&root.Shared, "shared", CardSharedEach, arena); err != nil {
		return err
	}
	if err := checkIDList(&root.Needs, "needs", arena); err != nil {
		return err
	}
	if err := checkExtraList(&root.Extra, arena); err != nil {
		return err
	}
	return nil
}

// checkExtraList enforces the extras rules before publishing: the declared key
// bound and grammar, the absence of CR/LF in a single-line value, no extra
// duplicating a mapped header key and the shared entry ceiling
// (the HEADER MAPPING paragraph; docs/SPEC-TABLES.md §2.9).
func checkExtraList(list *TableList[KeyValueRow], arena *TableArena) error {
	if list.Count < 0 || int64(list.Count) > MaxListEntries {
		return fmt.Errorf("cards: extra has %d entries, past the %d-entry ceiling (docs/SPEC-TABLES.md §2.9)", list.Count, MaxListEntries)
	}
	var elemErr error
	visit := func(e *KeyValueRow) bool {
		key := fixedString(e.Key[:], e.KeyLength)
		if !validKeyGrammar(key) || len(key) > 32 {
			elemErr = fmt.Errorf("cards: extra key %q is not a bounded declared key", key)
			return false
		}
		if mappedHeaderKeys[key] {
			elemErr = fmt.Errorf("cards: extra key %q duplicates a mapped header key", key)
			return false
		}
		if e.Value != 0 {
			v := stringData(&e.Value, arena)
			if !utf8.Valid(v) || bytes.IndexByte(v, 0) >= 0 {
				elemErr = fmt.Errorf("cards: extra %q value is not well-formed UTF-8 without NUL", key)
				return false
			}
			if bytes.IndexByte(v, '\n') >= 0 || bytes.IndexByte(v, '\r') >= 0 {
				elemErr = fmt.Errorf("cards: extra %q value carries CR or LF in a single-line header value", key)
				return false
			}
		}
		return true
	}
	if arena == nil {
		CardExtraEach(list, visit)
	} else {
		CardExtraEach(list, visit, arena)
	}
	return elemErr
}

func stringData(slot *int64, arena *TableArena) []byte {
	var v TableStringView
	if arena == nil {
		v = TableStringAt(slot)
	} else {
		v = TableStringAt(slot, arena)
	}
	return v.Data
}

func checkPathList(list *TableList[PathTextRow], name string,
	each func(*TableList[PathTextRow], func(*PathTextRow) bool, ...*TableArena) bool,
	arena *TableArena) error {
	if list.Count < 0 || int64(list.Count) > MaxListEntries {
		return fmt.Errorf("cards: %s has %d entries, past the %d-entry ceiling (docs/SPEC-TABLES.md §2.9)", name, list.Count, MaxListEntries)
	}
	var elemErr error
	visit := func(e *PathTextRow) bool {
		n := int32(len(e.S))
		if e.SLength < n {
			n = e.SLength
		}
		if n < 0 {
			n = 0
		}
		s := e.S[:n]
		if !utf8.Valid(s) || bytes.IndexByte(s, 0) >= 0 {
			elemErr = fmt.Errorf("cards: %s has an element that is not well-formed UTF-8 without NUL", name)
			return false
		}
		return true
	}
	if arena == nil {
		each(list, visit)
	} else {
		each(list, visit, arena)
	}
	return elemErr
}

func checkIDList(list *TableList[CardIDRow], name string, arena *TableArena) error {
	if list.Count < 0 || int64(list.Count) > MaxListEntries {
		return fmt.Errorf("cards: %s has %d entries, past the %d-entry ceiling (docs/SPEC-TABLES.md §2.9)", name, list.Count, MaxListEntries)
	}
	var elemErr error
	visit := func(e *CardIDRow) bool {
		n := int32(len(e.S))
		if e.SLength < n {
			n = e.SLength
		}
		if n < 0 {
			n = 0
		}
		s := e.S[:n]
		if !utf8.Valid(s) || bytes.IndexByte(s, 0) >= 0 {
			elemErr = fmt.Errorf("cards: %s has an element that is not well-formed UTF-8 without NUL", name)
			return false
		}
		return true
	}
	if arena == nil {
		CardNeedsEach(list, visit)
	} else {
		CardNeedsEach(list, visit, arena)
	}
	return elemErr
}
