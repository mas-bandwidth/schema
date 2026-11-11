package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

func TestSquirrelNodeTypeReservedIdRefused(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	units := manifestUnits(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	var graphdemo []string
	for _, unit := range units {
		if unit.key == "graphdemo" {
			graphdemo = unit.paths
			break
		}
	}
	if graphdemo == nil {
		t.Fatal("table manifest has no graphdemo unit")
	}
	u := loadUnit(t, root, graphdemo)
	dir := t.TempDir()
	module := emitUnit(t, dir, "graphdemo", u)

	cases := []struct {
		name string
		root string
		want string
	}{
		{"node_type_node_table_id", "Scene", "refused"},
		{"node_type_build_version_id", "Scene", "refused"},
		{"node_type_vocabulary_id", "Scene", "refused"},
		{"node_type_ref_zero", "Scene", "refused"},
		{"node_chain_two", "ListNode", "read"},
	}
	inputs := make(map[string][]byte, len(cases))
	var probe strings.Builder
	probe.WriteString(loadPrelude)
	for _, tc := range cases {
		data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "framing", tc.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[tc.name] = data
		fmt.Fprintf(&probe, "run(%q, %q, %q, %q);\n", "graphdemo", module, tc.root, tc.name)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe.String()))
	lines := strings.Split(strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", inputs))), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("got %d probe lines, want %d: %q", len(lines), len(cases), lines)
	}
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != cases[i].name {
			t.Fatalf("probe line %d is malformed: %q", i, line)
		}
		if got := reportVerdict(fields[1]); got != cases[i].want {
			t.Errorf("%s: strict read verdict %q, want %q", cases[i].name, got, cases[i].want)
		}
	}
}
