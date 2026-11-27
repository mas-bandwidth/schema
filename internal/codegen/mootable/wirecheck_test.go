package mootable_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/wirealone"
)

// framingVerdicts reads every `framing` row of the conformance manifest
// through the emitted table_load.moo's ReadStrict, the variable form's one
// read (a fixed root reads the form-1 vector the way the harness reads it,
// test/conformance/harness/generate.go framingVariableUnit), with edit
// applied to each unit's files first when it is not nil. It answers each
// case's verdict, `read` or `refused`, and the verdict the row pins.
func framingVerdicts(t *testing.T, edit func(t *testing.T, g *generatedUnit)) (got, want map[string]string) {
	t.Helper()
	units, _ := readManifest(t)
	byUnit := map[string][][]string{}
	want = map[string]string{}
	for _, f := range manifestLines(t, "framing") {
		byUnit[f[2]] = append(byUnit[f[2]], f)
		want[f[1]] = f[5]
	}
	keys := make([]string, 0, len(byUnit))
	for k := range byUnit {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	got = map[string]string{}
	for _, key := range keys {
		g := generateUnit(t, units, key)
		if g.err != nil {
			t.Fatalf("unit %s: %v", key, g.err)
		}
		if edit != nil {
			edit(t, g)
		}
		var b strings.Builder
		b.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\nlocal L = require(\"./table_load\")\n" + g.requireAll())
		inputs := map[string][]byte{}
		for i, f := range byUnit[key] {
			name := fmt.Sprintf("w%d", i)
			inputs[name] = readRoot(t, f[4])
			fmt.Fprintf(&b, "do\n\tlocal rep = L.newReport()\n\tlocal v = L.ReadStrict(%q, inputs.%s, rep)\n\tprint(%q .. \" \" .. (if v ~= nil and not rep.damaged and rep.reason == nil then \"read\" else \"refused\"))\nend\n", f[3], name, f[1])
		}
		for line := range strings.SplitSeq(g.run(t, b.String(), inputs), "\n") {
			name, verdict, ok := strings.Cut(line, " ")
			if !ok {
				t.Fatalf("unit %s: probe line %q", key, line)
			}
			got[name] = verdict
		}
	}
	return got, want
}

// TestMooReadStrictAnswersEveryFramingRow: THE WIRE-ALONE WALK (§6.5;
// internal/tablewire/wirecheck.go). Every framing vector of the manifest,
// read through moo's one read, answers the verdict its row pins: damage
// inside a field this reader cannot name (ill-formed nested framing, a
// pointer past the node table), a node record no pointer reaches and a node
// table that cannot be read whole all refuse the load, as the Go engine's
// phase one refuses them.
func TestMooReadStrictAnswersEveryFramingRow(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_load.moo)")
	t.Parallel()
	got, want := framingVerdicts(t, nil)
	if len(want) == 0 {
		t.Fatal("the manifest names no framing row")
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: moo answers %q, the row pins %q", name, got[name], w)
		}
	}
	t.Logf("%d framing rows answer their verdicts", len(want))
}

// The control removes phase one, the wire-alone walk, from the emitted read:
// the rows only it refuses then read, and every other row keeps its verdict.
// An id-table entry no reference names is the walk's alone to refuse (§3); an
// explicit null pointer reads and rewrites (docs/rulings.json
// reads-refuse-non-canonical), so node_index_null is no row the walk owns.
// A union nested in unknown union arms past the framed depth bound is the
// walk's too: the walk bounds framed depth (internal/tablewire/wirecheck.go,
// ir.TableBinaryMaxDepth), and without it moo reads those rows.
func TestMooReadStrictWireCheckNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_load.moo)")
	t.Parallel()
	got, want := framingVerdicts(t, func(t *testing.T, g *generatedUnit) {
		removeMooDiscard(t, g, "table_load.moo", wirePhaseOne, wirePhaseOneOff)
	})
	// The rows the walk alone refuses are WIRE-ALONE.txt's
	// (testdata/conformance/tables, internal/wirealone): each breaks a
	// condition docs/SPEC-TABLES.md §6.5 lists as DECIDABLE FROM THE WIRE
	// ALONE, which the walk holds before the typed read decodes a byte, so the
	// typed read need not hold it again (§6.5, DETECTION ORDER IS FREE). moo
	// states only its exceptions, the rows its typed read refuses as well.
	walkOnly, err := wirealone.Owned(root, mooWireCheckExceptions)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range walkOnly {
		if want[name] != "refused" || got[name] != "read" {
			t.Errorf("%s: without the walk moo answers %q (the row pins %q); the control wants it read", name, got[name], want[name])
		}
	}
	if len(want) == 0 {
		t.Fatal("want is empty: the loop below would check nothing")
	}
	owned := map[string]bool{}
	for _, name := range walkOnly {
		owned[name] = true
	}
	for name, w := range want {
		if !owned[name] && got[name] != w {
			t.Errorf("%s: removing the walk moved a row it does not own: %q, the row pins %q", name, got[name], w)
		}
	}
}

// mooWireCheckExceptions are the rows of WIRE-ALONE.txt that moo's typed read
// refuses as well as its wire-alone walk, so they stay refused with the walk
// removed. Each carries the reason.
var mooWireCheckExceptions = map[string]string{
	"frame_repeat_last_wins": "the typed read refuses a field id twice in one body as it reads the body (table_load.moo, A FIELD ID TWICE IN ONE BODY IS DAMAGE)",
	"array_L0_inert":         "the typed read refuses bytes left inside an array's L after its elements (table_load.moo, BYTES LEFT INSIDE AN ARRAY'S L AFTER ITS ELEMENTS ARE DAMAGE)",
	"array_L1_inert":         "the typed read refuses bytes left inside an array's L after its elements (table_load.moo, BYTES LEFT INSIDE AN ARRAY'S L AFTER ITS ELEMENTS ARE DAMAGE)",
	"keyed_L1_inert":         "the typed read refuses bytes left inside a keyed array's L after its slots (table_load.moo, BYTES LEFT INSIDE A KEYED ARRAY'S L AFTER ITS SLOTS ARE DAMAGE)",
	"map_L1_inert":           "the typed read refuses bytes left inside a map's L after its entries (table_load.moo, BYTES LEFT INSIDE A MAP'S L AFTER ITS ENTRIES ARE DAMAGE)",
	"node_table_twice":       "the typed read refuses the node table's id twice in the root body (table_load.moo, A FIELD ID TWICE IN ONE BODY IS DAMAGE)",
}
