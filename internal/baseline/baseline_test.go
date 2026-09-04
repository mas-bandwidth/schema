// The tables baseline's gate (docs/SPEC-TABLES.md §18.6): one fixture pair per
// refusal class and per warn class, each with its negative controls.
//
// TWO KINDS OF NEGATIVE CONTROL, because a gate can be wrong in two ways.
//
//   - The DISCRIMINATION control: the same field, the same vocabulary, edited
//     in the direction the wire absorbs — a bound grown instead of shrunk, a
//     flags variant appended instead of inserted, a field added instead of a
//     default changed. A gate that fires on those is a gate nobody will keep.
//   - The ATTRIBUTION control: the very same edit, re-judged with that one row
//     removed from the policy table, must pass. That is what proves the
//     refusal came from the check under test and not from something else in
//     the walk — an oracle that shares the code's own transformation cannot
//     tell you which check fired.
package baseline_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// baseSrc is the fixture unit every case edits: one table whose closure
// reaches an enum, a flags, a union and two plain types, so every vocabulary
// the baseline records is live.
const baseSrc = `package fixture

const Slots = 8

enum Grade { Bronze, Silver, Gold }

// declared and unreferenced: a different vocabulary, for the referent fixtures
enum Tier { Copper, Brass }

flags Perks { Shielded, Cloaked, Turbo }

// declared and unreferenced: Perks' three names at different bits
flags Traits { Turbo, Shielded, Cloaked }

type Buff
{
    multiplier float32 = 1.0
}

type Debuff
{
    amount int32 = 0
}

// same field ids as Buff, plus one, under identical facts: a declaration that
// really can stand in for Buff
type BuffPlus
{
    multiplier float32 = 1.0
    stacks     int32 = 1
}

// Buff's TWIN: the same field id under a different specified default. Id
// membership alone would let this through, and every stored body's elided
// multiplier would quietly mean 7.0
type Boon
{
    multiplier float32 = 7.0
}

// the same twin under a different wire kind
type BoonWide
{
    multiplier float64 = 1.0
}

// a pointer arm's target: a pointer points at a TABLE (§3.1)
table Chunk
{
    seq uint32
}

union Effect
{
    buff   Buff
    debuff Debuff
}

// AN ARM IS A FIELD LINE (§2.6), so this union carries one arm of every shape
// the arm refusal's sub-cases name (§18.6): a table body, a scalar, an array,
// a body a pointer can move, and an arm with no payload at all
union Shape
{
    body   Buff
    count  int32
    marks  [..8]float32
    chunk  Chunk
    ack
}

table Config
{
    damage  float32 = 21.0
    heading int16
    homing  bool
    hits    int32
    grade   Grade = Silver
    perks   Perks
    name    string(32)
    slots   [..Slots]int32
    boost   Buff
    effect  Effect
    shape   Shape
    swap_grade Grade
    swap_perks Perks
    swap Buff
    per_grade [Grade]int32
    gunner ?Buff
}
`

