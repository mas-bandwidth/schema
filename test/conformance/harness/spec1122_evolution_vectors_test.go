package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// SPEC ROWS ROOTS 1122, THE EVOLUTION AND FIXED-FORM READS.
//
// Two rules the json-hostile and framing rows of the same root set already
// owe the text and wire reads, owed again on the paths that do not run the
// text reader: the fixed form read by a build whose layout is the writer's,
// and the compiled plan a different writer's layout compiles.
//
//   - sp_enum_fixed_deprecated / sp_enum_evolved_deprecated: the writer holds
//     Mood.Old as a live variant and writes it in an array element; the reader
//     (tabledemo) has retired Old in place, so the element reads as the
//     element default, None (docs/SPEC-TABLES.md section 2.15). The evolved
//     row's writer also has the narrower bound [..3], so the reader compiles
//     a plan; the fixed row's has the reader's own bound.
//   - sp_count_min_evolved_*: the writer's CountedMin is [2..3] and the reader's
//     [2..5]. A count below the reader's minimum A refuses on the compiled plan
//     as it does everywhere (docs/SPEC.md section 4.6); the count at the
//     writer's own minimum and at its maximum read.
//
// The wires are fixtures this test rebuilds from the engine and holds the
// committed bytes to. `SP1122_WRITE=1 go test -run TestSpec1122EvolutionVectorsPinned
// ./test/conformance/harness` rewrites them, and `make conformance-generate`
// then writes the answers.

type spEvoRow struct {
	wire string
	root string
	// retire marks Old live in the writer's Mood; bound narrows Moods.row;
	// countB narrows CountedMin.few's maximum
	live  bool
	bound int64
	doc   string
	// forge sets the writer's used count to this after the write (0: none)
	forge uint64
}

var spEvoRows = []spEvoRow{
	{wire: "sp_enum_fixed_deprecated.bin", root: "Moods", live: true, doc: `{"row":["Calm","Old","Fierce"]}`},
	{wire: "sp_enum_evolved_deprecated.bin", root: "Moods", live: true, bound: 3, doc: `{"row":["Calm","Old","Fierce"]}`},
	{wire: "sp_count_min_evolved_at.bin", root: "CountedMin", bound: 3, doc: `{"few":[7,8]}`},
	{wire: "sp_count_min_evolved_max.bin", root: "CountedMin", bound: 3, doc: `{"few":[7,8,9]}`},
	{wire: "sp_count_min_evolved_below.bin", root: "CountedMin", bound: 3, doc: `{"few":[7,8]}`, forge: 1},
}

const spEvoDir = "testdata/conformance/tables/wire"

func spEvoWriter(m *Manifest, row spEvoRow) (*tabletext.Model, error) {
	unit, err := newUnits(m).get("tabledemo")
	if err != nil {
		return nil, err
	}
	if row.live {
		e := unit.Enums["Mood"]
		if e == nil {
			return nil, fmt.Errorf("tabledemo has no enum Mood")
		}
		for i := range e.VariantDeprecated {
			e.VariantDeprecated[i] = false
		}
	}
	if row.bound != 0 {
		st := unit.Tables[row.root]
		for _, f := range st.Fields {
			if f.Array == ir.ArrayCounted {
				f.ArrayBound = row.bound
			}
		}
	}
	return tabletext.NewModel(unit), nil
}

func spEvoBytes(m *Manifest, row spEvoRow) ([]byte, error) {
	w, err := spEvoWriter(m, row)
	if err != nil {
		return nil, err
	}
	v := w.New(w.Lookup(row.root))
	var rep tabletext.Report
	if !w.Read(v, []byte(row.doc), &rep) || rep.Refused {
		return nil, fmt.Errorf("%s: writer value does not read: %+v", row.wire, rep)
	}
	body, err := tablewire.Encode(w, v)
	if err != nil {
		return nil, err
	}
	if row.forge != 0 {
		// find the count by writing the same value one element longer
		longer := spEvoRow{root: row.root, bound: row.bound, doc: `{"few":[7,8,9]}`}
		lw, err := spEvoWriter(m, longer)
		if err != nil {
			return nil, err
		}
		lv := lw.New(lw.Lookup(row.root))
		if !lw.Read(lv, []byte(longer.doc), &rep) || rep.Refused {
			return nil, fmt.Errorf("longer: %+v", rep)
		}
		lb, err := tablewire.Encode(lw, lv)
		if err != nil || len(lb) != len(body) {
			return nil, fmt.Errorf("longer write: len %d vs %d err %v", len(lb), len(body), err)
		}
		pos := -1
		for i := range body {
			if body[i] == 2 && lb[i] == 3 {
				if pos >= 0 {
					return nil, fmt.Errorf("count byte is ambiguous at %d and %d", pos, i)
				}
				pos = i
			}
		}
		if pos < 0 {
			return nil, fmt.Errorf("count byte not found")
		}
		body[pos] = byte(row.forge)
	}
	return body, nil
}

func TestSpec1122EvolutionVectorsPinned(t *testing.T) {
	root := conformanceRoot(t)
	t.Chdir(root)
	m, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range spEvoRows {
		want, err := spEvoBytes(m, row)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(spEvoDir, row.wire)
		if os.Getenv("SP1122_WRITE") == "1" {
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from the engine's bytes (SP1122_WRITE=1 rewrites it)", path)
		}
	}
}
