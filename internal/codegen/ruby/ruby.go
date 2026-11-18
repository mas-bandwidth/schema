// Package ruby emits the Ruby target (docs/PORT-RUBY.md): one namespace per
// unit, named for the unit's package, holding one file per schema file and the
// runtime beside them, deterministic to the byte (SPEC §6.1).
//
//	<package>.rb             the unit module: its protocol id; requires the runtime and every file
//	<package>/<Base>.rb      one file per schema file, reopening the unit module
//	<package>/_schema.rb     the runtime: Refused and InvalidValue
//
// serialize.rb, the bit layer, is vendored beside _schema.rb by the packet
// layer (docs/PORT-RUBY.md, THE RUNTIME LAYOUT; R4).
//
// This is the skeleton (docs/plans/ruby-leg.md, R3). It carries no
// declaration: a unit that declares anything is refused by name, naming the
// layer that carries it, and a unit that declares nothing generates a
// namespace that loads under both pinned interpreters with no warning under
// ruby -w. Every name the namespace spells is held to the rules of
// IDENTIFIERS first (CheckNames).
package ruby

import (
	_ "embed"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const ext = "rb"

// RuntimeBase is the runtime file's base name inside every generated unit's
// directory: <package>/_schema.rb.
const RuntimeBase = "_schema"

// SerializeBase is the base name serialize.rb takes beside the runtime
// (docs/PORT-RUBY.md, THE RUNTIME LAYOUT). The packet layer vendors it; the
// skeleton reserves the name, so no schema file can take it first.
const SerializeBase = "serialize"

// runtimeUnitToken is the one module name the runtime source opens. A unit's
// runtime is that source with this token replaced by the unit's module
// constant, so each unit defines its own Refused and InvalidValue, reached as
// <Unit>::Refused (docs/PORT-RUBY.md, THE RUNTIME LAYOUT): a Ruby file loaded
// by require_relative is evaluated at the top level, so the module it opens
// has to be spelled in the file.
const runtimeUnitToken = "module SchemaRuntimeUnit\n"

// runtimeSource is the runtime, runtime/_schema.rb, embedded.
//
//go:embed runtime/_schema.rb
var runtimeSource string

// Generate returns path -> file contents for the unit, every path relative to
// the output directory: <package>.rb and the files under <package>/.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if err := CheckNames(u); err != nil {
		return nil, err
	}
	if err := refuseDeclarations(u); err != nil {
		return nil, err
	}
	mod := ModuleName(u.Package)
	pkg := u.Package
	out := map[string][]byte{}
	bases := make([]string, 0, len(u.Files))
	for _, f := range u.Files {
		bases = append(bases, f.Base)
		out[path.Join(pkg, f.Base+"."+ext)] = []byte(fileModule(f, mod))
	}
	sort.Strings(bases)
	out[pkg+"."+ext] = []byte(unitModule(u, mod, bases))
	runtime, err := Runtime(mod)
	if err != nil {
		return nil, err
	}
	out[path.Join(pkg, RuntimeBase+"."+ext)] = runtime
	return out, nil
}

// Runtime returns the runtime file for the unit whose module constant is mod:
// the embedded source with its one module line naming mod.
func Runtime(mod string) ([]byte, error) {
	if n := strings.Count(runtimeSource, runtimeUnitToken); n != 1 {
		return nil, fmt.Errorf("ruby: runtime/_schema.rb opens %q %d times; it must open it once", strings.TrimSpace(runtimeUnitToken), n)
	}
	return []byte(strings.Replace(runtimeSource, runtimeUnitToken, "module "+mod+"\n", 1)), nil
}

// ModuleName is the Ruby module constant of a package: split on `_`, each
// segment capitalized (tabledemo is Tabledemo, my_game is MyGame). CheckNames
// holds the package to the rules that make the mapping one to one.
func ModuleName(pkg string) string {
	var b strings.Builder
	for seg := range strings.SplitSeq(pkg, "_") {
		if seg == "" {
			continue
		}
		b.WriteString(strings.ToUpper(seg[:1]))
		b.WriteString(seg[1:])
	}
	return b.String()
}

// refuseDeclarations refuses the first declaration of the unit, by name and
// kind, naming the layer that carries its kind. The skeleton carries none.
func refuseDeclarations(u *ir.Unit) error {
	for _, f := range u.Files {
		for _, d := range f.Decls {
			kind, plural, name := declKind(d)
			return fmt.Errorf("ruby: %s.schema declares %s %s, and the Ruby target does not carry %s yet (docs/plans/ruby-leg.md, R4 the packet wire)", f.Base, kind, name, plural)
		}
		for _, t := range f.Tables {
			return fmt.Errorf("ruby: %s.schema declares table %s, and the Ruby target does not carry tables yet (docs/plans/ruby-leg.md, R5 fixed tables, R6 variable tables)", f.Base, t.Name)
		}
		for _, un := range f.TableUnions {
			return fmt.Errorf("ruby: %s.schema declares union %s with a table arm, and the Ruby target does not carry tables yet (docs/plans/ruby-leg.md, R6 variable tables)", f.Base, un.Name)
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

// header is the first lines of every generated file: the magic comment Ruby
// reads only on the first line, then the generated-code marker.
func header(from string) string {
	return "# frozen_string_literal: true\n\n# Code generated by the schema compiler from " + from + ". DO NOT EDIT.\n"
}

// fileModule is one schema file's file: it reopens the unit module. The
// declarations arrive with the packet layer.
func fileModule(f *ir.File, mod string) string {
	var b strings.Builder
	b.WriteString(header(f.Base + ".schema"))
	fmt.Fprintf(&b, "\n# The declarations of %s.schema.\nmodule %s\nend\n", f.Base, mod)
	return b.String()
}

// unitModule is <package>.rb: the runtime and one require_relative per schema
// file, so requiring it loads the whole unit, then the protocol id.
func unitModule(u *ir.Unit, mod string, bases []string) string {
	var b strings.Builder
	b.WriteString(header("the unit " + u.Package))
	b.WriteString("\n")
	fmt.Fprintf(&b, "require_relative %q\n", u.Package+"/"+RuntimeBase)
	for _, base := range bases {
		fmt.Fprintf(&b, "require_relative %q\n", u.Package+"/"+base)
	}
	fmt.Fprintf(&b, "\n# The schema unit %s.\nmodule %s\n  PROTOCOL_ID = 0x%016x\nend\n", u.Package, mod, u.ProtocolId)
	return b.String()
}
