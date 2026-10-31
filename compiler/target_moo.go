//go:build schema_leg_moo

// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// Scaffolded by `schema new-leg moo`. The schema_leg_moo tag keeps this
// target out of the default nine until the packet surface has claims in
// compiler/*_test.go; `go build -tags schema_leg_moo` is `schema generate --lang moo`.
// Drop the tag when the port joins the builtins.
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type mooTarget struct{}

func (mooTarget) Names() []string { return []string{"moo"} }

// Generate is the packet surface (one module per schema file,
// internal/codegen/moo) and, for a unit that declares tables, the table
// surface (internal/codegen/mootable): one <Base>Table.moo per file with
// tables and the table runtime beside them. A table-free unit's output is the
// packet surface's alone.
//
// THE VARIABLE WIRE CARRIES EVERY CONSTRUCT REGISTERED BELOW, each claimed by
// a named test against the Go reference engine
// (internal/codegen/mootable/construct_test.go,
// TestMooTableConstructsMatchTheGoEngine; the conformance corpus,
// TestMooTableSaveRoundTripsEveryInstance and
// TestMooTableLoadMatchesTheReports). What is not registered is refused here
// or in compiler/generate.go by name.
func (t mooTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return t.GenerateLineage(u, opts, nil)
}

// GenerateLineage is the second entry point of docs/FIXED-FORM-ALGORITHM.md
// §5.9 #1: the driver read the unit's lock once (compiler/lineage.go) and
// hands the fixed form's lineage down as DATA, so mootable opens no file and
// a build ships a reader for every layout the lock records.
func (mooTarget) GenerateLineage(u *ir.Unit, _ Options, lineage *FixedLineage) (map[string][]byte, error) {
	if err := refuseWideText(u, "moo"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "moo"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "moo"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "moo"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "moo"); err != nil {
		return nil, err
	}
	files, err := moo.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := mootable.GenerateLineage(u, mooTableLineage(u, lineage))
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file named <X>Table.schema beside <X>.schema with tables collides on the table module; rename one file (docs/SPEC-TABLES.md)", name)
		}
		files[name] = data
	}
	return files, nil
}

// mooTableLineage is the lock's lineage in the moo backend's own spelling.
// Nil in, nil out: a unit with no lock hands nothing.
func mooTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]mootable.FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]mootable.FixedLineageEntry{}
	for _, st := range ir.TableFixedFormRoots(u) {
		for _, e := range lineage.Entries(st.Name) {
			out[st.Name] = append(out[st.Name], mootable.FixedLineageEntry{
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
	// tables, a union with table arms, an array of unions and both byte
	// buffers ride the variable wire (construct_test.go: unions_maps_keyed,
	// blobs_pointers)
	registerBuiltin(mooTarget{}, true, true, true, true)
	// maps, []T and ?[N]T (construct_test.go: unions_maps_keyed,
	// optional_map, text_lists; the conformance corpus)
	registerMapCarrier("moo")
	registerListCarrier("moo")
	registerOptionalArrayCarrier("moo")
	// wstring(N): the packet wire (internal/codegen/moo, the packet-wide
	// gate) and the table wire's kind 33 (construct_test.go: wide_text)
	registerWideTextCarrier("moo")
	tableWideTextTargets["moo"] = true
	// string, bytes and flags defaults: the packet constructors, and the
	// table wire's elision and absent read (construct_test.go: defaults_at,
	// defaults_off)
	valueDefaultTargets = append(valueDefaultTargets, "moo")
	registerPacketValueDefaultCarrier("moo")
	// `was` on a variant, an arm or a type's field: the ids are the source
	// names' (ir.TableFieldWireId, VariantWireName, WireName)
	wasRowTargets = append(wasRowTargets, "moo")
	// the constructs compiler/generate.go gates are registered in
	// compiler/carries_moo.go
}
