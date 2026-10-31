// docs/SPEC-TABLES.md §4.1's table is the RECONCILIATION of the three frames an
// edit passes through, and §4.1, §18.2 and §20.4 derive from it by citation.
// This is its golden: a fixture per row, or the row named in framesLeftOut
// with the reason it is not pinned yet, each edit run through the READ
// REPORT, the BASELINE check and the BUILD VERSION, with all three verdicts
// pinned. A pinned verdict the engine no longer produces fails here, and
// [TestEvolutionTableDocsAgreeWithTheGolden] holds the page to the same pins —
// so the §4.1 table can go red.
package baseline_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// framesSrc is the unit every row edits. It reaches each vocabulary the table
// judges: a by-value fixed table (the `T`/`*T`/`?T` rows), a flags declaration,
// an enum, and scalar, string and default facts.
const framesSrc = `package fixture

flags Perks { Shielded, Cloaked, Turbo }

fixed table Chunk { seq uint32 }

table Config
{
    chunk  Chunk
    perk   Perks
    damage float32 = 21.0
    grade  Grade = Silver
    hits   int32
    name   string(32)
}

enum Grade { Bronze, Silver, Gold }
`

// framesRichSrc is the unit the remaining rows edit: it reaches the
// vocabularies framesSrc does not — a union, a `type` held by value, a keyed
// array, a declared range, a text total, a fixed-point field, a map, an
// unbounded array, a pointer target and an `if` guard — plus the stand-ins the
// referent and map rows swap in (Boon, Pair), declared and unreferenced.
const framesRichSrc = `package fixture

enum Grade { Bronze, Silver, Gold }

type Buff
{
    multiplier float32 = 1.0
}

// Buff's TWIN: the same field id under a different specified default
type Boon
{
    multiplier float32 = 7.0
}

table Chunk
{
    seq uint32
}

fixed table Pair
{
    key   string(16)
    value int32
}

union Effect
{
    boost Buff
    ping
}

table Config
{
    buff    Buff
    effect  Effect
    grade   Grade
    tally   [Grade]int32
    level   int32 | min = 0, max = 100
    rules   [..4]string(16) | total = 64
    pos     fixed(16, 16) | min = -100, max = 100
    label   string(16)
    marks   [..4]int32
    lookup  map[string(16)]int32
    scores  []int32
    holder  *Chunk
    gate    bool
    extra   int32
}
`

// frameRow is one row of the §4.1 table: the edit, an instance written under
// the BASE schema, and the three pinned verdicts.
type frameRow struct {
	name     string
	row      string // a fragment naming exactly one §4.1 table row: the row this fixture pins
	base     string // the schema before the edit; framesSrc when empty
	edited   string // the full edited schema source
	instance string // the §16 text of an instance written BEFORE the edit
	read     string // unknown,kind_mismatch,widened,clamped,duplicate,malformed
	baseline string // pass | warn | refuse
	// hint is the one Warn finding, Where and What exactly, that a row whose
	// cell says the baseline PASSES and HINTS carries: the verdict is pass
	// only when that finding, and nothing else, comes back
	hint string
	// readHeldBy names the test that pins the row's read cell where the
	// engine's read frame does not reach it: the row's read is not run here
	readHeldBy string
	build      string // moves | stands
}

// richEdit is a row's substitutions over base, each of which must appear
// exactly once: a rename that a declaration and its uses share names every
// occurrence it moves, so a row cannot claim an edit it did not make.
func richEdit(t *testing.T, base string, pairs ...string) string {
	t.Helper()
	for i := 0; i+1 < len(pairs); i += 2 {
		base = editOf(t, base, pairs[i], pairs[i+1])
	}
	return base
}

// frameEdit is the one substitution a row makes, so a row cannot claim an edit
// it did not make.
func frameEdit(t *testing.T, old, new string) string {
	t.Helper()
	if strings.Count(framesSrc, old) != 1 {
		t.Fatalf("fixture edit %q does not appear exactly once in framesSrc", old)
	}
	return strings.Replace(framesSrc, old, new, 1)
}

