package pythontable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v3/ir"
)

// Surfaces selects a generate call's table surfaces. It uses the shared
// parser and dependencies of SPEC-TABLES §8.8.
type Surfaces = gotable.Surfaces

// DefaultSurfaces preserves the historical complete generated output.
func DefaultSurfaces() Surfaces { return gotable.DefaultSurfaces() }

// ParseSurfaces parses the public comma-separated surface option.
func ParseSurfaces(value string) (Surfaces, error) { return gotable.ParseSurfaces(value) }

// surfaceCarries intersects the requested surfaces with a table's declaration.
func surfaceCarries(s Surfaces, st *ir.Struct, form string) bool {
	switch form {
	case "wire":
		return s.Wire && st.CarriesForm(form)
	case "json":
		return s.JSON && st.CarriesForm(form)
	case "cook":
		return s.Cook && st.CarriesForm(form)
	case "block":
		return s.Block && st.CarriesForm(form)
	}
	return false
}

// surfaceForms reports the effective forms in canonical descriptor order.
func surfaceForms(s Surfaces, st *ir.Struct) []string {
	out := make([]string, 0, 4)
	for _, form := range []string{"wire", "json", "block", "cook"} {
		if surfaceCarries(s, st, form) {
			out = append(out, form)
		}
	}
	return out
}

// unitHasCook reports whether any table exposes cook for this selection.
func unitHasCook(u *ir.Unit, s Surfaces) bool {
	for _, st := range u.Tables {
		if st != nil && surfaceCarries(s, st, "cook") {
			return true
		}
	}
	return false
}

func unitHasJSON(u *ir.Unit, s Surfaces) bool {
	for _, st := range u.Tables {
		if st != nil && !st.IsMapEntry() && !st.IsTextListElem() && surfaceCarries(s, st, "json") {
			return true
		}
	}
	return false
}

// ValidateSurfaces applies the shared explicit-list rule. The default
// set, including an explicit full set, is exempt. Synthetic entries never name
// the refusal, even though their owner declaration also governs them.
func ValidateSurfaces(u *ir.Unit, s Surfaces) error {
	if s == gotable.DefaultSurfaces() {
		return nil
	}
	var tables []*ir.Struct
	for _, st := range u.Tables {
		if st != nil && !st.IsMapEntry() && st.DeclaredForms() {
			tables = append(tables, st)
		}
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	for _, st := range tables {
		for _, asked := range []struct {
			name    string
			enabled bool
		}{{"json", s.JSON}, {"cook", s.Cook}, {"block", s.Block}} {
			if asked.enabled && !st.CarriesForm(asked.name) {
				return fmt.Errorf("--surface asks for %s, but table %s declares forms = %q, which does not carry it: a --surface naming a form the table does not declare is refused (docs/SPEC-TABLES.md §2.12)", asked.name, st.Name, strings.Join(st.Forms, ","))
			}
		}
	}
	return nil
}

// ValidateAvailableSurfaces refuses explicit requests for seams whose Python
// generators are not yet wired up. A later surface owner removes its entry
// when it connects the selection to emission; default remains today's output.
func ValidateAvailableSurfaces(s Surfaces) error {
	if s == gotable.DefaultSurfaces() {
		return nil
	}
	for _, item := range []struct {
		name    string
		enabled bool
	}{
		{"message", s.Message},
	} {
		if item.enabled {
			return fmt.Errorf("python does not yet carry the explicitly requested %s surface (docs/SPEC-TABLES.md §8.8)", item.name)
		}
	}
	return nil
}
