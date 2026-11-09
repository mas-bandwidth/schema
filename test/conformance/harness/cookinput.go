package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookInput constructs the value named by a cook-write recipe. Cook accepts
// shared blobs in memory even though the wire and JSON writers cannot encode
// that identity. The recipe supplies data and field names, never expected bytes.
func cookInput(unit *ir.Unit, inst Instance, recipe CookWrite, base string) (*tabletext.Model, *tabletext.Instance, error) {
	model := tabletext.NewModel(unit)
	root := model.Lookup(inst.Root)
	if root == nil {
		return nil, nil, fmt.Errorf("%s: unknown root %q", inst.Name, inst.Root)
	}
	value := model.New(root)
	input := recipe.Input
	if input == "" && !inst.NoText {
		input = inst.JSON
	}
	path := input
	if path == "" {
		path = inst.Wire
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var report tabletext.Report
	accepted := false
	if input != "" {
		accepted = strictReadJSON(model, value, data, &report)
	} else {
		accepted, err = strictDecode(model, value, data, &report)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("%s: cook construction input refused: %w", inst.Name, err)
	}
	if !accepted || report.Refused {
		return nil, nil, fmt.Errorf("%s: cook construction input refused", inst.Name)
	}
	if recipe.Share != "" {
		source, alias, ok := strings.Cut(recipe.Share, ":")
		from, hasFrom := value.FieldByKey(source)
		to, hasTo := value.FieldByKey(alias)
		if !ok || source == alias || !hasFrom || !hasTo || from.Cell.Blob == nil || to.Cell.Blob == nil {
			return nil, nil, fmt.Errorf("%s: shared blob recipe needs two populated blob fields: %q", inst.Name, recipe.Share)
		}
		to.Cell.Blob = from.Cell.Blob
	}
	return model, value, nil
}

func cookRecipe(unit *ir.Unit, inst Instance, recipe CookWrite, base string, big bool) ([]byte, error) {
	model, value, err := cookInput(unit, inst, recipe, base)
	if err != nil {
		return nil, err
	}
	return tablecook.Cook(model, value, tablecook.Options{Big: big})
}
