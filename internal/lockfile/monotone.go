// THE MONOTONE LAW (docs/FIXED-FORM-BILL-READS-BACKWARD.md §2 and §6, Glenn's
// ruling 2026-09-11 02:12Z: "lock."): a definition a fixed table stands on
// WIDENS or it stays, and every narrowing is refused here, by name, with the
// table, the definition, the rule and BOTH VALUES in the sentence.
//
// The bill's asymmetry is the whole of this file: "Newer versions of the fixed
// table should be able to read OLD versions. But old versions CANNOT read new
// versions." So a widening is not a break — it is a change the record must
// carry, which is the refusal [stale] already wrote and `schema lock` already
// takes. A narrowing is a break: a newer reader could not hold a value an
// older writer produced, and no default fills that gap.
//
// One mechanism, the same shape as the list refusals that were here first: one
// comparison per recorded fact, in the order that names the change best, the
// first difference is the finding, and the rule phrase is the clause a person
// greps for.
package lockfile

import (
	"fmt"
	"strconv"
	"strings"
)

// The scalar LADDERS (§2: "at most the reader's, same signedness ladder"). A
// width grows along one ladder and that is a read; a jump between ladders is
// not a version of anything.
const (
	ladderNone = iota
	ladderInt
	ladderUint
	ladderFloat
	ladderFixed
	ladderUFixed
)

// ladder answers which ladder a table-wire kind sits on and how many bits of
// storage it takes there. A kind on no ladder answers ladderNone.
func ladder(kind int) (fam, bits int) {
	switch kind {
	case 2:
		return ladderInt, 8
	case 3:
		return ladderInt, 16
	case 4:
		return ladderInt, 32
	case 5:
		return ladderInt, 64
	case 18:
		return ladderInt, 128
	case 6:
		return ladderUint, 8
	case 7:
		return ladderUint, 16
	case 8:
		return ladderUint, 32
	case 9:
		return ladderUint, 64
	case 19:
		return ladderUint, 128
	case 10:
		return ladderFloat, 32
	case 11:
		return ladderFloat, 64
	case 20:
		return ladderFixed, 8
	case 21:
		return ladderFixed, 16
	case 22:
		return ladderFixed, 32
	case 23:
		return ladderFixed, 64
	case 24:
		return ladderFixed, 128
	case 25:
		return ladderUFixed, 8
	case 26:
		return ladderUFixed, 16
	case 27:
		return ladderUFixed, 32
	case 28:
		return ladderUFixed, 64
	case 29:
		return ladderUFixed, 128
	}
	return ladderNone, 0
}

// kindName is a kind as a refusal spells it — the declaration's own word where
// there is one, so "narrowed (int16 -> int8)" reads as the schema reads.
func kindName(kind int) string {
	switch kind {
	case 12:
		return "string"
	case 14:
		return "array"
	case 16:
		return "keyed array"
	case 18:
		return "int128"
	case 19:
		return "uint128"
	case 33:
		return "wstring"
	case 20, 21, 22, 23, 24:
		return "fixed"
	case 25, 26, 27, 28, 29:
		return "ufixed"
	}
	if w := kindWord(kind); w != "" {
		return w
	}
	return "kind " + strconv.Itoa(kind)
}

// shapeWord is an array's shape as a refusal spells it, and "not an array" for
// a field that has none.
func shapeWord(shape string) string {
	if shape == "" {
		return "not an array"
	}
	return shape
}

// kindRule classifies a kind change on the scalar ladders: widen reports true,
// and everything else comes back as the rule phrase that names it.
func kindRule(want, got Entry) (rule string, widened bool) {
	wf, wb := ladder(want.Kind)
	gf, gb := ladder(got.Kind)
	switch {
	case wf == ladderNone || gf == ladderNone:
		return fmt.Sprintf("kind changed (%s -> %s)", kindName(want.Kind), kindName(got.Kind)), false
	case wf == gf && gb > wb:
		return "", true
	case wf == gf && (wf == ladderFixed || wf == ladderUFixed):
		return fmt.Sprintf("I narrowed (%d -> %d)", want.IBits, got.IBits), false
	case wf == gf:
		return fmt.Sprintf("narrowed (%s -> %s)", kindName(want.Kind), kindName(got.Kind)), false
	case wf == ladderInt && gf == ladderUint, wf == ladderUint && gf == ladderInt,
		wf == ladderFixed && gf == ladderUFixed, wf == ladderUFixed && gf == ladderFixed:
		return fmt.Sprintf("signedness changed (%s -> %s)", kindName(want.Kind), kindName(got.Kind)), false
	default:
		return fmt.Sprintf("ladder changed (%s -> %s)", kindName(want.Kind), kindName(got.Kind)), false
	}
}

