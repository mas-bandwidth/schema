package baseline_test

// The MAP's half of the baseline (docs/SPEC-TABLES.md §2.8, §18.1): the
// generated entry is anonymous, the map field's own line carries the key's
// two facts, and the move between `[..N]Pair` and `map[K]V` is the one shape
// edit that costs anything.
//
// Every case here carries the file's two controls: the DISCRIMINATION control
// (the same field edited in the direction the wire absorbs) and the
// ATTRIBUTION control (the same edit re-judged with that one policy row
// removed, which must pass).

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

// mapFixtureSrc is the map fixture: one map by name and one by number, so the key's
// kind and its bound are both live, plus the table the value names.
const mapFixtureSrc = `package fixture

table ShipConfig
{
    hull int32
}

table Fleet
{
    ships map[string(32)]ShipConfig
    by_id map[uint32]int32
}
`

// mapPairSrc is the SAME BYTES spelled the way a schema spelled a lookup before
// maps existed: a bounded array of a two-field table whose fields are exactly
// `key` and `value`, under the two constant ids that makes them (§2.8).
const mapPairSrc = `package fixture

table ShipConfig
{
    hull int32
}

table Pair
{
    key   string(32)
    value ShipConfig
}

table Fleet
{
    ships [..4]Pair
    by_id map[uint32]int32
}
`

func mapDiff(t *testing.T, base, live string, policy map[string]baseline.TokenRule) []baseline.Finding {
	t.Helper()
	return baseline.Diff(committed(t, base), baseline.Render(unit(t, live)), policy)
}

// TestTheEntryIsAnonymousInTheBaseline: an entry's NAME is derived from the
// map field's source spelling, and a `was` rename moves that while moving no
// byte — so the file names the entry by the holder's wire id and the field's,
// and the rename moves nothing at all.
func TestTheEntryIsAnonymousInTheBaseline(t *testing.T) {
	text := baseline.Render(unit(t, mapFixtureSrc)).Text()
	for _, generated := range []string{"FleetShipsEntry", "FleetByIdEntry"} {
		if strings.Contains(text, generated) {
			t.Errorf("the baseline names the generated entry %s:\n%s", generated, text)
		}
	}
	if !strings.Contains(text, "field ships id=0x2d39 kind=14 elem=13 array=map keykind=12 keybound=32\n") {
		t.Errorf("the map field's line does not carry the shape and the key's facts:\n%s", text)
	}
	if !strings.Contains(text, "field by_id id=0xff83 kind=14 elem=13 array=map keykind=8\n") {
		t.Errorf("an integer-keyed map's line does not carry the key kind alone:\n%s", text)
	}

	// AND THE RENAME MOVES NOTHING. The entry's lines are byte-identical
	// under the new spelling, and no finding names an entry — the only thing
	// a `was` produces here is §16.4's ordinary text-key hint, which every
	// renamed field gets and which is about the field, not the construct.
	renamed := editOf(t, mapFixtureSrc, "    ships map[string(32)]ShipConfig", `    vessels map[string(32)]ShipConfig | was = "ships"`)
	if got := entryLines(baseline.Render(unit(t, renamed)).Text()); got != entryLines(text) {
		t.Errorf("a was rename moved the entry's lines:\n--- before ---\n%s--- after ---\n%s", entryLines(text), got)
	}
	for _, f := range mapDiff(t, mapFixtureSrc, renamed, baseline.DefaultTokenPolicy) {
		if strings.Contains(f.Where, "entry:") || strings.Contains(f.What, "map key") || strings.Contains(f.What, "array shape") {
			t.Errorf("a was rename of a map field reported against the construct: %s", f)
		}
	}
}

