package goosetable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
)

// A refused typed pack must leave both caller-owned stores unchanged. A
// failed wire load (-1) must never turn into a usable default typed record.
func TestGooseValueBridgeRollbackAndRefusal(t *testing.T) {
	m := readJSONManifest(t)
	_, files, err := jsonUnitFiles(t, "tabledemo", m.units["tabledemo"])
	if err != nil {
		t.Fatal(err)
	}
	files["main.goose"] = []byte(`import os;
import .TablesTable;
import .tabledemo_unit;
import .wire_value;
fn main() {
    var v = tabledemo::RootConfig { .. };
    var data: u8[>..] = [7];
    var vals: schema_rt::LoadVal[>..] = [schema_rt::LoadVal { bits: schema_rt::Int128 { lo: 9, hi: 0 }, .. }];
    v.VersionNoteLength = 1; v.VersionNote[0] = 'x';
    v.WeaponsCount = 9;
    print(tabledemo::RootConfigToLoaded(v, tabledemo::TableUnit, data, vals), " ", data.len, " ", vals.len, " ", data[0], " ", vals[0].bits.lo);
    v.WeaponsCount = 0; v.VersionNoteLength = 17;
    print(tabledemo::RootConfigToLoaded(v, tabledemo::TableUnit, data, vals), " ", data.len, " ", vals.len);
    print(tabledemo::RootConfigFromLoaded(v, tabledemo::TableUnit, data, vals, -1), " ", v.VersionNoteLength);
    v.VersionNoteLength = 1; v.VersionNote[0] = 'x';
    let at = tabledemo::RootConfigToLoaded(v, tabledemo::TableUnit, data, vals);
    print(at >= 0, " ", tabledemo::RootConfigFromLoaded(v, tabledemo::TableUnit, data, vals, at), " ", v.VersionNoteLength, " ", v.VersionNote[0]);
}
`)
	got := strings.TrimSpace(string(gooserun.Run(t, t.TempDir(), "main.goose", files)))
	const want = "-1 1 1 7 9\n-1 1 1\nfalse 0\ntrue true 1 120"
	if got != want {
		t.Fatalf("bridge rollback/refusal:\n got %s\nwant %s", got, want)
	}
}
