package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// TestSquirrelCookWriteZeroesDeadAndAbsentBytes cooks two roots through the
// generated Squirrel cook writer in both byte orders and pins them, byte for
// byte, to the committed cook-write fixtures: keyed_config, whose absent
// optionals' payloads are dead bytes and stay zero whatever their declared
// defaults are, and root_full, whose counted arrays sit below their capacity
// so every slot past the live count is dead bytes and stays zero
// (docs/SPEC-TABLES.md §7.2, §7.6; docs/rulings.json fixed-dead-bytes-are-zero).
func TestSquirrelCookWriteZeroesDeadAndAbsentBytes(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	all, _ := readCookWriteRows(t, manifest)
	want := map[string]bool{"keyed_config": true, "root_full": true}
	var rows []cookWriteRow
	for _, r := range all {
		if want[r.instance] {
			rows = append(rows, r)
		}
	}
	if len(rows) != len(want) {
		t.Fatalf("found %d of the %d cook-write rows %v in the manifest", len(rows), len(want), want)
	}
	paths := map[string][]string{}
	for _, u := range manifestUnits(t, manifest) {
		paths[u.key] = u.paths
	}

	dir := t.TempDir()
	var p strings.Builder
	p.WriteString(cookWritePrelude)
	inputs := map[string][]byte{}
	generated := map[string]string{}
	read := func(rel string) []byte {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	for _, r := range rows {
		mod, ok := generated[r.unit]
		if !ok {
			unit, known := paths[r.unit]
			if !known {
				t.Fatalf("cook-write row %s names unit %q, which the manifest does not declare", r.instance, r.unit)
			}
			u := loadUnit(t, root, unit)
			files, err := squirrel.Generate(u)
			if err != nil {
				t.Fatalf("unit %s: the type surface refused: %v", r.unit, err)
			}
			tables, err := squirreltable.Generate(u)
			if err != nil {
				t.Fatalf("unit %s: the table surface refused: %v", r.unit, err)
			}
			out := filepath.Join(dir, r.unit)
			if err := os.MkdirAll(out, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, data := range files {
				writeFile(t, filepath.Join(out, name), data)
			}
			for name, data := range tables {
				writeFile(t, filepath.Join(out, name), data)
			}
			mod = squirreltable.ModuleName(u)
			generated[r.unit] = mod
		}
		inputs[r.instance+".wire"] = read(r.wire)
		if r.input != "" {
			inputs[r.instance+".json"] = read(r.input)
		}
		inputs[r.instance+".little"] = read(r.little)
		inputs[r.instance+".big"] = read(r.big)
		fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", mod, r.root, r.instance, r.unit)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))

	stdout := string(sqrun.Run(t, dir, "main.nut", inputs))
	for _, r := range rows {
		for _, order := range []string{"1", "2"} {
			ok := r.instance + " " + order + " ok"
			if !strings.Contains("\n"+stdout, "\n"+ok+"\n") {
				t.Errorf("cook-write %s order %s is not the fixture's bytes; probe output:\n%s", r.instance, order, stdout)
			}
		}
	}
}
