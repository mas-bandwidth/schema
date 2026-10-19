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

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type lobsterTarget struct{}

func (lobsterTarget) Names() []string { return []string{"lobster"} }

// Generate is the type surface (one module per schema file) and, for a unit
// that declares tables, the table surface (one <Base>Table.lobster per file
// with tables) — the descriptors and constructors, no codec yet
// (docs/PORT-LOBSTER.md, THE CARD MAP). The constructs the lobster leg does
// not carry are refused centrally, by name (compiler/carries_lobster.go); a
// construct the table surface does not describe is refused by lobstertable.
func (lobsterTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	// THE LEG HAS NO WIRE CODEC YET (docs/PORT-LOBSTER.md, THE CARD MAP), so
	// it carries none of the constructs the shared refusals hold back from a
	// port that has not met them: a string, bytes or flags default, a was row,
	// a union with table arms or a payload-free table arm, an array of
	// unions, a byte blob, an optional array, a map and an unbounded list —
	// each refused here by name, as every non-carrier refuses it. The
	// descriptor surface itself describes them all
	// (TestLobsterTableDescriptorsLoad generates every manifest unit through
	// lobster and lobstertable directly).
	for _, refuse := range []func(*ir.Unit, string) error{refuseUnported, refuseOptionalArrays, refuseMaps, refuseLists} {
		if err := refuse(u, "lobster"); err != nil {
			return nil, err
		}
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

func init() {
	registerBuiltin(lobsterTarget{}, false, false, false, false)
}