// framesRead runs the READ REPORT frame: it writes an instance under the base
// schema and reads those bytes under the edited one.
func framesRead(t *testing.T, baseText, liveText, instance string) string {
	t.Helper()
	bm := tabletext.NewModel(unit(t, baseText))
	inst := bm.New(bm.Lookup("Config"))
	var placed tabletext.Report
	if !bm.Read(inst, []byte(instance), &placed) {
		t.Fatalf("the fixture instance did not place cleanly: %+v", placed)
	}
	wire, err := tablewire.Encode(bm, inst)
	if err != nil {
		t.Fatalf("the base instance did not encode: %v", err)
	}
	lm := tabletext.NewModel(unit(t, liveText))
	back := lm.New(lm.Lookup("Config"))
	var r tabletext.Report
	if _, err := tablewire.Decode(lm, back, wire, &r); err != nil {
		t.Fatalf("the edited schema did not decode the wire: %v", err)
	}
	return fmt.Sprintf("%d,%d,%d,%d,%d,%t", r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed)
}

// framesBaseline runs the BASELINE frame: the committed projection taken
// before the edit against the live one, under the shipping policy.
func framesBaseline(t *testing.T, baseText, liveText, hint string) string {
	t.Helper()
	findings := baseline.Diff(committed(t, baseText), baseline.Render(unit(t, liveText)), baseline.DefaultTokenPolicy)
	refusals, warnings := baseline.Split(findings)
	if hint != "" && len(refusals) == 0 {
		if len(warnings) == 1 && warnings[0].String() == hint {
			return "pass"
		}
		return fmt.Sprintf("warn %q", warnings)
	}
	switch {
	case len(refusals) > 0:
		return "refuse"
	case len(warnings) > 0:
		return "warn"
	}
	return "pass"
}

// framesBuild runs the BUILD VERSION frame: whether the edit moves the cook
// projection's id.
func framesBuild(t *testing.T, baseText, liveText string) string {
	t.Helper()
	if ir.BuildVersion(unit(t, baseText)) != ir.BuildVersion(unit(t, liveText)) {
		return "moves"
	}
	return "stands"
}

