package rubytable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Surfaces selects a generate call's table surfaces (docs/SPEC-TABLES.md §8.8).
type Surfaces = gotable.Surfaces

// FixedLineageEntry is one fixed-form schema layout entry from the unit's lock.
type FixedLineageEntry struct {
	Wire    uint64
	Layout  []byte
	Record  int64
	Retired bool
	Reason  string
}

// DefaultSurfaces enables every carried Ruby table surface, including hand-text.
func DefaultSurfaces() Surfaces {
	s := gotable.DefaultSurfaces()
	s.TextForm = true
	return s
}

// ParseSurfaces parses the public comma-separated surface option.
func ParseSurfaces(value string) (Surfaces, error) {
	if strings.TrimSpace(value) == "" {
		return DefaultSurfaces(), nil
	}
	return gotable.ParseSurfaces(value)
}

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

// ValidateSurfaces applies the shared explicit-list rule (SPEC-TABLES §2.12).
func ValidateSurfaces(u *ir.Unit, s Surfaces) error {
	if s == DefaultSurfaces() || s == gotable.DefaultSurfaces() {
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

// ValidateAvailableSurfaces validates explicitly requested Ruby table surfaces.
func ValidateAvailableSurfaces(_ Surfaces) error {
	return nil
}