func unit(t *testing.T, src string) *ir.Unit {
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

// committed renders a unit and takes it through the file's own text form, so
// every diff in this file runs against a PARSED baseline exactly as a check
// against a committed file does.
func committed(t *testing.T, src string) *baseline.Unit {
	t.Helper()
	text := baseline.Render(unit(t, src)).Text()
	b, err := baseline.Parse("tables.baseline", []byte(text))
	if err != nil {
		t.Fatalf("a rendered baseline does not parse: %v\n%s", err, text)
	}
	return b
}

// diff judges an edit of baseSrc under the shipping policy.
func diff(t *testing.T, edited string, policy map[string]baseline.TokenRule) []baseline.Finding {
	t.Helper()
	return baseline.Diff(committed(t, baseSrc), baseline.Render(unit(t, edited)), policy)
}

// replace is the fixture edit: exactly one substitution, or the test is lying
// about what it changed.
func replace(t *testing.T, old, new string) string {
	t.Helper()
	return editOf(t, baseSrc, old, new)
}

// editOf is replace over any fixture unit.
func editOf(t *testing.T, src, old, new string) string {
	t.Helper()
	if strings.Count(src, old) != 1 {
		t.Fatalf("fixture edit %q does not appear exactly once in the fixture", old)
	}
	return strings.Replace(src, old, new, 1)
}

// find reports whether a finding of this verdict mentions both fragments.
func find(fs []baseline.Finding, v baseline.Verdict, where, what string) bool {
	for _, f := range fs {
		if f.Verdict == v && strings.Contains(f.Where, where) && strings.Contains(f.What, what) {
			return true
		}
	}
	return false
}

func summary(fs []baseline.Finding) string {
	if len(fs) == 0 {
		return "(no findings)"
	}
	var b strings.Builder
	for _, f := range fs {
		b.WriteString("\n  [" + f.Verdict.String() + "] " + f.String())
	}
	return b.String()
}

// without returns the shipping policy with one row removed — the attribution
// control's instrument.
func without(keys ...string) map[string]baseline.TokenRule {
	p := maps.Clone(baseline.DefaultTokenPolicy)
	for _, k := range keys {
		delete(p, k)
	}
	return p
}

// TestRefusals is one case per refusal class: the edits that change what data
// already written MEANS, with nothing on the wire to report them.
func TestRefusals(t *testing.T) {
	cases := []struct {
		name    string
		edited  string
		where   string
		what    string
		token   string // the policy row that must be the cause (empty: not a token rule)
		control string // an edit of the same shape the wire absorbs
	}{
		{
			name:   "a specified default changed",
			edited: replace(t, "damage  float32 = 21.0", "damage  float32 = 25.0"),
			where:  "Config.damage",
			what:   "specified default 21.0 -> 25.0",
			token:  "default",
			// the absorbed edit of the same shape: the same default DECLARED,
			// on a field nothing has ever written. (Adding a range to the
			// field instead would not do: a declared range is an extent, and
			// adding one narrows the kind's whole domain — see TestRangeFacts.)
			control: replace(t, "damage  float32 = 21.0", "damage  float32 = 21.0\n    bonus   float32 = 25.0"),
		},
		{
			name:    "a specified default removed",
			edited:  replace(t, "damage  float32 = 21.0", "damage  float32"),
			where:   "Config.damage",
			what:    "specified default 21.0 removed",
			token:   "default",
			control: replace(t, "damage  float32 = 21.0", "damage  float32 = 21.0\n    extra   int32"),
		},
		{
			name:    "a specified default added",
			edited:  replace(t, "homing  bool", "homing  bool = true"),
			where:   "Config.homing",
			what:    "specified default true added",
			token:   "default",
			control: replace(t, "homing  bool", "homing  bool\n    extra   bool = true"),
		},
		{
			name:    "an enum default renamed to another variant",
			edited:  replace(t, "grade   Grade = Silver", "grade   Grade = Gold"),
			where:   "Config.grade",
			what:    "specified default Silver -> Gold",
			token:   "default",
			control: replace(t, "enum Grade { Bronze, Silver, Gold }", "enum Grade { Bronze, Silver, Gold, Platinum }"),
		},
		{
			name:   "a field's kind changed",
			edited: replace(t, "heading int16", "heading int32"),
			where:  "Config.heading",
			what:   "wire kind 3 -> 4",
			token:  "kind",
			// the absorbed edit of the same shape: a NEW field of the changed
			// type beside the old one, which is the migration the wire wants
			control: replace(t, "heading int16", "heading int16\n    heading_wide int32"),
		},
		{
			name:    "an array's element kind changed",
			edited:  replace(t, "slots   [..Slots]int32", "slots   [..Slots]int64"),
			where:   "Config.slots",
			what:    "array element kind 4 -> 5",
			token:   "elem",
			control: replace(t, "slots   [..Slots]int32", "slots   [Slots]int32"), // fixed vs bounded frames identically
		},
		{
			// the ORIGINAL stays referenced, so nothing vanishes and no rename
			// pairing fires: this is a field repointed at a different
			// declaration, which is what the referent rule is for
			name:    "a field's flags TYPE swapped for a differently-ordered declaration",
			edited:  replace(t, "    swap_perks Perks", "    swap_perks Traits"),
			where:   "Config.swap_perks",
			what:    "flags Perks -> Traits",
			token:   "flags",
			control: replace(t, "flags Traits { Turbo, Shielded, Cloaked }", "flags Traits { Shielded, Cloaked, Turbo, Hardened }"),
		},
		{
			name:    "a field's enum TYPE swapped for another vocabulary",
			edited:  replace(t, "    swap_grade Grade", "    swap_grade Tier"),
			where:   "Config.swap_grade",
			what:    "enum Grade -> Tier",
			token:   "enum",
			control: replace(t, "enum Grade { Bronze, Silver, Gold }", "enum Grade { Bronze, Argent, Silver, Gold }"),
		},
		{
			// docs/SPEC-TABLES.md §4 states outright that this edit is NOT a kind
			// mismatch — both ride as kind 7 — so the runtime cannot report
			// it, which is the definition of this baseline's job
			name:    "an enum-typed field replaced by its raw storage integer",
			edited:  replace(t, "    grade   Grade = Silver", "    grade   uint16"),
			where:   "Config.grade",
			what:    "enum Grade removed",
			token:   "enum",
			control: replace(t, "    grade   Grade = Silver", "    grade   Grade = Silver\n    tier    uint16"),
		},
		{
			name:    "a raw integer field given an enum type",
			edited:  replace(t, "heading int16", "heading Grade"),
			where:   "Config.heading",
			what:    "enum Grade added",
			token:   "enum",
			control: replace(t, "heading int16", "heading int16\n    tier    Grade"),
		},
		{
			name:    "a nested table swapped for one whose ids do not ride",
			edited:  replace(t, "    swap Buff", "    swap Debuff"),
			where:   "Config.swap",
			what:    "nested table Buff -> Debuff, and 1 of 1 field ids do not ride",
			token:   "type",
			control: replace(t, "    swap Buff", "    swap BuffPlus"), // stands in: every id, same facts
		},
		{
			// THE TWIN. Every id rides and the declaration is still not
			// substitutable, because the fact under the shared id moved: every
			// stored body's elided multiplier now means 7.0.
			name:    "a nested table swapped for a twin carrying the same id under a different default",
			edited:  replace(t, "    swap Buff", "    swap Boon"),
			where:   "Config.swap",
			what:    "nested table Buff -> Boon, and multiplier's specified default 1.0 -> 7.0",
			token:   "default",
			control: replace(t, "    swap Buff", "    swap BuffPlus"),
		},
		{
			name:    "a nested table swapped for a twin carrying the same id under a different kind",
			edited:  replace(t, "    swap Buff", "    swap BoonWide"),
			where:   "Config.swap",
			what:    "nested table Buff -> BoonWide, and multiplier's wire kind 10 -> 11",
			token:   "kind",
			control: replace(t, "    swap Buff", "    swap BuffPlus"),
		},
		{
			name:    "a union arm's payload swapped for one whose ids do not ride",
			edited:  replace(t, "    buff   Buff\n", "    buff   Debuff\n"),
			where:   "union Effect.buff",
			what:    "arm payload Buff -> Debuff, and 1 of 1 field ids do not ride",
			token:   "payload",
			control: replace(t, "    buff   Buff\n", "    buff   BuffPlus\n"),
		},
		{
			name:    "a union arm's payload swapped for a twin carrying the same id under a different default",
			edited:  replace(t, "    buff   Buff\n", "    buff   Boon\n"),
			where:   "union Effect.buff",
			what:    "arm payload Buff -> Boon, and multiplier's specified default 1.0 -> 7.0",
			token:   "default",
			control: replace(t, "    buff   Buff\n", "    buff   BuffPlus\n"),
		},
		// THE ARM REFUSAL'S SUB-CASES (§18.6). An arm is a field line, so each
		// goes red for one reason, the token beside it.
		{
			name:    "a scalar arm's kind widened",
			edited:  replace(t, "    count  int32\n", "    count  int64\n"),
			where:   "union Shape.count",
			what:    "wire kind 4 -> 5",
			token:   "kind",
			control: replace(t, "    ack\n", "    ack\n    extra  int32\n"), // an arm ADDED: an id no reader names
		},
		{
			name:    "an array arm's element kind changed under one width",
			edited:  replace(t, "    marks  [..8]float32\n", "    marks  [..8]int32\n"),
			where:   "union Shape.marks",
			what:    "array element kind 10 -> 4",
			token:   "elem",
			control: replace(t, "    marks  [..8]float32\n", "    marks  [..16]float32\n"), // a bound GROWN
		},
		{
			name:    "a table arm moved to a pointer arm",
			edited:  replace(t, "    chunk  Chunk\n", "    chunk  *Chunk\n"),
			where:   "union Shape.chunk",
			what:    "arm payload Chunk removed",
			token:   "payload",
			control: replace(t, "    ack\n", "    ack\n    spare  Chunk\n"),
		},
		{
			name:    "a payload-free arm given a payload",
			edited:  replace(t, "    ack\n", "    ack    int32\n"),
			where:   "union Shape.ack",
			what:    "wire kind none -> 4",
			token:   "kind",
			control: replace(t, "    ack\n", "    ack\n    ack2\n"),
		},
		{
			// the keyed body and the positional body are different wire kinds
			// (docs/SPEC-TABLES.md §3.2), so a reader meeting the other skips —
			// and the values are silently gone
			name:    "an array changed from the keyed spelling to the positional one",
			edited:  replace(t, "    per_grade [Grade]int32", "    per_grade [4]int32"),
			where:   "Config.per_grade",
			what:    "wire kind 16 -> 14",
			token:   "kind",
			control: replace(t, "    per_grade [Grade]int32", "    per_grade [Grade]int32\n    per_tier  [Tier]int32"),
		},
		{
			name:    "an enum-keyed array's KEY enum swapped for another",
			edited:  replace(t, "    per_grade [Grade]int32", "    per_grade [Tier]int32"),
			where:   "Config.per_grade",
			what:    "array key enum Grade -> Tier",
			token:   "key",
			control: replace(t, "enum Grade { Bronze, Silver, Gold }", "enum Grade { Bronze, Argent, Silver, Gold }"),
		},
		{
			name:    "a flags variant inserted",
			edited:  replace(t, "flags Perks { Shielded, Cloaked, Turbo }", "flags Perks { Shielded, Hardened, Cloaked, Turbo }"),
			where:   "flags Perks",
			what:    "variant Cloaked moved from bit 1 to bit 2",
			token:   "flags-position",
			control: replace(t, "flags Perks { Shielded, Cloaked, Turbo }", "flags Perks { Shielded, Cloaked, Turbo, Hardened }"),
		},
		{
			name:    "a flags variant reordered",
			edited:  replace(t, "flags Perks { Shielded, Cloaked, Turbo }", "flags Perks { Cloaked, Shielded, Turbo }"),
			where:   "flags Perks",
			what:    "variant Shielded moved from bit 0 to bit 1",
			token:   "flags-position",
			control: replace(t, "flags Perks { Shielded, Cloaked, Turbo }", "flags Perks { Shielded, Cloaked, Turbo, Hardened }"),
		},
		{
			name:    "a flags variant renamed in place — a rename, or a spent bit reclaimed",
			edited:  replace(t, "flags Perks { Shielded, Cloaked, Turbo }", "flags Perks { Shielded, Stealth, Turbo }"),
			where:   "flags Perks",
			what:    "bit 1 was Cloaked and is now Stealth",
			token:   "flags-position",
			control: replace(t, "flags Perks { Shielded, Cloaked, Turbo }", "flags Perks { Shielded, Cloaked, Turbo, Hardened }"),
		},
		{
			name:    "a flags variant removed from the middle — every bit above it slides down",
			edited:  replace(t, "flags Perks { Shielded, Cloaked, Turbo }", "flags Perks { Shielded, Turbo }"),
			where:   "flags Perks",
			what:    "variant Turbo moved from bit 2 to bit 1",
			token:   "flags-position",
			control: replace(t, "flags Perks { Shielded, Cloaked, Turbo }", "flags Perks { Shielded, Cloaked, Turbo, Hardened }"),
		},
		{
			name:    "the last flags variant removed — the bit is freed, and a spent bit stays spent",
			edited:  replace(t, "flags Perks { Shielded, Cloaked, Turbo }", "flags Perks { Shielded, Cloaked }"),
			where:   "flags Perks",
			what:    "variant Turbo removed from bit 2",
			token:   "flags-position",
			control: replace(t, "flags Perks { Shielded, Cloaked, Turbo }", "flags Perks { Shielded, Cloaked, Turbo, Hardened }"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := diff(t, tc.edited, baseline.DefaultTokenPolicy)
			if !find(got, baseline.Refuse, tc.where, tc.what) {
				t.Fatalf("the edit was not refused; wanted %s: %s, got:%s", tc.where, tc.what, summary(got))
			}
			// the DISCRIMINATION control: the absorbed edit of the same shape
			if ctrl := diff(t, tc.control, baseline.DefaultTokenPolicy); len(ctrl) != 0 {
				t.Errorf("the control edit should be absorbed by the wire, got:%s", summary(ctrl))
			}
			// the ATTRIBUTION control: with that policy row gone, this
			// finding is gone — it came from THAT check and no other. (An
			// edit can trip more than one row at once: replacing an
			// enum-typed field with its raw storage integer also drops the
			// default, and both refusals are correct.)
			if tc.token == "" {
				return
			}
			if got := diff(t, tc.edited, without(tc.token)); find(got, baseline.Refuse, tc.where, tc.what) {
				t.Errorf("with the %q rule removed the refusal should be gone, got:%s", tc.token, summary(got))
			}
		})
	}
}

// TestWarnings is one case per warn class: the edits that lose something the
// runtime read report already counts, so the compiler reports rather than
// refuses.
func TestWarnings(t *testing.T) {
	cases := []struct {
		name    string
		edited  string
		where   string
		what    string
		token   string
		control string
	}{
		{
			name: "an array bound shrunk, through the constant it is declared with",
			// the bound is EVALUATED: the array declaration never moves, the
			// const does, and the baseline sees the value
			edited:  replace(t, "const Slots = 8", "const Slots = 4"),
			where:   "Config.slots",
			what:    "array bound 8 -> 4",
			token:   "bound",
			control: replace(t, "const Slots = 8", "const Slots = 16"),
		},
		{
			name:    "a string capacity shrunk",
			edited:  replace(t, "name    string(32)", "name    string(16)"),
			where:   "Config.name",
			what:    "capacity 32 -> 16",
			token:   "size",
			control: replace(t, "name    string(32)", "name    string(64)"),
		},
		{
			name:    "an enum variant removed",
			edited:  replace(t, "enum Grade { Bronze, Silver, Gold }", "enum Grade { Bronze, Silver }"),
			where:   "enum Grade",
			what:    "variant Gold removed",
			token:   "enum-variant",
			control: replace(t, "enum Grade { Bronze, Silver, Gold }", "enum Grade { Bronze, Argent, Silver, Gold }"),
		},
		{
			name:    "a union arm removed",
			edited:  replace(t, "    buff   Buff\n    debuff Debuff\n", "    buff   Buff\n"),
			where:   "union Effect",
			what:    "arm debuff removed",
			token:   "union-arm",
			control: replace(t, "    buff   Buff\n    debuff Debuff\n", "    buff   Buff\n    debuff Debuff\n    curse  Buff\n"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := diff(t, tc.edited, baseline.DefaultTokenPolicy)
			if !find(got, baseline.Warn, tc.where, tc.what) {
				t.Fatalf("the edit did not warn; wanted %s: %s, got:%s", tc.where, tc.what, summary(got))
			}
			if refusals, _ := baseline.Split(got); len(refusals) != 0 {
				t.Errorf("a warn-class edit must not refuse, got:%s", summary(refusals))
			}
			if ctrl := diff(t, tc.control, baseline.DefaultTokenPolicy); len(ctrl) != 0 {
				t.Errorf("the control edit should be absorbed by the wire, got:%s", summary(ctrl))
			}
			if tc.token == "" {
				return
			}
			if got := diff(t, tc.edited, without(tc.token)); find(got, baseline.Warn, tc.where, tc.what) {
				t.Errorf("with the %q rule removed the warning should be gone, got:%s", tc.token, summary(got))
			}
		})
	}
}

// TestVanishedMembers is finding 1 of the review, made a gate: a DECLARATION
// rename moves no byte, so the wire absorbs it — but it must not take
// everything inside the declaration out of coverage with it. A vanished
// baseline name warns, and where a same-shaped declaration appears the
// contents are judged under its new name, so a rename plus a semantic edit in
// one commit is still caught.
func TestVanishedMembers(t *testing.T) {
	cases := []struct {
		name     string
		edited   string
		warnAt   string
		warnWhat string
		verdict  baseline.Verdict
		where    string // the edit that must still be judged through the rename
		what     string
	}{
		{
			name: "a table renamed, and a specified default changed with it",
			edited: strings.NewReplacer("table Config", "table Ship",
				"damage  float32 = 21.0", "damage  float32 = 25.0").Replace(baseSrc),
			warnAt:   "table Config",
			warnWhat: "Ship carries 16 of its 16 identities",
			verdict:  baseline.Refuse,
			where:    "Ship.damage",
			what:     "specified default 21.0 -> 25.0",
		},
		{
			name: "a flags declaration renamed, and its bits reordered with it",
			edited: strings.NewReplacer(
				"flags Perks { Shielded, Cloaked, Turbo }", "flags Boons { Cloaked, Shielded, Turbo }",
				"perks   Perks", "perks   Boons",
				"swap_perks Perks", "swap_perks Boons").Replace(baseSrc),
			warnAt:   "flags Perks",
			warnWhat: "Boons carries 3 of its 3 identities",
			verdict:  baseline.Refuse,
			where:    "flags Boons",
			what:     "variant Shielded moved from bit 0 to bit 1",
		},
		{
			name: "an enum renamed, and a variant dropped with it",
			edited: strings.NewReplacer(
				"enum Grade { Bronze, Silver, Gold }", "enum Rank { Bronze, Silver }",
				"grade   Grade = Silver", "grade   Rank = Silver",
				"swap_grade Grade", "swap_grade Rank",
				"per_grade [Grade]int32", "per_grade [Rank]int32").Replace(baseSrc),
			warnAt:   "enum Grade",
			warnWhat: "Rank carries 2 of its 3 identities",
			verdict:  baseline.Warn,
			where:    "enum Rank",
			what:     "variant Gold removed",
		},
		{
			name: "a union renamed, and an arm dropped with it",
			edited: strings.NewReplacer(
				"union Effect\n{\n    buff   Buff\n    debuff Debuff\n}", "union Outcome\n{\n    buff   Buff\n}",
				"effect  Effect", "effect  Outcome").Replace(baseSrc),
			warnAt:   "union Effect",
			warnWhat: "Outcome carries 1 of its 2 identities",
			verdict:  baseline.Warn,
			where:    "union Outcome",
			what:     "arm debuff removed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := diff(t, tc.edited, baseline.DefaultTokenPolicy)
			if !find(got, baseline.Warn, tc.warnAt, tc.warnWhat) {
				t.Errorf("a vanished member must warn, naming where it went; wanted %s: %s, got:%s", tc.warnAt, tc.warnWhat, summary(got))
			}
			if !find(got, tc.verdict, tc.where, tc.what) {
				t.Fatalf("the edit inside the renamed declaration must still be judged; wanted %s: %s, got:%s", tc.where, tc.what, summary(got))
			}
			// the ATTRIBUTION control: without the member row there is no
			// pairing, and the edit inside the rename goes unseen — which is
			// exactly the hole this test closes
			if got := diff(t, tc.edited, without("member")); find(got, tc.verdict, tc.where, tc.what) {
				t.Errorf("with the %q rule removed the finding should be gone, got:%s", "member", summary(got))
			}
		})
	}
}

