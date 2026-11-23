package luautable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestLuauJsonWriterRefusesACountBelowMin holds the Luau JSON writer to a
// counted array's Min (docs/SPEC.md §5.1 row 3; §16.3, what a writer accepts
// is what a read accepts), on the shapes unit's Bounds (test/tables/shapes):
// octets [2..8]uint8, held as one buffer, and recs [1..4]Rec, a table of
// elements. A count below Min refuses (the conformance row
// jref_shapes_octets_count), and the count at Min writes.
func TestLuauJsonWriterRefusesACountBelowMin(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	unitDir := filepath.Join("..", "..", "..", "test", "tables", "shapes")
	entries, err := os.ReadDir(unitDir)
	if err != nil {
		t.Fatal(err)
	}
	schemas := map[string]string{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".schema") {
			text, err := os.ReadFile(filepath.Join(unitDir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			schemas[e.Name()] = string(text)
		}
	}
	g := generateFiles(t, schemas)
	var b strings.Builder
	b.WriteString(jsonModules(g))
	b.WriteString(`local json = require("./schema_json")
local function fresh()
	local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
	local v = json.FromJson("Bounds", buffer.fromstring('{"octets":[1,2,3],"recs":[{"a":1},{"a":2}]}'), report)
	assert(v ~= nil, "FromJson refused the base value")
	return v
end
local function verdict(name, field, n)
	local v = fresh()
	v[field] = n
	local out = json.ToJson("Bounds", v, {})
	print(name .. "|" .. (if out ~= nil then "wrote" else "refused"))
end
verdict("octets_1", "OctetsCount", 1)
verdict("octets_2", "OctetsCount", 2)
verdict("recs_0", "RecsCount", 0)
verdict("recs_1", "RecsCount", 1)
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"unused": nil}))
	want := map[string]string{"octets_1": "refused", "octets_2": "wrote", "recs_0": "refused", "recs_1": "wrote"}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		name, verdict, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok {
			t.Fatalf("probe line %q: want name|verdict", line)
		}
		got[name] = verdict
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: the Luau JSON writer %s, want %s", name, got[name], w)
		}
	}
}
