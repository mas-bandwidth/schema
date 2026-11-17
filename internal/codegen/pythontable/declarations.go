package pythontable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A table unit's module carries the schema file's constants, enums and flags
// sets beside its table classes: the packet host emits each one exactly as the
// packet surface spells it (internal/codegen/python, surface.go;
// docs/PORT-PYTHON.md, THE VALUE MAPPING), and the package's __init__.py
// re-exports each of them.

// valueDecls is a file's constants, enums and flags sets in emission order.
// Tables and their unions are emitted by the table frame; packet types and
// packet unions are re-exported with the classes.
func valueDecls(f *ir.File) []ir.Decl {
	if f == nil {
		return nil
	}
	var out []ir.Decl
	for _, d := range ir.EmissionOrder(f) {
		switch d.(type) {
		case *ir.Const, *ir.Enum, *ir.Flags:
			out = append(out, d)
		}
	}
	return out
}

// valueDeclName is a constant's, enum's or flags set's declared name.
func valueDeclName(d ir.Decl) string {
	switch d := d.(type) {
	case *ir.Const:
		return d.Name
	case *ir.Enum:
		return d.Name
	case *ir.Flags:
		return d.Name
	}
	return ""
}
