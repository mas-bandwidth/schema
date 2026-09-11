// THE MONOTONE LAW, NUMBERS AND KINDS
// (docs/FIXED-FORM-BILL-READS-BACKWARD.md §6).
//
// Glenn, 2026-09-10: "Newer versions of the fixed table should be able to read
// OLD versions. But old versions CANNOT read new versions, and complain loudly
// and refuse." "You can take an enum and widen it, but you cannot narrow, or
// incompatible." "wstring/strings/bytes can be widened only, not narrowed,
// because a narrowed string/array cannot read the old." "same with arrays and
// strings etc."
//
// A FIXED TABLE READS BACKWARD, so every definition that inputs into one may
// only GROW. The principle behind every row of the bill's §2 table is one
// sentence: a newer reader must be able to hold every value an older writer
// could produce, exactly. Where a widening keeps that true it is allowed;
// where it cannot, the edit is incompatible and this file refuses it at
// commit, which is what makes the reader's `layout_newer` refusal a thing no
// legal peer ever meets.
//
// WHAT THIS FILE IS, NEXT TO §18. The tables baseline asks a save game's
// question: what does an old FILE mean to a new reader, and the answers are
// the three verdicts of §18.2, where a shrunk bound WARNS because the runtime
// counts the clamp. A fixed table has no such middle: the clamp is gone from
// the reference (bill §5), so the same edit has no runtime report left and the
// only correct answer is REFUSE. So this law does not change a §18 row — it
// adds refusals, and only ever for a table whose live declaration says `fixed
// table`. A variable-only unit keeps every verdict it had.
//
// IT ONLY EVER ADDS. Nothing here turns a §18 refusal into a pass, and that
// asymmetry is deliberate: the bill ALLOWS an int widened along its ladder
// (the reader lands it exactly, `COUNT widened`), while §18's `kind` row
// refuses every kind change for the save-game reason. Loosening that row is a
// decision about the whole baseline and it belongs to the bill's §8 backport,
// not to this file. So a widening the law allows may still be refused by the
// `kind` row above it — monotoneNumbers says only that the LAW has no
// complaint, which is what its tests assert.
//
// THE GATE IS THE LIVE DECLARATION, and [Table.Fixed] is a live-only fact
// (see its comment): a table that has just become fixed comes under the law at
// that commit, and the committed file needs no new token to say what it used
// to be.
package baseline

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// monotoneNumbers holds the bill's §6 law over the NUMBERS and the KINDS of
// the fields of every FIXED table, and returns one line per edit that is not a
// widening: "fixed <Table>: <field>: <rule> (<old> -> <new>)".
//
// old is the committed baseline, new the live projection. Fields are matched
// BY WIRE ID, as every walk in this package matches them: the id is the
// identity a reader keys on, and a field whose id is gone was removed, which
// is the bill's field row and not a number's (an older writer still sending it
// is `layout_newer`'s business at plan time, and the lists half of the law
// holds the append-only field order at commit).
//
// Tables are matched BY NAME and nothing else. A renamed table is paired by
// [pairMembers] for §18's walks, and pairing it here too would make the law's
// refusals depend on a heuristic; a `was` rename keeps the WIRE name, which is
// the name on both lines, so the case that matters is not a rename at all.
func monotoneNumbers(old, new *Unit) []string {
	live := map[string]Table{}
	for _, t := range new.Tables {
		live[t.Name] = t
	}
	var out []string
	for _, bt := range old.Tables {
		lt, ok := live[bt.Name]
		// THE GATE: the law is the FIXED form's. A plain `table` is the
		// variable wire whatever its fields happen to be (§2.2), and it keeps
		// §18's verdicts.
		if !ok || !lt.Fixed {
			continue
		}
		liveFields := map[uint64]Field{}
		for _, f := range lt.Fields {
			liveFields[f.Id] = f
		}
		for _, bf := range bt.Fields {
			lf, still := liveFields[bf.Id]
			if !still {
				continue
			}
			out = append(out, monotoneFieldNumbers(lt.Name, lf.Name, bf, lf)...)
		}
	}
	return out
}