// frameRows is the golden's fixtures, one or more per §4.1 row.
func frameRows(t *testing.T) []frameRow {
	t.Helper()
	return []frameRow{
		{
			name:     "a field moved to *T",
			row:      "a field moved to or from `*T`",
			edited:   frameEdit(t, "    chunk  Chunk\n", "    chunk  *Chunk\n"),
			instance: `{"chunk":{"seq":7}}`,
			read:     "0,1,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "a field moved to ?T",
			row:      "a field moved between `T` and `?T`",
			edited:   frameEdit(t, "    chunk  Chunk\n", "    chunk  ?Chunk\n"),
			instance: `{"chunk":{"seq":7}}`,
			read:     "0,0,0,0,0,false",
			baseline: "pass",
			build:    "moves",
		},
		{
			name:     "a scalar kind changed",
			row:      "a field's wire KIND, or an array's ELEMENT kind, changed",
			edited:   frameEdit(t, "    hits   int32\n", "    hits   bool\n"),
			instance: `{"hits":5}`,
			read:     "0,1,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "a scalar kind widened",
			row:      "kind WIDENED",
			edited:   frameEdit(t, "    hits   int32\n", "    hits   int64\n"),
			instance: `{"hits":5}`,
			read:     "0,0,1,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "a specified default changed",
			row:      "a specified DEFAULT changed",
			edited:   frameEdit(t, "damage float32 = 21.0", "damage float32 = 25.0"),
			instance: `{}`,
			read:     "0,0,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "a field added",
			row:      "a field added, removed or reordered",
			edited:   frameEdit(t, "    hits   int32\n", "    hits   int32\n    bonus  int32\n"),
			instance: `{}`,
			read:     "0,0,0,0,0,false",
			baseline: "pass",
			build:    "moves",
		},
		{
			name:     "a field removed",
			row:      "a field added, removed or reordered",
			edited:   frameEdit(t, "    hits   int32\n", ""),
			instance: `{"hits":3}`,
			read:     "1,0,0,0,0,false",
			baseline: "pass",
			build:    "moves",
		},
		{
			name:     "a field reordered",
			row:      "a field added, removed or reordered",
			edited:   frameEdit(t, "    chunk  Chunk\n    perk   Perks\n    damage float32 = 21.0\n    grade  Grade = Silver\n    hits   int32\n    name   string(32)\n", "    name   string(32)\n    hits   int32\n    grade  Grade = Silver\n    damage float32 = 21.0\n    perk   Perks\n    chunk  Chunk\n"),
			instance: `{"chunk":{"seq":7},"hits":3}`,
			read:     "0,0,0,0,0,false",
			baseline: "pass",
			build:    "moves",
		},
		{
			name:     "a flags variant inserted",
			row:      "a FLAGS variant inserted",
			edited:   frameEdit(t, "flags Perks { Shielded, Cloaked, Turbo }", "flags Perks { Shielded, Hardened, Cloaked, Turbo }"),
			instance: `{"perk":["Cloaked"]}`,
			read:     "0,0,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "a flags variant appended",
			row:      "a FLAGS variant inserted",
			edited:   frameEdit(t, "flags Perks { Shielded, Cloaked, Turbo }", "flags Perks { Shielded, Cloaked, Turbo, Hardened }"),
			instance: `{"perk":["Cloaked"]}`,
			read:     "0,0,0,0,0,false",
			baseline: "pass",
			build:    "moves",
		},
		{
			name:     "an enum variant added",
			row:      "an `enum`'s or a `union`'s variant order or names moved",
			edited:   frameEdit(t, "enum Grade { Bronze, Silver, Gold }", "enum Grade { Bronze, Silver, Gold, Platinum }"),
			instance: `{"grade":"Gold"}`,
			read:     "0,0,0,0,0,false",
			baseline: "pass",
			build:    "moves",
		},
		{
			name:     "an enum variant removed",
			row:      "an `enum`'s or a `union`'s variant order or names moved",
			edited:   frameEdit(t, "enum Grade { Bronze, Silver, Gold }", "enum Grade { Bronze, Silver }"),
			instance: `{"grade":"Gold"}`,
			read:     "1,0,0,0,0,false",
			baseline: "warn",
			build:    "moves",
		},

		// the rows below edit framesRichSrc (or a base derived from it, where
		// the row is a SECOND rename and its base carries the first)
		{
			name:     "a field's referent swapped for one that cannot stand in",
			row:      "a field's REFERENT dropped",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "    buff    Buff\n", "    buff    Boon\n"),
			instance: `{"buff":{}}`,
			read:     "0,0,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "an array changed from keyed to positional",
			row:      "an array changed between keyed and positional",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "    tally   [Grade]int32\n", "    tally   [..3]int32\n"),
			instance: `{"tally":{"Gold":2}}`,
			read:     "0,1,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "a field changed from an enum to its raw integer",
			row:      "between an `enum` and its raw integer",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "    grade   Grade\n", "    grade   uint8\n"),
			instance: `{"grade":"Gold"}`,
			read:     "0,1,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "a union arm's declared type changed",
			row:      "a union ARM's declared TYPE changed",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "    boost Buff\n", "    boost int32\n"),
			instance: `{"effect":{"boost":{"multiplier":2.0}}}`,
			read:     "0,1,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "a declared range tightened",
			row:      "a declared RANGE tightened",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "| min = 0, max = 100", "| min = 0, max = 50"),
			instance: `{"level":80}`,
			read:     "0,0,0,1,0,false",
			baseline: "warn",
			build:    "moves",
		},
		{
			name:     "a fixed field's F moved under the same storage width",
			row:      "a fixed field's `F` moved",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "    pos     fixed(16, 16) | min = -100, max = 100\n", "    pos     fixed(20, 12) | min = -100, max = 100\n"),
			instance: `{"pos":1.5}`,
			read:     "0,0,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "a field renamed a second time, was naming the intermediate spelling",
			row:      "a field renamed a SECOND time",
			base:     richEdit(t, framesRichSrc, "    label   string(16)\n", "    title   string(16) | was = \"label\"\n"),
			edited:   richEdit(t, framesRichSrc, "    label   string(16)\n", "    heading string(16) | was = \"title\"\n"),
			instance: `{"title":"x"}`,
			read:     "1,0,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "an array bound shrunk",
			row:      "an array BOUND or a string/`bytes` capacity moved",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "    marks   [..4]int32\n", "    marks   [..2]int32\n"),
			instance: `{"marks":[1,2,3]}`,
			read:     "0,0,0,1,0,false",
			baseline: "warn",
			build:    "moves",
		},
		{
			name:     "a map's key kind changed",
			row:      "a MAP's KEY kind changed",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "map[string(16)]int32", "map[uint32]int32"),
			instance: `{"lookup":{"a":1}}`,
			read:     "0,1,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "a map's key bound tightened",
			row:      "a MAP's KEY BOUND tightened",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "map[string(16)]int32", "map[string(4)]int32"),
			instance: `{"lookup":{"abcdefgh":1,"b":2}}`,
			read:     "0,0,0,1,0,false",
			baseline: "warn",
			build:    "moves",
		},
		{
			name:     "a map changed to [..N]Pair",
			row:      "a MAP changed to or from `[..N]Pair`",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "    lookup  map[string(16)]int32\n", "    lookup  [..4]Pair\n"),
			instance: `{"lookup":{"a":1,"b":2}}`,
			read:     "0,0,0,0,0,false",
			baseline: "warn",
			build:    "moves",
		},
		{
			name:     "a map changed to a scalar",
			row:      "a MAP changed to or from `[E]T`",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "    lookup  map[string(16)]int32\n", "    lookup  int32\n"),
			instance: `{"lookup":{"a":1}}`,
			read:     "0,1,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "an unbounded array given a bound its count passes",
			row:      "an array changed between `[]T` and `[..N]T`",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "    scores  []int32\n", "    scores  [..2]int32\n"),
			instance: `{"scores":[1,2,3]}`,
			read:     "0,0,0,1,0,false",
			baseline: "warn",
			build:    "moves",
		},
		{
			name:     "an if guard added",
			row:      "an `if` GUARD added or removed",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "    extra   int32\n", "    if gate\n    {\n        extra int32\n    }\n"),
			instance: `{"gate":true,"extra":3}`,
			read:     "0,0,0,0,0,false",
			baseline: "pass",
			build:    "stands",
		},
		{
			name:     "a type held by value renamed",
			row:      "a DECLARATION renamed",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "type Buff\n", "type Bonus\n", "    buff    Buff\n", "    buff    Bonus\n", "    boost Buff\n", "    boost Bonus\n"),
			instance: `{"buff":{"multiplier":2.0}}`,
			read:     "0,0,0,0,0,false",
			baseline: "warn",
			build:    "moves",
		},
		{
			name:     "a table renamed where it is a pointer target",
			row:      "a TABLE renamed where it is a POINTER TARGET",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "table Chunk\n", "table Block\n", "    holder  *Chunk\n", "    holder  *Block\n"),
			instance: `{"holder":{"seq":3}}`,
			read:     "1,0,0,0,0,false",
			baseline: "warn",
			build:    "moves",
		},
		{
			name:     "a table renamed under was",
			row:      "a TABLE renamed under `was`",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "table Chunk\n", "table Block | was = \"Chunk\"\n", "    holder  *Chunk\n", "    holder  *Block\n"),
			instance: `{"holder":{"seq":3}}`,
			read:     "0,0,0,0,0,false",
			baseline: "pass",
			build:    "stands",
		},
		{
			name:     "a table renamed a second time, was naming the intermediate spelling",
			row:      "a TABLE renamed a SECOND time",
			base:     richEdit(t, framesRichSrc, "table Chunk\n", "table Block | was = \"Chunk\"\n", "    holder  *Chunk\n", "    holder  *Block\n"),
			edited:   richEdit(t, framesRichSrc, "table Chunk\n", "table Slab | was = \"Block\"\n", "    holder  *Chunk\n", "    holder  *Slab\n"),
			instance: `{"holder":{"seq":3}}`,
			read:     "1,0,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "a type's field renamed bare",
			row:      "a `type`'s FIELD renamed BARE",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "    multiplier float32 = 1.0\n", "    mult float32 = 1.0\n"),
			instance: `{"buff":{"multiplier":2.0}}`,
			read:     "1,0,0,0,0,false",
			baseline: "warn",
			build:    "moves",
		},
		{
			name:     "an enum variant renamed under was",
			row:      "an enum VARIANT or a union ARM renamed under `was`",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "enum Grade { Bronze, Silver, Gold }", "enum Grade\n{\n    Bronze,\n    Argent | was = \"Silver\"\n    Gold\n}"),
			instance: `{"grade":"Silver"}`,
			read:     "0,0,0,0,0,false",
			baseline: "pass",
			build:    "stands",
		},
		{
			name:     "an enum variant renamed bare",
			row:      "an enum VARIANT or a union ARM renamed BARE",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "enum Grade { Bronze, Silver, Gold }", "enum Grade { Bronze, Argent, Gold }"),
			instance: `{"grade":"Silver"}`,
			read:     "1,0,0,0,0,false",
			baseline: "warn",
			build:    "moves",
		},
		{
			name:     "an enum variant renamed a second time, was naming the intermediate spelling",
			row:      "a VARIANT or an ARM renamed a SECOND time",
			base:     richEdit(t, framesRichSrc, "enum Grade { Bronze, Silver, Gold }", "enum Grade\n{\n    Bronze,\n    Argent | was = \"Silver\"\n    Gold\n}"),
			edited:   richEdit(t, framesRichSrc, "enum Grade { Bronze, Silver, Gold }", "enum Grade\n{\n    Bronze,\n    Silvered | was = \"Argent\"\n    Gold\n}"),
			instance: `{"grade":"Argent"}`,
			read:     "1,0,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:       "a text list's total tightened",
			row:        "a text list's `total` tightened",
			base:       framesRichSrc,
			edited:     richEdit(t, framesRichSrc, "| total = 64", "| total = 8"),
			instance:   `{"rules":["abcdef","ghijkl"]}`,
			readHeldBy: "internal/codegen/gotable TestTextListTotal",
			baseline:   "warn",
			build:      "moves",
		},
		{
			name:     "a field renamed under was",
			row:      "a field renamed under `was`",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "    label   string(16)\n", "    title   string(16) | was = \"label\"\n"),
			instance: `{"label":"x"}`,
			read:     "0,0,0,0,0,false",
			baseline: "pass",
			hint:     "Config.title: renamed under was = \"label\", which keeps the wire id and NOT the text key — the JSON key is now \"title\"; pair json = \"label\" if an existing text must still read (docs/SPEC-TABLES.md §16.4)",
			build:    "stands",
		},
		{
			name:     "a type's field renamed under was",
			row:      "a `type`'s FIELD renamed under `was`",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "    multiplier float32 = 1.0\n", "    mult float32 = 1.0 | was = \"multiplier\"\n"),
			instance: `{"buff":{"multiplier":2.0}}`,
			read:     "0,0,0,0,0,false",
			baseline: "pass",
			hint:     "Buff.mult: renamed under was = \"multiplier\", which keeps the wire id and NOT the text key — the JSON key is now \"mult\"; pair json = \"multiplier\" if an existing text must still read (docs/SPEC-TABLES.md §16.4)",
			build:    "stands",
		},
		{
			name:     "an unbounded array's element retyped",
			row:      "an unbounded array's ELEMENT retyped",
			base:     framesRichSrc,
			edited:   richEdit(t, framesRichSrc, "    scores  []int32\n", "    scores  []float32\n"),
			instance: `{"scores":[1,2,3]}`,
			read:     "0,1,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
	}
}

