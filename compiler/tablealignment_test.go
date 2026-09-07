package compiler

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
	"strings"
	"testing"
)

// A variable wide root used to fail C++'s Alloc<T> assertion (16 <= 8).
// Every indirect closure path must influence the arena and framing oracle.
func TestCppVariableWideAlignment(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		align        int64
	}{
		{"narrow", "table Root { n uint64\nnext *Root }", 8},
		{"direct", "table Root { n uint128\nnext *Root }", 16},
		{"pointer", "table Wide { n uint128 }\ntable Root { next *Wide }", 16},
		{"union", "union Choice { wide uint128 | overlay }\ntable Root { choice Choice\nnext *Root }", 16},
		{"list", "table Wide { n uint128 }\ntable Root { values []Wide }", 16},
		{"map", "table Wide { n uint128 }\ntable Root { values map[uint8]Wide }", 16},
		{"array", "table Wide { n uint128 }\ntable Root { values [2]Wide\nnext *Root }", 16},
		{"nested", "table Wide { n uint128 }\ntable Root { value Wide\nnext *Root }", 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := unitFromSource(t, "package fixture\n"+tc.source+"\n")
			if got := ir.TableRegionAlign(u); got != tc.align {
				t.Fatalf("alignment %d, want %d", got, tc.align)
			}
			for _, lang := range []string{"cpp", "go"} {
				files, err := New().Generate(u, lang, Options{})
				if err != nil {
					t.Fatal(err)
				}
				needle := fmt.Sprintf("kTableAlign       = %d;", tc.align)
				suffix := "Table.h"
				if lang == "go" {
					needle = fmt.Sprintf("tableRegionAlign int64 = %d", tc.align)
					suffix = "Table.go"
				}
				found := false
				for name, content := range files {
					if strings.HasSuffix(name, suffix) && strings.Contains(string(content), needle) {
						found = true
					}
				}
				if !found {
					t.Fatalf("%s arena missing %q", lang, needle)
				}
			}
		})
	}
}
