// Package squirrel emits the Squirrel target's TYPE surface: one .nut module
// per schema file — Constants.schema -> Constants.nut — deterministic to the
// byte (SPEC §6.1), plus the runtime files under runtime/ emitted beside them
// as schema_<name>.nut. docs/PORT-SQUIRREL.md is the port's design; this file
// is its THE TYPE MAPPING and THE RUNTIME LAYOUT sections in code.
//
// THIS FILE CARRIES NO CODEC. The type wire's read and write join every module
// through [RegisterSurface], from their own files' init (read.go, write.go),
// with their runtime as runtime/bitreader.nut and runtime/bitwriter.nut, so no
// later card edits this file (docs/PORT-SQUIRREL.md, THE CARD MAP).
//
// A MODULE is a script the loader (runtime/module.nut) runs with two
// arguments: the exports table it fills, and the loader's require. Every
// declaration is a slot of that table; a reference to a declaration in
// another file goes through that file's table, required once at the top.
//
// Storage is the §6.1 storage principle in Squirrel's value domain (sq64 built
// with SQUSEDOUBLE, docs/PORT-SQUIRREL.md, 64-BIT AND 128-BIT INTEGERS): a
// record is a CLASS INSTANCE whose every member the constructor initializes,
// in declaration order, with ir.GoExportName member names — the Go, C# and JS
// mapping, so the checker's collision detection covers this target too and no
// schema name meets a Squirrel keyword. Every integer up to 64 bits is the
// 64-bit integer (uint64 is the same bits, unsigned by the codec's reading);
// a 128-bit value is a two-element array [lo, hi] of those; a float is the
// 64-bit float, and a float32 field holds a value float32 can represent.
package squirrel

import (
	"embed"
	"fmt"
	"io/fs"
	"math"
	"math/big"
	"path"
	"sort"
	"strconv"
	"strings"

	schemacheck "github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const ext = "nut"

// RuntimePrefix is what every runtime file is emitted under: runtime/x.nut is
// schema_x.nut beside the modules (docs/PORT-SQUIRREL.md, THE RUNTIME LAYOUT).
// A schema file whose base starts with it is refused, so no module and no
// runtime file can claim one name.
const RuntimePrefix = "schema_"

// runtimeFS is the type-wire runtime: every runtime/*.nut, by glob, so a later
// card adds its file and edits nothing here.
//
//go:embed runtime/*.nut
var runtimeFS embed.FS

// Runtime returns the type surface's runtime files, keyed by the name they are
// emitted under (schema_module.nut first among them: the loader).
func Runtime() map[string][]byte {
	return RuntimeFiles(runtimeFS, "runtime", RuntimePrefix)
}

// RuntimeFiles reads every .nut under dir of fsys and keys it prefix+name.
// The table surface (internal/codegen/squirreltable) emits its own runtime
// directory through it, under its own prefix.
func RuntimeFiles(fsys fs.FS, dir, prefix string) map[string][]byte {
	out := map[string][]byte{}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		panic(err) // the directory is embedded: a failure is a build defect
	}
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != "."+ext {
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			panic(err)
		}
		out[prefix+e.Name()] = data
	}
	return out
}

// Surface is a part of every module a later file adds (docs/PORT-SQUIRREL.md,
// THE CARD MAP): Decl runs after each declaration's types are emitted, File
// once after the last. Either may be nil.
type Surface struct {
	Name string
	Decl func(m *Module, d ir.Decl)
	File func(m *Module)
}

var surfaces []Surface

// RegisterSurface adds a surface to every module this package generates. It
// is called from an init, so the order is the files' (Go runs a package's
// inits in file-name order): read.go before write.go.
func RegisterSurface(s Surface) { surfaces = append(surfaces, s) }

// Module is one file's module while it is generated: what a [Surface] writes
// through.
type Module struct {
	Unit *ir.Unit
	File *ir.File

	body     strings.Builder
	deps     map[string]bool // other schema files this module requires
	runtimes map[string]bool // runtime modules (schema_<name>) it requires
	helpers  map[string]bool // module-scope helpers the body names
}

// Printf appends to the module body.
func (m *Module) Printf(format string, args ...any) { fmt.Fprintf(&m.body, format, args...) }

