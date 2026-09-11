// The MONOTONE LAW's gate, numbers and kinds
// (docs/FIXED-FORM-BILL-READS-BACKWARD.md §6): one test per forbidden change,
// one per allowed widening, each named by GLENN'S OWN SENTENCE for the rule it
// holds.
//
// It is an INTERNAL test file (package baseline) because the law's entry point
// is monotoneNumbers, and the law is what these cases are about: every case
// states an old baseline — rendered and taken through the file's own text form,
// so the old side is a PARSED baseline exactly as a committed one is — against
// the unit as it now stands, and asserts the law's line or its silence. The two
// cases that go through [Diff] instead say where the law lands: in the refusal
// path the check already fails a compile with.
package baseline

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// monotoneSrc is the fixture: ONE FIXED TABLE whose fields carry every number
// and kind the bill's §6 table names — a bound behind a constant, a fixed-size
// array, the three capacities, a declared range, a bits width, a fixed-point
// scale, an optional, and one field of each numeric ladder.
const monotoneSrc = `package fixture

const Slots = 8

type Buff
{
    multiplier float32 = 1.0
}

// another body of the same shape-less kind: a nested type swapped for this one
// is not a widening of anything
type Debuff
{
    amount int32 = 0
}

fixed table Ship
{
    hull    int32
    plain   uint32
    speed   float32
    drag    float64
    score   int32 | min = 0, max = 1000
    channel bits(12)
    angle   fixed(16, 16) | min = -180, max = 180
    heading ufixed(16, 16) | min = 0, max = 360
    name    string(32)
    label   wstring(16)
    blob    bytes(64)
    slots   [..Slots]int32
    cells   [4]int32
    boost   Buff
    gunner  ?Buff
}
`

// variableSrc is the SAME table on the variable wire: the law's gate is the
// `fixed` keyword, and this fixture is what proves the gate is not a no-op.
var variableSrc = strings.Replace(monotoneSrc, "fixed table Ship", "table Ship", 1)

func monotoneUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Fixture.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Fixture.schema", Name: "Fixture.schema", Base: "Fixture", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// monotoneCommitted is the OLD side: rendered, written as the file writes it,
// and parsed back, so every case runs against a committed baseline's own facts.
func monotoneCommitted(t *testing.T, src string) *Unit {
	t.Helper()
	text := Render(monotoneUnit(t, src)).Text()
	b, err := Parse("tables.baseline", []byte(text))
	if err != nil {
		t.Fatalf("a rendered baseline does not parse: %v\n%s", err, text)
	}
	return b
}

// monotoneEdit is the fixture edit: exactly one substitution, or the case is
// lying about what it changed.
func monotoneEdit(t *testing.T, src, old, new string) string {
	t.Helper()
	if strings.Count(src, old) != 1 {
		t.Fatalf("fixture edit %q does not appear exactly once in the fixture", old)
	}
	return strings.Replace(src, old, new, 1)
}

// law runs the monotone law over one edit of one fixture.
func law(t *testing.T, base, edited string) []string {
	t.Helper()
	return monotoneNumbers(monotoneCommitted(t, base), Render(monotoneUnit(t, edited)))
}

