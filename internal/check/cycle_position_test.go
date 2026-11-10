package check

import (
	"strings"
	"testing"
)

func TestCompositionCyclePosition(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		sources map[string]string
		want    string
	}{
		{"self", map[string]string{"self.schema": "package t\n\n  type A { again A }\n"}, "self.schema:3:3: type composition cycle: A -> A"},
		{"mutual", map[string]string{
			"first.schema":  "package t\n\n  type A { b B }\n",
			"second.schema": "package t\ntype B { a A }\n",
		}, "first.schema:3:3: type composition cycle: A -> B -> A"},
		{"table", map[string]string{"table.schema": "package t\n\n  table A { again A }\n"}, "table.schema:3:3: type composition cycle: A -> A"},
		{"prefixed", map[string]string{"cyc.schema": "package t\n\n  type A { next B }\n  type B { next C }\n  type C { back B }\n"}, "cyc.schema:4:3: type composition cycle: A -> B -> C -> B"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := runUnit(t, tc.sources)
			for _, err := range errs {
				if strings.HasPrefix(err.Error(), tc.want) {
					return
				}
			}
			t.Fatalf("want diagnostic beginning %q, got %v", tc.want, errs)
		})
	}
}