// Ref is the expression that names declaration decl from this module: _M.X
// here, or _<Base>_.X through the file that declares it.
func (m *Module) Ref(decl string) string { return m.ref(decl, decl) }

// ref names member of the module that declares decl: _M.<member> here, or
// _<Base>_.<member> through the file that declares it.
func (m *Module) ref(decl, member string) string {
	base, ok := m.Unit.DeclFile[decl]
	if !ok || base == m.File.Base {
		return "_M." + member
	}
	m.deps[base] = true
	return depLocal(base) + "." + member
}

// depLocal is the module-scope local that holds another schema file's module.
// The trailing underscore keeps it apart from every other local a module
// declares (_M, _require, _rt_<name>, the helpers, a function's own locals),
// none of which ends in one, so no schema file name can shadow them.
func depLocal(base string) string { return "_" + base + "_" }

// Helper declares the module-scope helper name once and returns the local
// that holds it (see helperSource).
func (m *Module) Helper(name string) string {
	m.helpers[name] = true
	return name
}

// Runtime requires the runtime module schema_<name> and returns the local
// that holds it.
func (m *Module) Runtime(name string) string {
	m.runtimes[name] = true
	return "_rt_" + name
}

// Member is a field's member name in every record: ir.GoExportName.
func Member(f *ir.Field) string { return ir.GoExportName(f.Name) }

// Generate returns basename.nut -> file contents for every file of the unit,
// and the runtime beside them.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	for _, f := range u.Files {
		if strings.HasPrefix(f.Base, RuntimePrefix) {
			return nil, fmt.Errorf("schema file %s.schema collides with the Squirrel runtime, which is emitted as %s<name>.nut beside the modules (docs/PORT-SQUIRREL.md, THE RUNTIME LAYOUT); rename the file", f.Base, RuntimePrefix)
		}
	}
	out := Runtime()
	home := ir.ProtocolIdHome(u)
	for _, f := range u.Files {
		m := &Module{Unit: u, File: f, deps: map[string]bool{}, runtimes: map[string]bool{}, helpers: map[string]bool{}}
		emitFile(m, f.Base == home)
		out[f.Base+"."+ext] = assemble(m)
	}
	return out, nil
}

func assemble(m *Module) []byte {
	var h strings.Builder
	// the basename, not the invocation-relative path: output is deterministic
	// to the byte wherever the compiler runs (SPEC §6.1)
	fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", m.File.Base)
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — protocol id 0x%016x\n", m.Unit.Package, m.Unit.ProtocolId)
	h.WriteString("//\n")
	h.WriteString("// Load through the loader beside this file (docs/PORT-SQUIRREL.md):\n")
	fmt.Fprintf(&h, "//   local %s = dofile(dir + \"/%smodule.nut\", true).require(%q);\n\n", m.File.Base, RuntimePrefix, m.File.Base)
	h.WriteString("local _M = vargv[0];\n")
	h.WriteString("local _require = vargv[1];\n")
	for _, name := range sortedKeys(m.runtimes) {
		fmt.Fprintf(&h, "local _rt_%s = _require(%q);\n", name, RuntimePrefix+name)
	}
	for _, base := range sortedKeys(m.deps) {
		fmt.Fprintf(&h, "local %s = _require(%q);\n", depLocal(base), base)
	}
	h.WriteString("\n")
	for _, name := range helperOrder {
		if m.helpers[name] {
			h.WriteString(helperSource[name])
			h.WriteString("\n")
		}
	}
	h.WriteString(m.body.String())
	// ONE NEWLINE ENDS THE FILE (make/schema-squirrel-generated-lint-gate.mk,
	// the canonical-text rules): every declaration is written with a blank
	// line after it, so the last one's is trimmed here.
	return []byte(strings.TrimRight(h.String(), "\n") + "\n")
}

// THE MODULE-SCOPE HELPERS, each declared only where the body names it, in
// this order. They are locals of the module, so a call reaches them as an
// outer variable, never through a table.
var helperOrder = []string{holder, usedBytes, validText}

const (
	holder    = "_h"
	usedBytes = "_usedBytes"
	validText = "_validText"
)

