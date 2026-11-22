package baseline_test

// EVERY DIAGNOSTIC THAT SAYS WHAT A READER DOES is held to a real read here.
// Each case edits a schema, takes the finding the check gives for the edit,
// writes a save under the OLD schema, reads it under the NEW one, and holds
// the finding's sentence to what that read did: the refusal reason it names is
// the one the read gave, the counter it names is the one that moved, the value
// it promises is the value that arrived. The extents (a range, a bound, a
// capacity, a total) are held the same way by
// TestTighteningWarningsDescribeTheStrictRead and
// TestEveryReaderConstraintMovesTheProjection, and the minimum count by
// TestCountMinimumPredictionsAgreeWithTheRead.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// a prediction is one diagnostic and the read that holds it.
type prediction struct {
	name, before, after string
	save                string // the old save, as JSON
	verdict             baseline.Verdict
	where               string
	says                []string // every fragment the finding must carry
	neverSays           []string // a recovery or a silence the reader does not make
	// what the read does
	reason  string // the refusal reason, "" when the save reads
	unknown int    // the `unknown` counter after a save that reads
	text    string // what the new schema holds after the read, as JSON; "" leaves it unchecked
}

func (p prediction) run(t *testing.T) {
	t.Helper()
	got := baseline.Diff(committed(t, p.before), baseline.Render(unit(t, p.after)), baseline.DefaultTokenPolicy)
	var said string
	for _, f := range got {
		if f.Verdict == p.verdict && strings.Contains(f.Where, p.where) && strings.Contains(f.What, p.says[0]) {
			said = f.What
		}
	}
	if said == "" {
		t.Fatalf("no %s finding at %s saying %q, got:%s", p.verdict, p.where, p.says[0], summary(got))
	}
	for _, want := range p.says {
		if !strings.Contains(said, want) {
			t.Errorf("the finding does not say %q:\n  %s", want, said)
		}
	}
	for _, never := range p.neverSays {
		if strings.Contains(said, never) {
			t.Errorf("the finding says %q, which the reader does not do:\n  %s", never, said)
		}
	}

	r := readOldSave(t, p.before, p.after, "Save", p.save)
	if p.reason != "" {
		if r.OK || r.Reason != p.reason {
			t.Fatalf("the old save must refuse %s under the new schema, got ok=%t reason=%q text=%s", p.reason, r.OK, r.Reason, r.Text)
		}
		if !strings.Contains(said, p.reason) {
			t.Errorf("the finding does not name the refusal the read gave (%s):\n  %s", p.reason, said)
		}
		return
	}
	if !r.OK {
		t.Fatalf("the old save must read under the new schema, and it refused %s", r.Reason)
	}
	for _, refusal := range []string{"no longer reads", "refuses"} {
		if strings.Contains(said, refusal) {
			t.Errorf("the finding promises a refusal (%q) and the save read:\n  %s", refusal, said)
		}
	}
	if r.Report.Unknown != p.unknown {
		t.Errorf("the read counted unknown=%d, want %d", r.Report.Unknown, p.unknown)
	}
	if counts := strings.Contains(said, "count unknown") || strings.Contains(said, "counts unknown"); counts && r.Report.Unknown == 0 {
		t.Errorf("the finding says unknown is counted and the read counted nothing:\n  %s", said)
	}
	if p.text != "" && r.Text != compact(p.text) {
		t.Errorf("the new schema holds %s after the read, want %s", r.Text, p.text)
	}
}

const pairArraySrc = "package fixture\n\ntable Pair\n{\n    key uint16\n    value int32\n}\n\ntable Save\n{\n    items [..4]Pair\n}\n"
const pairAsMapSrc = "package fixture\n\ntable Save\n{\n    items map[uint16]int32\n}\n"

