// THE MONOTONE LAW, LISTS (docs/FIXED-FORM-BILL-READS-BACKWARD.md §6), one
// test per forbidden change and one per change the law allows. THE TEST NAMES
// ARE THE OWNER'S WORDS, verbatim, because the words are the law:
//
//	"types or tables referenced in a fixed table can only have new fields
//	 added at end, not existing entries modified or removed (they can be
//	 deprecated sure)"
//	"enums can have entries added at end, no shuffling of entries for meaning,
//	 new entries at end"
//	"Flags can only ever have new flags added. Old flags cannot be removed"
//	"fixed tables must only allow other fixed tables to be included in them,
//	 and not recursively include themselves"
//	— Glenn Fiedler, 2026-09-10
//
// Every refusal is asserted two ways: that it FIRES, and that it NAMES the
// definition and the rule — a refusal nobody can act on is a refusal that
// teaches nothing (docs/SPEC-TABLES.md §18.2).
package baseline_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

// the fixture: one FIXED table whose closure holds a nested `type`, an enum, a
// flags mask and a union, and one VARIABLE table beside it, so every case can
// be spelled on the fixed half where it is refused.
const monotoneSrc = `package monotone

enum Hull { Interceptor, Gunship, Freighter }

flags Perks { Shielded, Cloaked }

type Vec
{
    x float32
    y float32
    z float32
}

type Buff
{
    multiplier float32 = 1.0
}

type Debuff
{
    penalty float32 = 1.0
}

type Boost
{
    amount float32 = 1.0
}

union Effect
{
    buff   Buff
    debuff Debuff
}

fixed table Vessel
{
    hull   Hull = Gunship
    perks  Perks
    effect Effect
    offset Vec
    speed  float32 = 1.0
}

table Fleet
{
    ships []Vessel
}
`

// monotone judges one edit of the fixture under the shipping policy.
func monotone(t *testing.T, old, new string) []baseline.Finding {
	t.Helper()
	return baseline.Diff(committed(t, old), baseline.Render(unit(t, new)), baseline.DefaultTokenPolicy)
}

// refuses asserts the refusal fires and names both the definition and the rule.
func refuses(t *testing.T, fs []baseline.Finding, where, rule string) {
	t.Helper()
	if !find(fs, baseline.Refuse, where, rule) {
		t.Errorf("the baseline must refuse, naming %s and %q, got:%s", where, rule, summary(fs))
	}
	// AND IT CITES THE LAW. A refusal a person cannot look up is a refusal
	// that teaches nothing, so every line this file asserts carries the bill.
	if !find(fs, baseline.Refuse, where, "FIXED-FORM-BILL-READS-BACKWARD.md") && !find(fs, baseline.Refuse, where, "SPEC-TABLES.md") {
		t.Errorf("the refusal cites no law:%s", summary(fs))
	}
}

// allows asserts the edit draws no refusal at all. A WARNING is left alone:
// the variable-table rules are unchanged by this law, and they are what warn.
func allows(t *testing.T, fs []baseline.Finding) {
	t.Helper()
	if refusals, _ := baseline.Split(fs); len(refusals) != 0 {
		t.Errorf("the law allows this edit and the baseline refused it:%s", summary(fs))
	}
}

// ---- "not existing entries modified or removed (they can be deprecated sure)" ----

// TestAFieldRemovedFromAFixedTablesClosureIsRefused: a fixed record is walked
// by OFFSET, so every field after a removed one slides and a reader holding an
// older record reads one field as the next.
func TestAFieldRemovedFromAFixedTablesClosureIsRefused(t *testing.T) {
	edited := editOf(t, monotoneSrc, "    y float32\n    z float32\n", "    y float32\n")
	fs := monotone(t, monotoneSrc, edited)
	refuses(t, fs, "fixed Vessel", "gone from the declaration")
	refuses(t, fs, "fixed Vessel", "DEPRECATED IN PLACE, never removed")
	if !find(fs, baseline.Refuse, "fixed Vessel", "Vec.z") {
		t.Errorf("the refusal must name the field that is gone:%s", summary(fs))
	}
	// the field removed from the FIXED TABLE itself, not only from a `type`
	// it reaches
	dropped := editOf(t, monotoneSrc, "    offset Vec\n    speed  float32 = 1.0\n", "    offset Vec\n")
	refuses(t, monotone(t, monotoneSrc, dropped), "fixed Vessel", "Vessel.speed")
}

