//go:build schema_leg_goose

// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// Scaffolded by `schema new-leg goose`; docs/PORT-GOOSE.md is the port's
// design. The schema_leg_goose tag keeps this target out of the default nine
// until the packet surface has claims in compiler/*_test.go;
// `go build -tags schema_leg_goose` is `schema generate --lang goose`. Drop
// the tag when the port joins the builtins.
package compiler

import (
	"maps"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type gooseTarget struct{}

// gooseWideTableKinds is set by the surface that carries the fixed-point and
// 128-bit table kinds (compiler/carries_goose_tablewire.go, §15); until then
// the target refuses them by name.
var gooseWideTableKinds bool

func (gooseTarget) Names() []string { return []string{"goose"} }

// Generate is the NO-LINEAGE case: a unit whose lock the caller did not read,
// or that has none. Every fixed table then carries the single entry it can
// always compute, its own (docs/FIXED-FORM-ALGORITHM.md §5.9 #1).
func (t gooseTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return t.GenerateLineage(u, opts, nil)
}

// GenerateLineage is the type surface (one module per schema file) and, for a unit
// that declares tables, the table surface (one <Base>Table.goose per file
// with tables) — the descriptors and constructors, no codec yet
// (docs/PORT-GOOSE.md, THE CARD MAP).
//
// THE SKELETON REFUSES EVERY CONSTRUCT IT DOES NOT CARRY YET, by name, through
// the shared refusal chain every port calls, the one `schema new-leg` writes
// (docs/PORT-GOOSE.md, THE CARRIES PATTERN): the wide table kinds and wide
// text; the bundle refuseUnported holds (string, bytes and flags defaults,
// was-rows, table-closure unions and their payload-free arms, arrays of
// unions, byte and string buffers); optional arrays, maps and unbounded
// arrays. The constructs keyed in compiler/carries.go are refused by the
// driver (compiler/generate.go) against the goose carries set, which
// compiler/carries_goose.go registers EMPTY. A later card that carries a
// construct joins that construct's registry from its OWN hook file
// (carryGoose(carryForms) as a package-level var, or registerListCarrier("goose")
// and its kin from an init) and never edits this file: each refusal below
// stands down by itself, because each reads its registry, not this chain.
//
// THE LINEAGE IS THE DRIVER'S (§5.9 #1): compiler/generate.go reads the unit's
// lock once (compiler/lineage.go) and hands the entries down as data, so
// goosetable opens no file and a build ships a reader for every layout the
// lock records, with its retirement floor.
func (gooseTarget) GenerateLineage(u *ir.Unit, _ Options, lineage *FixedLineage) (map[string][]byte, error) {
	if !gooseWideTableKinds {
		if err := ir.RefuseWideTableKinds(u, "goose"); err != nil {
			return nil, err
		}
	}
	if err := refuseWideText(u, "goose"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "goose"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "goose"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "goose"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "goose"); err != nil {
		return nil, err
	}
	files, err := goose.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := goosetable.GenerateLineage(u, gooseTableLineage(u, lineage))
	if err != nil {
		return nil, err
	}
	maps.Copy(files, tables)
	return files, nil
}

// gooseTableLineage is the lock's lineage in the Goose backend's own spelling.
// Nil in, nil out: a unit with no lock hands nothing. Each entry's Record is
// already the whole record (FixedLineage.Entries) and passes through as it
// stands.
func gooseTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]goosetable.FixedEvolutionEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]goosetable.FixedEvolutionEntry{}
	for _, st := range ir.TableFixedRoots(u) {
		for _, e := range lineage.Entries(st.Name) {
			out[st.Name] = append(out[st.Name], goosetable.FixedEvolutionEntry{
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
	registerBuiltin(gooseTarget{}, false, false, false, false)
}
