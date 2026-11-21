// Package ruby emits the Ruby target (docs/PORT-RUBY.md): one namespace per
// unit, named for the unit's package, holding one file per schema file, its
// RBS signature, and the runtime beside them, deterministic to the byte
// (SPEC §6.1).
//
//	<package>.rb                 the unit module: its protocol id; requires the runtime and every file
//	<package>.rbs                the unit's RBS type signature for Steep strict mode
//	<package>/<Base>.rb          one file per schema file, reopening the unit module
//	<package>/_schema.rb         the runtime: Refused, InvalidValue, LoadReport and validators
//	<package>/serialize.rb       serialize.rb entrypoint, vendored by sha256
//	<package>/serialize/core.rb  serialize.rb bit layer, scoped inside the unit module
package ruby

import (
	_ "embed"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/genheader"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const ext = "rb"

// RuntimeBase is the runtime file's base name inside every generated unit's
// directory: <package>/_schema.rb.
const RuntimeBase = "_schema"

// SerializeBase is the base name serialize.rb takes beside the runtime
// (docs/PORT-RUBY.md, THE RUNTIME LAYOUT).
const SerializeBase = "serialize"

// runtimeUnitToken is the one module name the runtime source opens.
const runtimeUnitToken = "module SchemaRuntimeUnit\n"

//go:embed runtime/_schema.rb
var runtimeSource string

//go:embed runtime/serialize.rb
var serializeEntrySource string

//go:embed runtime/serialize/core.rb
var serializeCoreSource string

// Generate returns path -> file contents for the unit, every path relative to
// the output directory: <package>.rb, <package>.rbs, and the files under
// <package>/.
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
	mod := ModuleName(u.Package)
	pkg := u.Package
	out := map[string][]byte{}
	bases := make([]string, 0, len(u.Files))
	for _, f := range u.Files {
		bases = append(bases, f.Base)
		out[path.Join(pkg, f.Base+"."+ext)] = []byte(emitModule(u, f, mod))
	}
	sort.Strings(bases)
	out[pkg+"."+ext] = []byte(unitModule(u, mod, bases))
	out[pkg+".rbs"] = []byte(emitRBS(u, mod))
	rtFiles, err := RuntimeFiles(mod)
	if err != nil {
		return nil, err
	}
	for rel, data := range rtFiles {
		out[path.Join(pkg, rel)] = data
	}
	return out, nil
}

// Runtime returns the unit's _schema.rb file for the unit whose module
// constant is mod.
func Runtime(mod string) ([]byte, error) {
	if n := strings.Count(runtimeSource, runtimeUnitToken); n != 1 {
		return nil, fmt.Errorf("ruby: runtime/_schema.rb opens %q %d times; it must open it once", strings.TrimSpace(runtimeUnitToken), n)
	}
	return []byte(strings.Replace(runtimeSource, runtimeUnitToken, "module "+mod+"\n", 1)), nil
}

// RuntimeFiles returns every runtime file emitted under <package>/ for the
// unit whose module constant is mod.
func RuntimeFiles(mod string) (map[string][]byte, error) {
	schemaRB, err := Runtime(mod)
	if err != nil {
		return nil, err
	}
	coreRB, err := scopedSerializeCore(mod)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{
		RuntimeBase + "." + ext:   schemaRB,
		SerializeBase + "." + ext: []byte(serializeEntrySource),
		"serialize/core.rb":       coreRB,
	}, nil
}

// RawSerializeRuntime returns the verbatim vendored serialize.rb files as
// embedded in runtime/.
func RawSerializeRuntime() map[string][]byte {
	return map[string][]byte{
		"serialize.rb":      []byte(serializeEntrySource),
		"serialize/core.rb": []byte(serializeCoreSource),
	}
}

// scopedSerializeCore wraps the vendored serialize/core.rb inside module <mod>
// and aliases Stream, WriteStream, ReadStream and MeasureStream onto <mod>, so
// two generated units loaded in one interpreter never share or re-initialize
// a top-level Serialize module under ruby -w.
func scopedSerializeCore(mod string) ([]byte, error) {
	const magic = "# frozen_string_literal: true\n"
	if !strings.HasPrefix(serializeCoreSource, magic) {
		return nil, fmt.Errorf("ruby: runtime/serialize/core.rb lacks frozen_string_literal header")
	}
	rest := strings.TrimPrefix(serializeCoreSource, magic)
	rest = strings.TrimLeft(rest, "\n")
	var b strings.Builder
	b.WriteString(magic)
	b.WriteString("\nmodule ")
	b.WriteString(mod)
	b.WriteString("\n")
	for line := range strings.SplitSeq(strings.TrimRight(rest, "\n"), "\n") {
		if line == "" {
			b.WriteString("\n")
		} else {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString("  Stream = Serialize::Stream\n")
	b.WriteString("  WriteStream = Serialize::WriteStream\n")
	b.WriteString("  ReadStream = Serialize::ReadStream\n")
	b.WriteString("  MeasureStream = Serialize::MeasureStream\n")
	b.WriteString("end\n")
	return []byte(b.String()), nil
}

// ModuleName is the Ruby module constant of a package: split on `_`, each
// segment capitalized (tabledemo is Tabledemo, my_game is MyGame).
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

// refuseDeclarations refuses tables and table unions until R5 and R6 carry them.
func refuseDeclarations(u *ir.Unit) error {
	for _, f := range u.Files {
		for _, t := range f.Tables {
			return fmt.Errorf("ruby: %s.schema declares table %s, and the Ruby target does not carry tables yet (docs/plans/ruby-leg.md, R5 fixed tables, R6 variable tables)", f.Base, t.Name)
		}
		for _, un := range f.TableUnions {
			return fmt.Errorf("ruby: %s.schema declares union %s with a table arm, and the Ruby target does not carry tables yet (docs/plans/ruby-leg.md, R6 variable tables)", f.Base, un.Name)
		}
	}
	return nil
}

func unitModule(u *ir.Unit, mod string, bases []string) string {
	var b strings.Builder
	b.WriteString("# frozen_string_literal: true\n\n")
	fmt.Fprintf(&b, "# Code generated by the schema compiler from the unit %s. DO NOT EDIT.\n", u.Package)
	b.WriteString(genheader.Line("#") + "\n")
	fmt.Fprintf(&b, "require_relative \"%s/%s\"\n", u.Package, RuntimeBase)
	fmt.Fprintf(&b, "require_relative \"%s/%s\"\n", u.Package, SerializeBase)
	for _, base := range bases {
		fmt.Fprintf(&b, "require_relative \"%s/%s\"\n", u.Package, base)
	}
	fmt.Fprintf(&b, "\nmodule %s\n  PROTOCOL_ID = 0x%016x\nend\n", mod, u.ProtocolId)
	return b.String()
}
