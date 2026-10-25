//go:build schema_leg_lobster

// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// Scaffolded by `schema new-leg lobster`; docs/PORT-LOBSTER.md is the port's
// design. The schema_leg_lobster tag keeps this target out of the default
// nine until the packet surface has claims in compiler/*_test.go;
// `go build -tags schema_leg_lobster` is `schema generate --lang lobster`.
// Drop the tag when the port joins the builtins.
package compiler

import (
	"fmt"
	"strings"

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
func (lobsterTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
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
	// A PACKET type's string, bytes or flags default is still refused by
	// name: the type surface has no type-wire read path yet, so nothing
	// proves those defaults (docs/PORT-LOBSTER.md, THE CARD MAP).
	if err := lobsterRefusePacketDefaults(u); err != nil {
		return nil, err
	}
	files, err := lobster.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := lobstertable.Generate(u)
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

// lobsterRefusePacketDefaults is refuseValueDefaults' packet half (SPEC §4.2):
// a string, bytes or flags default on a declaration no table closure reaches.
// The table half is carried by the table runtime and is not refused.
func lobsterRefusePacketDefaults(u *ir.Unit) error {
	full := ir.TableClosure(u)
	var names []string
	for _, name := range ir.ValueDefaultFields(u) {
		owner, _, _ := strings.Cut(name, ".") // ValueDefaultFields returns Decl.field
		if !full[owner] {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	carry, flags := carriers(packetValueDefaultTargets)
	return fmt.Errorf("unit declares a string, bytes or flags default (%s): packet-wire defaults are %s only today, and the lobster form is a named follow-on; generate with %s, or drop the default (SPEC §4.2)",
		englishList(names), englishList(carry), englishList(flags))
}

func init() {
	registerBuiltin(lobsterTarget{}, false, false, false, false)
}
