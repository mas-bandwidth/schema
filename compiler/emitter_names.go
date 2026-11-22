package compiler

import (
	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rubytable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The emitters' name rules run at check (SPEC §4.6): each target's whole
// name check, as its generate runs it, under every surface a generate can
// select. A name one of them refuses is refused by schema check for every
// target, naming the target, instead of at generate.
func init() {
	check.RegisterEmitterNameRule("Goose", func(u *ir.Unit) error { return goose.CheckUnit(u, nil) })
	check.RegisterEmitterNameRule("Python", pythontable.CheckNamesAtCheck)
	check.RegisterEmitterNameRule("Ruby", rubytable.CheckNamesAtCheck)
}