// heldBy reports whether "<dir> <TestName>" names a test function in a
// _test.go file of that directory, relative to the repository root.
func heldBy(t *testing.T, ref string) bool {
	t.Helper()
	dir, name, ok := strings.Cut(ref, " ")
	if !ok {
		t.Fatalf("readHeldBy %q is not \"<dir> <TestName>\"", ref)
	}
	files, err := filepath.Glob(filepath.Join("../..", dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "\nfunc "+name+"(t *testing.T) {") {
			return true
		}
	}
	return false
}

// TestEvolutionTableFrames is the golden behind §4.1's table.
func TestEvolutionTableFrames(t *testing.T) {
	for _, row := range frameRows(t) {
		t.Run(row.name, func(t *testing.T) {
			base := row.base
			if base == "" {
				base = framesSrc
			}
			if row.readHeldBy == "" {
				if got := framesRead(t, base, row.edited, row.instance); got != row.read {
					t.Errorf("read report = %s, want %s", got, row.read)
				}
			} else if !heldBy(t, row.readHeldBy) {
				t.Errorf("the read cell is held by %s, which no longer exists", row.readHeldBy)
			}
			if got := framesBaseline(t, base, row.edited, row.hint); got != row.baseline {
				t.Errorf("baseline = %s, want %s", got, row.baseline)
			}
			if got := framesBuild(t, base, row.edited); got != row.build {
				t.Errorf("build version = %s, want %s", got, row.build)
			}
		})
	}
}