// TestMonotoneNumbersRefused is one case per FORBIDDEN change: the law refuses,
// and the line names the definition, the old value, the new value and the rule.
func TestMonotoneNumbersRefused(t *testing.T) {
	cases := []struct {
		// name is GLENN'S SENTENCE for the rule, then the edit it holds
		name  string
		old   string
		new   string
		field string // "Ship: slots"
		rule  string // a fragment of the rule the line must state
		move  string // "(8 -> 4)"
	}{
		{
			name:  "same with arrays and strings etc. — a bounded array's bound shrunk THROUGH A CONSTANT (the baseline records the EVALUATED value)",
			old:   "const Slots = 8",
			new:   "const Slots = 4",
			field: "Ship: slots",
			rule:  "an array bound only grows",
			move:  "(8 -> 4)",
		},
		{
			name:  "same with arrays and strings etc. — a bounded array's bound shrunk",
			old:   "slots   [..Slots]int32",
			new:   "slots   [..2]int32",
			field: "Ship: slots",
			rule:  "an array bound only grows",
			move:  "(8 -> 2)",
		},
		{
			name:  "same with arrays and strings etc. — a FIXED-SIZE array's N shrunk",
			old:   "cells   [4]int32",
			new:   "cells   [2]int32",
			field: "Ship: cells",
			rule:  "an array bound only grows",
			move:  "(4 -> 2)",
		},
		{
			name:  "wstring/strings/bytes can be widened only, not narrowed — string(N) shrunk",
			old:   "name    string(32)",
			new:   "name    string(16)",
			field: "Ship: name",
			rule:  "capacity only grows",
			move:  "(32 -> 16)",
		},
		{
			name:  "wstring/strings/bytes can be widened only, not narrowed — wstring(N) shrunk",
			old:   "label   wstring(16)",
			new:   "label   wstring(8)",
			field: "Ship: label",
			rule:  "capacity only grows",
			move:  "(16 -> 8)",
		},
		{
			name:  "wstring/strings/bytes can be widened only, not narrowed — bytes(N) shrunk",
			old:   "blob    bytes(64)",
			new:   "blob    bytes(32)",
			field: "Ship: blob",
			rule:  "capacity only grows",
			move:  "(64 -> 32)",
		},
		{
			name:  "a narrowed string/array cannot read the old — a ranged scalar's MAX moved inward",
			old:   "score   int32 | min = 0, max = 1000",
			new:   "score   int32 | min = 0, max = 500",
			field: "Ship: score",
			rule:  "a declared maximum only moves outward",
			move:  "(1000 -> 500)",
		},
		{
			name:  "a narrowed string/array cannot read the old — a ranged scalar's MIN moved inward",
			old:   "score   int32 | min = 0, max = 1000",
			new:   "score   int32 = 10 | min = 10, max = 1000",
			field: "Ship: score",
			rule:  "a declared minimum only moves outward",
			move:  "(0 -> 10)",
		},
		{
			name:  "you cannot narrow, or incompatible — an integer NARROWED",
			old:   "hull    int32",
			new:   "hull    int16",
			field: "Ship: hull",
			rule:  "only widens: a narrower int cannot hold every value",
			move:  "(4 -> 3)",
		},
		{
			name:  "you cannot narrow, or incompatible — a float NARROWED",
			old:   "drag    float64",
			new:   "drag    float32",
			field: "Ship: drag",
			rule:  "only widens: a narrower float cannot hold every value",
			move:  "(11 -> 10)",
		},
		{
			name:  "you cannot narrow, or incompatible — SIGNEDNESS changed (int32 to uint32)",
			old:   "hull    int32",
			new:   "hull    uint32",
			field: "Ship: hull",
			rule:  "the int ladder and the uint ladder are different kinds",
			move:  "(4 -> 8)",
		},
		{
			name:  "you cannot narrow, or incompatible — A DIFFERENT LADDER (int32 to float32)",
			old:   "hull    int32",
			new:   "hull    float32",
			field: "Ship: hull",
			rule:  "the int ladder and the float ladder are different kinds",
			move:  "(4 -> 10)",
		},
		{
			name:  "you cannot narrow, or incompatible — bits(N) shrunk",
			old:   "channel bits(12)",
			new:   "channel bits(10)",
			field: "Ship: channel",
			rule:  "a bits(N) width only grows",
			move:  "(12 -> 10)",
		},
		{
			name:  "you cannot narrow, or incompatible — an ARRAY's ELEMENT narrowed",
			old:   "slots   [..Slots]int32",
			new:   "slots   [..Slots]int16",
			field: "Ship: slots",
			rule:  "an array's element kind only widens",
			move:  "(4 -> 3)",
		},
		{
			name:  "a field's kind changed — a scalar respelled as a string",
			old:   "plain   uint32",
			new:   "plain   string(8)",
			field: "Ship: plain",
			rule:  "a field's kind is fixed",
			move:  "(8 -> 12)",
		},
		{
			name:  "a field's kind changed — a scalar respelled as a NESTED TYPE",
			old:   "plain   uint32",
			new:   "plain   Buff",
			field: "Ship: plain",
			rule:  "a field's kind is fixed",
			move:  "(8 -> 13)",
		},
		{
			name:  "?T to T is refused — an optional dropped",
			old:   "gunner  ?Buff",
			new:   "gunner  Buff",
			field: "Ship: gunner",
			rule:  "an optional only appears",
			move:  "(?T -> T)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lines := law(t, monotoneSrc, monotoneEdit(t, monotoneSrc, c.old, c.new))
			if len(lines) != 1 {
				t.Fatalf("the law states one refusal for one edit; it stated %d:\n  %s", len(lines), strings.Join(lines, "\n  "))
			}
			line := lines[0]
			for _, want := range []string{"fixed " + c.field, c.rule, c.move} {
				if !strings.Contains(line, want) {
					t.Errorf("the refusal must name %q:\n  %s", want, line)
				}
			}
			// THE GATE: the very same edit on the VARIABLE wire is no business
			// of this law — it keeps the §18 verdict it always had.
			if loose := law(t, variableSrc, monotoneEdit(t, variableSrc, c.old, c.new)); len(loose) != 0 {
				t.Errorf("the law is the FIXED form's; a plain `table` drew:\n  %s", strings.Join(loose, "\n  "))
			}
		})
	}
}