var helperSource = map[string]string{
	// the serialize port's { value } holder: every call in the module shares
	// it, and a call always consumes it in the statement that fills it, so the
	// read and write paths allocate nothing per field
	holder: "local _h = { value = 0 };\n",
	// _usedBytes(b, n): the first n bytes of a string or bytes buffer as the
	// blob a write hands the serialize port whole: the buffer itself when the
	// value fills it, else one native copy of the used prefix
	usedBytes: `local _noBytes = blob(0);
local _usedBytes = function(b, n) {
  if (n == b.len()) return b;
  if (n == 0) return _noBytes;
  b.seek(0);
  return b.readblob(n);
};
`,
	// _validText(b, n): whether b[0..n) is well-formed UTF-8 (Unicode Table
	// 3-7: no overlong form, no surrogate, nothing past U+10FFFF, no truncated
	// tail) with no zero byte (SPEC §4.7)
	validText: `local _validText = function(b, n) {
  for (local i = 0; i < n; ) {
    local c = b[i]; i++;
    if (c < 0x80) {
      if (c == 0) return false;
      continue;
    }
    local rem, point;
    if ((c & 0xE0) == 0xC0) { rem = 1; point = c & 0x1F; }
    else if ((c & 0xF0) == 0xE0) { rem = 2; point = c & 0x0F; }
    else if ((c & 0xF8) == 0xF0) { rem = 3; point = c & 0x07; }
    else return false;
    if (rem > n - i) return false;
    for (local j = 0; j < rem; j++) {
      local d = b[i]; i++;
      if ((d & 0xC0) != 0x80) return false;
      point = (point << 6) | (d & 0x3F);
    }
    if (rem == 1 && point < 0x80) return false;
    if (rem == 2 && (point < 0x800 || (point >= 0xD800 && point <= 0xDFFF))) return false;
    if (rem == 3 && (point < 0x10000 || point > 0x10FFFF)) return false;
  }
  return true;
};
`,
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// emitFile writes one file's declarations in ir.EmissionOrder: a constant's
// folded value never needs another, but a later surface's top-level tables
// may, and the order is the C/C++ discipline every target shares.
func emitFile(m *Module, carriesProtocolId bool) {
	if carriesProtocolId {
		m.Printf("// The unit's protocol id — the hash of its wire shape (SPEC §3.1), the\n")
		m.Printf("// 64 bits of a uint64 in Squirrel's 64-bit integer.\n")
		m.Printf("_M.ProtocolId <- %s;\n\n", IntLit(new(big.Int).SetUint64(m.Unit.ProtocolId)))
	}
	for _, d := range ir.EmissionOrder(m.File) {
		switch d := d.(type) {
		case *ir.Const:
			emitConst(m, d)
		case *ir.Enum:
			emitEnum(m, d)
		case *ir.Flags:
			emitFlags(m, d)
		case *ir.Struct:
			emitClass(m, d)
		case *ir.Union:
			emitUnion(m, d)
		}
		for _, s := range surfaces {
			if s.Decl != nil {
				s.Decl(m, d)
			}
		}
	}
	for _, s := range surfaces {
		if s.File != nil {
			s.File(m)
		}
	}
}

// emitConst is a schema const as a slot of the module (SPEC §4.2), at its
// folded value: an integer up to 64 bits is the 64-bit integer, a wider one
// the [lo, hi] pair, a float the float (docs/PORT-SQUIRREL.md, THE TYPE
// MAPPING).
func emitConst(m *Module, d *ir.Const) {
	m.Printf("%s", ir.DocComment(d.Doc, "", "//"))
	if d.IsFloat {
		if d.Storage == "float32" {
			m.Printf("_M.%s <- %s;%s\n\n", d.Name, Float32Lit(d.Float), exprComment(d.Expr))
			return
		}
		m.Printf("_M.%s <- %s;%s\n\n", d.Name, Float64Lit(d.Float), exprComment(d.Expr))
		return
	}
	if fitsUint64OrInt64(d.Int) {
		m.Printf("_M.%s <- %s;%s\n\n", d.Name, IntLit(d.Int), exprComment(d.Expr))
		return
	}
	m.Printf("_M.%s <- %s;%s\n\n", d.Name, Int128Lit(d.Int), exprComment(d.Expr))
}

func exprComment(e ir.Expr) string {
	if e == nil {
		return ""
	}
	s := ir.RenderExpr(e)
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return ""
	}
	return " // = " + s
}

