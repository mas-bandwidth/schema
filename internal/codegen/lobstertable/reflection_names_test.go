package lobstertable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
)

func TestLobsterUnitViewGeneratedNames(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, source string }{
		{"FooDescriptor", "type Foo { n int32 }\nconst FooDescriptor = 1\n"},
		{"BarDescriptor", "type Payload { n int32 }\nunion Bar { one Payload }\ntype BarDescriptor { n int32 }\n"},
		{"FooDescriptor", "table Foo { n int32 }\nconst FooDescriptor = 1\n"},
		{"UnitView", "const UnitView = 1\n"},
		{"FooFixedRecordCodec", "fixed table Foo { n int32 }\nconst FooFixedRecordCodec = 1\n"},
	}
	for _, suffix := range []string{"Slot", "At", "Each"} {
		cases = append(cases, struct{ name, source string }{"OwnerItems" + suffix, "enum Key { One }\ntype Owner { items [Key]int32 }\nconst OwnerItems" + suffix + " = 1\n"})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "Probe.schema")
			check := func(source string, reject bool) {
				if err := os.WriteFile(path, []byte("package namesprobe\n"+source), 0644); err != nil {
					t.Fatal(err)
				}
				u, err := compiler.New().Load([]string{path})
				if err != nil {
					// UnitView is already reserved by the common checker.
					if reject && tc.name == "UnitView" && strings.Contains(err.Error(), "declaration UnitView collides") {
						return
					}
					t.Fatal(err)
				}
				out, err := lobstertable.Generate(u)
				if reject {
					if err == nil || !strings.Contains(err.Error(), "generated reflection name "+tc.name) {
						t.Fatalf("expected named collision %s, got %v", tc.name, err)
					}
					if len(out) != 0 {
						t.Fatal("collision emitted partial output")
					}
				} else if err != nil {
					t.Fatalf("renamed control refused: %v", err)
				}
			}
			check(tc.source, true)
			check(strings.ReplaceAll(tc.source, tc.name, tc.name+"User"), false)
		})
	}
}

// Variant members flatten into the package namespace and can collide with a
// descriptor even though no schema declaration uses the complete spelling.
func TestLobsterUnitViewVocabularyMemberNames(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"enum", "flags"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "Probe.schema")
			for _, variant := range []string{"BetaDescriptor", "BetaDescriptorUser"} {
				source := "package namesprobe\ntype AlphaBeta { n int32 }\n" + kind + " Alpha { " + variant + " }\n"
				if err := os.WriteFile(path, []byte(source), 0644); err != nil {
					t.Fatal(err)
				}
				u, err := compiler.New().Load([]string{path})
				if err != nil {
					t.Fatal(err)
				}
				out, err := lobstertable.Generate(u)
				if variant == "BetaDescriptor" {
					if err == nil || !strings.Contains(err.Error(), "generated reflection name AlphaBetaDescriptor") || len(out) != 0 {
						t.Fatalf("missing atomic named refusal: files=%d err=%v", len(out), err)
					}
				} else if err != nil {
					t.Fatalf("renamed variant refused: %v", err)
				}
			}
		})
	}
}