// TestMonotoneNumbersAllowed is one case per ALLOWED WIDENING: the law is
// silent, because a newer reader can hold every value an older writer wrote.
//
// Silence HERE is the law's silence, not the whole baseline's: §18's own `kind`
// row still refuses a kind change for the save-game reason, and loosening that
// row for a fixed closure belongs to the bill's §8 backport (see this file's
// package comment).
func TestMonotoneNumbersAllowed(t *testing.T) {
	cases := []struct {
		name string
		old  string
		new  string
	}{
		{
			name: "you can take an enum and widen it — a bounded array's bound GROWN through a constant",
			old:  "const Slots = 8",
			new:  "const Slots = 32",
		},
		{
			name: "same with arrays and strings etc. — a FIXED-SIZE array's N grown",
			old:  "cells   [4]int32",
			new:  "cells   [8]int32",
		},
		{
			name: "wstring/strings/bytes can be widened only — string(N) grown",
			old:  "name    string(32)",
			new:  "name    string(64)",
		},
		{
			name: "wstring/strings/bytes can be widened only — wstring(N) grown",
			old:  "label   wstring(16)",
			new:  "label   wstring(64)",
		},
		{
			name: "wstring/strings/bytes can be widened only — bytes(N) grown",
			old:  "blob    bytes(64)",
			new:  "blob    bytes(128)",
		},
		{
			name: "you can take an enum and widen it — a ranged scalar's ends moved OUTWARD",
			old:  "score   int32 | min = 0, max = 1000",
			new:  "score   int32 | min = -50, max = 5000",
		},
		{
			name: "you can take an enum and widen it — a declared range REMOVED, the largest widening there is",
			old:  "score   int32 | min = 0, max = 1000",
			new:  "score   int32",
		},
		{
			name: "you can take an enum and widen it — int32 to int64",
			old:  "hull    int32",
			new:  "hull    int64",
		},
		{
			name: "you can take an enum and widen it — float32 to float64",
			old:  "speed   float32",
			new:  "speed   float64",
		},
		{
			name: "you can take an enum and widen it — bits(N) grown",
			old:  "channel bits(12)",
			new:  "channel bits(16)",
		},
		{
			name: "you can take an enum and widen it — fixed(I,F)'s I widened, F untouched",
			old:  "angle   fixed(16, 16) | min = -180, max = 180",
			new:  "angle   fixed(48, 16) | min = -180, max = 180",
		},
		{
			name: "T to ?T is allowed — an optional added",
			old:  "boost   Buff",
			new:  "boost   ?Buff",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if lines := law(t, monotoneSrc, monotoneEdit(t, monotoneSrc, c.old, c.new)); len(lines) != 0 {
				t.Errorf("a widening the bill allows drew a refusal:\n  %s", strings.Join(lines, "\n  "))
			}
		})
	}
}