// TestMapShapePredictionsAgreeWithTheRead: a field moved from the `[..N]Pair`
// its entries already are to a map gains the order check, and the read is
// strict: an old save whose pairs do not ascend strictly by key — out of
// order, or a key repeated — refuses the WHOLE load. It keeps no ascending
// prefix and merges nothing, and a save whose pairs ascend reads as the map.
// In the other direction every save a map wrote reads whole as pairs.
func TestMapShapePredictionsAgreeWithTheRead(t *testing.T) {
	toMap := prediction{
		before: pairArraySrc, after: pairAsMapSrc, verdict: baseline.Warn, where: "Save.items",
		says:      []string{"array shape bounded -> map", "do not ascend strictly by key", "out of order", "key repeated", "no longer reads"},
		neverSays: []string{"prefix", "reads short", "merge"},
		reason:    tablewire.ReasonWireDamaged,
	}
	for name, save := range map[string]string{
		"pairs out of order":   `{"items":[{"key":2,"value":22},{"key":1,"value":11}]}`,
		"a key repeated":       `{"items":[{"key":1,"value":11},{"key":1,"value":22}]}`,
		"a repeat after order": `{"items":[{"key":1,"value":11},{"key":2,"value":22},{"key":2,"value":33}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			p := toMap
			p.save = save
			p.run(t)
			// the control: the old schema still reads its own save whole
			if r := readOldSave(t, pairArraySrc, pairArraySrc, "Save", save); !r.OK || r.Text != compact(save) {
				t.Errorf("the old schema must read its own save whole, got ok=%t text=%s", r.OK, r.Text)
			}
		})
	}
	t.Run("pairs ascending read as the map", func(t *testing.T) {
		r := readOldSave(t, pairArraySrc, pairAsMapSrc, "Save", `{"items":[{"key":1,"value":11},{"key":2,"value":22}]}`)
		if !r.OK || r.Text != `{"items":{"1":11,"2":22}}` || r.Report.Unknown != 0 {
			t.Errorf("an ascending save must read whole as the map, got ok=%t reason=%q text=%s", r.OK, r.Reason, r.Text)
		}
	})
	t.Run("a map read as pairs", func(t *testing.T) {
		prediction{
			before: pairAsMapSrc, after: pairArraySrc, verdict: baseline.Warn, where: "Save.items",
			save:      `{"items":{"1":11,"2":22}}`,
			says:      []string{"array shape map -> bounded", "every file the map wrote reads as the pairs it holds", "LOSES the order check"},
			neverSays: []string{"merge", "arrive as ordinary"},
			text:      `{"items":[{"key":1,"value":11},{"key":2,"value":22}]}`,
		}.run(t)
	})
}

const rekeySrc = `package fixture

enum Team
{
    Red
    Blue
}

enum Axis
{
    East
    West
}

enum Side
{
    Red
    Green
}

enum Squad
{
    Blue
    Red
    Gold
}

table Save
{
    values [Team]uint8
    team   Team
    axis   Axis
    side   Side
    squad  Squad
}
`

// TestEnumRekeyPredictionAgreesWithTheRead: an enum-keyed array whose KEY enum
// is swapped for another of the same size. On the type wire `[Team]T`,
// `[Axis]T` and `[2]T` are one layout and one protocol id (SPEC §4.6), so
// nothing there moves and the positions keep their values under new names. A
// SAVE is the table wire, where a slot rides under its variant's NAME id
// (docs/SPEC-TABLES.md §3.2): the check refuses the rekey, and the sentence is
// held to the read — the stored slots whose names the new key enum does not
// declare are dropped and counted unknown, the load is not refused, and the
// new enum's unreached slots read their defaults.
func TestEnumRekeyPredictionAgreesWithTheRead(t *testing.T) {
	const save = `{"values":{"Red":11,"Blue":22}}`
	rekey := func(t *testing.T, to string) string {
		return editOf(t, rekeySrc, "values [Team]uint8", "values ["+to+"]uint8")
	}
	t.Run("no name rides", func(t *testing.T) {
		prediction{
			before: rekeySrc, after: rekey(t, "Axis"), save: save, verdict: baseline.Refuse, where: "Save.values",
			says:      []string{"array key enum Team -> Axis", "the key vocabulary changed", "2 of Team's 2 variant ids do not ride", "Axis declares no variant under them", "dropped and counts unknown", "read their defaults"},
			neverSays: []string{"read as None", "keep their values"},
			unknown:   2,
			text:      `{"values":{"East":0,"West":0},"team":"None","axis":"None","side":"None","squad":"None"}`,
		}.run(t)
	})
	t.Run("one name rides", func(t *testing.T) {
		prediction{
			before: rekeySrc, after: rekey(t, "Side"), save: save, verdict: baseline.Refuse, where: "Save.values",
			says:    []string{"array key enum Team -> Side", "1 of Team's 2 variant ids do not ride", "Side declares no variant under them", "dropped and counts unknown"},
			unknown: 1,
			text:    `{"values":{"Red":11,"Green":0},"team":"None","axis":"None","side":"None","squad":"None"}`,
		}.run(t)
	})
	t.Run("every name rides, so the rekey loses nothing and passes", func(t *testing.T) {
		after := rekey(t, "Squad")
		got := baseline.Diff(committed(t, rekeySrc), baseline.Render(unit(t, after)), baseline.DefaultTokenPolicy)
		if len(got) != 0 {
			t.Errorf("a key enum that declares every stored name stands in for the old one, got:%s", summary(got))
		}
		r := readOldSave(t, rekeySrc, after, "Save", save)
		if !r.OK || r.Report.Unknown != 0 || !strings.Contains(r.Text, `"values":{"Blue":22,"Red":11,"Gold":0}`) {
			t.Errorf("every stored slot must arrive under its own name, got ok=%t unknown=%d text=%s", r.OK, r.Report.Unknown, r.Text)
		}
	})
	t.Run("attribution", func(t *testing.T) {
		got := baseline.Diff(committed(t, rekeySrc), baseline.Render(unit(t, rekey(t, "Axis"))), without("key"))
		if find(got, baseline.Refuse, "Save.values", "key vocabulary") {
			t.Errorf("with the key row removed the rekey should not be judged, got:%s", summary(got))
		}
	})
}

const deprecateSrc = `package fixture

enum Team
{
    Red
    Blue
}

table Save
{
    lead  Team = Red
    bench [..4]Team
}
`

// TestDeprecatedVariantPredictionAgreesWithTheRead: a variant deprecated in
// place (docs/SPEC-TABLES.md §2.15) keeps its id and its ordinal, so no other
// fact on its line moves; the read is what moves. The baseline records the
// marker and warns when it turns on, and the sentence is held to the read: a
// stored value naming the variant arrives as the field's declared default, an
// element as the element's default, and nothing is counted.
func TestDeprecatedVariantPredictionAgreesWithTheRead(t *testing.T) {
	deprecated := editOf(t, deprecateSrc, "    Blue\n", "    Blue | deprecated\n")
	prediction{
		before: deprecateSrc, after: deprecated, verdict: baseline.Warn, where: "enum Team",
		save:    `{"lead":"Blue","bench":["Blue","Red"]}`,
		says:    []string{"variant Blue deprecated", "read as the field's declared default", "element as the element's default", "nothing is counted"},
		unknown: 0,
		text:    `{"lead":"Red","bench":["None","Red"]}`,
	}.run(t)

	if text := baseline.Render(unit(t, deprecated)).Text(); !strings.Contains(text, "variant Blue id=0xecf3d3a7c1693e2d deprecated=true\n") {
		t.Errorf("the marker is not on the variant's line:\n%s", text)
	}
	if text := baseline.Render(unit(t, deprecateSrc)).Text(); strings.Contains(text, "deprecated") {
		t.Errorf("a variant that is not deprecated renders no marker:\n%s", text)
	}
	// attribution, and the marker turning OFF: a deprecated variant is never
	// written, so nothing stored names it but saves that predate the marker
	if got := baseline.Diff(committed(t, deprecateSrc), baseline.Render(unit(t, deprecated)), without("enum-deprecated")); len(got) != 0 {
		t.Errorf("with the enum-deprecated row removed the edit should be silent, got:%s", summary(got))
	}
	if got := baseline.Diff(committed(t, deprecated), baseline.Render(unit(t, deprecateSrc)), baseline.DefaultTokenPolicy); len(got) != 0 {
		t.Errorf("a marker turned off must pass, got:%s", summary(got))
	}
}

// TestEvolutionPredictionsAgreeWithTheRead is the rest of the audit: one case
// per remaining sentence in this package that says what a reader does with a
// file written before the edit.
func TestEvolutionPredictionsAgreeWithTheRead(t *testing.T) {
	const enumSrc = "package fixture\n\nenum Team\n{\n    Red\n    Blue\n}\n\ntable Save\n{\n    lead Team\n    n    int32\n}\n"
	const keyedSrc = "package fixture\n\nenum Team\n{\n    Red\n    Blue\n}\n\ntable Save\n{\n    values [Team]uint8\n}\n"
	const armSrc = "package fixture\n\nunion Pick\n{\n    a int32\n    b int32\n}\n\ntable Save\n{\n    pick Pick\n    n    int32\n}\n"
	const flagsSrc = "package fixture\n\nflags Perks\n{\n    Quad\n    Haste\n}\n\ntable Save\n{\n    perks Perks\n}\n"
	const scalar = "package fixture\n\ntable Save\n{\n    %s\n}\n"
	one := func(decl string) string { return strings.Replace(scalar, "%s", decl, 1) }

	for _, p := range []prediction{
		{name: "an enum variant removed", before: enumSrc, after: editOf(t, enumSrc, "    Blue\n", ""),
			save: `{"lead":"Blue","n":5}`, verdict: baseline.Warn, where: "enum Team",
			says: []string{"variant Blue removed", "read as None", "count unknown"}, unknown: 1, text: `{"lead":"None","n":5}`},
		{name: "a key enum's variant removed", before: keyedSrc, after: editOf(t, keyedSrc, "    Blue\n", ""),
			save: `{"values":{"Red":11,"Blue":22}}`, verdict: baseline.Warn, where: "enum Team",
			says: []string{"variant Blue removed", "a stored slot of an array keyed by it is dropped and counts unknown"}, unknown: 1, text: `{"values":{"Red":11}}`},
		{name: "a union arm removed", before: armSrc, after: editOf(t, armSrc, "    b int32\n", ""),
			save: `{"pick":{"b":5},"n":5}`, verdict: baseline.Warn, where: "union Pick",
			says: []string{"arm b removed", "skipped", "count unknown"}, unknown: 1, text: `{"pick":{},"n":5}`},
		{name: "a specified default changed", before: one("x int32 = 21"), after: one("x int32 = 25"),
			save: `{"x":21}`, verdict: baseline.Refuse, where: "Save.x",
			says: []string{"specified default 21 -> 25"}, unknown: 0, text: `{"x":25}`},
		{name: "required added", before: one("x int32"), after: one("x int32 | required"),
			save: `{}`, verdict: baseline.Refuse, where: "Save.x",
			says: []string{"required added", "a reader refuses them as missing_required"}, reason: tablewire.ReasonMissingRequired},
		{name: "a flags variant moved", before: flagsSrc, after: editOf(t, flagsSrc, "    Quad\n    Haste\n", "    Haste\n    Quad\n"),
			save: `{"perks":["Quad"]}`, verdict: baseline.Refuse, where: "flags Perks",
			says: []string{"variant Quad moved from bit 0 to bit 1", "remapped", "nothing on the wire says so"}, unknown: 0, text: `{"perks":["Haste"]}`},
		{name: "a second rename aimed at the intermediate name", before: one(`speed int32 | was = "velocity"`), after: one(`max_speed int32 | was = "speed"`),
			save: `{"speed":5}`, verdict: baseline.Refuse, where: "Save.max_speed",
			says: []string{"an id no byte was ever written under", `write was = "velocity"`}, unknown: 1, text: `{"max_speed":0}`},
		{name: "a bare rename", before: one("speed int32"), after: one("velocity int32"),
			save: `{"speed":5}`, verdict: baseline.Warn, where: "table Save",
			says: []string{"speed removed and velocity added", "every stored value orphans"}, unknown: 1, text: `{"velocity":0}`},
	} {
		t.Run(p.name, p.run)
	}

	// A KIND CHANGE carries no sentence of its own; what speaks for it is the
	// line every refusal ends with, and that line is held too: the read
	// refuses `kind_mismatch`, so the edit does not pass unreported by every
	// reader, and the line must not say it does.
	t.Run("the line every refusal ends with", func(t *testing.T) {
		before, after := one("x int32"), one("x float32")
		r := readOldSave(t, before, after, "Save", `{"x":5}`)
		if r.OK || r.Reason != tablewire.ReasonKindMismatch {
			t.Fatalf("a stored int32 met by a float32 declaration must refuse kind_mismatch, got ok=%t reason=%q", r.OK, r.Reason)
		}
		dir, paths := writeUnit(t, after)
		if err := os.WriteFile(filepath.Join(dir, baseline.FileName), []byte(baseline.Render(unit(t, before)).Text()), 0o644); err != nil {
			t.Fatal(err)
		}
		_, errs := baseline.Check(unit(t, after), paths)
		if len(errs) != 1 {
			t.Fatalf("a kind change must refuse once, got %v", errs)
		}
		msg := errs[0].Error()
		if !strings.Contains(msg, "wire kind 4 -> 10") || !strings.Contains(msg, "or stops it reading") {
			t.Errorf("the refusal must name the kind change and allow that the read may stop:\n  %s", msg)
		}
		if strings.Contains(msg, "no reader can report it") {
			t.Errorf("the refusal says no reader can report the edit, and the read refuses %s:\n  %s", r.Reason, msg)
		}
	})

	// THE RETIRED-NAMES LEDGER says a re-added name decodes old bytes as
	// plausible new values with nothing on the wire to report it: a save
	// written while the field stood, read by a schema that re-added it, hands
	// the old value to the new field and counts nothing.
	t.Run("a retired name re-added", func(t *testing.T) {
		const stood = "package fixture\n\ntable Save\n{\n    hp int32\n    n  int32\n}\n"
		base := committed(t, editOf(t, stood, "    hp int32\n", ""))
		live := baseline.Render(unit(t, stood))
		var id uint64
		for _, f := range live.Tables[0].Fields {
			if f.Name == "hp" {
				id = f.Id
			}
		}
		base.Retired = append(base.Retired, baseline.Retired{Vocab: "field", Name: "Save.hp", Id: id, Date: "2026-01-01"})
		got := baseline.Diff(base, live, baseline.DefaultTokenPolicy)
		if !find(got, baseline.Refuse, "Save.hp", "decodes old bytes as plausible new values, and nothing on the wire reports it") {
			t.Fatalf("a re-added retired name must refuse, saying what the read does, got:%s", summary(got))
		}
		r := readOldSave(t, stood, stood, "Save", `{"hp":75,"n":1}`)
		if !r.OK || r.Report.Unknown != 0 || r.Text != `{"hp":75,"n":1}` {
			t.Errorf("the old bytes must arrive as the re-added field's value, uncounted, got ok=%t unknown=%d text=%s", r.OK, r.Report.Unknown, r.Text)
		}
	})
}
