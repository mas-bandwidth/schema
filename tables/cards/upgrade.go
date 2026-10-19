// upgrade.go — the retained wire upgrade and the strict unsupported-data read.
//
// Upgrade is a REGION round trip through the generated retaining path
// (docs/SPEC-TABLES.md §6.6): CardLoadRetain keeps every field the current
// declaration cannot name, and CardSaveRetain writes them back at the end of
// their own body. It is pure and byte-idempotent after the first save, and it
// never mutates the input wire. ReadStrict is the strict half of the tolerant
// read (§4): it loads through the ordinary path and refuses any data the
// current declaration cannot support.
package cards

import (
	"errors"
	"fmt"
)

// MaxRetainBytes is the customer ceiling on the retained-record byte store.
// It caps extra retention memory for a card with a 64-KiB body and bounded
// collections; it is not a promise that arbitrary wire fits and it never grows
// on demand (docs/SPEC-TABLES.md §6.6).
const MaxRetainBytes = 1 << 20

// MaxRetainIDs is the customer ceiling on the retained-id entry store, the
// caller-owned id list §6.6 keeps inside TableRetain.
const MaxRetainIDs = 4096

// maxHostInt is the largest slice length this host can allocate; a measured
// size past it must be refused before any allocation, not let make() panic.
const maxHostInt = int(^uint(0) >> 1)

// Upgrade rewrites wire through the generated retaining region path so that
// every field the current declaration cannot name is preserved, byte for byte,
// at the end of its own body (§6.6). It returns nil output and a named error
// when any value would be lost — RetainLost, KindMismatch, Clamped or
// Malformed — or when the wire is refused; the input wire is never mutated and
// no Record replacement occurs on error. The result is idempotent: upgrading an
// already-upgraded wire is byte-for-byte the same (§6.6).
//
// The checked abstraction of this contract is tla/CardVersioning.tla: one
// atomic retained round trip — retained decode (CardLoadRetain), the
// accumulated loss-report check, retained encode (CardSaveRetain), then publish
// or refuse — holding NoFieldLost, BodyUnchanged, RefusalKeepsOriginal and
// ShippedUpgradeSucceeds. Loss reported by SAVE is checked before publishing
// too. The concrete generated-runtime proof is TestUpgradeKeepsEveryField.
func Upgrade(wire []byte) ([]byte, Report, error) {
	return upgrade(wire, MaxRetainBytes, MaxRetainIDs)
}

// upgrade is the package-private retention seam: Upgrade fixes the two customer
// capacities, and the refusal probes drive smaller caller-owned storage through
// the same path so a tiny capacity can be shown to name RetainLost and return
// nil while the input is preserved.
func upgrade(wire []byte, retainBytes, retainIDs int) ([]byte, Report, error) {
	if retainBytes < 0 || retainIDs < 0 {
		return nil, Report{}, errors.New("cards: upgrade: negative retention capacity")
	}
	var report TableReport
	n := CardLoadMeasure(wire)
	if n < 0 {
		_, reason := CardLoadMeasureReason(wire)
		if reason != nil {
			report.Reason = reason.Error()
		}
		CardLoadTolerant(make([]byte, 64), wire, &report)
		return nil, report, fmt.Errorf("cards: upgrade: wire is not a card: %v", reason)
	}
	if n > int64(maxHostInt)-64 {
		return nil, report, fmt.Errorf("cards: upgrade: measured region %d overflows the host", n)
	}
	region := alignedRegion(n)
	retain := TableRetain{Bytes: make([]byte, retainBytes), Ids: make([]TableRetainId, retainIDs)}
	root := CardLoadRetain(region, wire, &retain, &report)
	if root == nil {
		return nil, report, errors.New("cards: upgrade: wire is malformed")
	}
	if err := checkCard(root, nil); err != nil {
		return nil, report, fmt.Errorf("cards: upgrade: %w", err)
	}
	size := CardMeasureRetain(root, &retain)
	if size < 0 {
		return nil, report, errors.New("cards: upgrade: measure refused the retained card")
	}
	if size > int64(maxHostInt) {
		return nil, report, fmt.Errorf("cards: upgrade: measured output %d overflows the host", size)
	}
	out := make([]byte, size)
	if CardSaveRetain(root, &retain, out, &report) != size {
		return nil, report, errors.New("cards: upgrade: save did not fill the measured wire")
	}
	if err := lossErr(&report); err != nil {
		return nil, report, err
	}
	return out, report, nil
}

// lossErr names the report events that make a rewrite unsafe (§6.6): any
// retained field that could not be kept, any kind mismatch, any clamped value,
// or framing damage. Unknown, Retained and Widened alone are not loss (§4).
func lossErr(report *TableReport) error {
	if report.RetainLost > 0 {
		return fmt.Errorf("cards: upgrade: %d retained field(s) lost, refusing to replace", report.RetainLost)
	}
	if report.KindMismatch > 0 {
		return fmt.Errorf("cards: upgrade: %d kind mismatch(es), refusing to replace", report.KindMismatch)
	}
	if report.Clamped > 0 {
		return fmt.Errorf("cards: upgrade: %d clamped value(s), refusing to replace", report.Clamped)
	}
	if report.Malformed {
		return errors.New("cards: upgrade: malformed wire, refusing to replace")
	}
	return nil
}

// ReadStrict reads wire and refuses any data the current declaration cannot
// support: Unknown, KindMismatch, Clamped, Malformed and every load failure
// (docs/SPEC-TABLES.md §4). A safe Widened event need not refuse. It returns no
// usable Record on rejection.
func ReadStrict(wire []byte) (*Record, error) {
	rec, report, err := Load(wire)
	if err != nil {
		return nil, err
	}
	if report.Unknown > 0 {
		return nil, fmt.Errorf("cards: strict: %d unknown field(s), unsupported data", report.Unknown)
	}
	if report.KindMismatch > 0 {
		return nil, fmt.Errorf("cards: strict: %d kind mismatch(es), unsupported data", report.KindMismatch)
	}
	if report.Clamped > 0 {
		return nil, fmt.Errorf("cards: strict: %d clamped value(s), unsupported data", report.Clamped)
	}
	if report.Malformed {
		return nil, errors.New("cards: strict: malformed wire, unsupported data")
	}
	return rec, nil
}