// emitEnum is an enum as a table of integers (SPEC §4.2): None = 0, the
// variants dense from 1, Count and Max beside them. A Squirrel `enum` is a
// compile-time constant of one file and no other module can read it, so the
// enum is data.
func emitEnum(m *Module, d *ir.Enum) {
	m.Printf("%s", ir.DocComment(d.Doc, "", "//"))
	m.Printf("// %s — None = 0 implicit, variants dense from 1, wire range [0, %d] (SPEC §4.2)\n", d.Name, d.Max)
	m.Printf("_M.%s <- {\n", d.Name)
	m.Printf("  None = 0,\n")
	for i, v := range d.Variants {
		m.Printf("%s", ir.DocComment(d.VariantDocs[i], "  ", "//"))
		m.Printf("  %s = %d,\n", TableKey(v), i+1)
	}
	m.Printf("  Max = %d, // the exported extent (SPEC §4.2)\n", d.Max)
	m.Printf("};\n\n")
	emitNameFunction(m, "EnumName"+d.Name, d.Variants)
}

// emitNameFunction is the debug/log name of a wire value: out-of-set values
// (wire-legal up to the declared max) name as "???".
func emitNameFunction(m *Module, name string, variants []string) {
	m.Printf("_M.%s <- function(value) {\n", name)
	m.Printf("  switch (value) {\n")
	m.Printf("    case 0: return \"None\";\n")
	for i, v := range variants {
		m.Printf("    case %d: return %s;\n", i+1, StringLit([]byte(v)))
	}
	m.Printf("    default: return \"???\";\n  }\n};\n\n")
}

// emitFlags is a flags declaration as one mask slot per variant, in the Go
// spelling (<Flags><Variant>), and the count (SPEC §4.2). A flags field stores
// uint64 in every target: here the 64-bit integer, bit 63 its sign bit.
func emitFlags(m *Module, d *ir.Flags) {
	m.Printf("%s", ir.DocComment(d.Doc, "", "//"))
	m.Printf("// %s — one bit per variant, consumed as masks; wire %d bits (SPEC §4.2)\n", d.Name, d.WireBits)
	for i, v := range d.Variants {
		m.Printf("%s", ir.DocComment(d.VariantDocs[i], "", "//"))
		m.Printf("_M.%s%s <- 1 << %d;\n", d.Name, v, i)
	}
	m.Printf("_M.%sCount <- %d; // the declared variant count (SPEC §4.2)\n", d.Name, len(d.Variants))
	m.Printf("_M.FlagName%s <- function(bit) {\n", d.Name)
	m.Printf("  switch (bit) {\n")
	for i, v := range d.Variants {
		m.Printf("    case %d: return %s;\n", i, StringLit([]byte(v)))
	}
	m.Printf("    default: return \"???\";\n  }\n};\n\n")
}

// emitClass is a record as a class (docs/PORT-SQUIRREL.md, THE TYPE MAPPING):
// a member per field, every one initialized by the constructor to its
// specified default or its §5 zero, and every buffer pre-allocated — the
// storage principle (SPEC §6.1). The members are declared null in the class
// body because a class-body initializer is SHARED by every instance in
// Squirrel; only the constructor's assignments are the instance's own.
func emitClass(m *Module, d *ir.Struct) {
	m.Printf("%s", ir.DocComment(d.Doc, "", "//"))
	m.Printf("// type %s\n", d.Name)
	m.Printf("_M.%s <- class {\n", d.Name)
	for _, f := range d.Fields {
		for _, member := range fieldMembers(f) {
			m.Printf("  %s = null;\n", member)
		}
	}
	m.Printf("  constructor() {\n")
	if len(d.Fields) == 0 {
		m.Printf("    // empty body — presence is the payload (SPEC §4.6)\n")
	}
	prevGuard := ""
	for _, f := range d.Fields {
		if f.Guard != prevGuard {
			if f.Guard != "" {
				m.Printf("    // %s — wire branch; storage holds both sides, a read zeroes the\n", f.Guard)
				m.Printf("    // untaken side (SPEC §5)\n")
			}
			prevGuard = f.Guard
		}
		m.Printf("%s", ir.DocComment(f.Doc, "    ", "//"))
		emitFieldInit(m, f, "this."+Member(f), "    ")
	}
	m.Printf("  }\n};\n\n")
	maxBits := ir.MaxBitsStruct(d)
	m.Printf("// %sMaxBits is the longest wire path, align padding at its worst case;\n", d.Name)
	m.Printf("// %sMaxBytes is that, rounded up to the 8-byte write-buffer granularity.\n", d.Name)
	m.Printf("_M.%sMaxBits <- %d;\n", d.Name, maxBits)
	m.Printf("_M.%sMaxBytes <- %d;\n\n", d.Name, ir.MaxBytes(maxBits))
}