// TestMonotoneNumbersLandsInTheRefusalPath is WHERE the law lands: [Diff]'s
// refusals, which are what [Check] fails a compile with. The same edit on the
// variable wire stays the WARNING §18 has always given it — the law adds
// refusals to the fixed form and takes nothing away from anyone else.
func TestMonotoneNumbersLandsInTheRefusalPath(t *testing.T) {
	shrink := func(src string) []Finding {
		return Diff(monotoneCommitted(t, src), Render(monotoneUnit(t, monotoneEdit(t, src, "name    string(32)", "name    string(16)"))), DefaultTokenPolicy)
	}

	refusals, warnings := Split(shrink(monotoneSrc))
	var found bool
	for _, r := range refusals {
		if strings.Contains(r.String(), "fixed Ship: name: a string/wstring/bytes capacity only grows") && strings.Contains(r.What, "(32 -> 16)") {
			found = true
		}
	}
	if !found {
		t.Errorf("a fixed table's shrunk capacity must REFUSE, by name:%s", monotoneSummary(append(refusals, warnings...)))
	}

	refusals, warnings = Split(shrink(variableSrc))
	if len(refusals) != 0 {
		t.Errorf("the variable wire keeps its own verdicts; it refused:%s", monotoneSummary(refusals))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].What, "capacity 32 -> 16") {
		t.Errorf("the variable wire's shrunk capacity stays §18's one warning:%s", monotoneSummary(warnings))
	}
}

// TestMonotoneFixedScaleAndNestedBodyAlreadyRefuse are the two rows of the
// bill's §6 table a check ALREADY held, asserted over a FIXED closure so the
// law's coverage is not claimed twice and not left assumed:
//
//   - `fixed(I,F)`'s F CHANGED — §4.1's one wire-invisible fact on a wide kind,
//     refused by the `frac` row. The scale moves, every stored raw value reads
//     as a different number, and no counter can fire.
//   - a NESTED TYPE swapped for another — refused by the referent rule, which
//     asks whether the replacement can stand in for data already written.
func TestMonotoneFixedScaleAndNestedBodyAlreadyRefuse(t *testing.T) {
	cases := []struct {
		name  string
		old   string
		new   string
		where string
		what  string
	}{
		{
			name:  "you cannot narrow, or incompatible — fixed(I,F)'s F changed (the storage width held still)",
			old:   "angle   fixed(16, 16) | min = -180, max = 180",
			new:   "angle   fixed(24, 8) | min = -180, max = 180",
			where: "Ship.angle",
			what:  "fractional bits 16 -> 8",
		},
		{
			name:  "a field's kind changed — a nested type swapped for another",
			old:   "boost   Buff",
			new:   "boost   Debuff",
			where: "Ship.boost",
			what:  "nested table Buff -> Debuff",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			edited := monotoneEdit(t, monotoneSrc, c.old, c.new)
			refusals, _ := Split(Diff(monotoneCommitted(t, monotoneSrc), Render(monotoneUnit(t, edited)), DefaultTokenPolicy))
			var found bool
			for _, r := range refusals {
				if strings.Contains(r.Where, c.where) && strings.Contains(r.What, c.what) {
					found = true
				}
			}
			if !found {
				t.Errorf("a fixed closure must refuse %s: %s:%s", c.where, c.what, monotoneSummary(refusals))
			}
		})
	}
}

func monotoneSummary(fs []Finding) string {
	if len(fs) == 0 {
		return " (no findings)"
	}
	var b strings.Builder
	for _, f := range fs {
		b.WriteString("\n  [" + f.Verdict.String() + "] " + f.String())
	}
	return b.String()
}
