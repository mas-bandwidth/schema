package luatable

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// selfReachSchema declares tables that reach themselves: a table pointing at
// itself from a field, a fixed array and a list; two tables pointing at each
// other; and a union whose arm points back at the table holding it.
const selfReachSchema = `package selfreach

table Node
{
    v     int32
    left  *Node
    right *Node
    kids  [..3]*Node
    more  []*Node
}

table Ping
{
    pong *Pong
}

table Pong
{
    ping *Ping
}

union Hop
{
    back Back
}

table Back
{
    host *Host
}

table Host
{
    hop Hop
}
`

// TestLuaTestgenTerminatesOnSelfReachingTables pins `schema testgen --lang
// lua` (docs/SPEC-TABLES.md §16.11) on tables that reach themselves: every
// walk of the closure visits a table once, so testgen terminates with depth
// bounded by the closure's table count and spells a recursive pointer as
// null on the path that would revisit it.
func TestLuaTestgenTerminatesOnSelfReachingTables(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "selfreach.schema"), selfReachSchema)
	u := luarun.LoadUnit(t, dir)
	tests, err := Testgen(u)
	if err != nil {
		t.Fatalf("testgen: %v", err)
	}
	src, ok := tests["selfreachTable_gen_test.lua"]
	if !ok {
		names := make([]string, 0, len(tests))
		for n := range tests {
			names = append(names, n)
		}
		t.Fatalf("testgen wrote %v, want selfreachTable_gen_test.lua", names)
	}
	if !strings.Contains(string(src), "null") {
		t.Fatalf("no sample ends a recursive pointer in null:\n%s", src)
	}
}