// TestAFieldInsertedNotAtTheEndIsRefused: "a field is added at the BOTTOM, and
// nowhere else."
func TestAFieldInsertedNotAtTheEndIsRefused(t *testing.T) {
	edited := editOf(t, monotoneSrc, "    x float32\n    y float32", "    x float32\n    w float32\n    y float32")
	fs := monotone(t, monotoneSrc, edited)
	refuses(t, fs, "fixed Vessel", "entry 2 is field w")
	refuses(t, fs, "fixed Vessel", "never inserted, moved or removed")
}

// TestFieldsReorderedAreRefused: the ORDER is the contract, and a swap moves
// no byte in the file while moving every value in the record.
func TestFieldsReorderedAreRefused(t *testing.T) {
	edited := editOf(t, monotoneSrc, "    x float32\n    y float32", "    y float32\n    x float32")
	fs := monotone(t, monotoneSrc, edited)
	refuses(t, fs, "fixed Vessel", "Vec.x")
	refuses(t, fs, "fixed Vessel", "a reader at this offset would read one field as the other")
}

// TestAFieldsKindModifiedIsRefused is the one representative MODIFY case this
// file holds — the field's KIND. A width, a bound, a size, a range and a
// `frac` are the numbers half of §6 and live next door.
func TestAFieldsKindModifiedIsRefused(t *testing.T) {
	edited := editOf(t, monotoneSrc, "    x float32\n", "    x int32\n")
	fs := monotone(t, monotoneSrc, edited)
	refuses(t, fs, "fixed Vessel", "Vec.x")
	refuses(t, fs, "fixed Vessel", "keeps its type")
}

// TestAFieldDeprecatedInPlaceIsAllowed is "they can be deprecated sure": the
// slot stays exactly where it is, every wire fact on it is unchanged, and the
// marker is a statement about MEANING (docs/SPEC-TABLES.md §2.10).
func TestAFieldDeprecatedInPlaceIsAllowed(t *testing.T) {
	edited := editOf(t, monotoneSrc, "    z float32\n", "    z float32 | deprecated\n")
	allows(t, monotone(t, monotoneSrc, edited))
}

// TestAFieldAppendedAtTheEndIsAllowed is the owner's own example, both
// directions: "vec {x,y,z}" to "vec {x,y,z,w}" passes, and to "vec {x,y}"
// refuses.
func TestAFieldAppendedAtTheEndIsAllowed(t *testing.T) {
	grown := editOf(t, monotoneSrc, "    z float32\n", "    z float32\n    w float32\n")
	allows(t, monotone(t, monotoneSrc, grown))
	narrowed := editOf(t, monotoneSrc, "    y float32\n    z float32\n", "    y float32\n")
	if refusals, _ := baseline.Split(monotone(t, monotoneSrc, narrowed)); len(refusals) == 0 {
		t.Errorf("vec {x,y,z} narrowed to vec {x,y} must refuse")
	}
	// and the same append on the FIXED TABLE's own bottom
	appended := editOf(t, monotoneSrc, "    speed  float32 = 1.0\n", "    speed  float32 = 1.0\n    armor  int32 = 0\n")
	allows(t, monotone(t, monotoneSrc, appended))
}

// ---- "enums can have entries added at end, no shuffling of entries for meaning" ----

// TestAnEnumVariantRemovedIsRefused: a fixed record stores the PLACE, not the
// name, so a dropped variant is every later variant's place moving.
func TestAnEnumVariantRemovedIsRefused(t *testing.T) {
	edited := editOf(t, monotoneSrc, "enum Hull { Interceptor, Gunship, Freighter }", "enum Hull { Interceptor, Gunship }")
	fs := monotone(t, monotoneSrc, edited)
	refuses(t, fs, "fixed Vessel", "enum Hull")
	refuses(t, fs, "fixed Vessel", "variant 3, Freighter, is in the baseline and gone")
}