// TestPairedRenameDoesNotRaiseTheVerdict: a rename the pairing RECOGNISES —
// one that keeps at least half the declaration's identities — must draw the
// same verdict the same wire loss draws without it. A paired rename is the
// declaration under a new name, so its own walk judges it and the referent rule
// stays out; otherwise repointing a field at "the same thing, renamed" would
// refuse what an in-place edit only warns about. Below the threshold the rule
// is deliberately different, and TestBelowThresholdRenameIsARepoint states it.
func TestPairedRenameDoesNotRaiseTheVerdict(t *testing.T) {
	cases := []struct{ name, alone, renamed, where, what string }{
		{
			name:  "an enum variant dropped",
			alone: replace(t, "enum Grade { Bronze, Silver, Gold }", "enum Grade { Bronze, Silver }"),
			renamed: strings.NewReplacer(
				"enum Grade { Bronze, Silver, Gold }", "enum Rank { Bronze, Silver }",
				"grade   Grade = Silver", "grade   Rank = Silver",
				"swap_grade Grade", "swap_grade Rank",
				"per_grade [Grade]int32", "per_grade [Rank]int32").Replace(baseSrc),
			where: "variant Gold removed",
			what:  "Gold",
		},
		{
			name:  "a union arm dropped",
			alone: replace(t, "    buff   Buff\n    debuff Debuff\n", "    buff   Buff\n"),
			renamed: strings.NewReplacer(
				"union Effect\n{\n    buff   Buff\n    debuff Debuff\n}", "union Outcome\n{\n    buff   Buff\n}",
				"effect  Effect", "effect  Outcome").Replace(baseSrc),
			where: "arm debuff removed",
			what:  "debuff",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			aloneRefusals, _ := baseline.Split(diff(t, tc.alone, baseline.DefaultTokenPolicy))
			renamedRefusals, _ := baseline.Split(diff(t, tc.renamed, baseline.DefaultTokenPolicy))
			if len(aloneRefusals) != 0 {
				t.Fatalf("the edit alone must not refuse, got:%s", summary(aloneRefusals))
			}
			if len(renamedRefusals) != 0 {
				t.Errorf("the same edit plus a declaration rename must not refuse either — a rename moves no byte, got:%s", summary(renamedRefusals))
			}
		})
	}
}

