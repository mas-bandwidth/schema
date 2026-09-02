// The tables baseline's gate (SPEC-TABLES.md §16): one fixture pair per
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

flags Perks { Shielded, Cloaked, Turbo }

type Buff
{
    multiplier float32 = 1.0
}

type Debuff
{
    amount int32 = 0
}

// same field ids as Buff, plus one: the referent rule's "the ids ride" side
type BuffPlus
{
    multiplier float32 = 1.0
    stacks     int32 = 1
}

union Effect
{
    buff   Buff
    debuff Debuff
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
	if strings.Count(baseSrc, old) != 1 {
		t.Fatalf("fixture edit %q does not appear exactly once in the base schema", old)
	}
	return strings.Replace(baseSrc, old, new, 1)
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
			name:    "a specified default changed",
			edited:  replace(t, "damage  float32 = 21.0", "damage  float32 = 25.0"),
			where:   "Config.damage",
			what:    "specified default 21.0 -> 25.0",
			token:   "default",
			control: replace(t, "damage  float32 = 21.0", "damage  float32 = 21.0 | min = 0.0, max = 100.0, resolution = 0.01"),
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
			name: "a field's flags TYPE swapped for a differently-ordered flags declaration",
			edited: replace(t, "    perks   Perks",
				"    perks   Traits") + "\nflags Traits { Turbo, Shielded, Cloaked }\n",
			where:   "Config.perks",
			what:    "flags Perks -> Traits",
			token:   "flags",
			control: replace(t, "flags Perks { Shielded, Cloaked, Turbo }", "flags Perks { Shielded, Cloaked, Turbo, Hardened }"),
		},
		{
			name:    "a field's enum TYPE swapped for another vocabulary",
			edited:  replace(t, "    grade   Grade = Silver", "    grade   Tier = Copper") + "\nenum Tier { Copper, Brass }\n",
			where:   "Config.grade",
			what:    "enum Grade -> Tier",
			token:   "enum",
			control: replace(t, "enum Grade { Bronze, Silver, Gold }", "enum Grade { Bronze, Argent, Silver, Gold }"),
		},
		{
			// SPEC-TABLES.md §4 states outright that this edit is NOT a kind
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
			edited:  replace(t, "    boost   Buff", "    boost   Debuff"),
			where:   "Config.boost",
			what:    "nested table Buff -> Debuff",
			token:   "type",
			control: replace(t, "    boost   Buff", "    boost   BuffPlus"), // every id rides
		},
		{
			name:    "a union arm's payload swapped for one whose ids do not ride",
			edited:  replace(t, "    buff   Buff\n", "    buff   Debuff\n"),
			where:   "union Effect.buff",
			what:    "arm payload Buff -> Debuff",
			token:   "payload",
			control: replace(t, "    buff   Buff\n", "    buff   BuffPlus\n"), // every id rides
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
			warnWhat: "Ship carries 10 of its 10 identities",
			verdict:  baseline.Refuse,
			where:    "Ship.damage",
			what:     "specified default 21.0 -> 25.0",
		},
		{
			name: "a flags declaration renamed, and its bits reordered with it",
			edited: strings.NewReplacer(
				"flags Perks { Shielded, Cloaked, Turbo }", "flags Traits { Cloaked, Shielded, Turbo }",
				"perks   Perks", "perks   Traits").Replace(baseSrc),
			warnAt:   "flags Perks",
			warnWhat: "Traits carries 3 of its 3 identities",
			verdict:  baseline.Refuse,
			where:    "flags Traits",
			what:     "variant Shielded moved from bit 0 to bit 1",
		},
		{
			name: "an enum renamed, and a variant dropped with it",
			edited: strings.NewReplacer(
				"enum Grade { Bronze, Silver, Gold }", "enum Rank { Bronze, Silver }",
				"grade   Grade = Silver", "grade   Rank = Silver").Replace(baseSrc),
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
		{"a field renamed under was", replace(t, "hits    int32", "strikes int32 | was = \"hits\"")},
		{"a flags variant appended", replace(t, "flags Perks { Shielded, Cloaked, Turbo }", "flags Perks { Shielded, Cloaked, Turbo, Hardened }")},
		{"an enum variant inserted in the middle", replace(t, "enum Grade { Bronze, Silver, Gold }", "enum Grade { Bronze, Argent, Silver, Gold }")},
		{"an array bound grown", replace(t, "const Slots = 8", "const Slots = 64")},
		{"a string capacity grown", replace(t, "name    string(32)", "name    string(128)")},
		{"a bounded array made fixed", replace(t, "slots   [..Slots]int32", "slots   [Slots]int32")},
		{"a field moved under a guard", replace(t, "    hits    int32", "    guard   bool\n    if guard\n    {\n        hits int32\n    }")},
		{"a table added", baseSrc + "\ntable Extra\n{\n    x int32 = 1\n}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := diff(t, tc.edited, baseline.DefaultTokenPolicy); len(got) != 0 {
				t.Errorf("the wire absorbs this edit; the baseline must be silent, got:%s", summary(got))
			}
		})
	}
}

// TestEnumKeyedArrayHook is the enum-keyed array's rule, live before the
// construct is: the day `[E]T` lands the renderer emits one `key=` token and
// swapping the key enum is a refusal, because the policy row is already here.
// Synthesised, since no schema can spell the construct yet.
func TestEnumKeyedArrayHook(t *testing.T) {
	member := func(key string) *baseline.Unit {
		return &baseline.Unit{
			Version: baseline.Version,
			Package: "fixture",
			Tables: []baseline.Table{{Name: "Config", Fields: []baseline.Field{{
				Name: "slots", Id: 0x1234,
				Tokens: []baseline.Token{{Key: "kind", Value: "14"}, {Key: "elem", Value: "4"}, {Key: "key", Value: key}},
			}}}},
		}
	}
	got := baseline.Diff(member("Grade"), member("Tier"), baseline.DefaultTokenPolicy)
	if !find(got, baseline.Refuse, "Config.slots", "array key enum Grade -> Tier") {
		t.Fatalf("swapping an enum-keyed array's enum must be refused, got:%s", summary(got))
	}
	if got := baseline.Diff(member("Grade"), member("Tier"), without("key")); len(got) != 0 {
		t.Errorf("with the \"key\" rule removed the edit should pass, got:%s", summary(got))
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
		{"another unit's baseline", "schema-tables-baseline 1\npackage elsewhere\n", "baseline is for package elsewhere"},
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