// fieldMembers is every member one field occupies: the value, and beside it
// the used length of a string, bytes or wstring, or the count of a counted
// array.
func fieldMembers(f *ir.Field) []string {
	name := Member(f)
	out := []string{name}
	switch {
	case f.Array == ir.ArrayNone && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString):
		out = append(out, name+"Length")
	case f.Array == ir.ArrayCounted:
		out = append(out, name+"Count")
	}
	return out
}

// emitFieldInit writes the constructor's initialization of one field into
// target (docs/PORT-SQUIRREL.md, THE TYPE MAPPING). Exported to the surfaces
// through [Module.Init].
func emitFieldInit(m *Module, f *ir.Field, target, ind string) {
	switch {
	case f.Array == ir.ArrayNone && f.Type.Kind == ir.TWString:
		m.Printf("%s%s = array(%d, 0); // wstring(%d): UTF-16 code units, used length beside it (SPEC §4.12)\n", ind, target, f.Type.Size, f.Type.Size)
		m.Printf("%s%sLength = 0;\n", ind, target)
	case f.Array == ir.ArrayNone && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes):
		what := "string"
		if f.Type.Kind == ir.TBytes {
			what = "bytes"
		}
		m.Printf("%s%s = blob(%d); // %s(%d): fixed buffer, used length beside it (SPEC §4.7)\n", ind, target, f.Type.Size, what, f.Type.Size)
		length := 0
		if f.HasDefault {
			for i, b := range f.DefBytes {
				m.Printf("%s%s[%d] = 0x%02x;\n", ind, target, i, b)
			}
			length = len(f.DefBytes)
		}
		m.Printf("%s%sLength = %d;\n", ind, target, length)
	case f.Array != ir.ArrayNone:
		n := f.ArrayBound
		switch {
		case f.Type.Kind == ir.TNamed && isClassRef(f.Type.Ref):
			m.Printf("%s%s = array(%d);\n", ind, target, n)
			m.Printf("%sfor (local i = 0; i < %d; i++) %s[i] = %s();\n", ind, n, target, m.Ref(f.Type.Name))
		case isByteElem(f.Type):
			// [N]uint8 is a blob: the byte buffer the bulk read and write
			// hand over whole (docs/PORT-SQUIRREL.md, BYTE BUFFERS)
			m.Printf("%s%s = blob(%d);\n", ind, target, n)
		default:
			m.Printf("%s%s = array(%d, %s);\n", ind, target, n, zeroValue(f.Type))
		}
		if f.Array == ir.ArrayCounted {
			// a [A..B] count is born at A, the one wire-legal count a fresh
			// value can carry; a [..N] count is born empty (SPEC §4.6)
			m.Printf("%s%sCount = %d;\n", ind, target, f.BornCount())
		}
	case f.Type.Kind == ir.TNamed && isClassRef(f.Type.Ref):
		m.Printf("%s%s = %s();\n", ind, target, m.Ref(f.Type.Name))
	case f.HasDefault:
		m.Printf("%s%s = %s;%s\n", ind, target, defaultValue(f), defaultComment(f))
	default:
		m.Printf("%s%s = %s;\n", ind, target, zeroValue(f.Type))
	}
}

// Init writes the constructor's initialization of field f into target: the
// same code the class constructor runs, for a surface that re-initializes a
// value in place (a read's fresh arm, SPEC §5).
func (m *Module) Init(f *ir.Field, target, ind string) { emitFieldInit(m, f, target, ind) }

func defaultComment(f *ir.Field) string {
	if f.DefVariant != "" {
		return " // " + f.Type.Name + "." + f.DefVariant
	}
	return ""
}

// isClassRef reports a named reference whose storage is a pre-allocated class
// instance: a record or a union (SPEC §4.8).
func isClassRef(ref ir.Decl) bool {
	switch ref.(type) {
	case *ir.Struct, *ir.Union:
		return true
	}
	return false
}