// elemRule is [kindRule] over an array's ELEMENT, which follows the same
// ladder one level in (§2: "an element type follows the widening ladder, an
// element narrowed is refused").
func elemRule(want, got Entry) (rule string, widened bool) {
	wf, wb := ladder(want.ElemKind)
	gf, gb := ladder(got.ElemKind)
	switch {
	case wf != ladderNone && wf == gf && gb > wb:
		return "", true
	case wf != ladderNone && wf == gf && (wf == ladderFixed || wf == ladderUFixed):
		return fmt.Sprintf("element I narrowed (%s -> %s)", kindName(want.ElemKind), kindName(got.ElemKind)), false
	case wf != ladderNone && wf == gf:
		return fmt.Sprintf("element narrowed (%s -> %s)", kindName(want.ElemKind), kindName(got.ElemKind)), false
	default:
		return fmt.Sprintf("element kind changed (%s -> %s)", elemWord(want.ElemKind, want.ElemWidth), elemWord(got.ElemKind, got.ElemWidth)), false
	}
}

// rangeRule classifies a move of a field's declared bounds or its scale. A
// range OUTWARD is a widening: every value an older writer could hold is still
// inside. A range INWARD, a range added where none was, and a moved scale are
// refusals — the first two because an old value now clamps, the third because
// the same stored number means something else (§2).
func rangeRule(want, got Entry) (rule string, widened bool) {
	switch {
	case want.Frac != got.Frac:
		return fmt.Sprintf("F changed (%d -> %d)", want.Frac, got.Frac), false
	case want.Res != got.Res:
		return fmt.Sprintf("resolution changed (%s -> %s)", want.rangeText(), got.rangeText()), false
	case !want.ranged() && got.ranged():
		return fmt.Sprintf("range added where none was (%s -> %s)", want.rangeText(), got.rangeText()), false
	case want.ranged() && !got.ranged():
		return "", true
	case boundOutward(want.Min, got.Min, true) && boundOutward(want.Max, got.Max, false):
		return "", true
	default:
		return fmt.Sprintf("range narrowed (%s -> %s)", want.rangeText(), got.rangeText()), false
	}
}

// ranged reports whether an entry declares any bound at all.
func (e Entry) ranged() bool { return e.Min != "" || e.Max != "" }

// boundOutward reports whether one end of a range moved OUTWARD or stayed. A
// value this file cannot read as a number is not silently a widening: the
// answer is false, and the refusal says the bounds moved.
func boundOutward(was, now string, low bool) bool {
	if was == now {
		return true
	}
	w, err1 := strconv.ParseFloat(was, 64)
	n, err2 := strconv.ParseFloat(now, 64)
	if err1 != nil || err2 != nil {
		return false
	}
	if low {
		return n <= w
	}
	return n >= w
}

// sameFacts reports whether two entries agree on everything but their NAME and
// the id derived from it — which is what a rename without `was` looks like, and
// the one thing it must not be mistaken for.
func sameFacts(a, b Entry) bool {
	a.Name, b.Name = "", ""
	a.Id, b.Id = 0, 0
	return a == b
}

// hasId reports whether a record carries a field under this wire id anywhere.
func (t *Table) hasId(id uint64) bool {
	for _, e := range t.Entries {
		if e.Id == id {
			return true
		}
	}
	return false
}

// idRule names what happened at a position whose field is not the field the
// lock put there: the record has no ids in it, so this is the refusal a person
// reads first, and the four cases are four different mistakes.
func idRule(lk, lv *Table, want, got Entry) string {
	inLive, inLock := lv.hasId(want.Id), lk.hasId(got.Id)
	switch {
	case !inLive && !inLock && len(lk.Entries) == len(lv.Entries) && sameFacts(want, got):
		return "renamed without was"
	case !inLive:
		return "field removed"
	case !inLock:
		return "field inserted not at the end"
	default:
		return "fields reordered"
	}
}

