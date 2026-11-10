// Package python emits the Python target (docs/PORT-PYTHON.md): one Python
// package per unit, named for the unit's package, holding one module per schema
// file and the runtime beside them, deterministic to the byte (SPEC §6.1).
//
//	<package>/__init__.py   the unit: its protocol id, Refused and InvalidValue
//	<package>/py.typed      the package carries its own types (PEP 561)
//	<package>/<Base>.py     one module per schema file
//	<package>/_schema/      the runtime: the errors, and serialize.py vendored
//
// The packet core carries the class surface of every declaration and the codec
// of the kinds the first packet card names (bool, int8..uint64, enums, flags,
// types by value, unions and if/else branches). A field or item of any other
// kind is refused by name, naming the design's packet layer
// (docs/PORT-PYTHON.md, THE PACKET CORE CUT).
package python

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const ext = "py"

// RuntimeDir is the runtime subpackage's name inside every generated package.
const RuntimeDir = "_schema"

// runtimeFS is the runtime, emitted verbatim under <package>/_schema/: the
// errors module and serialize.py, vendored by sha256 (runtime/SERIALIZE_PIN,
// TestVendoredPythonSerializeIsThePin). The all: prefix keeps the files whose
// names start with an underscore (__init__.py, _core.py), which a bare
// "go:embed" pattern leaves out.
//
//go:embed all:runtime/_schema
var runtimeFS embed.FS

// Generate returns path -> file contents for the unit, every path relative
// to the output directory and starting with the package's directory.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if err := CheckNames(u); err != nil {
		return nil, err
	}
	if err := refuseDeclarations(u); err != nil {
		return nil, err
	}
	if err := checkCarried(u); err != nil {
		return nil, err
	}
	pkg := u.Package
	out := map[string][]byte{}
	bases := make([]string, 0, len(u.Files))
	for _, f := range u.Files {
		bases = append(bases, f.Base)
		out[path.Join(pkg, f.Base+"."+ext)] = []byte(emitModule(u, f))
	}
	sort.Strings(bases)
	out[path.Join(pkg, "__init__.py")] = []byte(packageInit(u, bases))
	out[path.Join(pkg, "py.typed")] = []byte{}
	runtime, err := Runtime()
	if err != nil {
		return nil, err
	}
	for rel, data := range runtime {
		out[path.Join(pkg, rel)] = data
	}
	return out, nil
}

// Runtime returns the runtime's files keyed by their path inside a generated
// package (_schema/__init__.py, _schema/serialize/_core.py, ...).
func Runtime() (map[string][]byte, error) {
	out := map[string][]byte{}
	err := fs.WalkDir(runtimeFS, "runtime", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := runtimeFS.ReadFile(p)
		if err != nil {
			return err
		}
		out[strings.TrimPrefix(p, "runtime/")] = data
		return nil
	})
	return out, err
}

// refuseDeclarations refuses a table or a table union: the table wire is a
// later layer (docs/PORT-PYTHON.md, THE CARD MAP: P5 and P6).
func refuseDeclarations(u *ir.Unit) error {
	for _, f := range u.Files {
		for _, t := range f.Tables {
			return fmt.Errorf("python: %s.schema declares table %s, and the Python target does not carry tables yet (docs/PORT-PYTHON.md, THE CARD MAP: P5 fixed tables, P6 variable tables)", f.Base, t.Name)
		}
		for _, un := range f.TableUnions {
			return fmt.Errorf("python: %s.schema declares union %s with a table arm, and the Python target does not carry tables yet (docs/PORT-PYTHON.md, THE CARD MAP: P6 variable tables)", f.Base, un.Name)
		}
	}
	return nil
}

func declKind(d ir.Decl) (kind, plural, name string) {
	switch d := d.(type) {
	case *ir.Const:
		return "const", "constants", d.Name
	case *ir.Enum:
		return "enum", "enums", d.Name
	case *ir.Flags:
		return "flags", "flags sets", d.Name
	case *ir.Struct:
		return "type", "types", d.Name
	case *ir.Union:
		return "union", "unions", d.Name
	}
	return "declaration", "declarations", fmt.Sprintf("%T", d)
}

// emitModule is one schema file's module: its header, imports, declarations
// and codec.
func emitModule(u *ir.Unit, f *ir.File) string { return emitModuleMode(u, f, false) }

func emitModuleMode(u *ir.Unit, f *ir.File, surfaceOnly bool) string {
	m := &pyModule{
		surfaceOnly: surfaceOnly,
		u:           u,
		f:           f,
		imports:     map[string]bool{},
		schema:      map[string]bool{},
		serial:      map[string]bool{},
		sibling:     map[string]string{},
	}
	for _, d := range ir.EmissionOrder(f) {
		switch d := d.(type) {
		case *ir.Const:
			m.emitConst(d)
		case *ir.Enum:
			m.emitEnum(d)
		case *ir.Flags:
			m.emitFlags(d)
		case *ir.Struct:
			m.emitStruct(d)
		case *ir.Union:
			m.emitUnion(d)
		}
	}
	return m.assemble()
}

// packageInit is the unit's __init__.py: the protocol id, the errors every
// read and write raises, and one import per module so `import <package>`
// loads the whole unit.
func packageInit(u *ir.Unit, bases []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Code generated by the schema compiler from the unit %s. DO NOT EDIT.\n", u.Package)
	fmt.Fprintf(&b, "\"\"\"The schema unit %s.\"\"\"\n\n", u.Package)
	b.WriteString("from typing import Final\n\n")
	b.WriteString("from ._schema import InvalidValue, Refused\n")
	for _, base := range bases {
		fmt.Fprintf(&b, "from . import %s\n", base)
	}
	exported := []string{"InvalidValue", "PROTOCOL_ID", "Refused"}
	for _, f := range u.Files {
		for _, d := range f.Decls {
			_, _, name := declKind(d)
			fmt.Fprintf(&b, "from .%s import %s\n", f.Base, name)
			exported = append(exported, name)
			if u, ok := d.(*ir.Union); ok {
				fmt.Fprintf(&b, "from .%s import %sType\n", f.Base, u.Name)
				exported = append(exported, u.Name+"Type")
			}
		}
	}
	fmt.Fprintf(&b, "\nPROTOCOL_ID: Final = 0x%016x\n\n", u.ProtocolId)
	names := append(append([]string{}, exported...), bases...)
	sort.Strings(names)
	b.WriteString("__all__ = [\n")
	for _, n := range names {
		fmt.Fprintf(&b, "    %q,\n", n)
	}
	b.WriteString("]\n")
	return b.String()
}
