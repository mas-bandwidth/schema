package check

import (
	"strings"
	"testing"
)

// A union arm reaches its named element exactly as a table field does (§2.6).
// The nested cases deliberately give Leaf no direct table reference.
func TestTableUnionClosureTransitiveArms(t *testing.T) {
	for _, tc := range []struct{ name, declarations string }{
		{"direct", "table Root { leaf Leaf }"},
		{"union", "union Choice { leaf Leaf }\ntable Root { choice Choice }"},
		{"fixed_array", "union Choice { leaves [2]Leaf }\ntable Root { choice Choice }"},
		{"bounded_array", "union Choice { leaves [..2]Leaf }\ntable Root { choice Choice }"},
		{"nested_bounded_array", "union Choice { leaves [..2]Leaf }\nunion Outer { choices [..2]Choice }\ntable Root { outer Outer }"},
		{"shared_leaf", "union Left { leaf Leaf }\nunion Right { leaf Leaf }\ntable Root {\n left Left\n right Right\n}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := buildUnit(t, "package closure\nunion Leaf { count int32 }\n"+tc.declarations+"\n")
			if u.TableUnions["Leaf"] == nil {
				t.Fatal("reached Leaf missing from table union IR")
			}
			if u.Unions["Leaf"] != nil {
				t.Fatal("table-only Leaf leaked into packet union IR")
			}
		})
	}
}

func TestTableUnionClosureStillRefusesUnreachable(t *testing.T) {
	for _, declarations := range []string{
		"union Orphan { count int32 }\ntable Root { value int32 }",
		"union Orphan { count int32 }\nunion Choice { leaf Leaf }\nunion Leaf { count int32 }\ntable Root { choice Choice }",
	} {
		errs := runUnit(t, map[string]string{"T.schema": "package closure\n" + declarations + "\n"})
		if len(errs) != 1 || !strings.Contains(errs[0].Error(), "no table reaches Orphan") {
			t.Fatalf("expected only the genuinely unreachable union refusal, got %v", errs)
		}
	}
}