// TestBelowThresholdRenameIsARepoint is the stated exception to the test above.
// A declaration that keeps too little of its identity is not recognisable as a
// rename — no evidence says the new name is the old declaration — so it is
// judged as what it is indistinguishable from: a field repointed at a different
// declaration. That refuses where the in-place edit only warns, and the warning
// says the pairing did not happen, so the harsher verdict is never silent.
func TestBelowThresholdRenameIsARepoint(t *testing.T) {
	// two of three variants dropped: below the half a rename needs. The
	// defaulted variant survives, so the ONLY edit is the loss.
	dropped := replace(t, "enum Grade { Bronze, Silver, Gold }", "enum Grade { Silver }")
	if refusals, warns := baseline.Split(diff(t, dropped, baseline.DefaultTokenPolicy)); len(refusals) != 0 || len(warns) == 0 {
		t.Fatalf("dropping variants in place warns and does not refuse, got:%s", summary(append(refusals, warns...)))
	}

	renamed := strings.NewReplacer(
		"enum Grade { Bronze, Silver, Gold }", "enum Rank { Silver }",
		"grade   Grade = Silver", "grade   Rank = Silver",
		"swap_grade Grade", "swap_grade Rank",
		"per_grade [Grade]int32", "per_grade [Rank]int32").Replace(baseSrc)
	got := diff(t, renamed, baseline.DefaultTokenPolicy)
	if !find(got, baseline.Refuse, "Config.swap_grade", "enum Grade -> Rank") {
		t.Fatalf("below the threshold the change of referent is judged as a repoint, got:%s", summary(got))
	}
	if !find(got, baseline.Warn, "enum Grade", "below the half needed to call it a rename") {
		t.Errorf("and the warning must say the pairing did not happen, got:%s", summary(got))
	}
}

// TestUnpairedVanishedMemberNamesWhatItFound: on the unpaired path the warning
// is the ONLY thing between a reader and the coverage hole, so it says what it
// actually found rather than asserting nothing was close.
func TestUnpairedVanishedMemberNamesWhatItFound(t *testing.T) {
	// a root table renamed with most of its fields dropped: too little in
	// common to call it a rename, and something in common all the same
	edited := strings.NewReplacer(
		"table Config", "table Archive",
		"    heading int16\n", "",
		"    homing  bool\n", "",
		"    hits    int32\n", "",
		"    grade   Grade = Silver\n", "",
		"    swap_grade Grade\n", "",
		"    name    string(32)\n", "",
		"    slots   [..Slots]int32\n", "",
		"    per_grade [Grade]int32\n", "",
		"    swap Buff\n", "").Replace(baseSrc)
	got := diff(t, edited, baseline.DefaultTokenPolicy)
	if !find(got, baseline.Warn, "table Config", "below the half needed to call it a rename") {
		t.Fatalf("the unpaired warning must say what it found, got:%s", summary(got))
	}
	if find(got, baseline.Warn, "table Config", "no declaration carries any of its identities") {
		t.Errorf("a candidate DID carry some identities; the message must not deny it, got:%s", summary(got))
	}
	// and the zero-overlap wording is still reachable, on a member nothing resembles
	gone := strings.Replace(baseSrc, "    effect  Effect\n", "", 1)
	if got := diff(t, gone, baseline.DefaultTokenPolicy); !find(got, baseline.Warn, "union Effect", "no declaration carries any of its identities") {
		t.Errorf("wanted the zero-overlap wording, got:%s", summary(got))
	}
}

// pairSrc is a standalone unit for the pairing fixtures: one root table
// nesting A, so A is in the closure and vanishes when it is renamed.
const pairSrc = `package pairing

type A
{
    x int32 = 1
    y int32 = 2
    z int32 = 3
    w int32 = 4
}

table Holder
{
    a A
}
`

// TestPairingWillNotGuessBetweenCandidates is the reviewer's mis-pair shape.
// `A` is renamed to `A2`, which drops one of its four fields; an UNRELATED new
// declaration `B` reuses all four field names, so identity overlap alone scores
// the stranger higher than the real rename. Pairing with the stranger would
// attribute four confident refusals to edits nobody made — so when more than
// one candidate reaches the threshold, the pairing asks a second question
// (whose own facts are closest) and pairs only on a candidate that wins both.
// Here it wins neither pair of answers together, so nothing is paired and the
// warning names both.
func TestPairingWillNotGuessBetweenCandidates(t *testing.T) {
	edited := `package pairing

type A2
{
    x int32 = 1
    y int32 = 2
    z int32 = 3
}

type B
{
    x int32 = 77
    y int32 = 88
    z int32 = 99
    w int32 = 66
}

table Holder
{
    a A2
    b B
}
`
	got := baseline.Diff(committed(t, pairSrc), baseline.Render(unit(t, edited)), baseline.DefaultTokenPolicy)
	if !find(got, baseline.Warn, "table A", "A2 and B each carry enough of its 4 identities") {
		t.Fatalf("an undecidable rename must name its contenders, got:%s", summary(got))
	}
	for _, f := range got {
		if f.Verdict == baseline.Refuse && strings.HasPrefix(f.Where, "B.") {
			t.Errorf("B is brand new; no edit inside it ever happened, got: %s", f)
		}
	}
	// the repoint is still judged, which is what keeps the loss visible
	if !find(got, baseline.Refuse, "Holder.a", "nested table A -> A2") {
		t.Errorf("the field repointed at A2 must still be judged, got:%s", summary(got))
	}
}

// TestPairingTieNamesTheTie: two candidates carrying EVERY identity cannot be
// told apart, and the message must not blame a threshold the numbers in the
// same sentence disprove.
func TestPairingTieNamesTheTie(t *testing.T) {
	edited := `package pairing

type A2
{
    x int32 = 1
    y int32 = 2
    z int32 = 3
    w int32 = 4
}

type A3
{
    x int32 = 1
    y int32 = 2
    z int32 = 3
    w int32 = 4
}

table Holder
{
    a A2
    b A3
}
`
	got := baseline.Diff(committed(t, pairSrc), baseline.Render(unit(t, edited)), baseline.DefaultTokenPolicy)
	if !find(got, baseline.Warn, "table A", "A2 and A3 each carry enough of its 4 identities") {
		t.Fatalf("a tie must be reported as a tie, got:%s", summary(got))
	}
	if find(got, baseline.Warn, "table A", "below the half needed") {
		t.Errorf("4 of 4 is not below half — the reason must not contradict the numbers, got:%s", summary(got))
	}
}

// TestPairingTakesTheRenameWhenItIsTheOnlyCandidate: the tiebreak must not cost
// the ordinary case. One candidate reaching the threshold is paired, and the
// semantic edit inside the rename is caught.
func TestPairingTakesTheRenameWhenItIsTheOnlyCandidate(t *testing.T) {
	edited := strings.NewReplacer(
		"type A\n", "type A2\n",
		"    x int32 = 1", "    x int32 = 11",
		"    a A", "    a A2").Replace(pairSrc)
	got := baseline.Diff(committed(t, pairSrc), baseline.Render(unit(t, edited)), baseline.DefaultTokenPolicy)
	if !find(got, baseline.Warn, "table A", "A2 carries 4 of its 4 identities") {
		t.Fatalf("the only candidate must be paired, got:%s", summary(got))
	}
	if !find(got, baseline.Refuse, "A2.x", "specified default 1 -> 11") {
		t.Errorf("the edit inside the rename must be judged under the new name, got:%s", summary(got))
	}
}

// TestVanishedMemberWithNoSuccessor: a member that really is gone warns too —
// removals stay legal, and a coverage hole is never invisible.
func TestVanishedMemberWithNoSuccessor(t *testing.T) {
	edited := strings.Replace(baseSrc, "    effect  Effect\n", "", 1)
	got := diff(t, edited, baseline.DefaultTokenPolicy)
	for _, want := range []string{"union Effect", "table Debuff"} {
		if !find(got, baseline.Warn, want, "no longer in the closure") {
			t.Errorf("wanted a coverage warning for %s, got:%s", want, summary(got))
		}
	}
	if refusals, _ := baseline.Split(got); len(refusals) != 0 {
		t.Errorf("removing a member is legal — it warns, it does not refuse, got:%s", summary(refusals))
	}
	if got := diff(t, edited, without("member")); len(got) != 0 {
		t.Errorf("with the \"member\" rule removed the removal should be silent, got:%s", summary(got))
	}
}