// TestAnEnumVariantInsertedMidListIsRefused: "new entries at end."
func TestAnEnumVariantInsertedMidListIsRefused(t *testing.T) {
	edited := editOf(t, monotoneSrc, "enum Hull { Interceptor, Gunship, Freighter }", "enum Hull { Interceptor, Scout, Gunship, Freighter }")
	fs := monotone(t, monotoneSrc, edited)
	refuses(t, fs, "fixed Vessel", "variant 2, Gunship, is Scout in the declaration")
	refuses(t, fs, "fixed Vessel", "a new entry goes at the END")
}

// TestEnumVariantsReorderedAreRefused: "no shuffling of entries for meaning."
func TestEnumVariantsReorderedAreRefused(t *testing.T) {
	edited := editOf(t, monotoneSrc, "enum Hull { Interceptor, Gunship, Freighter }", "enum Hull { Gunship, Interceptor, Freighter }")
	refuses(t, monotone(t, monotoneSrc, edited), "fixed Vessel", "variant 1, Interceptor, is Gunship in the declaration")
}

// TestAnEnumVariantRenamedWithoutWasIsRefused: a variant rides under the hash
// of its wire name, so a rename with no `was` puts a name no stored value was
// ever written under at that place.
func TestAnEnumVariantRenamedWithoutWasIsRefused(t *testing.T) {
	edited := editOf(t, monotoneSrc, "enum Hull { Interceptor, Gunship, Freighter }", "enum Hull { Interceptor, Gunship, Hauler }")
	refuses(t, monotone(t, monotoneSrc, edited), "fixed Vessel", "variant 3, Freighter, is Hauler in the declaration")
}

// TestAnEnumVariantAppendedIsAllowed, and the note the other half of Glenn's
// sentence needs: THIS LANGUAGE HAS NO PER-VARIANT DEPRECATION MARKER —
// `| deprecated` is a FIELD tag (docs/SPEC-TABLES.md §2.10) — so the allowed
// edits to an enum in a fixed closure are exactly one: append at the end.
func TestAnEnumVariantAppendedIsAllowed(t *testing.T) {
	edited := editOf(t, monotoneSrc, "enum Hull { Interceptor, Gunship, Freighter }", "enum Hull { Interceptor, Gunship, Freighter, Hauler }")
	allows(t, monotone(t, monotoneSrc, edited))
}

// ---- a union's arms: the same five ----

func TestAUnionArmRemovedIsRefused(t *testing.T) {
	edited := editOf(t, monotoneSrc, "    buff   Buff\n    debuff Debuff\n", "    buff   Buff\n")
	fs := monotone(t, monotoneSrc, edited)
	refuses(t, fs, "fixed Vessel", "union Effect")
	refuses(t, fs, "fixed Vessel", "arm 2, debuff, is in the baseline and gone")
}

func TestAUnionArmInsertedNotAtTheEndIsRefused(t *testing.T) {
	edited := editOf(t, monotoneSrc, "    buff   Buff\n    debuff Debuff\n", "    buff   Buff\n    boost  Boost\n    debuff Debuff\n")
	refuses(t, monotone(t, monotoneSrc, edited), "fixed Vessel", "arm 2, debuff, is boost in the declaration")
}

func TestUnionArmsReorderedAreRefused(t *testing.T) {
	edited := editOf(t, monotoneSrc, "    buff   Buff\n    debuff Debuff\n", "    debuff Debuff\n    buff   Buff\n")
	refuses(t, monotone(t, monotoneSrc, edited), "fixed Vessel", "arm 1, buff, is debuff in the declaration")
}

func TestAUnionArmRenamedWithoutWasIsRefused(t *testing.T) {
	edited := editOf(t, monotoneSrc, "    debuff Debuff\n", "    curse  Debuff\n")
	refuses(t, monotone(t, monotoneSrc, edited), "fixed Vessel", "arm 2, debuff, is curse in the declaration")
}

