// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// docs/PORT-LOBSTER.md is the port's design.
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type lobsterTarget struct{}

func (lobsterTarget) Names() []string { return []string{"lobster"} }

// Generate is the type surface (one module per schema file) and, for a unit
// that declares tables, the table surface (one <Base>Table.lobster per file
// with tables): the descriptors, constructors and the runtime the forms walk
// (docs/PORT-LOBSTER.md, THE CARD MAP). The constructs the lobster leg does
// not carry are refused centrally, by name (compiler/carries_lobster.go); a
// construct the table surface does not describe is refused by lobstertable.
func (t lobsterTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return t.GenerateLineage(u, opts, nil)
}

// GenerateLineage is the second entry point of §5.9 #1
// (docs/FIXED-FORM-ALGORITHM.md): the driver read the unit's lock once
// (compiler/lineage.go) and hands the lineage down as DATA, so lobstertable
// opens no file and a build ships a reader for every layout the lock records.
func (lobsterTarget) GenerateLineage(u *ir.Unit, _ Options, lineage *FixedLineage) (map[string][]byte, error) {
	// THE LEG CARRIES THE VARIABLE WIRE (docs/PORT-LOBSTER.md, THE LOAD): its
	// load and save walk the descriptors for every construct the shared
	// refusals hold back from a port that has not met them — a string, bytes
	// or flags default, a was row, a union with table arms or a payload-free
	// arm, an array of unions, a byte blob, an optional array, a map and an
	// unbounded list — so it applies none of them, as the Luau leg applies
	// none. The conformance leg holds every manifest unit to the reference
	// (test/conformance/lobster/driver), and TestLobsterTableLoadMatchesTheReports
	// and TestLobsterTableSaveRoundTripsEveryInstance hold them to the Go
	// engine. The constructs of compiler/carries.go stay refused centrally
	// unless carries_lobster*.go registers them.
	//
	// A PACKET type's string, bytes or flags default is carried too: the
	// constructor writes it and the type wire reads and writes over it
	// (internal/codegen/lobster/packet_defaults_test.go reads the C++ pins
	// of test/packet-defaults), so lobster registers as a packet carrier.
	files, err := lobster.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := lobstertable.GenerateLineage(u, lobsterTableLineage(u, lineage))
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice by the lobster target; rename the schema file (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT)", name)
		}
		files[name] = data
	}
	return files, nil
}

// lobsterTableLineage is the lock's lineage in the Lobster backend's own
// spelling; nil in, nil out (target_elixir.go's elixirTableLineage is the
// shape this copies).
func lobsterTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]lobstertable.FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]lobstertable.FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(u) {
		for _, e := range lineage.Entries(st.Name) {
			out[st.Name] = append(out[st.Name], lobstertable.FixedLineageEntry{
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
	// The append IS the registration, as target_elixir.go registers: the
	// table runtime carries a string, bytes or flags default and the was rows
	// (docs/PORT-LOBSTER.md, THE LOAD), so the shared refusals read lobster
	// as a carrier of each.
	valueDefaultTargets = append(valueDefaultTargets, "lobster")
	wasRowTargets = append(wasRowTargets, "lobster")
	registerListCarrier("lobster")
	registerMapCarrier("lobster")
	registerOptionalArrayCarrier("lobster")
	// the packet wire carries a string, bytes or flags default and
	// wstring(N) (internal/codegen/lobster: packet_defaults_test.go,
	// packet_wide_test.go)
	registerPacketValueDefaultCarrier("lobster")
	registerWideTextCarrier("lobster")
	// tables, arms, unionArrays and blobs: the variable form carries a union
	// whose arms are field lines of any type, payload-free arms among them,
	// an array of unions and the *string / *bytes blobs (docs/SPEC-TABLES.md
	// §2.5, §2.6, §3).
	registerBuiltin(lobsterTarget{}, true, true, true, true)
}