// TestEvolutionTableDocsAgreeWithTheGolden holds the page to the golden. The
// §4.1 table is the reconciliation, so a cell that disagrees with the frame it
// names is the defect #446 exists to catch; the same pins the rows above carry
// are read back here out of docs/SPEC-TABLES.md.
func TestEvolutionTableDocsAgreeWithTheGolden(t *testing.T) {
	page, err := os.ReadFile("../../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(page)

	// the T -> *T row: read kind_mismatch, baseline REFUSES (the pointer is
	// kind 17, an ordinary kind change), build moves.
	row := tableRow(t, text, "moved to or from `*T`")
	if cell := tableCell(t, row, 2); !strings.Contains(cell, "kind_mismatch") {
		t.Errorf("the T -> *T row's read report cell is %q, want kind_mismatch", cell)
	}
	if cell := tableCell(t, row, 3); !strings.Contains(cell, "refuse") {
		t.Errorf("the T -> *T row's baseline cell is %q, want refuses — the pointer is kind 17, so the baseline refuses it as any kind change", cell)
	}
	if cell := tableCell(t, row, 4); !strings.Contains(cell, "moves") {
		t.Errorf("the T -> *T row's build version cell is %q, want moves", cell)
	}

	// §18.2's PASSES list restates the same rows: T and ?T are one framing,
	// and *T is NOT — it is the kind change the REFUSES list names. The check
	// is scoped to that paragraph, because §20.4's MOVES list correctly names
	// all three.
	passes := text
	if i := strings.Index(passes, "**PASSES, in silence**"); i >= 0 {
		passes = passes[i:]
	} else {
		t.Fatal("§18.2 no longer carries a PASSES list")
	}
	if i := strings.Index(passes, "**The BLOCK FORM"); i >= 0 {
		passes = passes[:i]
	}
	if !strings.Contains(passes, "`T` and `?T`") {
		t.Error("§18.2's PASSES list no longer states that `T` and `?T` are one framing")
	}
	if strings.Contains(passes, "`T`, `?T` and `*T`") {
		t.Error("§18.2's PASSES list still names `*T` beside `T` and `?T`; the pointer is a kind change and belongs to REFUSES")
	}
}

// tableRow finds the one markdown table line containing fragment.
func tableRow(t *testing.T, text, fragment string) string {
	t.Helper()
	var found []string
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "|") && strings.Contains(line, fragment) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("found %d table rows containing %q, want exactly one", len(found), fragment)
	}
	return found[0]
}

