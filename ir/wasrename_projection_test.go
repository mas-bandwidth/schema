// A `was` rename keeps the protocol id wherever the renamed name reaches the
// projection (docs/SPEC-TABLES.md §5): the field line, a branch condition that
// names the field, and a default that names an enum variant.
package ir_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/ir"
)

const wasProjectionSource = `package demo

enum Tier
{
    Bronze
    Silver
    Gold
}

type Player
{
    active bool
    if active
    {
        score int32 | min = 0, max = 1000
    }
    tier Tier = Silver
}

table Roster
{
    lead Player
}
`

func TestWasRenameKeepsTheProtocolId(t *testing.T) {
	base := unitFrom(t, wasProjectionSource)
	for _, tc := range []struct{ name, under, bare string }{
		{
			"a bool field a branch names",
			strings.Replace(strings.Replace(wasProjectionSource, "    active bool", `    enabled bool | was = "active"`, 1), "if active", "if enabled", 1),
			strings.Replace(strings.Replace(wasProjectionSource, "    active bool", "    enabled bool", 1), "if active", "if enabled", 1),
		},
		{
			"the default enum variant",
			strings.Replace(strings.Replace(wasProjectionSource, "    Silver\n", "    Argent | was = \"Silver\"\n", 1), "Tier = Silver", "Tier = Argent", 1),
			strings.Replace(strings.Replace(wasProjectionSource, "    Silver\n", "    Argent\n", 1), "Tier = Silver", "Tier = Argent", 1),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			under := unitFrom(t, tc.under)
			if under.ProtocolId != base.ProtocolId {
				t.Errorf("a rename under was moved the protocol id: 0x%016x -> 0x%016x\n--- base ---\n%s\n--- under ---\n%s",
					base.ProtocolId, under.ProtocolId, ir.WireProjection(base), ir.WireProjection(under))
			}
			// the DISCRIMINATION control: the same rename without `was` moves it
			if unitFrom(t, tc.bare).ProtocolId == base.ProtocolId {
				t.Error("a bare rename did not move the protocol id")
			}
		})
	}
}
