// TestLuaGeneratesWideKinds: the lua target emits the fixed-point and 128-bit
// table kinds instead of refusing them (docs/SPEC-TABLES.md §3, §15): the
// fixed form names no kind, so the wide scalars are their declared storage
// images, and the message form carries their announced packing.
package compiler

import (
	"bytes"
	"testing"
)

func TestLuaGeneratesWideKinds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, typename string }{
		{"fixed-point", "package probe\ntable T\n{\n    q fixed(16, 16) | min = -1, max = 1\n}\n", "fixed(16, 16)"},
		{"int128", "package probe\ntable T\n{\n    i int128 | min = -1000, max = 1000\n}\n", "int128"},
		{"uint128", "package probe\ntable T\n{\n    u uint128\n}\n", "uint128"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := unitFromSource(t, tc.src)
			files, err := luaGenerate(u, nil)
			if err != nil {
				t.Fatalf("lua refused %s: %v", tc.name, err)
			}
			var module []byte
			for name, data := range files {
				if bytes.Contains(data, []byte("_M.Tables = {}")) {
					module = data
					_ = name
				}
			}
			if module == nil {
				t.Fatalf("lua emitted no table module for %s", tc.name)
			}
			if !bytes.Contains(module, []byte(tc.typename)) {
				t.Errorf("the table module does not name %s", tc.typename)
			}
		})
	}
}