func TestAUnionArmAppendedIsAllowed(t *testing.T) {
	edited := editOf(t, monotoneSrc, "    debuff Debuff\n", "    debuff Debuff\n    boost  Boost\n")
	allows(t, monotone(t, monotoneSrc, edited))
}

// ---- "Flags can only ever have new flags added. Old flags cannot be removed" ----

// TestAFlagRemovedIsRefused and TestAFlagMovedIsRefused hold Glenn's sentence
// where the law already lives: bit i is variant i on EVERY wire, so this
// package's `flags-position` rule refuses a removed or moved bit for a fixed
// and a variable table alike (docs/SPEC-TABLES.md §18.2), and the monotone law
// of the fixed form adds nothing to it rather than refusing it twice.
func TestAFlagRemovedIsRefused(t *testing.T) {
	edited := editOf(t, monotoneSrc, "flags Perks { Shielded, Cloaked }", "flags Perks { Shielded }")
	fs := monotone(t, monotoneSrc, edited)
	if !find(fs, baseline.Refuse, "flags Perks", "variant Cloaked removed from bit 1") {
		t.Errorf("an old flag removed must be refused:%s", summary(fs))
	}
	if !find(fs, baseline.Refuse, "flags Perks", "a spent bit stays spent") {
		t.Errorf("the refusal must name the rule:%s", summary(fs))
	}
}

func TestAFlagMovedIsRefused(t *testing.T) {
	edited := editOf(t, monotoneSrc, "flags Perks { Shielded, Cloaked }", "flags Perks { Cloaked, Shielded }")
	fs := monotone(t, monotoneSrc, edited)
	if !find(fs, baseline.Refuse, "flags Perks", "moved from bit") {
		t.Errorf("a flag moved must be refused:%s", summary(fs))
	}
}

func TestAFlagAppendedIsAllowed(t *testing.T) {
	edited := editOf(t, monotoneSrc, "flags Perks { Shielded, Cloaked }", "flags Perks { Shielded, Cloaked, Stealthed }")
	allows(t, monotone(t, monotoneSrc, edited))
}

// ---- the `fixed` keyword: a different FORM, not a version ----

// TestTheFixedKeywordDroppedIsRefused: a fixed table's layout is a promise to
// every record already written, and the variable wire is not a later version
// of it — no reader of one reads the other (bill §2).
func TestTheFixedKeywordDroppedIsRefused(t *testing.T) {
	edited := editOf(t, monotoneSrc, "fixed table Vessel", "table Vessel")
	fs := monotone(t, monotoneSrc, edited)
	refuses(t, fs, "fixed Vessel", "the `fixed` keyword: dropped")
	refuses(t, fs, "fixed Vessel", "A DIFFERENT FORM, NOT A VERSION")
}

// TestTheFixedKeywordAddedToALockedVariableTableIsRefused is the other
// direction: every record already written rode the id-table wire, and a reader
// at an offset would read an id for a value.
func TestTheFixedKeywordAddedToALockedVariableTableIsRefused(t *testing.T) {
	// Fleet holds an unbounded array, which cannot be fixed at all; the
	// variable table this case needs is one whose body a fixed table could
	// hold, so the baseline's refusal is about the KEYWORD and nothing else.
	src := editOf(t, monotoneSrc, "table Fleet\n{\n    ships []Vessel\n}", "table Fleet\n{\n    ships [..4]Vessel\n}")
	edited := editOf(t, src, "table Fleet", "fixed table Fleet")
	fs := monotone(t, src, edited)
	refuses(t, fs, "fixed Fleet", "the `fixed` keyword: added")
	refuses(t, fs, "fixed Fleet", "A DIFFERENT FORM, NOT A VERSION")
}

