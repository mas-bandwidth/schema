package compiler

import (
	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The emitters' package and type-name rules run at check (SPEC §4.6):
// Ruby's constant spelling and the constants the interpreter defines,
// Python's standard-library packages and builtins, Goose's reserved
// namespaces. A name one of them refuses is refused by schema check for
// every target, naming the target, instead of at generate.
func init() {
	check.RegisterEmitterNameRule("Goose", func(u *ir.Unit) error { return goose.CheckUnit(u, nil) })
	check.RegisterEmitterNameRule("Python", python.CheckPackageAndTypeNames)
	check.RegisterEmitterNameRule("Ruby", ruby.CheckPackageAndTypeNames)
}