// tableCell is the nth cell (0 the leading empty split) of a markdown table row.
func tableCell(t *testing.T, row string, n int) string {
	t.Helper()
	cells := strings.Split(row, "|")
	if n >= len(cells) {
		t.Fatalf("row %q has no cell %d", row, n)
	}
	return strings.TrimSpace(cells[n])
}

// framesLeftOut names the §4.1 rows the golden does not pin yet, each with the
// reason. A row lands here only when its spec cell and the engine disagree, or
// the cell does not say which verdict the frame returns: the fixture is owed,
// and the contract is not this file's to settle.
var framesLeftOut = map[string]string{}

// evolutionTableRows is §4.1's table, one line per row, header and rule
// excluded.
func evolutionTableRows(t *testing.T, text string) []string {
	t.Helper()
	const header = "| the edit | the read report | the baseline | the build version |"
	i := strings.Index(text, header)
	if i < 0 {
		t.Fatal("docs/SPEC-TABLES.md no longer carries §4.1's evolution table header")
	}
	var rows []string
	for n, line := range strings.Split(text[i:], "\n") {
		if n < 2 {
			continue // the header and the rule
		}
		if !strings.HasPrefix(line, "|") {
			break
		}
		rows = append(rows, line)
	}
	return rows
}

// evolutionRow is the one §4.1 row containing fragment.
func evolutionRow(t *testing.T, rows []string, fragment string) string {
	t.Helper()
	var found []string
	for _, row := range rows {
		if strings.Contains(row, fragment) {
			found = append(found, row)
		}
	}
	if len(found) != 1 {
		t.Fatalf("found %d §4.1 rows containing %q, want exactly one", len(found), fragment)
	}
	return found[0]
}

