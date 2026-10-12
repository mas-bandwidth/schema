package cards

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"unicode/utf8"
	"unsafe"
)

// TaskHash returns a digest of a record's TASK, the fields a human's memory
// of a card is made of: body, paths, new, shared, test and done. Everything
// else — identity/coordination, needs/tier/priority, machine fields — is
// deliberately excluded, so a change to any of those leaves the digest
// untouched. The digest is framed: each text is an unsigned 64-bit
// little-endian byte length followed by its exact bytes, and each list is an
// entry count followed by its length-prefixed elements, so field and element
// boundaries cannot collide (docs/SPEC-TABLES.md §5: identity is the fields a
// task is, an the wire).
//
// The attempt-bound rule: the digest is only meaningful for a record whose
// wire decoded cleanly. A nil, failed, malformed or lossy record (unknown or
// mismatched fields, clamped values, retain loss) is refused with an error,
// never hashed into an apparently valid digest.
func TaskHash(r *Record) ([32]byte, error) {
	var zero [32]byte
	if r == nil || r.root == nil {
		return zero, errors.New("cards: nil record")
	}
	if err := refuseLossy(r.report); err != nil {
		return zero, err
	}
	h := sha256.New()
	if err := hashText(h, bodyBytes(r.root)); err != nil {
		return zero, err
	}
	if err := hashPathList(h, &r.root.Paths, "paths"); err != nil {
		return zero, err
	}
	if err := hashPathList(h, &r.root.New, "_new"); err != nil {
		return zero, err
	}
	if err := hashPathList(h, &r.root.Shared, "shared"); err != nil {
		return zero, err
	}
	if err := hashText(h, rootTestBytes(r.root)); err != nil {
		return zero, err
	}
	if err := hashText(h, doneBytes(r.root)); err != nil {
		return zero, err
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

// refuseLossy names the read events that make a record unsafe to hash or
// rewrite: a silent loss means the "task" the hash would cover is incomplete
// (docs/SPEC-TABLES.md §4: wire tolerance; §6.6: retain-unknown).
func refuseLossy(rep TableReport) error {
	if rep.Unknown > 0 || rep.KindMismatch > 0 || rep.Clamped > 0 || rep.RetainLost > 0 || rep.Malformed {
		return fmt.Errorf("cards: record has loss events (unknown=%d kindMismatch=%d clamped=%d retainLost=%d malformed=%v): refused, never hashed or rewritten",
			rep.Unknown, rep.KindMismatch, rep.Clamped, rep.RetainLost, rep.Malformed)
	}
	return nil
}

func hashU64(h hash.Hash, v uint64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	h.Write(b[:])
}

func hashText(h hash.Hash, text []byte) error {
	if len(text) < 0 {
		return errors.New("cards: negative text length")
	}
	hashU64(h, uint64(len(text)))
	h.Write(text)
	return nil
}

func hashPathList(h hash.Hash, list *TableList[PathTextRow], name string) error {
	if list == nil || list.Count < 0 {
		return fmt.Errorf("cards: %s list is malformed", name)
	}
	hashU64(h, uint64(list.Count))
	var elemErr error
	CardPathsEach(list, func(e *PathTextRow) bool {
		n := int32(len(e.S))
		if e.SLength < n {
			n = e.SLength
		}
		if n < 0 {
			n = 0
		}
		if err := hashText(h, e.S[:n]); err != nil {
			elemErr = err
			return false
		}
		return true
	})
	return elemErr
}

func rootTestBytes(root *Card) []byte {
	n := int32(len(root.Test))
	if root.TestLength < n {
		n = root.TestLength
	}
	if n < 0 {
		n = 0
	}
	return root.Test[:n]
}

func bodyBytes(root *Card) []byte {
	if root.Body == 0 {
		return nil
	}
	return stringData(&root.Body, nil)
}

func doneBytes(root *Card) []byte {
	if root.Done == 0 {
		return nil
	}
	return stringData(&root.Done, nil)
}

// SetNeeds replaces the record's needs list atomically. The incoming values
// are validated first through the same policy the reads use (each element well
// formed, within its string bound, and the list within the shared 512-entry
// ceiling); only a fully validated, independently built and successfully
// serialized replacement is published. On any refusal the original wire is
// preserved byte for byte (docs/SPEC-TABLES.md §2.9, §6.2).
func SetNeeds(r *Record, needs []string) error {
	if err := validateStrings("needs", needs, 64); err != nil {
		return err
	}
	if len(needs) > MaxListEntries {
		return fmt.Errorf("cards: needs has %d entries, past the %d-entry ceiling (docs/SPEC-TABLES.md §2.9)", len(needs), MaxListEntries)
	}
	return edit(r, func(root *Card, w *TableWorker) error {
		root.Needs.Clear()
		for _, s := range needs {
			e := CardNeedsAdd(w, &root.Needs)
			if e == nil {
				return errors.New("cards: needs alloc failed")
			}
			setString(e.S[:], &e.SLength, s)
		}
		return nil
	})
}

// SetPaths replaces the record's paths list atomically (docs/SPEC-TABLES.md §2.9).
func SetPaths(r *Record, paths []string) error {
	if err := validateStrings("paths", paths, 192); err != nil {
		return err
	}
	if len(paths) > MaxListEntries {
		return fmt.Errorf("cards: paths has %d entries, past the %d-entry ceiling (docs/SPEC-TABLES.md §2.9)", len(paths), MaxListEntries)
	}
	return edit(r, func(root *Card, w *TableWorker) error {
		root.Paths.Clear()
		for _, s := range paths {
			e := CardPathsAdd(w, &root.Paths)
			if e == nil {
				return errors.New("cards: paths alloc failed")
			}
			setString(e.S[:], &e.SLength, s)
		}
		return nil
	})
}

// SetTier changes a record's tier atomically; only a declared variant is
// accepted (docs/SPEC-TABLES.md §8: the vocabulary bounds the value).
func SetTier(r *Record, tier Tier) error {
	if tier > TierMax {
		return fmt.Errorf("cards: tier %d is not a declared variant (docs/SPEC-TABLES.md §8)", tier)
	}
	return edit(r, func(root *Card, w *TableWorker) error {
		root.Tier = tier
		return nil
	})
}

// SetPriority changes a record's priority atomically (docs/SPEC-TABLES.md §8).
func SetPriority(r *Record, priority Priority) error {
	if priority > PriorityMax {
		return fmt.Errorf("cards: priority %d is not a declared variant (docs/SPEC-TABLES.md §8)", priority)
	}
	return edit(r, func(root *Card, w *TableWorker) error {
		root.Priority = priority
		return nil
	})
}

// SetColumn changes a record's column atomically (docs/SPEC-TABLES.md §8).
func SetColumn(r *Record, column Column) error {
	if column > ColumnMax {
		return fmt.Errorf("cards: column %d is not a declared variant (docs/SPEC-TABLES.md §8)", column)
	}
	return edit(r, func(root *Card, w *TableWorker) error {
		root.Column = column
		return nil
	})
}

// SetHolder changes a record's holder atomically; the value must be well
// formed and within its declared bound (docs/SPEC-TABLES.md §6.1).
func SetHolder(r *Record, holder string) error {
	if err := validateStrings("holder", []string{holder}, 32); err != nil {
		return err
	}
	return edit(r, func(root *Card, w *TableWorker) error {
		setString(root.Holder[:], &root.HolderLength, holder)
		return nil
	})
}

// validateStrings applies the shared element policy: well-formed UTF-8
// without NUL and within the declared string bound (docs/SPEC-TABLES.md §6.1).
func validateStrings(name string, values []string, bound int) error {
	for _, s := range values {
		if len(s) > bound {
			return fmt.Errorf("cards: %s element is %d bytes, past its %d-byte bound", name, len(s), bound)
		}
		if !utf8.ValidString(s) || indexNUL(s) >= 0 {
			return fmt.Errorf("cards: %s has an element that is not well-formed UTF-8 without NUL", name)
		}
	}
	return nil
}

func indexNUL(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return i
		}
	}
	return -1
}

