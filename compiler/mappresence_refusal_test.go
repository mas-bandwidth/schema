// The OPTIONAL MAP is carried by Go first (docs/SPEC-TABLES.md §2.8): every
// other target refuses `?map[K]V` BY NAME, centrally, in Generate, including
// the targets that carry a plain map, and the declarations holding only a
// plain map generate the same code whether or not the unit beside them uses
// one (checkRefusalUnchanged).
package compiler

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// plainMapSchema holds a plain map beside the shapes the optional map shares
// machinery with: an optional scalar, an optional counted array and a map
// whose VALUE is optional.
const plainMapSchema = "package probe\n\ntable Holder\n{\n    tags map[string(8)]int32\n    maybe ?int32\n    some ?[..4]int32\n    values map[uint32]?int32\n}\n"

// plainMapProbes are the differentials of the "unchanged" claim
// (checkRefusalUnchanged): each optional map beside plainMapSchema, against
// the same declarations with the plain map in its place, so the two units
// differ by the construct alone.
// optionalMapSupport is what only a unit declaring an optional map carries in
// its Go output (gotable optionalMapJson, docs/SPEC-TABLES.md §2.8, §16.2): the
// JSON walk's null that clears the map's slot.
var optionalMapSupport = []string{"clear(unsafe.Slice((*byte)(unsafe.Add(base, uintptr(f.Offset))), 16))"}

var plainMapProbes = withSupport(optionalMapSupport, func() []unchangedProbe {
	usesOptionalMap := func(u *ir.Unit) bool { return u != nil && refuseOptionalMaps(u, "cpp") != nil }
	base := map[string]string{"go": plainMapSchema, "cpp": plainMapSchema}
	keep := []string{"Holder"}
	return []unchangedProbe{
		{"optional map", base, "table Zmap\n{\n    tags ?map[string(8)]int32\n}\n", "table Zmap\n{\n    tags map[string(8)]int32\n}\n", keep, "optional map on field Zmap.tags", usesOptionalMap, nil},
		{"optional map behind a table", base, "table Zinner\n{\n    by_id ?map[uint32]int32\n}\ntable Zouter\n{\n    inner Zinner\n}\n", "table Zinner\n{\n    by_id map[uint32]int32\n}\ntable Zouter\n{\n    inner Zinner\n}\n", keep, "optional map on field Zinner.by_id", usesOptionalMap, nil},
		{"optional map of maps", base, "table Zmap\n{\n    nest ?map[uint8]map[uint8]int32\n}\n", "table Zmap\n{\n    nest map[uint8]map[uint8]int32\n}\n", keep, "optional map on field Zmap.nest", usesOptionalMap, nil},
	}
}())

func TestMapPresenceRefusedOffGo(t *testing.T) {
	t.Parallel()
	c := New()
	cases := []struct {
		name, src, want string
	}{
		{"optional map", "package probe\ntable M\n{\n    tags ?map[string(8)]int32\n}\n", "optional map on field M.tags"},
		{"optional map behind a table", "package probe\ntable Inner\n{\n    by_id ?map[uint32]int32\n}\ntable Outer\n{\n    inner Inner\n}\n", "optional map on field Inner.by_id"},
		{"optional map of maps", "package probe\ntable M\n{\n    nest ?map[uint8]map[uint8]int32\n}\n", "optional map on field M.nest"},
	}
	for _, tc := range cases {
		u := unitFromSource(t, tc.src)
		for _, lang := range c.Targets() {
			_, err := c.Generate(u, lang, nil)
			if lang == "go" {
				if err != nil {
					t.Errorf("%s: go refused: %v", tc.name, err)
				}
				continue
			}
			if err == nil {
				t.Errorf("%s: target %s accepted the construct", tc.name, lang)
				continue
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.want) || !strings.Contains(msg, "target \""+lang+"\"") {
				t.Errorf("%s: target %s refusal does not name the construct and the target: %s", tc.name, lang, msg)
			}
		}
	}
	for _, p := range plainMapProbes {
		if err := checkRefusalUnchanged(compilerGenerate(c), unchangedTargets, p); err != nil {
			t.Error(err)
		}
	}
}
