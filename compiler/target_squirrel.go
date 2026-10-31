// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// Scaffolded by `schema new-leg squirrel`; a built-in target since the port
// joined main (docs/PORT-SQUIRREL.md).
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type squirrelTarget struct{}

func (squirrelTarget) Names() []string { return []string{"squirrel"} }

// Generate emits the TYPE surface (internal/codegen/squirrel) for every unit
// and the TABLE surface (internal/codegen/squirreltable) for a unit that
// declares tables. Neither carries a codec yet; the refusals below are the
// skeleton's, each lifted by the card that carries its construct
// (docs/PORT-SQUIRREL.md, THE REGISTER).
func (t squirrelTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	// Wide table kinds (fixed(I, F), ufixed(I, F), int128, uint128) are carried
	// on the Squirrel variable wire, JSON text form, and fixed form
	// (docs/SPEC-TABLES.md §3, §15, §16.2; ir.WideTableKindScope in
	// ir/tablekind.go scopes §15's refusal to the form-1 accelerators). Wide
	// TEXT is carried too (registerWideTextCarrier below), so this call refuses
	// nothing for squirrel and stays as the chain every target runs.
	if err := refuseWideText(u, "squirrel"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "squirrel"); err != nil {
		return nil, err
	}
	// squirrel carries the optional array (compiler/target_squirrel_optionalarrays.go,
	// docs/SPEC-TABLES.md §2.3): the refuser below reads that registration and
	// rides for squirrel while still refusing an unregistered target.
	if err := refuseOptionalArrays(u, "squirrel"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "squirrel"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "squirrel"); err != nil {
		return nil, err
	}
	return t.generateSquirrel(u, nil)
}

// GenerateLineage is the second entry point of §5.9 #1: the driver read the
// unit's lock once (compiler/lineage.go) and hands the lineage down as DATA, so
// squirreltable opens no file and a build ships a reader for every layout the
// lock records.
func (t squirrelTarget) GenerateLineage(u *ir.Unit, opts Options, lineage *FixedLineage) (map[string][]byte, error) {
	// Wide table kinds (fixed(I, F), ufixed(I, F), int128, uint128) are carried
	// on the Squirrel variable wire, JSON text form, and fixed form
	// (docs/SPEC-TABLES.md §3, §15, §16.2; ir.WideTableKindScope in
	// ir/tablekind.go scopes §15's refusal to the form-1 accelerators). Wide
	// TEXT is carried too (registerWideTextCarrier below), so this call refuses
	// nothing for squirrel and stays as the chain every target runs.
	if err := refuseWideText(u, "squirrel"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "squirrel"); err != nil {
		return nil, err
	}
	// squirrel carries the optional array (compiler/target_squirrel_optionalarrays.go,
	// docs/SPEC-TABLES.md §2.3): the refuser below reads that registration and
	// rides for squirrel while still refusing an unregistered target.
	if err := refuseOptionalArrays(u, "squirrel"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "squirrel"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "squirrel"); err != nil {
		return nil, err
	}
	return t.generateSquirrel(u, lineage)
}

func (t squirrelTarget) generateSquirrel(u *ir.Unit, lineage *FixedLineage) (map[string][]byte, error) {
	files, err := squirrel.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := squirreltable.GenerateLineage(u, squirrelTableLineage(u, lineage))
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — the table module %s.nut and a schema file of that base collide; rename the file (docs/PORT-SQUIRREL.md)", name, squirreltable.ModuleName(u))
		}
		files[name] = data
	}
	return files, nil
}

// squirrelTableLineage is the lock's lineage in the Squirrel backend's own
// spelling. Nil in, nil out: a unit with no lock hands nothing.
func squirrelTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]squirreltable.FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]squirreltable.FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(u) {
		entries := lineage.Entries(st.Name)
		if len(entries) == 0 {
			continue
		}
		for _, e := range entries {
			out[st.Name] = append(out[st.Name], squirreltable.FixedLineageEntry{
				Wire:    e.Wire,
				Layout:  e.Layout,
				Record:  e.Record,
				Retired: e.Retired,
				Reason:  e.Reason,
			})
		}
	}
	return out
}

func init() {
	registerBuiltin(squirrelTarget{}, false, false, false, false)
	// wstring(N) on the packet wire and as table kind 33 in every Squirrel
	// form (internal/codegen/squirrel read.go/write.go, SPEC §4.12;
	// internal/codegen/squirreltable, docs/SPEC-TABLES.md §3, §3.3, §3.4, §16.2)
	registerWideTextCarrier("squirrel")
}
