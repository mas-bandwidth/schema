package moo_test

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
)

// The table surface joins the strict analyzer gate (docs/PORTING.md J4):
// strictgate_test.go analyzes what this hands it beside the packet surface.
func init() {
	moo.StrictGateTables = mootable.Generate
	moo.StrictGateTableRuntime = mootable.Runtime
}