// setString places an already-validated Go string into a generated string(N)
// buffer and its used-length companion (docs/SPEC-TABLES.md §6.1). It lives
// here, not in a _test.go file, because edit's production code writes strings
// and the generated conformance module compiles this package without its
// tests; a production write must not depend on a test-only symbol.
func setString(dst []byte, n *int32, s string) {
	copy(dst, s)
	*n = int32(len(s))
}

// edit is the shared atomic rewrite: check the current record is safe,
// reload it into a fresh independent builder, apply mutate to the builder's
// root, validate the whole rebuilt card, pack it, measure and save it, and
// only on complete success swap the record's owned wire and backing region.
// Any refusal leaves the original wire untouched (docs/SPEC-TABLES.md §6.2).
func edit(r *Record, mutate func(root *Card, w *TableWorker) error) error {
	if r == nil || r.root == nil {
		return errors.New("cards: nil record")
	}
	if err := refuseLossy(r.report); err != nil {
		return err
	}
	var b CardBuilder
	if !b.Init() {
		return errors.New("cards: builder init failed")
	}
	defer b.Shutdown()
	var rep TableReport
	if !CardLoadBuilder(&b, r.wire, &rep) {
		return errors.New("cards: reload into builder failed")
	}
	if err := refuseLossy(rep); err != nil {
		return err
	}
	if err := mutate(b.GetRoot(), &b.Main); err != nil {
		return err
	}
	if err := checkCard(b.GetRoot(), &b.Arena); err != nil {
		return err
	}
	if r.failEdit != nil {
		if err := r.failEdit(); err != nil {
			return err
		}
	}
	if !b.Lock() {
		return errors.New("cards: pack failed")
	}
	packed := b.AsConst()
	size, reason := CardMeasureReason(packed)
	if size < 0 {
		return fmt.Errorf("cards: measure refused rebuilt card: %v", reason)
	}
	wire := make([]byte, size)
	if CardSave(packed, wire) != size {
		return errors.New("cards: save did not fill the measured wire")
	}
	region := make([]byte, len(b.Region()))
	copy(region, b.Region())
	own := (*Card)(unsafe.Pointer(&region[0]))
	r.wire, r.region, r.root, r.report = wire, region, own, rep
	return nil
}
