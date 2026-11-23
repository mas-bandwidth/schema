//go:build schema_leg_moo

package compiler

import (
	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The moo leg's module-name rules run at check (SPEC §4.6) in a build that
// has the leg, as its target does.
func init() {
	check.RegisterEmitterNameRule("MOO", func(u *ir.Unit) error {
		if err := moo.CheckBases(u); err != nil {
			return err
		}
		return mootable.CheckNames(u)
	})
}