// uncoveredRows is every table row no fixture names and framesLeftOut does not
// excuse, and every excuse that names no row.
func uncoveredRows(t *testing.T, text string, fixtures []frameRow) []string {
	t.Helper()
	rows := evolutionTableRows(t, text)
	covered := map[string]bool{}
	for _, f := range fixtures {
		covered[evolutionRow(t, rows, f.row)] = true
	}
	for fragment := range framesLeftOut {
		covered[evolutionRow(t, rows, fragment)] = true
	}
	var out []string
	for _, row := range rows {
		if !covered[row] {
			out = append(out, row)
		}
	}
	return out
}

// TestEvolutionTableEveryRowHasAFixture holds the golden to the table's
// length: every §4.1 row is named by at least one fixture or left out by name
// with its reason, so a row added to the page without a fixture goes red here.
func TestEvolutionTableEveryRowHasAFixture(t *testing.T) {
	page, err := os.ReadFile("../../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(page)
	fixtures := frameRows(t)
	for _, row := range uncoveredRows(t, text, fixtures) {
		t.Errorf("§4.1 row has no fixture and no named reason: %s", row)
	}
	rows := evolutionTableRows(t, text)
	for fragment := range framesLeftOut {
		for _, f := range fixtures {
			if evolutionRow(t, rows, f.row) == evolutionRow(t, rows, fragment) {
				t.Errorf("%q is both pinned by %q and left out; drop it from framesLeftOut", fragment, f.name)
			}
		}
	}

	// THE NEGATIVE CONTROL: drop the one fixture a row has and the check must
	// name that row.
	var sole []frameRow
	for _, f := range fixtures {
		if f.row != "a MAP's KEY kind changed" {
			sole = append(sole, f)
		}
	}
	if len(sole) == len(fixtures) {
		t.Fatal("the control's fixture is gone; re-anchor it to a row with a single fixture")
	}
	missing := uncoveredRows(t, text, sole)
	if len(missing) != 1 || !strings.Contains(missing[0], "a MAP's KEY kind changed") {
		t.Errorf("with the map key kind fixture removed the check names %q, want that row alone", missing)
	}
}
