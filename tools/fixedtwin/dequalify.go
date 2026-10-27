package main

import (
	"fmt"
	"maps"
	"path/filepath"

	"github.com/mas-bandwidth/schema/v2/compiler"
	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cqualify"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/ctable"
)

// pairedUnit is the unit generated/bench/paired is emitted from.
var pairedUnit = []string{"bench/corpus/Bench.schema", "bench/corpus/FixedTable.schema"}

// dequalifyC returns the C header in the C emitters' own spellings: every C
// name carries its unit's package (SPEC §6.3, internal/codegen/cqualify), and
// the twin map is written against the names the emitters write, which is
// also what the C++ reference spells. The inverse is the qualification map of
// the paired unit itself, so nothing here restates the spelling rule.
func dequalifyC(src, root string) (string, error) {
	paths := make([]string, len(pairedUnit))
	for i, p := range pairedUnit {
		paths[i] = filepath.Join(root, p)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		return "", fmt.Errorf("load the paired unit: %w", err)
	}
	files, err := cgen.Generate(u)
	if err != nil {
		return "", err
	}
	tables, err := ctable.GenerateLineage(u, nil)
	if err != nil {
		return "", err
	}
	maps.Copy(files, tables)
	names, err := cqualify.Names(files, u.Package)
	if err != nil {
		return "", err
	}
	inverse := make(map[string]string, len(names))
	for from, to := range names {
		inverse[to] = from
	}
	return cqualify.Rewrite(src, inverse), nil
}