func isByteElem(t ir.FieldType) bool {
	return t.Kind == ir.TInt && !t.Signed && t.Width == 8
}

// zeroValue is the §5 zero form of a scalar slot.
func zeroValue(t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return "false"
	case ir.TFloat32, ir.TFloat64:
		return "0.0"
	case ir.TInt, ir.TFixed, ir.TBits:
		if t.Width > 64 {
			return "[0, 0]"
		}
	}
	return "0"
}

func defaultValue(f *ir.Field) string {
	switch {
	case f.Type.Kind == ir.TBool:
		if f.DefBool {
			return "true"
		}
		return "false"
	case f.DefVariant != "":
		if e, ok := f.Type.Ref.(*ir.Enum); ok {
			for i, v := range e.Variants {
				if v == f.DefVariant {
					return strconv.Itoa(i + 1)
				}
			}
		}
		return "0" // the implicit None variant
	case f.Type.Kind == ir.TFloat32:
		return Float32Lit(f.DefFloat)
	case f.Type.Kind == ir.TFloat64:
		return Float64Lit(f.DefFloat)
	case f.DefInt == nil:
		return zeroValue(f.Type)
	case (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed || f.Type.Kind == ir.TBits) && f.Type.Width > 64:
		return Int128Lit(f.DefInt)
	default:
		return IntLit(f.DefInt)
	}
}

// emitUnion is a one-of (SPEC §4.8): the <Name>Type tag table, then the class
// — the tag beside one pre-allocated arm per variant with a payload. A
// payload-free arm has no storage.
func emitUnion(m *Module, d *ir.Union) {
	m.Printf("// %sType: union %s's tag — None = 0, then each variant in declared order (SPEC §4.8)\n", d.Name, d.Name)
	m.Printf("_M.%sType <- {\n", d.Name)
	m.Printf("  None = 0,\n")
	names := make([]string, len(d.Variants))
	for i, v := range d.Variants {
		names[i] = ir.GoExportName(v.Name)
		m.Printf("%s", ir.DocComment(v.Doc, "  ", "//"))
		m.Printf("  %s = %d,\n", TableKey(names[i]), i+1)
	}
	m.Printf("  Max = %d, // the exported extent (SPEC §4.2)\n", len(d.Variants))
	m.Printf("};\n\n")
	emitNameFunction(m, "EnumName"+d.Name+"Type", names)

	m.Printf("%s", ir.DocComment(d.Doc, "", "//"))
	m.Printf("// %s — at most one of the arms; Type says which. Construction is the empty\n", d.Name)
	m.Printf("// union (None); every arm is pre-allocated, and a read initializes the\n")
	m.Printf("// selected arm as a fresh value before decoding it (SPEC §4.8, §5).\n")
	m.Printf("_M.%s <- class {\n", d.Name)
	m.Printf("  Type = null;\n")
	for i, v := range d.Variants {
		if !v.Void() {
			m.Printf("  %s = null;\n", names[i])
		}
	}
	m.Printf("  constructor() {\n")
	m.Printf("    this.Type = 0;\n")
	for i, v := range d.Variants {
		if v.Void() {
			continue
		}
		armType := v.Type
		if armType == "" && v.F != nil && v.F.Array == ir.ArrayNone && v.F.Type.Kind == ir.TNamed {
			armType = v.F.Type.Name
		}
		if armType != "" {
			m.Printf("    this.%s = %s();\n", names[i], m.Ref(armType))
			continue
		}
		emitFieldInit(m, v.F, "this."+names[i], "    ")
	}
	m.Printf("  }\n};\n\n")
	maxBits := ir.MaxBitsUnion(d)
	m.Printf("// %sMaxBits is the tag plus the largest arm, None the tag alone;\n", d.Name)
	m.Printf("// %sMaxBytes is that, rounded up to the 8-byte write-buffer granularity.\n", d.Name)
	m.Printf("_M.%sMaxBits <- %d;\n", d.Name, maxBits)
	m.Printf("_M.%sMaxBytes <- %d;\n\n", d.Name, ir.MaxBytes(maxBits))
}