// entryLines is the baseline's anonymous-entry sections, so a test can
// compare them across an edit that moves the generated name.
func entryLines(text string) string {
	var out []string
	keep := false
	for line := range strings.SplitSeq(text, "\n") {
		if !strings.HasPrefix(line, " ") {
			keep = strings.HasPrefix(line, "table entry:")
		}
		if keep {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n") + "\n"
}

// TestTheKeyKindIsFixed: a key arriving under a kind the reader does not
// declare resets the whole map to EMPTY and counts one kind_mismatch (§2.8) —
// every entry gone, and nothing on the wire says which key was meant.
func TestTheKeyKindIsFixed(t *testing.T) {
	edited := editOf(t, mapFixtureSrc, "by_id map[uint32]int32", "by_id map[uint64]int32")
	fs := mapDiff(t, mapFixtureSrc, edited, baseline.DefaultTokenPolicy)
	if !find(fs, baseline.Refuse, "Fleet.by_id", "map key kind 8 -> 9") {
		t.Errorf("a changed key kind is not refused on the map's own line: %s", summary(fs))
	}
	// ATTRIBUTION: drop the keykind row and the map's own line goes quiet.
	// The ENTRY's `key` field is judged by the ordinary `kind` row and still
	// speaks, which is §18.1's own sentence — the baseline judges the entry's
	// two fields as it judges any field — so the control drops both.
	if fs := mapDiff(t, mapFixtureSrc, edited, without("keykind", "kind")); len(fs) != 0 {
		t.Errorf("with keykind and kind dropped the edit still reports: %s", summary(fs))
	}
	// DISCRIMINATION: the same field, the same map, a value kind that widens
	// nothing about the key — the key rows must not fire.
	widened := editOf(t, mapFixtureSrc, "by_id map[uint32]int32", "by_id map[uint32]int64")
	for _, f := range mapDiff(t, mapFixtureSrc, widened, baseline.DefaultTokenPolicy) {
		if strings.Contains(f.What, "map key") {
			t.Errorf("a VALUE edit reported against the key: %s", f)
		}
	}
}

// TestTheKeyBoundIsAnExtent: a tightened `string(N)` key does not clamp. An
// entry whose key does not fit is skipped WHOLE and counted, because a
// clamped key is a merged entry (§2.8) — lossy, reported, and so a warning.
func TestTheKeyBoundIsAnExtent(t *testing.T) {
	tightened := editOf(t, mapFixtureSrc, "map[string(32)]ShipConfig", "map[string(16)]ShipConfig")
	fs := mapDiff(t, mapFixtureSrc, tightened, baseline.DefaultTokenPolicy)
	if !find(fs, baseline.Warn, "Fleet.ships", "map key capacity 32 -> 16") {
		t.Errorf("a tightened key bound is not warned on the map's own line: %s", summary(fs))
	}
	if !find(fs, baseline.Warn, "Fleet.ships", "skipped WHOLE") {
		t.Errorf("the warning does not say what a key that does not fit costs: %s", summary(fs))
	}
	// ATTRIBUTION: the map line's row, and the entry's own `size` row beside it
	if fs := mapDiff(t, mapFixtureSrc, tightened, without("keybound", "size")); len(fs) != 0 {
		t.Errorf("with keybound and size dropped the edit still reports: %s", summary(fs))
	}
	// DISCRIMINATION: a WIDENED key bound loses nothing
	widened := editOf(t, mapFixtureSrc, "map[string(32)]ShipConfig", "map[string(64)]ShipConfig")
	if fs := mapDiff(t, mapFixtureSrc, widened, baseline.DefaultTokenPolicy); len(fs) != 0 {
		t.Errorf("a widened key bound is not silent: %s", summary(fs))
	}
}

// TestTheArrayToMapEditWarns is the migration §2.8 names: a schema that spelled
// its lookup `[..N]Pair` respells it `map[K]V`. The BYTES ARE IDENTICAL when
// Pair's fields are exactly `key` and `value` — they ride under the same two
// constant ids — and what the read gains is the ORDER CHECK, so a wire whose
// entries were not written ascending keeps the ascending prefix and reads
// short. Warned, never refused: nothing already written changes meaning.
func TestTheArrayToMapEditWarns(t *testing.T) {
	fs := mapDiff(t, mapPairSrc, mapFixtureSrc, baseline.DefaultTokenPolicy)
	for _, f := range fs {
		if f.Verdict == baseline.Refuse {
			t.Errorf("the [..N]Pair -> map[K]V edit refuses: %s", summary(fs))
			break
		}
	}
	if !find(fs, baseline.Warn, "Fleet.ships", "array shape bounded -> map") {
		t.Errorf("the shape move is not reported: %s", summary(fs))
	}
	if !find(fs, baseline.Warn, "Fleet.ships", "the same bytes in both directions where the element's fields are exactly a `key` and a `value`") {
		t.Errorf("the warning does not state the condition under which the bytes hold: %s", summary(fs))
	}
	if !find(fs, baseline.Warn, "Fleet.ships", "GAINS THE ORDER CHECK") {
		t.Errorf("the warning does not say what the map direction gains: %s", summary(fs))
	}
	// and the OTHER direction is the same one edit, losing what the map added
	back := mapDiff(t, mapFixtureSrc, mapPairSrc, baseline.DefaultTokenPolicy)
	if !find(back, baseline.Warn, "Fleet.ships", "array shape map -> bounded") {
		t.Errorf("the reverse shape move is not reported: %s", summary(back))
	}

	// THE CARVE-OUT: under the shipping policy the shape move is the ONE
	// finding on this field. The tokens the construct owns — the referent
	// that became anonymous, the key facts that arrived with it — describe
	// the same edit, and reporting them beside it would say it three times
	// and say it harsher.
	onField := 0
	for _, f := range fs {
		if strings.Contains(f.Where, "Fleet.ships") {
			onField++
		}
	}
	if onField != 1 {
		t.Errorf("the shape move produced %d findings on Fleet.ships, want exactly one: %s", onField, summary(fs))
	}

	// ATTRIBUTION: with the array row gone the shape check is gone, and no
	// finding anywhere says a shape moved.
	quiet := mapDiff(t, mapPairSrc, mapFixtureSrc, without("array"))
	for _, f := range quiet {
		if strings.Contains(f.What, "array shape") {
			t.Errorf("with the array row dropped, a shape move is still reported: %s", f)
		}
	}

	// DISCRIMINATION: fixed to bounded is the shape move the wire absorbs, and
	// the row must stay silent for it (§3).
	fixedSrc := editOf(t, mapPairSrc, "ships [..4]Pair", "ships [4]Pair")
	for _, f := range mapDiff(t, mapPairSrc, fixedSrc, baseline.DefaultTokenPolicy) {
		if strings.Contains(f.What, "array shape") {
			t.Errorf("fixed <-> bounded reported a shape move: %s", f)
		}
	}
}

// TestTheMapShapeCarveOutIsNarrow: the carve-out silences the tokens the map
// construct OWNS and nothing else. An element kind that really did move is
// still refused, because that is the half of the shape warning's claim the
// author has to be told about.
func TestTheMapShapeCarveOutIsNarrow(t *testing.T) {
	const scalarArray = `package fixture

table ShipConfig
{
    hull int32
}

table Fleet
{
    ships [..4]int32
    by_id map[uint32]int32
}
`
	fs := mapDiff(t, scalarArray, mapFixtureSrc, baseline.DefaultTokenPolicy)
	if !find(fs, baseline.Refuse, "Fleet.ships", "array element kind 4 -> 13") {
		t.Errorf("an array of int32 respelled as a map of tables is not refused on its element kind: %s", summary(fs))
	}
}

// TestEveryMapTokenHasAPolicyRow holds the policy honest from the other side:
// a token the renderer emits and the policy has no row for is judged on
// nothing, silently, which is the one failure mode this whole file exists to
// prevent.
func TestEveryMapTokenHasAPolicyRow(t *testing.T) {
	policy := maps.Clone(baseline.DefaultTokenPolicy)
	for _, tbl := range baseline.Render(unit(t, mapFixtureSrc)).Tables {
		for _, f := range tbl.Fields {
			for _, tok := range f.Tokens {
				if _, ok := policy[tok.Key]; !ok {
					t.Errorf("%s.%s carries the token %q and DefaultTokenPolicy has no row for it", tbl.Name, f.Name, tok.Key)
				}
			}
		}
	}
}