// hasValue reports whether a list carries this value anywhere.
func (v *ValueList) hasValue(s string) bool {
	for _, got := range v.Values {
		if got == s {
			return true
		}
	}
	return false
}

// moveWord is the word a reorder takes per declaration: a flags mask's bits
// MOVE (a bit position), an enum's variants and a union's arms are REORDERED (a
// dense ordinal). Glenn's rows name them that way and so does the refusal.
func (v *ValueList) moveWord() string {
	if v.Decl == DeclFlags {
		return "moved"
	}
	return "reordered"
}

// ruleChild is the word a RULE PHRASE uses for one of a list's members: a
// flags mask's members are FLAGS (a bit position each), and the design's rows
// name them that way — "flag moved", "flag removed" — where an enum's are
// variants and a union's are arms.
func (v *ValueList) ruleChild() string {
	if v.Decl == DeclFlags {
		return "flag"
	}
	return v.Child()
}

// valueRule is [idRule] for an enum, a flags mask or a union: the list is a
// numbering, so the four cases are the same four, one level of meaning in.
func valueRule(lk, lv *ValueList, want, got string) string {
	inLive, inLock := lv.hasValue(want), lk.hasValue(got)
	switch {
	case !inLive && !inLock && len(lk.Values) == len(lv.Values):
		return lk.ruleChild() + " renamed without was"
	case !inLive:
		return lk.ruleChild() + " removed"
	case !inLock:
		return lk.ruleChild() + " inserted mid-list"
	default:
		return lk.ruleChild() + " " + lk.moveWord()
	}
}