// ---------------------------------------------------------------------------
// LITERALS (docs/PORT-SQUIRREL.md, 64-BIT AND 128-BIT INTEGERS)
// ---------------------------------------------------------------------------

var (
	minInt64  = big.NewInt(math.MinInt64)
	maxInt64  = big.NewInt(math.MaxInt64)
	maxUint64 = new(big.Int).SetUint64(math.MaxUint64)
	mask64    = new(big.Int).SetUint64(math.MaxUint64)
)

func fitsUint64OrInt64(v *big.Int) bool {
	return v.Cmp(minInt64) >= 0 && v.Cmp(maxUint64) <= 0
}

// IntLit renders an integer in [-2^63, 2^64) as a Squirrel 64-bit integer
// literal: decimal where the value is an int64 the lexer reads whole, and the
// sixteen-digit hex of its bits otherwise — -2^63, whose decimal magnitude the
// lexer cannot hold, and every uint64 past 2^63 - 1, which is the same bits
// read unsigned. A value outside the range is a caller's defect: it panics.
func IntLit(v *big.Int) string {
	if !fitsUint64OrInt64(v) {
		panic(fmt.Sprintf("squirrel.IntLit: %s is wider than 64 bits", v))
	}
	if v.Cmp(minInt64) == 0 {
		return "0x8000000000000000"
	}
	if v.Cmp(maxInt64) <= 0 {
		return v.String()
	}
	return fmt.Sprintf("0x%016x", v.Uint64())
}

// HexLit renders 64 bits as the sixteen-digit hex literal of those bits: how a
// wire id (docs/SPEC-TABLES.md §5), which is a uint64 hash, is spelled.
func HexLit(v uint64) string { return fmt.Sprintf("0x%016x", v) }

// Int128Lit renders a 128-bit two's complement value as the pair [lo, hi]:
// two 64-bit integers, the low half first, each the bits of its half.
func Int128Lit(v *big.Int) string {
	bits := new(big.Int).Set(v)
	if bits.Sign() < 0 {
		bits.Add(bits, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	lo := new(big.Int).And(bits, mask64)
	hi := new(big.Int).And(new(big.Int).Rsh(bits, 64), mask64)
	return "[" + IntLit(lo) + ", " + IntLit(hi) + "]"
}

// Float64Lit renders a float64 as a Squirrel float literal that reads back to
// the same double: the shortest round-trip spelling, always with a point or
// an exponent so the lexer reads a float. The non-finite values are
// expressions, because Squirrel has no literal for them.
func Float64Lit(v float64) string {
	switch {
	case math.IsNaN(v):
		return "(0.0 / 0.0)"
	case math.IsInf(v, 1):
		return "(1.0 / 0.0)"
	case math.IsInf(v, -1):
		return "(-1.0 / 0.0)"
	}
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEn") {
		s += ".0"
	}
	return s
}

// Float32Lit renders a float32 value as the double that IS that float32
// exactly (f32 ⊂ f64), not the shortest decimal that rounds to it: the stored
// value is then one a float32 slot can hold, and the blob's 'f' write is
// exact (docs/PORT-SQUIRREL.md, BYTE BUFFERS).
func Float32Lit(v float64) string {
	return Float64Lit(float64(float32(v)))
}

// StringLit renders bytes as a Squirrel string literal: printable ASCII as
// written, a quote and a backslash escaped, every other byte as \xhh (the
// lexer reads exactly two hex digits after \x).
func StringLit(b []byte) string {
	var s strings.Builder
	s.WriteByte('"')
	for _, c := range b {
		switch {
		case c == '"' || c == '\\':
			s.WriteByte('\\')
			s.WriteByte(c)
		case c >= 0x20 && c < 0x7f:
			s.WriteByte(c)
		default:
			fmt.Fprintf(&s, "\\x%02x", c)
		}
	}
	s.WriteByte('"')
	return s.String()
}

// TableKey renders a table-literal key: the bare identifier, or ["name"] for
// one the lexer reads as a keyword.
func TableKey(name string) string {
	if keywords[name] {
		return "[" + StringLit([]byte(name)) + "]"
	}
	return name
}

// keywords is Squirrel 3.2's reserved words (squirrel/sqlexer.cpp), read from
// the one table (check.ReservedIn, SPEC §4.6) and never copied here.
var keywords = schemacheck.ReservedIn("Squirrel")
