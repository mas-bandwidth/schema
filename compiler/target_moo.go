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
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type mooTarget struct{}

func (mooTarget) Names() []string { return []string{"moo"} }

func (mooTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	if err := ir.RefuseWideTableKinds(u, "moo"); err != nil {
		return nil, err
	}
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
	if err := refuseTables(u, "moo"); err != nil {
		return nil, err
	}
	return moo.Generate(u)
}

func init() {
	registerBuiltin(mooTarget{}, false, false, false, false)
}