// monotone is the law over one field already in the lock: the first narrowing
// is the refusal, and a widening comes back as widened with the clause [stale]
// says it under.
//
// THE ORDER IS THE ORDER THAT NAMES THE CHANGE BEST, and it is the order the
// entry line grew in: the `?`, the shape, the kind, the type a slot holds, the
// element, the key, the bound, the capacity, the bits, the scale and the
// range, the default, the deprecation — then the WIDTH, which is every one of
// those added up and is the refusal for a move none of them explains.
func monotone(where string, want, got Entry) (widened bool, what string, err error) {
	what = "widened in the declaration and not in the lock"

	if want.Optional != got.Optional {
		if want.Optional {
			return false, "", fmt.Errorf("%s, is %s in the lock and %s in the declaration: optional removed — a field already in the lock keeps its `?`: an optional carries a presence bool beside its value INSIDE the record, so a reader that expects one where none is written takes the next field's first byte for the answer (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
				where, optionalText(want.Optional), optionalText(got.Optional))
		}
		// `T` to `?T` is a widening: every old value lands present (§2)
		widened = true
	}
	if want.Shape != got.Shape {
		return false, "", fmt.Errorf("%s, is %s in the lock and %s in the declaration: shape changed (%s -> %s) — a field already in the lock keeps its SHAPE: a fixed array is slots alone, a counted one carries its count, and a keyed one is numbered by an enum's variants, so a reader walks three different records (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
			where, shapeWord(want.Shape), shapeWord(got.Shape), shapeWord(want.Shape), shapeWord(got.Shape))
	}
	if want.Kind != got.Kind {
		rule, ok := kindRule(want, got)
		if !ok {
			return false, "", fmt.Errorf("%s, is kind %d in the lock and kind %d in the declaration: %s — a field already in the lock keeps its type: every record already written holds the old one, and nothing on the wire says which (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
				where, want.Kind, got.Kind, rule)
		}
		// a wider int, uint, float or fixed storage: the reader lands every
		// old value exactly, which is the bill's one asymmetry (§2)
		widened = true
	}
	if want.HeldName != got.HeldName {
		return false, "", fmt.Errorf("%s, held %s in the lock and %s in the declaration: held type changed (%s -> %s) — a field already in the lock keeps the type it holds: every record already written holds the old one, and nothing on the wire says which (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
			where, heldName(want.HeldName), heldName(got.HeldName), heldName(want.HeldName), heldName(got.HeldName))
	}
	if want.ElemKind != got.ElemKind || want.ElemWidth != got.ElemWidth {
		rule, ok := elemRule(want, got)
		if !ok {
			return false, "", fmt.Errorf("%s, holds %s elements in the lock and %s elements in the declaration: %s — a field already in the lock keeps its element type: every record already written holds the old one, and nothing on the wire says which (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
				where, elemWord(want.ElemKind, want.ElemWidth), elemWord(got.ElemKind, got.ElemWidth), rule)
		}
		widened = true
	}
	if want.KeyName != got.KeyName {
		return false, "", fmt.Errorf("%s, is keyed by %s in the lock and %s in the declaration: key changed (%s -> %s) — a field already in the lock keeps the enum it is keyed by: the key's variants ARE the slots, in declared order, so every record already written put its values in the slots the old list numbered (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
			where, heldName(want.KeyName), heldName(got.KeyName), heldName(want.KeyName), heldName(got.KeyName))
	}
	if want.Bound != got.Bound {
		if got.Bound < want.Bound {
			return false, "", fmt.Errorf("%s, is bounded at %d elements in the lock and %d in the declaration: bound narrowed (%d -> %d) — a bound only GROWS: a newer reader defaults the slots an older writer never wrote, and a narrower one cannot hold what the older writer did write (docs/FIXED-FORM-BILL-READS-BACKWARD.md §2, docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
				where, want.Bound, got.Bound, want.Bound, got.Bound)
		}
		widened = true
	}
	if want.Cap != got.Cap {
		if got.Cap < want.Cap {
			return false, "", fmt.Errorf("%s, holds %d in the lock and %d in the declaration: capacity narrowed (%d -> %d) — Glenn: \"wstring/strings/bytes can be widened only, not narrowed, because a narrowed string/array cannot read the old\" (docs/FIXED-FORM-BILL-READS-BACKWARD.md §2, docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
				where, want.Cap, got.Cap, want.Cap, got.Cap)
		}
		widened = true
	}
	if want.Bits != got.Bits {
		if got.Bits < want.Bits {
			return false, "", fmt.Errorf("%s, is %d bits wide in the lock and %d in the declaration: bits narrowed (%d -> %d) — `bits(N)` only GROWS: N at most the reader's is a read, and a narrower N cannot hold the value the older writer packed (docs/FIXED-FORM-BILL-READS-BACKWARD.md §2, docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
				where, want.Bits, got.Bits, want.Bits, got.Bits)
		}
		widened = true
	}
	if !got.sameRange(want) {
		rule, ok := rangeRule(want, got)
		if !ok {
			return false, "", fmt.Errorf("%s, is %s in the lock and %s in the declaration: %s — a field already in the lock keeps its range: the bounds and the resolution are the scale a stored value is read back at, so moving them reads every record already written as a different number (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
				where, want.rangeText(), got.rangeText(), rule)
		}
		widened = true
	}
	if got.Default != want.Default {
		return false, "", fmt.Errorf("%s, defaults to %s in the lock and %s in the declaration: default changed (%s -> %s) — a field already in the lock keeps its default: an older writer's missing field is filled from this default, and a deprecated slot holds it, so a record written before the change reads differently after it (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
			where, want.Default, got.Default, want.Default, got.Default)
	}
	switch {
	case want.Deprecated && !got.Deprecated:
		// UNDEPRECATING IS ALLOWED (docs/FIXED-FORM-BILL-READS-BACKWARD.md
		// §2): the slot never moved, the writers that ran since left the
		// default in it, and a reader that starts reading it again reads the
		// default. What is refused is the field LEAVING the layout.
		widened, what = true, "live in the declaration and deprecated in the lock"
	case !want.Deprecated && got.Deprecated:
		widened, what = true, "deprecated in the declaration and live in the lock"
	}
	// THE WIDTH LAST. It is every fact above added up, so it is the refusal
	// for a move none of them explains — and it is SILENT when a widening
	// above explains it, or when the field holds a named type whose own block
	// in this file is where its change is reported.
	if want.Width != got.Width && !widened && want.HeldName == "" {
		return false, "", fmt.Errorf("%s, is %d bytes wide in the lock and %d in the declaration: width changed (%d -> %d) — a field already in the lock keeps its width: a fixed record is walked by offset, so widening one field moves every field after it (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
			where, want.Width, got.Width, want.Width, got.Width)
	}
	return widened, what, nil
}

// ensure the rule phrases stay greppable from the docs that name them
var _ = strings.TrimSpace
