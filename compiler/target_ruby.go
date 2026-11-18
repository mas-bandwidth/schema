//go:build schema_leg_ruby

// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// Scaffolded by `schema new-leg ruby`; docs/PORT-RUBY.md is the port's design.
// The schema_leg_ruby tag keeps this target out of the builtins until the
// packet surface has claims in compiler/*_test.go;
// `go build -tags schema_leg_ruby` is `schema generate --lang ruby`. The tag is
// dropped when the port joins the builtins (docs/plans/ruby-leg.md, R4 the
// packet wire).
package compiler

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type rubyTarget struct{}

func (rubyTarget) Names() []string { return []string{"ruby"} }

// Generate is the skeleton (docs/plans/ruby-leg.md, R3). The shared refusal
// chain every port calls refuses, by name, each construct the target does not
// carry, as it does for every skeleton; the constructs keyed in
// compiler/carries.go are refused by the driver against the ruby carries set,
// which compiler/carries_ruby.go registers empty. Then the emitter holds every
// name to the leg's identifier rules and refuses each declaration kind it does
// not carry yet, naming the layer that carries it. A unit that declares
// nothing generates its namespace.
func (rubyTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	if err := ir.RefuseWideTableKinds(u, "ruby"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "ruby"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "ruby"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "ruby"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "ruby"); err != nil {
		return nil, err
	}
	if err := refuseTables(u, "ruby"); err != nil {
		return nil, err
	}
	return ruby.Generate(u)
}

func init() {
	registerBuiltin(rubyTarget{}, false, false, false, false)
}
