// The two `was` rows against the two ids (docs/VERSIONING.md promise 4): a
// table renamed under `was` moves neither, and a string, bytes or flags
// default is a projection fact exactly as an integer default is.
package ir_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const wasRowsSource = `package demo

flags Caps { Jump, Crouch }

type Badge
{
    label string(8) = "new"
    caps  Caps = { Jump }
}

table Vessel
{
    name string(32) = "untitled"
    tag  bytes(4) = "ab"
    caps Caps = { Jump, Crouch }
}

table Fleet
{
    flagship *Vessel
    badge    Badge
}
`

func TestTableRenamedUnderWasMovesNeitherId(t *testing.T) {
	base := unitFrom(t, wasRowsSource)
	renamed := strings.ReplaceAll(wasRowsSource, "Vessel", "Ship")
	renamed = strings.Replace(renamed, "table Ship\n{", "table Ship | was = \"Vessel\"\n{", 1)
	under := unitFrom(t, renamed)
	if under.ProtocolId != base.ProtocolId {
		t.Errorf("a table rename under was moved the protocol id: 0x%016x -> 0x%016x", base.ProtocolId, under.ProtocolId)
	}
	if ir.BuildVersion(under) != ir.BuildVersion(base) {
		t.Errorf("a table rename under was moved the build version:\n--- base ---\n%s\n--- under ---\n%s", ir.CookProjection(base), ir.CookProjection(under))
	}
	if ir.TableWireId(under.Tables["Ship"].WireName()) != ir.TableWireId("Vessel") {
		t.Error("the renamed table's node type id is not the old name's hash")
	}
	// the DISCRIMINATION control: the bare rename moves the build version
	if ir.BuildVersion(unitFrom(t, strings.ReplaceAll(wasRowsSource, "Vessel", "Ship"))) == ir.BuildVersion(base) {
		t.Error("a bare table rename did not move the build version")
	}
}

func TestValueDefaultsProject(t *testing.T) {
	base := unitFrom(t, wasRowsSource)
	for _, tc := range []struct{ name, old, new string }{
		{"a type's string default", `label string(8) = "new"`, `label string(8) = "old"`},
		{"a type's flags default", `caps  Caps = { Jump }`, `caps  Caps = { Crouch }`},
	} {
		edited := unitFrom(t, strings.Replace(wasRowsSource, tc.old, tc.new, 1))
		if edited.ProtocolId == base.ProtocolId {
			t.Errorf("%s changed and the protocol id stood still", tc.name)
		}
	}
	for _, tc := range []struct{ name, old, new string }{
		{"a table's string default", `name string(32) = "untitled"`, `name string(32) = "unnamed"`},
		{"a table's bytes default", `tag  bytes(4) = "ab"`, `tag  bytes(4) = "ac"`},
		{"a table's flags default", `caps Caps = { Jump, Crouch }`, `caps Caps = { Jump }`},
	} {
		edited := unitFrom(t, strings.Replace(wasRowsSource, tc.old, tc.new, 1))
		if edited.ProtocolId != base.ProtocolId {
			t.Errorf("%s changed and the protocol id moved: a table field edit never reaches it", tc.name)
		}
		if ir.BuildVersion(edited) == ir.BuildVersion(base) {
			t.Errorf("%s changed and the build version stood still", tc.name)
		}
	}
}
