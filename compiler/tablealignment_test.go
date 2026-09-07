package compiler

import (
	"fmt"
	"strings"
	"testing"
)

// A variable wide root used to fail C++'s Alloc<T> assertion (16 <= 8).
// Check the emitted allocation contract for both widths; the same constant
// rounds every node in the arena, LoadMeasure and Lock.
func TestCppVariableWideAlignment(t *testing.T) {
	for _, width := range []int{64, 128} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			u := unitFromSource(t, fmt.Sprintf("package fixture\ntable Root { n uint%d\nnext *Root }\n", width))
			files, err := New().Generate(u, "cpp", Options{})
			if err != nil {
				t.Fatal(err)
			}
			align := 8
			if width == 128 {
				align = 16
			}
			needle := fmt.Sprintf("kTableAlign       = %d;", align)
			found := false
			for name, content := range files {
				if strings.HasSuffix(name, "Table.h") && strings.Contains(string(content), needle) {
					found = true
				}
			}
			if !found {
				t.Fatalf("generated arena missing %q", needle)
			}
		})
	}
}
