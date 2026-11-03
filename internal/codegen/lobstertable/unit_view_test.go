package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

func TestLobsterUnitViewIncludesUnreachedFiles(t *testing.T) {
	for _, tables := range []bool{false, true} {
		t.Run(fmt.Sprintf("tables_%v", tables), func(t *testing.T) {
			dir := t.TempDir()
			sources := map[string]string{
				"Orphan.schema":     "package registryprobe\ntype Orphan { n int32 }\n",
				"Vocabulary.schema": "package registryprobe\nenum Mode { One, Two }\nflags Options { Fast, Safe }\nunion Choice { one Orphan }\n",
				"Constants.schema":  "package registryprobe\nconst Answer = 42\n",
			}
			if tables {
				sources["Root.schema"] = "package registryprobe\ntable Root { n int32 }\n"
			}
			var paths []string
			// Keep input ordering stable so this also checks that the entry point
			// imports every file, rather than accidentally depending on root order.
			for _, name := range []string{"Orphan.schema", "Vocabulary.schema", "Constants.schema", "Root.schema"} {
				if src, ok := sources[name]; ok {
					p := filepath.Join(dir, name)
					if err := os.WriteFile(p, []byte(src), 0644); err != nil {
						t.Fatal(err)
					}
					paths = append(paths, p)
				}
			}
			g, err := generateLoadUnit(t, paths)
			if err != nil {
				t.Fatal(err)
			}
			tableCount := 0
			if tables {
				tableCount = 1
			}
			program := fmt.Sprintf(`import .registryprobeView
let v = registryprobe.UnitView()
assert v.package == "registryprobe" and v.protocol_id != 0
assert length(v.types) == 1 and v.types[0].name == "Orphan"
assert length(v.tables) == %d
assert length(v.constants) == 1 and v.constants[0].name == "Answer" and v.constants[0].int_value == 42
assert length(v.enums) == 1 and v.enums[0].name == "Mode"
assert length(v.flags) == 1 and v.flags[0].name == "Options"
assert length(v.unions) == 1 and v.unions[0].name == "Choice"
print("whole unit passed")
`, tableCount)
			if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program), 0644); err != nil {
				t.Fatal(err)
			}
			if out := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", nil))); out != "whole unit passed" {
				t.Fatal(out)
			}
			// Remove a declaration-only module from the entry point. The
			// otherwise valid unit must expose the missing constant, proving
			// that the entry point imports it rather than relying on codecs.
			entry := filepath.Join(g.dir, "registryprobeView.lobster")
			raw, err := os.ReadFile(entry)
			if err != nil {
				t.Fatal(err)
			}
			const anchor = "import .ConstantsTable\n"
			if strings.Count(string(raw), anchor) != 1 {
				t.Fatal("constant module import must occur once")
			}
			if err := os.WriteFile(entry, []byte(strings.Replace(string(raw), anchor, "", 1)), 0644); err != nil {
				t.Fatal(err)
			}
			control := `import .registryprobeView
let v = registryprobe.UnitView()
assert length(v.types) == 1 and length(v.enums) == 1 and length(v.flags) == 1 and length(v.unions) == 1
assert length(v.constants) == 0
print("constant omission detected")
`
			if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(control), 0644); err != nil {
				t.Fatal(err)
			}
			if out := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", nil))); out != "constant omission detected" {
				t.Fatal(out)
			}

		})
	}
}
