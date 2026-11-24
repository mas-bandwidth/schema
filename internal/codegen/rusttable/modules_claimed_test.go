package rusttable

import (
	"maps"
	"regexp"
	"slices"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rust"
)

// modDecl matches a module lib.rs declares at the crate root.
var modDecl = regexp.MustCompile(`(?m)^(?:pub )?mod ([a-z_][a-z0-9_]*)\b`)

// TestRustModulesAreClaimed holds the checker's claim on the Rust crate's
// module names (check.RustModuleNames, SPEC §4.6) to what lib.rs actually
// declares: a module the claim misses is a name a declaration can take, and
// the declaration then collides with the module (E0255) or is shadowed by it
// at the crate root. Every table unit in the repository is generated, and
// every `mod` in its lib.rs must be claimed. Planted red: drop "_records"
// from the claim's suffixes and the units with records fail here.
func TestRustModulesAreClaimed(t *testing.T) {
	t.Parallel()
	units := []string{
		"tables/arms", "tables/backend", "tables/blobs", "tables/block", "tables/blockhome",
		"tables/cards", "tables/examples", "tables/lists", "tables/maps", "tables/messages",
		"tables/pointers", "tables/scalars", "tables/stream", "tables/vocab", "tables/vocab9",
		"tables/widelist",
	}
	for _, dir := range units {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()
			u := generateUnit(t, "../../..", []string{dir})
			files, err := rust.Generate(u)
			if err != nil {
				t.Fatalf("rust.Generate: %v", err)
			}
			tables, err := Generate(u)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			shadowed := rust.ShadowedTypes(files, tables)
			maps.Copy(files, tables)
			lib, err := rust.Lib(u, Modules(tables), shadowed)
			if err != nil {
				t.Fatalf("rust.Lib: %v", err)
			}
			var bases []string
			for _, f := range u.Files {
				bases = append(bases, f.Base)
			}
			claimed := check.RustModuleNames(bases, len(u.Tables) > 0)
			mods := modDecl.FindAllStringSubmatch(string(lib), -1)
			if len(mods) == 0 {
				t.Fatalf("lib.rs declares no module:\n%s", lib)
			}
			for _, m := range mods {
				if !slices.Contains(claimed, m[1]) {
					t.Errorf("lib.rs declares mod %s, which check.RustModuleNames does not claim (%v)", m[1], claimed)
				}
			}
		})
	}
}