// TestTheProjectionRecordsTheFixedKeyword is the format addition itself: the
// token rides at the END of the table line, so every baseline written before
// it still parses and an untouched unit regenerates with one token added and
// no rendering version moved (docs/SPEC-TABLES.md §18.1).
func TestTheProjectionRecordsTheFixedKeyword(t *testing.T) {
	text := baseline.Render(unit(t, monotoneSrc)).Text()
	if !strings.Contains(text, "table Vessel fixed=true\n") {
		t.Errorf("the fixed table's line must carry fixed=true:\n%s", text)
	}
	if !strings.Contains(text, "table Fleet\n") {
		t.Errorf("a variable table's line carries no fixed token:\n%s", text)
	}
	// a baseline written before the token existed still parses, and says
	// nothing about a unit that has not moved
	older := strings.ReplaceAll(text, " fixed=true", "")
	parsed, err := baseline.Parse("tables.baseline", []byte(older))
	if err != nil {
		t.Fatalf("a baseline written before the token must still parse: %v", err)
	}
	if fs := baseline.Diff(parsed, baseline.Render(unit(t, monotoneSrc)), baseline.DefaultTokenPolicy); len(fs) != 0 {
		t.Errorf("a baseline from before the token must greet an untouched schema in silence:%s", summary(fs))
	}
	// and the token round-trips
	back, err := baseline.Parse("tables.baseline", []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if back.Text() != text {
		t.Errorf("the table line does not round-trip:\n--- got ---\n%s\n--- want ---\n%s", back.Text(), text)
	}
}

// TestAVariableOnlyUnitKeepsItsOwnRules is the law's SCOPE (the bill's §6 is
// the fixed form's): the same edits on a unit with no fixed table in it are
// judged exactly as they were — the id-table wire finds a field by its id, so
// a removal is absorbed and a reorder moves no byte.
func TestAVariableOnlyUnitKeepsItsOwnRules(t *testing.T) {
	const variableSrc = `package variableonly

type Vec
{
    x float32
    y float32
    z float32
}

table Fleet
{
    offset Vec
    speed  float32 = 1.0
}
`
	for _, tc := range []struct{ name, old, new string }{
		{"a field removed", "    y float32\n    z float32\n", "    y float32\n"},
		{"fields reordered", "    x float32\n    y float32", "    y float32\n    x float32"},
		{"a field inserted", "    x float32\n    y float32", "    x float32\n    w float32\n    y float32"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			edited := editOf(t, variableSrc, tc.old, tc.new)
			if refusals, _ := baseline.Split(monotone(t, variableSrc, edited)); len(refusals) != 0 {
				t.Errorf("a variable-only unit keeps the rules it had:%s", summary(monotone(t, variableSrc, edited)))
			}
		})
	}
}

// TestOneLinePerBreakNotOnePerFixedTable is the output's BOUND. A `type`, an
// enum or a union is commonly in the closure of many fixed tables — twenty of
// them in one unit is ordinary — and one edit inside it is ONE edit to fix. The
// count of refusals is the count of definitions that moved, never the size of
// the unit.
func TestOneLinePerBreakNotOnePerFixedTable(t *testing.T) {
	const sharedSrc = `package shared

enum Hull { Interceptor, Gunship, Freighter }

type Vec
{
    x float32
    y float32
    z float32
}

fixed table One
{
    offset Vec
    hull   Hull = Gunship
}

fixed table Two
{
    offset Vec
    hull   Hull = Gunship
}

fixed table Three
{
    offset Vec
    hull   Hull = Gunship
}
`
	edited := editOf(t, sharedSrc, "    y float32\n    z float32\n", "    y float32\n")
	refusals, _ := baseline.Split(monotone(t, sharedSrc, edited))
	if len(refusals) != 1 {
		t.Errorf("one field removed from a shared `type` is one refusal, got %d:%s", len(refusals), summary(refusals))
	}
	both := editOf(t, edited, "enum Hull { Interceptor, Gunship, Freighter }", "enum Hull { Interceptor, Gunship }")
	refusals, _ = baseline.Split(monotone(t, sharedSrc, both))
	if len(refusals) != 2 {
		t.Errorf("two definitions moved is two refusals, got %d:%s", len(refusals), summary(refusals))
	}
}