// monotoneFieldNumbers judges one field's numbers and kinds, old against new.
func monotoneFieldNumbers(table, field string, was, now Field) []string {
	var out []string
	say := func(rule, before, after string) {
		out = append(out, fmt.Sprintf("fixed %s: %s: %s (%s -> %s)", table, field, rule, before, after))
	}

	// ---- the extents that may only GROW ----
	//
	// An array bound, a string/wstring/bytes capacity, and `bits(N)`. The
	// baseline's values are EVALUATED (§18.1), so a constant behind any of
	// them is judged here by the number it now produces and needs no rule of
	// its own: `[..Slots]` with Slots moved from 8 to 4 is a bound of 8 against
	// a bound of 4, exactly as a hand-written 4 would be.
	for _, key := range growOnly {
		before, had := was.Get(key.token)
		after, has := now.Get(key.token)
		if !had || !has {
			// A TOKEN THAT ARRIVES OR LEAVES is not a number moving. `bound=`
			// appearing or vanishing is the unbounded array's capacity fact
			// (§18.1), and an unbounded array cannot sit in a fixed closure at
			// all — the compiler refuses it (§2.2) — so there is nothing here
			// for this loop to say about either.
			continue
		}
		// A KEYED ARRAY'S BOUND IS ITS KEY ENUM'S SIZE (§3.2), not a capacity
		// anyone declared: it shrinks only when a variant goes, which is the
		// enum's own row of the law and is reported there, by name.
		if shape, _ := was.Get("array"); key.token == "bound" && shape == "keyed" {
			continue
		}
		if tightened(RuleShrink, before, after) {
			say(key.rule, before, after)
		}
	}

	// ---- a ranged scalar: neither end may move INWARD ----
	//
	// A fixed table's reader has no cross-version clamp left (bill §5), so a
	// stored value outside the new range is a value the reader cannot hold and
	// cannot report. Moving either end OUTWARD is a widening and passes: every
	// value already written still fits.
	monotoneRange(say, was, now)

	// ---- the kind: the same ladder, never narrower ----
	//
	// `elem` is the same vocabulary one level in — an array's element kind —
	// and is judged by the same rule, because an element narrowed loses
	// exactly what a scalar narrowed loses.
	for _, key := range []string{"kind", "elem"} {
		before, had := was.Get(key)
		after, has := now.Get(key)
		if !had || !has || before == after {
			continue
		}
		if rule, refused := kindMove(key, before, after); refused {
			say(rule, before, after)
		}
	}

	// ---- an optional only ever APPEARS ----
	//
	// `T` where the reader has `?T` lands present (bill §2); `?T` where the
	// reader has `T` has no way to land an absence, so dropping the `?` is
	// refused. The token itself is judged on nothing by §18 — T, ?T and *T are
	// one framing on the wire (§3.1) — and this is the one place the fixed
	// form's read makes the direction matter.
	if _, had := was.Get("optional"); had {
		if _, has := now.Get("optional"); !has {
			say("an optional only appears: a reader with no presence companion cannot land a stored absence (bill §2)", "?T", "T")
		}
	}
	return out
}

// growOnly is the extents that may only grow, and the rule each one states
// when it shrinks. One row per definition the bill's §2 table names, in the
// words of the row.
var growOnly = []struct {
	token string
	rule  string
}{
	{"bound", "an array bound only grows: a narrowed array cannot read the old, and a fixed table has no clamp left to report it (bill §2, §5)"},
	{"size", "a string/wstring/bytes capacity only grows: a narrowed string cannot read the old (bill §2)"},
	{"bits", "a bits(N) width only grows: a narrower N cannot hold every value the old writer could produce (bill §2)"},
}

// monotoneRange judges a declared range's two ends. Raising a minimum and
// lowering a maximum are the same edit from opposite sides, and DECLARING a
// range where the field had none is the same edit from the kind's whole
// domain — the bill's row reads "inside the reader's", and a range the old
// writer never had was the kind's own extent.
func monotoneRange(say func(rule, before, after string), was, now Field) {
	for _, end := range []struct {
		token string
		rule  TokenRule
		words string
	}{
		{"min", RuleRaise, "a declared minimum only moves outward: a stored value below it has nowhere to land and no clamp to count it (bill §2, §5)"},
		{"max", RuleShrink, "a declared maximum only moves outward: a stored value above it has nowhere to land and no clamp to count it (bill §2, §5)"},
	} {
		before, had := was.Get(end.token)
		after, has := now.Get(end.token)
		switch {
		case had && has:
			if tightened(end.rule, before, after) {
				say(end.words, before, after)
			}
		case !had && has:
			// A RANGE DECLARED WHERE THERE WAS NONE is an extent tightened
			// from the kind's whole domain onto a narrower one — §18.2 warns
			// on it for that reason, and in a fixed closure the warning has no
			// runtime report behind it any more.
			say(end.words, "none", after)
		}
		// a range REMOVED is the largest widening there is, and passes.
	}
}