// TestAbsorbedEdits is the other half of the gate: everything the table wire
// handles on its own must pass in SILENCE. A baseline that cries wolf is a
// baseline someone deletes.
func TestAbsorbedEdits(t *testing.T) {
	cases := []struct{ name, edited string }{
		{"a field added", replace(t, "    hits    int32", "    hits    int32\n    added   int32")},
		{"a field removed", replace(t, "    hits    int32\n", "")},
		{"fields reordered", replace(t, "    damage  float32 = 21.0\n    heading int16", "    heading int16\n    damage  float32 = 21.0")},
		// the wire id survives, and the pairing that keeps the TEXT key is
		// declared beside it — the hint in TestRenameHintsTheJsonPairing is
		// exactly what a bare `was` draws instead
		{"a field renamed under was, with the text key paired", replace(t, "hits    int32", "strikes int32 | was = \"hits\", json = \"hits\"")},
		{"a flags variant appended", replace(t, "flags Perks { Shielded, Cloaked, Turbo }", "flags Perks { Shielded, Cloaked, Turbo, Hardened }")},
		{"an enum variant inserted in the middle", replace(t, "enum Grade { Bronze, Silver, Gold }", "enum Grade { Bronze, Argent, Silver, Gold }")},
		{"an array bound grown", replace(t, "const Slots = 8", "const Slots = 64")},
		{"a string capacity grown", replace(t, "name    string(32)", "name    string(128)")},
		{"a bounded array made fixed", replace(t, "slots   [..Slots]int32", "slots   [Slots]int32")},
		{"a field moved under a guard", replace(t, "    hits    int32", "    guard   bool\n    if guard\n    {\n        hits int32\n    }")},
		{"a table added", baseSrc + "\ntable Extra\n{\n    x int32 = 1\n}\n"},
		// T, ?T and *T are one framing (docs/SPEC-TABLES.md §3.1): presence is
		// recorded in the file and judged on nothing
		{"a field made optional", replace(t, "    boost   Buff", "    boost   ?Buff")},
		{"an optional field made plain", replace(t, "    gunner ?Buff", "    gunner Buff")},
		// a keyed array's bound is its enum's size: growing the enum grows the
		// array, and the slots ride by variant NAME, so nothing moves
		{"a keyed array's enum grown", replace(t, "enum Grade { Bronze, Silver, Gold }", "enum Grade { Bronze, Silver, Gold, Platinum }")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := diff(t, tc.edited, baseline.DefaultTokenPolicy); len(got) != 0 {
				t.Errorf("the wire absorbs this edit; the baseline must be silent, got:%s", summary(got))
			}
		})
	}
}

// TestKeyEnumIsCovered: an enum-keyed array's key enum reaches the closure
// only through the array, never through a field's own type — and its variants
// are wire identities all the same, because the slots ride under their name
// ids (docs/SPEC-TABLES.md §3.2). A projection that forgot it would leave every
// keyed collection's vocabulary uncovered.
func TestKeyEnumIsCovered(t *testing.T) {
	const src = `package keyed

enum Slot { Head, Chest, Legs }

type Piece
{
    armour int32 = 1
}

table Loadout
{
    pieces [Slot]Piece
}
`
	b := committed(t, src)
	var names []string
	for _, e := range b.Enums {
		names = append(names, e.Name)
	}
	if len(names) != 1 || names[0] != "Slot" {
		t.Fatalf("the key enum must be projected, got %v", names)
	}

	dropped := strings.Replace(src, "enum Slot { Head, Chest, Legs }", "enum Slot { Head, Chest }", 1)
	got := baseline.Diff(b, baseline.Render(unit(t, dropped)), baseline.DefaultTokenPolicy)
	if !find(got, baseline.Warn, "enum Slot", "variant Legs removed") {
		t.Errorf("dropping a key enum variant loses those slots and must warn, got:%s", summary(got))
	}
	if got := baseline.Diff(b, baseline.Render(unit(t, dropped)), without("enum-variant")); find(got, baseline.Warn, "enum Slot", "variant Legs removed") {
		t.Errorf("with the \"enum-variant\" rule removed the warning should be gone, got:%s", summary(got))
	}
	// growing it is absorbed: a new variant is a new slot nothing has written
	grown := strings.Replace(src, "enum Slot { Head, Chest, Legs }", "enum Slot { Head, Chest, Legs, Boots }", 1)
	if got := baseline.Diff(b, baseline.Render(unit(t, grown)), baseline.DefaultTokenPolicy); len(got) != 0 {
		t.Errorf("growing a key enum is absorbed, got:%s", summary(got))
	}
}

// TestKeyedBoundIsNotJudgedAsAnExtent: a keyed array's bound IS its key
// enum's size, and its slots ride under variant name ids (docs/SPEC-TABLES.md
// §3.2) — an unknown key is skipped and counted `unknown`, with no bounded
// prefix and nothing clamped. The enum walk already says what went and by
// name, so the extent row must not say it a second time in the wrong words.
func TestKeyedBoundIsNotJudgedAsAnExtent(t *testing.T) {
	const src = `package keyed

enum Slot { Head, Chest, Legs }

table Loadout
{
    pieces  [Slot]int32
    spares  [..3]int32
}
`
	dropped := strings.Replace(src, "enum Slot { Head, Chest, Legs }", "enum Slot { Head, Chest }", 1)
	got := baseline.Diff(committed(t, src), baseline.Render(unit(t, dropped)), baseline.DefaultTokenPolicy)
	if !find(got, baseline.Warn, "enum Slot", "variant Legs removed") {
		t.Fatalf("the enum walk must report the lost slot by name, got:%s", summary(got))
	}
	if find(got, baseline.Warn, "Loadout.pieces", "array bound") {
		t.Errorf("a keyed array's bound must not be judged as an extent — nothing clamps, and the enum walk already said it, got:%s", summary(got))
	}
	// the POSITIONAL sibling still warns on a shrunk extent: this is a
	// discrimination, not a blanket exemption
	shrunk := strings.Replace(src, "spares  [..3]int32", "spares  [..2]int32", 1)
	if got := baseline.Diff(committed(t, src), baseline.Render(unit(t, shrunk)), baseline.DefaultTokenPolicy); !find(got, baseline.Warn, "Loadout.spares", "array bound 3 -> 2") {
		t.Errorf("a positional array's shrunk bound still warns, got:%s", summary(got))
	}
}

