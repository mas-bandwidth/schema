// docs/SPEC-TABLES.md §4.1's table is the RECONCILIATION of the three frames an
// edit passes through, and §4.1, §18.2 and §20.4 derive from it by citation.
// This is its golden: one fixture per row, each edit run through the READ
// REPORT, the BASELINE check and the BUILD VERSION, with all three verdicts
// pinned. A pinned verdict the engine no longer produces fails here, and
// [TestEvolutionTableDocsAgreeWithTheGolden] holds the page to the same pins —
// so the §4.1 table can go red.
package baseline_test

import (
	"fmt"
	"os"
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

// frameRow is one row of the §4.1 table: the edit, an instance written under
// the BASE schema, and the three pinned verdicts.
type frameRow struct {
	name     string
	edited   string // the full edited schema source
	instance string // the §16 text of an instance written BEFORE the edit
	read     string // unknown,kind_mismatch,widened,clamped,duplicate,malformed
	baseline string // pass | warn | refuse
	build    string // moves | stands
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
func framesBaseline(t *testing.T, baseText, liveText string) string {
	t.Helper()
	findings := baseline.Diff(committed(t, baseText), baseline.Render(unit(t, liveText)), baseline.DefaultTokenPolicy)
	refusals, warnings := baseline.Split(findings)
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

// TestEvolutionTableFrames is the golden behind §4.1's table.
func TestEvolutionTableFrames(t *testing.T) {
	rows := []frameRow{
		{
			name:     "a field moved to *T",
			edited:   frameEdit(t, "    chunk  Chunk\n", "    chunk  *Chunk\n"),
			instance: `{"chunk":{"seq":7}}`,
			read:     "0,1,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "a field moved to ?T",
			edited:   frameEdit(t, "    chunk  Chunk\n", "    chunk  ?Chunk\n"),
			instance: `{"chunk":{"seq":7}}`,
			read:     "0,0,0,0,0,false",
			baseline: "pass",
			build:    "moves",
		},
		{
			name:     "a scalar kind changed",
			edited:   frameEdit(t, "    hits   int32\n", "    hits   bool\n"),
			instance: `{"hits":5}`,
			read:     "0,1,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "a scalar kind widened",
			edited:   frameEdit(t, "    hits   int32\n", "    hits   int64\n"),
			instance: `{"hits":5}`,
			read:     "0,0,1,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "a specified default changed",
			edited:   frameEdit(t, "damage float32 = 21.0", "damage float32 = 25.0"),
			instance: `{}`,
			read:     "0,0,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "a field added",
			edited:   frameEdit(t, "    hits   int32\n", "    hits   int32\n    bonus  int32\n"),
			instance: `{}`,
			read:     "0,0,0,0,0,false",
			baseline: "pass",
			build:    "moves",
		},
		{
			name:     "a field removed",
			edited:   frameEdit(t, "    hits   int32\n", ""),
			instance: `{"hits":3}`,
			read:     "1,0,0,0,0,false",
			baseline: "pass",
			build:    "moves",
		},
		{
			name:     "a field reordered",
			edited:   frameEdit(t, "    chunk  Chunk\n    perk   Perks\n    damage float32 = 21.0\n    grade  Grade = Silver\n    hits   int32\n    name   string(32)\n", "    name   string(32)\n    hits   int32\n    grade  Grade = Silver\n    damage float32 = 21.0\n    perk   Perks\n    chunk  Chunk\n"),
			instance: `{"chunk":{"seq":7},"hits":3}`,
			read:     "0,0,0,0,0,false",
			baseline: "pass",
			build:    "moves",
		},
		{
			name:     "a flags variant inserted",
			edited:   frameEdit(t, "flags Perks { Shielded, Cloaked, Turbo }", "flags Perks { Shielded, Hardened, Cloaked, Turbo }"),
			instance: `{"perk":["Cloaked"]}`,
			read:     "0,0,0,0,0,false",
			baseline: "refuse",
			build:    "moves",
		},
		{
			name:     "a flags variant appended",
			edited:   frameEdit(t, "flags Perks { Shielded, Cloaked, Turbo }", "flags Perks { Shielded, Cloaked, Turbo, Hardened }"),
			instance: `{"perk":["Cloaked"]}`,
			read:     "0,0,0,0,0,false",
			baseline: "pass",
			build:    "moves",
		},
		{
			name:     "an enum variant added",
			edited:   frameEdit(t, "enum Grade { Bronze, Silver, Gold }", "enum Grade { Bronze, Silver, Gold, Platinum }"),
			instance: `{"grade":"Gold"}`,
			read:     "0,0,0,0,0,false",
			baseline: "pass",
			build:    "moves",
		},
		{
			name:     "an enum variant removed",
			edited:   frameEdit(t, "enum Grade { Bronze, Silver, Gold }", "enum Grade { Bronze, Silver }"),
			instance: `{"grade":"Gold"}`,
			read:     "1,0,0,0,0,false",
			baseline: "warn",
			build:    "moves",
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			if got := framesRead(t, framesSrc, row.edited, row.instance); got != row.read {
				t.Errorf("read report = %s, want %s", got, row.read)
			}
			if got := framesBaseline(t, framesSrc, row.edited); got != row.baseline {
				t.Errorf("baseline = %s, want %s", got, row.baseline)
			}
			if got := framesBuild(t, framesSrc, row.edited); got != row.build {
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
	for _, line := range strings.Split(text, "\n") {
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