// kindMove judges one kind change, and is the bill's row "an integer or float
// width: at most the reader's, SAME SIGNEDNESS LADDER; refuse wider, or a
// different kind". It answers (rule, refused): a widening along one ladder is
// silent, because the reader lands it exactly and counts `widened` (bill §3).
func kindMove(token, before, after string) (string, bool) {
	noun := "a field's kind"
	if token == "elem" {
		noun = "an array's element kind"
	}
	from, okFrom := kindLadders[atoiOr(before, 0)]
	to, okTo := kindLadders[atoiOr(after, 0)]
	switch {
	case !okFrom || !okTo:
		// One side is not on a numeric ladder at all — a string, a nested
		// table, a union, an array, a pointer. There is no widening between a
		// number and one of those, or between two of those: it is a different
		// definition wearing the same field id.
		return noun + " is fixed: a kind change is a different definition, not a version of one (bill §2)", true
	case from.ladder != to.ladder:
		return fmt.Sprintf("%s is fixed: the %s ladder and the %s ladder are different kinds, not widths of one (bill §2)", noun, from.ladder, to.ladder), true
	case to.rung < from.rung:
		return fmt.Sprintf("%s only widens: a narrower %s cannot hold every value the old writer could produce (bill §2)", noun, from.ladder), true
	}
	return "", false
}

// kindLadders is the four numeric ladders of the table wire (§3), and the
// SIGNEDNESS is part of the ladder's identity, not a step along it: int32 to
// uint32 moves no byte and changes what every stored negative value means, so
// it is a different kind and not a width.
//
// `fixed(I,F)` and `ufixed(I,F)` ride as their RAW scaled storage integer, one
// kind per storage width (§3), so widening I moves a fixed field UP its own
// ladder and is a widening here. F is the scale, it is not a width at all, and
// it is already held fixed by `frac=` (§4.1, §18.1) — a moved F reads every
// stored raw value as a different number, and nothing can report it.
//
// `bits(N)` has no ladder of its own: it rides under the unsigned kind its
// storage width gives it, and its N is the `bits=` token.
var kindLadders = map[int]struct {
	ladder string
	rung   int
}{
	ir.TableKindI8:   {"int", 1},
	ir.TableKindI16:  {"int", 2},
	ir.TableKindI32:  {"int", 3},
	ir.TableKindI64:  {"int", 4},
	ir.TableKindI128: {"int", 5},

	ir.TableKindU8:   {"uint", 1},
	ir.TableKindU16:  {"uint", 2},
	ir.TableKindU32:  {"uint", 3},
	ir.TableKindU64:  {"uint", 4},
	ir.TableKindU128: {"uint", 5},

	ir.TableKindF32: {"float", 1},
	ir.TableKindF64: {"float", 2},

	ir.TableKindFixed8:   {"fixed", 1},
	ir.TableKindFixed16:  {"fixed", 2},
	ir.TableKindFixed32:  {"fixed", 3},
	ir.TableKindFixed64:  {"fixed", 4},
	ir.TableKindFixed128: {"fixed", 5},

	ir.TableKindUFixed8:   {"ufixed", 1},
	ir.TableKindUFixed16:  {"ufixed", 2},
	ir.TableKindUFixed32:  {"ufixed", 3},
	ir.TableKindUFixed64:  {"ufixed", 4},
	ir.TableKindUFixed128: {"ufixed", 5},
}

// monotoneFindings turns the law's lines into the refusals the check already
// carries, so a fixed table's monotone break arrives where every other
// refusal does: as an error on the compile, named, with the old value, the new
// one and the rule. The line's own first segment is its `Where`, which keeps
// [Finding.String] byte-identical to the line.
func monotoneFindings(lines []string) []Finding {
	out := make([]Finding, 0, len(lines))
	for _, line := range lines {
		where, what, found := strings.Cut(line, ": ")
		if !found {
			where, what = line, ""
		}
		out = append(out, Finding{Refuse, where, what})
	}
	return out
}

// atoiOr reads a token's integer value, or a fallback for a value this
// rendering did not write.
func atoiOr(s string, fallback int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return fallback
}