// TestRenderIsIdempotent: the text form round-trips through the parser with
// no drift, which is what makes `--update` byte-stable and the file diffable.
func TestRenderIsIdempotent(t *testing.T) {
	first := baseline.Render(unit(t, baseSrc)).Text()
	parsed, err := baseline.Parse("tables.baseline", []byte(first))
	if err != nil {
		t.Fatal(err)
	}
	if second := parsed.Text(); second != first {
		t.Errorf("render -> parse -> render is not the identity:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// ---- the file side: when the check runs, and what --update writes ----

// writeUnit lays a one-file unit down in a temp directory and returns the
// paths the compiler would gather.
func writeUnit(t *testing.T, src string) (dir string, paths []string) {
	t.Helper()
	dir = t.TempDir()
	p := filepath.Join(dir, "Fixture.schema")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, []string{p}
}

// TestNoFileNoCheck: the baseline is opt-in. A unit that never committed one
// is untouched by any of this.
func TestNoFileNoCheck(t *testing.T) {
	_, paths := writeUnit(t, baseSrc)
	warns, errs := baseline.Check(unit(t, baseSrc), paths)
	if len(warns) != 0 || len(errs) != 0 {
		t.Fatalf("a unit with no baseline must be checked against nothing: %v %v", warns, errs)
	}
}

// TestUpdateRefusesWithoutReason is the owner's ruling made mechanical: an
// intentional break is declared, or it does not happen.
func TestUpdateRefusesWithoutReason(t *testing.T) {
	dir, paths := writeUnit(t, baseSrc)
	for _, reason := range []string{"", "   "} {
		if _, _, err := baseline.Update(unit(t, baseSrc), paths, reason); err == nil {
			t.Fatalf("--update with reason %q must be refused", reason)
		} else if !strings.Contains(err.Error(), "--reason") {
			t.Errorf("the refusal must name --reason, got: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, baseline.FileName)); !os.IsNotExist(err) {
		t.Error("a refused --update must write nothing")
	}
}

// TestUpdateWritesAndIsIdempotent: the first update creates the file and says
// what it does not cover; a second update over an unmoved unit writes nothing
// at all, so the committed file is byte-stable.
func TestUpdateWritesAndIsIdempotent(t *testing.T) {
	dir, paths := writeUnit(t, baseSrc)
	u := unit(t, baseSrc)

	path, rewrote, err := baseline.Update(u, paths, "the first baseline")
	if err != nil || !rewrote {
		t.Fatalf("first update: %v (rewrote=%v)", err, rewrote)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), baseline.HistoryHeading) {
		t.Error("a created baseline carries a history section")
	}
	if !strings.Contains(string(first), "the first baseline") {
		t.Error("the reason must land in the history")
	}
	if !strings.Contains(string(first), "data written BEFORE this point is not covered") {
		t.Error("a created baseline says what it does not cover")
	}

	_, rewrote, err = baseline.Update(u, paths, "again, with nothing to record")
	if err != nil {
		t.Fatal(err)
	}
	if rewrote {
		t.Error("an update over an unmoved unit must write nothing")
	}
	second, err := os.ReadFile(filepath.Join(dir, baseline.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) {
		t.Errorf("the baseline is not byte-stable under a second update:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// TestUpdateRecordsTheBreak: the whole point of --reason. The history entry
// names what moved, old to new, beside why.
func TestUpdateRecordsTheBreak(t *testing.T) {
	dir, paths := writeUnit(t, baseSrc)
	if _, _, err := baseline.Update(unit(t, baseSrc), paths, "the first baseline"); err != nil {
		t.Fatal(err)
	}

	edited := replace(t, "damage  float32 = 21.0", "damage  float32 = 25.0")
	if err := os.WriteFile(paths[0], []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	live := unit(t, edited)

	// before the override, the check refuses and names the edit
	_, errs := baseline.Check(live, paths)
	if len(errs) != 1 {
		t.Fatalf("the edit must be refused exactly once, got %v", errs)
	}
	for _, want := range []string{"Config.damage", "specified default 21.0 -> 25.0", "--update --reason"} {
		if !strings.Contains(errs[0].Error(), want) {
			t.Errorf("the refusal must mention %q, got: %v", want, errs[0])
		}
	}

	// the override records it
	if _, _, err := baseline.Update(live, paths, "damage rebalanced for season 2; old saves read the new value"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, baseline.FileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"the first baseline",
		"damage rebalanced for season 2",
		"- Config.damage: specified default 21.0 -> 25.0 [refuse]",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the history must carry %q:\n%s", want, data)
		}
	}
	// and the check is silent again
	if warns, errs := baseline.Check(live, paths); len(warns) != 0 || len(errs) != 0 {
		t.Errorf("after the override the check must pass: %v %v", warns, errs)
	}
}

// TestCheckReportsWarningsWithoutRefusing: the warn class reaches the caller
// as text, and the load still succeeds.
func TestCheckReportsWarningsWithoutRefusing(t *testing.T) {
	_, paths := writeUnit(t, baseSrc)
	if _, _, err := baseline.Update(unit(t, baseSrc), paths, "the first baseline"); err != nil {
		t.Fatal(err)
	}
	edited := replace(t, "const Slots = 8", "const Slots = 4")
	if err := os.WriteFile(paths[0], []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	warns, errs := baseline.Check(unit(t, edited), paths)
	if len(errs) != 0 {
		t.Fatalf("a shrunk bound warns, it does not refuse: %v", errs)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "array bound 8 -> 4") {
		t.Fatalf("wanted one bound warning, got %v", warns)
	}
}

// TestUpdateSalvagesHistoryFromAnUnreadableBaseline is the review's finding 3
// made a gate: every parse refusal names `--update --reason` as the remedy, so
// that command has to WORK on a baseline the parser rejects — and it must not
// cost the one artifact in the file that cannot be regenerated. The
// version-bump case is the one that matters, because a Version bump puts every
// committed baseline in the estate on exactly this path.
func TestUpdateSalvagesHistoryFromAnUnreadableBaseline(t *testing.T) {
	history := []string{
		"### 2024-01-01 — the break we are never allowed to forget",
		"- Config.damage: specified default 10.0 -> 21.0 [refuse]",
	}
	cases := []struct{ name, head string }{
		{"a rendering version this compiler does not write", fmt.Sprintf("schema-tables-baseline %d\npackage fixture\n", baseline.Version+1)},
		{"a file that is not a baseline at all", "hello world\n"},
		{"a member line this parser cannot read", "schema-tables-baseline 1\npackage fixture\n\ntable Config\n    field ???\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, paths := writeUnit(t, baseSrc)
			path := filepath.Join(dir, baseline.FileName)
			stale := tc.head + "\n" + baseline.HistoryHeading + "\n" + strings.Join(history, "\n") + "\n"
			if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
				t.Fatal(err)
			}

			// the check refuses and names the remedy
			_, errs := baseline.Check(unit(t, baseSrc), paths)
			if len(errs) != 1 || !strings.Contains(errs[0].Error(), "--update --reason") {
				t.Fatalf("wanted one refusal naming the remedy, got %v", errs)
			}
			// and the remedy runs
			if _, rewrote, err := baseline.Update(unit(t, baseSrc), paths, "regenerating as instructed"); err != nil || !rewrote {
				t.Fatalf("the advertised remedy must work: %v (rewrote=%v)", err, rewrote)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range append(history,
				"regenerating as instructed",
				"baseline REGENERATED over an unreadable one",
				"table Config") {
				if !strings.Contains(string(data), want) {
					t.Errorf("the regenerated baseline must carry %q:\n%s", want, data)
				}
			}
			// and the file it produced is one the check accepts
			if warns, errs := baseline.Check(unit(t, baseSrc), paths); len(warns) != 0 || len(errs) != 0 {
				t.Errorf("after the regeneration the check must pass: %v %v", warns, errs)
			}
		})
	}
}

// TestParseHoldsBitPositionToLineOrder: `bit=` states in the file what line
// order already decides, and the parser holds the two to each other rather
// than guessing which half a hand-edit meant.
func TestParseHoldsBitPositionToLineOrder(t *testing.T) {
	text := baseline.Render(unit(t, baseSrc)).Text()
	scrambled := strings.Replace(text,
		"    variant Shielded bit=0\n    variant Cloaked bit=1\n",
		"    variant Cloaked bit=1\n    variant Shielded bit=0\n", 1)
	if scrambled == text {
		t.Fatal("the fixture edit did not apply")
	}
	_, err := baseline.Parse("tables.baseline", []byte(scrambled))
	if err == nil {
		t.Fatal("two variant lines swapped without their bit= values is a file this parser must refuse")
	}
	if !strings.Contains(err.Error(), "bit position is line order here") {
		t.Errorf("the refusal must say why, got: %v", err)
	}
}

// TestCheckRefusesAForeignBaseline: a baseline belongs to the unit it sits
// beside, and a misread one is a check that lies.
func TestCheckRefusesAForeignBaseline(t *testing.T) {
	dir, paths := writeUnit(t, baseSrc)
	for _, tc := range []struct{ name, data, want string }{
		{"not a baseline at all", "hello\n", "not a schema tables baseline"},
		{"a future rendering version", "schema-tables-baseline 99\npackage fixture\n", "this compiler writes"},
		{"another unit's baseline", fmt.Sprintf("schema-tables-baseline %d\npackage elsewhere\n", baseline.Version), "baseline is for package elsewhere"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, baseline.FileName), []byte(tc.data), 0o644); err != nil {
				t.Fatal(err)
			}
			_, errs := baseline.Check(unit(t, baseSrc), paths)
			if len(errs) != 1 || !strings.Contains(errs[0].Error(), tc.want) {
				t.Fatalf("wanted a refusal mentioning %q, got %v", tc.want, errs)
			}
		})
	}
}

// ---- the range facts (docs/SPEC-TABLES.md §18.1, §18.2) ----

// rangeSrc is the ranged fixture: one field per family the table wire clamps
// on load — a ranged integer, a fixed-point field whose bounds are its
// WHOLE-UNIT ones, and a compressed float — with an unranged integer beside
// them, so a case can move one range and leave the rest still. Every default
// sits well inside its bounds, so an edit here moves the range and nothing
// else.
const rangeSrc = `package ranged

table Ship
{
    hull  int32 = 50          | min = 0, max = 1000
    angle fixed(16, 16) = 0   | min = -180, max = 180
    speed float32 = 1.0       | min = 0.0, max = 100.0, resolution = 0.01
    tally int32 = 0
}
`

// rangeDiff judges an edit of rangeSrc.
func rangeDiff(t *testing.T, edited string, policy map[string]baseline.TokenRule) []baseline.Finding {
	t.Helper()
	return baseline.Diff(committed(t, rangeSrc), baseline.Render(unit(t, edited)), policy)
}

// TestRangeFacts is #443 made a gate. A tightened range CLAMPS every stored
// value past the new bound on load, counted once and permanent on the next
// save (docs/SPEC-TABLES.md §4) — and until the range was in the file the
// baseline passed it in silence, because the file carried no range token at
// all. It warns rather than refuses, for the reason every extent does: the
// data survives and the read report counts what was lost.
func TestRangeFacts(t *testing.T) {
	cases := []struct {
		name    string
		edited  string
		where   string
		what    string
		token   string
		control string // the same extent moved the way the wire absorbs
	}{
		{
			name:    "a maximum lowered",
			edited:  editOf(t, rangeSrc, "hull  int32 = 50          | min = 0, max = 1000", "hull  int32 = 50          | min = 0, max = 100"),
			where:   "Ship.hull",
			what:    "declared maximum 1000 -> 100 (a stored value above the new maximum reads back AS the maximum and counts clamped)",
			token:   "max",
			control: editOf(t, rangeSrc, "hull  int32 = 50          | min = 0, max = 1000", "hull  int32 = 50          | min = 0, max = 10000"),
		},
		{
			name:    "a minimum raised",
			edited:  editOf(t, rangeSrc, "hull  int32 = 50          | min = 0, max = 1000", "hull  int32 = 50          | min = 10, max = 1000"),
			where:   "Ship.hull",
			what:    "declared minimum 0 -> 10 (a stored value below the new minimum reads back AS the minimum and counts clamped)",
			token:   "min",
			control: editOf(t, rangeSrc, "hull  int32 = 50          | min = 0, max = 1000", "hull  int32 = 50          | min = -10, max = 1000"),
		},
		{
			// a FIXED field's declared bounds are whole units and the raw
			// scale is `frac`, recorded beside them: narrowing the units
			// narrows the raw range at the same F, with no kind to report it
			name:    "a fixed field's whole-unit bounds narrowed",
			edited:  editOf(t, rangeSrc, "angle fixed(16, 16) = 0   | min = -180, max = 180", "angle fixed(16, 16) = 0   | min = -90, max = 180"),
			where:   "Ship.angle",
			what:    "declared minimum -180 -> -90",
			token:   "min",
			control: editOf(t, rangeSrc, "angle fixed(16, 16) = 0   | min = -180, max = 180", "angle fixed(16, 16) = 0   | min = -360, max = 180"),
		},
		{
			// the compressed float's bounds are FLOATS, so the comparison is
			// not an integer one — the fact is exact either way
			name:    "a compressed float's maximum lowered",
			edited:  editOf(t, rangeSrc, "speed float32 = 1.0       | min = 0.0, max = 100.0, resolution = 0.01", "speed float32 = 1.0       | min = 0.0, max = 10.5, resolution = 0.01"),
			where:   "Ship.speed",
			what:    "declared maximum 100.0 -> 10.5",
			token:   "max",
			control: editOf(t, rangeSrc, "speed float32 = 1.0       | min = 0.0, max = 100.0, resolution = 0.01", "speed float32 = 1.0       | min = 0.0, max = 1000.5, resolution = 0.01"),
		},
		{
			// a range where the declaration had none narrows the kind's WHOLE
			// domain onto a slice of it: the same clamp, from the widest
			// possible starting point
			name:    "a range added where the field had none",
			edited:  editOf(t, rangeSrc, "tally int32 = 0", "tally int32 = 0           | min = 0, max = 10"),
			where:   "Ship.tally",
			what:    "declared maximum 10 added (a stored value above the new maximum reads back AS the maximum and counts clamped)",
			token:   "max",
			control: editOf(t, rangeSrc, "tally int32 = 0", "tally int32 = 0\n    extra int32 = 0     | min = 0, max = 10"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rangeDiff(t, tc.edited, baseline.DefaultTokenPolicy)
			if !find(got, baseline.Warn, tc.where, tc.what) {
				t.Fatalf("a tightened range must warn; wanted %s: %s, got:%s", tc.where, tc.what, summary(got))
			}
			if refusals, _ := baseline.Split(got); len(refusals) != 0 {
				t.Errorf("a tightened range warns and does not refuse, got:%s", summary(refusals))
			}
			// the DISCRIMINATION control: the same extent LOOSENED, which
			// nothing already written falls outside of
			if ctrl := rangeDiff(t, tc.control, baseline.DefaultTokenPolicy); len(ctrl) != 0 {
				t.Errorf("a loosened range is absorbed; the baseline must be silent, got:%s", summary(ctrl))
			}
			// the ATTRIBUTION control
			if got := rangeDiff(t, tc.edited, without(tc.token)); find(got, baseline.Warn, tc.where, tc.what) {
				t.Errorf("with the %q rule removed the warning should be gone, got:%s", tc.token, summary(got))
			}
		})
	}
}

// TestRangeRemovedIsAbsorbed: dropping a declared range widens the field to
// its kind's whole domain, and nothing already written sits outside that.
func TestRangeRemovedIsAbsorbed(t *testing.T) {
	edited := editOf(t, rangeSrc, "hull  int32 = 50          | min = 0, max = 1000", "hull  int32 = 50")
	if got := rangeDiff(t, edited, baseline.DefaultTokenPolicy); len(got) != 0 {
		t.Errorf("a dropped range loosens; the baseline must be silent, got:%s", summary(got))
	}
}

// TestRangeIsRecorded: the projection carries the EVALUATED bounds, which is
// what lets the check see a range that moved through a constant.
func TestRangeIsRecorded(t *testing.T) {
	text := baseline.Render(unit(t, rangeSrc)).Text()
	for _, want := range []string{
		"field hull id=0x80da8ccc11daadf6 kind=4 min=0 max=1000 default=50",
		"min=-180 max=180",
		"min=0.0 max=100.0",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the projection must record %q:\n%s", want, text)
		}
	}

	// and it is the EVALUATED value: a const that moves moves the fact
	const constSrc = `package ranged

const MaxHull = 1000

table Ship
{
    hull int32 = 50 | min = 0, max = MaxHull
}
`
	edited := editOf(t, constSrc, "const MaxHull = 1000", "const MaxHull = 100")
	got := baseline.Diff(committed(t, constSrc), baseline.Render(unit(t, edited)), baseline.DefaultTokenPolicy)
	if !find(got, baseline.Warn, "Ship.hull", "declared maximum 1000 -> 100") {
		t.Errorf("a range tightened through its constant must warn, got:%s", summary(got))
	}
}

// ---- `was` misuse and the rename pair (docs/SPEC-TABLES.md §5, §18.2) ----

// wasSrc is the twice-renamed fixture: a field that has ALREADY been renamed
// once and rides under the first name's hash.
const wasSrc = `package renaming

table Ship
{
    speed float32 = 500.0 | was = "velocity"
    hull  int32 = 50      | min = 0, max = 1000
}
`

// TestWasChainIsRefused is #444's flagship. `was` names the FIRST wire name,
// forever: once `velocity` became `speed | was = "velocity"` the field rides
// under hash("velocity"), so a later `max_speed | was = "speed"` hashes a name
// no byte was ever written under and every stored value orphans — with nothing
// on the wire to report it, because the reader simply finds no such id.
func TestWasChainIsRefused(t *testing.T) {
	edited := editOf(t, wasSrc, `speed float32 = 500.0 | was = "velocity"`, `max_speed float32 = 500.0 | was = "speed"`)
	got := baseline.Diff(committed(t, wasSrc), baseline.Render(unit(t, edited)), baseline.DefaultTokenPolicy)
	if !find(got, baseline.Refuse, "Ship.max_speed",
		`was = "speed" names speed, which itself rode under was = "velocity" — `+"`was`"+` names the FIRST wire name, forever`) {
		t.Fatalf("a second `was` naming the field's own current spelling must be refused, got:%s", summary(got))
	}
	if !find(got, baseline.Refuse, "Ship.max_speed", `write was = "velocity"`) {
		t.Errorf("the refusal must name the spelling that is correct, got:%s", summary(got))
	}

	// the DISCRIMINATION control: the second rename done RIGHT — the first
	// wire name carried forward — moves no byte and says nothing
	right := editOf(t, wasSrc, `speed float32 = 500.0 | was = "velocity"`, `max_speed float32 = 500.0 | was = "velocity"`)
	if ctrl := baseline.Diff(committed(t, wasSrc), baseline.Render(unit(t, right)), baseline.DefaultTokenPolicy); len(ctrl) != 0 {
		t.Errorf("carrying the first wire name forward is the whole point of `was`; it must be silent, got:%s", summary(ctrl))
	}
	// the ATTRIBUTION control
	if got := baseline.Diff(committed(t, wasSrc), baseline.Render(unit(t, edited)), without("was-chain")); len(got) != 0 {
		t.Errorf("with the \"was-chain\" rule removed the edit passes as it did before, got:%s", summary(got))
	}
}

// TestRenameHintsTheJsonPairing is #444's second half. A `was` keeps the WIRE
// id through a rename and does not keep the TEXT key, which is the field's name
// (docs/SPEC-TABLES.md §16.4) — so an existing JSON file keyed on the old name
// stops matching. The hint is said once, at the edit that adds the `was`, and
// only to a field with no key of its own.
func TestRenameHintsTheJsonPairing(t *testing.T) {
	edited := replace(t, "hits    int32", `strikes int32 | was = "hits"`)
	got := diff(t, edited, baseline.DefaultTokenPolicy)
	if !find(got, baseline.Warn, "Config.strikes", `renamed under was = "hits", which keeps the wire id and NOT the text key — the JSON key is now "strikes"; pair json = "hits"`) {
		t.Fatalf("a bare rename must hint the text-key pairing, got:%s", summary(got))
	}
	if refusals, _ := baseline.Split(got); len(refusals) != 0 {
		t.Errorf("a hint is a hint: it must not refuse, got:%s", summary(refusals))
	}

	// the DISCRIMINATION control: the pairing DECLARED beside the rename
	paired := replace(t, "hits    int32", `strikes int32 | was = "hits", json = "hits"`)
	if ctrl := diff(t, paired, baseline.DefaultTokenPolicy); len(ctrl) != 0 {
		t.Errorf("a field that already pairs its text key has answered the question, got:%s", summary(ctrl))
	}
	// SAID ONCE: with the rename committed, the next check is silent
	if again := baseline.Diff(committed(t, edited), baseline.Render(unit(t, edited)), baseline.DefaultTokenPolicy); len(again) != 0 {
		t.Errorf("the hint belongs to the edit that renames, not to every check after it, got:%s", summary(again))
	}
	// the ATTRIBUTION control
	if got := diff(t, edited, without("was-json")); find(got, baseline.Warn, "Config.strikes", "pair json =") {
		t.Errorf("with the \"was-json\" rule removed the hint should be gone, got:%s", summary(got))
	}
}

// TestRemovalAndAdditionWarnAsAPossibleRename is #444's third half, and the one
// mechanism that can see a BARE rename at all. To the compiler a rename with no
// `was` is a removal plus an addition, and §18.2 passes both in silence; the
// baseline is the one place the PAIR is visible. Two independent edits in one
// commit are legitimate, so it warns and never refuses.
func TestRemovalAndAdditionWarnAsAPossibleRename(t *testing.T) {
	renamed := replace(t, "hits    int32", "strikes int32")
	got := diff(t, renamed, baseline.DefaultTokenPolicy)
	if !find(got, baseline.Warn, "table Config",
		"hits removed and strikes added in one edit — if that is a rename the wire id moved with the name and every stored value orphans") {
		t.Fatalf("a removal and an addition in one table must warn, got:%s", summary(got))
	}
	if !find(got, baseline.Warn, "table Config", "declare it `strikes ... | was = \"hits\"`, and pair `json = \"hits\"` if the text key must survive") {
		t.Errorf("the warning must name both remedies, got:%s", summary(got))
	}
	if refusals, _ := baseline.Split(got); len(refusals) != 0 {
		t.Errorf("two independent edits in one commit are legitimate: this warns, never refuses, got:%s", summary(refusals))
	}

	// THE NEGATIVE CONTROLS the issue names: each half alone is an ordinary
	// edit the wire absorbs, and must stay silent
	for _, tc := range []struct{ name, edited string }{
		{"a removal alone", replace(t, "    hits    int32\n", "")},
		{"an addition alone", replace(t, "    hits    int32", "    hits    int32\n    strikes int32")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if ctrl := diff(t, tc.edited, baseline.DefaultTokenPolicy); len(ctrl) != 0 {
				t.Errorf("half of the pair is not the pair; the baseline must be silent, got:%s", summary(ctrl))
			}
		})
	}
	// a rename that DECLARES its `was` keeps the id, so there is no pair to see
	declared := replace(t, "hits    int32", `strikes int32 | was = "hits", json = "hits"`)
	if ctrl := diff(t, declared, baseline.DefaultTokenPolicy); len(ctrl) != 0 {
		t.Errorf("a declared rename moves no id and draws no pair warning, got:%s", summary(ctrl))
	}
	// the ATTRIBUTION control
	if got := diff(t, renamed, without("field-pair")); find(got, baseline.Warn, "table Config", "removed and strikes added") {
		t.Errorf("with the \"field-pair\" rule removed the warning should be gone, got:%s", summary(got))
	}
}

// ---- the no-baseline notice (docs/SPEC-TABLES.md §18.1) ----

// TestNudge is #445. The baseline is opt-in — no file, no check — so the
// DEFAULT posture of a save-game unit is unguarded against every edit §4.1
// marks silent, and nothing said so. One line says it, and committing a
// baseline silences it.
func TestNudge(t *testing.T) {
	dir, paths := writeUnit(t, baseSrc)
	msg := baseline.Nudge(unit(t, baseSrc), paths)
	for _, want := range []string{
		"fixture declares 2 tables and",
		"holds no tables.baseline",
		"save-game evolution is unguarded (docs/SPEC-TABLES.md §18)",
		`commit one with: schema tables-baseline --update --reason "first baseline"`,
		dir,
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the notice must carry %q, got: %s", want, msg)
		}
	}
	if strings.Contains(msg, "\n") {
		t.Errorf("the notice is ONE line, got: %q", msg)
	}

	// COMMITTING A BASELINE SILENCES IT — the whole point of a nudge
	if _, _, err := baseline.Update(unit(t, baseSrc), paths, "the first baseline"); err != nil {
		t.Fatal(err)
	}
	if msg := baseline.Nudge(unit(t, baseSrc), paths); msg != "" {
		t.Errorf("a unit with a baseline is guarded and must be told nothing, got: %s", msg)
	}
}

// TestNudgeIsAboutTABLES: a unit that declares no table has no table wire to
// guard, so it is told nothing however long it lives without a baseline.
func TestNudgeIsAboutTables(t *testing.T) {
	const noTables = `package plain

type Vec3
{
    x float32
    y float32
    z float32
}
`
	_, paths := writeUnit(t, noTables)
	if msg := baseline.Nudge(unit(t, noTables), paths); msg != "" {
		t.Errorf("a unit with no tables has nothing to guard, got: %s", msg)
	}
}

// TestPreBumpBaselineRepairs is the version bump's own path, run on the shape
// every committed baseline in an estate takes the day a new judged token lands
// (#443's range facts are that token): the file is a rendering this compiler
// does not read, the check refuses and names `--update` as the remedy, and the
// remedy regenerates the projection in the CURRENT rendering while carrying
// every history line across verbatim.
func TestPreBumpBaselineRepairs(t *testing.T) {
	dir, paths := writeUnit(t, rangeSrc)
	path := filepath.Join(dir, baseline.FileName)

	// a baseline in the rendering BEFORE this one: the projection as it was
	// written, with no range facts in it
	stale := fmt.Sprintf(`schema-tables-baseline %d
package ranged

table Ship
    field hull id=0x80da8ccc11daadf6 kind=4 default=50

%s
### 2024-01-01 — the break we are never allowed to forget
- Ship.hull: specified default 10 -> 50 [refuse]
`, baseline.Version-1, baseline.HistoryHeading)
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	_, errs := baseline.Check(unit(t, rangeSrc), paths)
	if len(errs) != 1 {
		t.Fatalf("a baseline in an older rendering must refuse exactly once, got %v", errs)
	}
	for _, want := range []string{
		fmt.Sprintf("baseline version %d, this compiler writes %d", baseline.Version-1, baseline.Version),
		"--update --reason",
	} {
		if !strings.Contains(errs[0].Error(), want) {
			t.Errorf("the refusal must mention %q, got: %v", want, errs[0])
		}
	}

	if _, rewrote, err := baseline.Update(unit(t, rangeSrc), paths, "the range facts landed"); err != nil || !rewrote {
		t.Fatalf("the advertised remedy must work: %v (rewrote=%v)", err, rewrote)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		fmt.Sprintf("schema-tables-baseline %d", baseline.Version),
		"field hull id=0x80da8ccc11daadf6 kind=4 min=0 max=1000 default=50", // the new judged token
		"### 2024-01-01 — the break we are never allowed to forget",
		"- Ship.hull: specified default 10 -> 50 [refuse]", // salvaged verbatim
		"the range facts landed",
		"baseline REGENERATED over an unreadable one",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the repaired baseline must carry %q:\n%s", want, data)
		}
	}
	if warns, errs := baseline.Check(unit(t, rangeSrc), paths); len(warns) != 0 || len(errs) != 0 {
		t.Errorf("after the repair the check must pass: %v %v", warns, errs)
	}
}
