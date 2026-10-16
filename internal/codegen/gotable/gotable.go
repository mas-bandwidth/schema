// Package gotable emits a unit's Go table surface (docs/SPEC-TABLES.md): the
// TABLE-wire codecs and the reflection descriptors in <Base>Table.go, the
// TEXT form in <Base>TableJson.go, and the two ACCELERATORS on the side in
// <Base>Block.go (§19) and <Base>Cook.go (§7). One file per unit file,
// emitted only when the unit declares tables.
//
// The C++ backend (internal/codegen/cpptable) is the REFERENCE: this port
// follows its framing, elision decisions, clamps and recovery rules. The
// compiler engine (internal/tablewire) is the independent conformance oracle.
// The contract is docs/SPEC-TABLES.md, including the id-table wire (form 1, the variable form). Where Go
// forces a different spelling the reason is stated at the site.
//
// Storage follows the Go PACKET emitter's conventions exactly
// (internal/codegen/golang): structs with exported fields, string(N) and
// bytes(N) as [N]byte beside an int32 used length, arrays as [N]T beside an
// int32 used count, enum-keyed arrays as [E.Max]T, unions as a tag beside one
// arm per variant. That is not a free choice — a table's closure contains
// plain `type` declarations whose storage the packet emitter already wrote,
// and the table codecs decode into those very structs. One unit, one spelling.
//
// The codecs are FREE FUNCTIONS taking a *T, name-first (<Name>Measure,
// <Name>Save, <Name>Load), which is both §11's claimed surface and the Go
// packet emitter's own shape. NOTHING ON THE READ PATH ALLOCATES: every buffer
// is inside the value the caller owns, sub-readers are stack values, and Load
// overlays in place after restoring the declared defaults.
//
// Variable units use canonical POD Row storage, a segmented builder arena,
// and caller-owned regions with self-relative references. LoadMeasure derives
// the exact region size from framing; Load fills it without allocation.
package gotable

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	schemacheck "github.com/mas-bandwidth/schema/v2/internal/check"
	schemaparser "github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// table-wire kinds (docs/SPEC-TABLES.md §3) — the numbers are the wire's, not a
// backend's, and they are duplicated from cpptable deliberately: a port that
// derived them from the reference emitter's private helpers would break the
// day the two files disagree, and this way a disagreement shows up in the
// shared golden bytes instead.
const (
	tkBool   = 1
	tkI8     = 2
	tkI16    = 3
	tkI32    = 4
	tkI64    = 5
	tkU8     = 6
	tkU16    = 7
	tkU32    = 8
	tkU64    = 9
	tkF32    = 10
	tkF64    = 11
	tkString = 12
	tkTable  = 13
	tkArray  = 14
	tkUnion  = 15
)

func tableScalarKind(f *ir.Field) int { return ir.TableScalarKind(f) }

type tableGen struct {
	unit     *ir.Unit
	file     *ir.File
	home     bool // this file carries the unit's shared table runtime
	anyKeyed bool
	owner    *ir.Struct // the closure member whose codec is being emitted

	types  strings.Builder // the table storage structs
	body   strings.Builder // everything else
	indent string          // extra per-line indent while emitting inside a branch guard

	// The OPTIONAL forms this generator emits beside the core: pf writes to
	// out while a form's declarations are being emitted (into), and to body
	// otherwise. The emitters run in one order on one generator either way, so
	// a form's declarations are the bytes they always were; only their file
	// moves (docs/SPEC-TABLES.md, "GO's optional forms").
	out     *strings.Builder
	message strings.Builder // the MESSAGE form: <Base>TableMessage.go
	cook    strings.Builder // the COOK form's write entry points: <Base>Cook.go

	lineage       map[string][]FixedLineageEntry // the locked layouts, oldest first (§5.2)
	needsMath     bool
	needsTime     bool
	unsafeUsed    bool
	wireSerial    int
	retain        bool
	retainOrdinal int
	retainIndex   string
	retainArm     bool
	regional      bool
	armOffset     *int64
	viewPacket    map[string]int
	viewUnions    map[string]int
	viewNoIds     bool
	idOrdinal     map[uint64]int // compile-time slot of each id TableWireIds names

	// Every union declaration reached by this unit's table closure has one
	// immutable descriptor slot, shared by fields, arrays and nested arms.
	unionArmSlot map[string]int
}

func wireIdOrdinals(u *ir.Unit) map[uint64]int {
	ids := ir.TableWireIds(u)
	out := make(map[uint64]int, len(ids))
	for i, id := range ids {
		out[id] = i
	}
	return out
}

func (g *tableGen) knownOrdinal(id uint64) int {
	ord, ok := g.idOrdinal[id]
	if !ok {
		panic(fmt.Sprintf("table id 0x%016x is not in TableWireIds", id))
	}
	return ord
}

// unionArmSlots assigns one immutable descriptor slot per union definition.
func unionArmSlots(u *ir.Unit, _ map[string]bool) map[string]int {
	slots := map[string]int{}
	closure := ir.TableClosureVocabulary(u)
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if un, ok := d.(*ir.Union); ok && closure[un.Name] {
				slots[un.Name] = len(slots)
			}
		}
		for _, un := range f.TableUnions {
			if closure[un.Name] {
				slots[un.Name] = len(slots)
			}
		}
	}
	return slots
}

// needsUnsafe marks this file as reaching for the layout model — every
// descriptor does, through unsafe.Sizeof and unsafe.Offsetof, which are
// CONSTANT EXPRESSIONS of the compiler's own model rather than a guess about
// it (docs/SPEC-TABLES.md §8).
func (g *tableGen) needsUnsafe() { g.unsafeUsed = true }

func (g *tableGen) tf(format string, args ...any) {
	fmt.Fprintf(&g.types, format, args...)
}

func (g *tableGen) pf(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	if g.indent != "" && s != "" {
		trailing := strings.HasSuffix(s, "\n")
		if trailing {
			s = s[:len(s)-1]
		}
		s = g.indent + strings.ReplaceAll(s, "\n", "\n"+g.indent)
		if trailing {
			s += "\n"
		}
	}
	if g.out != nil {
		g.out.WriteString(s)
		return
	}
	g.body.WriteString(s)
}

// into emits one optional form's declarations into that form's own buffer.
func (g *tableGen) into(b *strings.Builder, emit func()) {
	saved := g.out
	g.out = b
	emit()
	g.out = saved
}

// runtimeHome is the basename of the file that carries a unit's shared Go table
// runtime — TableReport, the reflection descriptors, the wire and message
// runtimes, the text walk's home, the region machinery when the unit is
// variable. IT IS THE PACKAGE, and nothing else.
//
// A Go package is one namespace across its files, so a shared runtime need only
// be emitted ONCE, ANYWHERE — and "anywhere" wants a name that does not move.
// Any rule that picks a FILE picks it off the file order, and relocates the
// runtime the day the unit gains a file that sorts earlier: correct output, and
// a diff nobody can read (#347). The package names one home per unit and never
// moves. A unit whose own <Package>.schema exists carries the runtime in that
// file's generated output — same name, one file, no collision; every other unit
// gets the file emitted FOR it, which is what makes the home unconditional.
//
// A file basename that differs from the package ONLY BY CASE is the same file
// name on a case-insensitive filesystem and two on a case-sensitive one, so the
// match is case-insensitive and the FILE's spelling wins.
func runtimeHome(u *ir.Unit) string {
	home := ir.GoExportName(u.Package)
	for _, f := range u.Files {
		if strings.EqualFold(f.Base, home) {
			return f.Base
		}
	}
	return home
}

// unitHasFile reports whether a schema file's basename is base.
func unitHasFile(u *ir.Unit, base string) bool {
	for _, f := range u.Files {
		if f.Base == base {
			return true
		}
	}
	return false
}

// Generate returns <Base>Table.go (plus the accelerators) for every file of a
// unit that declares tables, and nothing when it declares none — a table-free
// unit's generated tree is byte-identical with or without this package.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	return GenerateLineage(u, nil)
}

// Form is one OPTIONAL form of a unit's Go table output, as a set of bits
// (docs/SPEC-TABLES.md, "GO's optional forms"). The core — every
// <Base>Table.go and the unit's <Package>View.go — is never optional; each form
// is a set of files that only ADD that form's entry points, so any subset of
// them may be left out and the package still compiles.
type Form uint8

const (
	FormJson    Form = 1 << iota // <Base>TableJson.go: the TEXT form (§16)
	FormCook                     // <Base>Cook.go: the COOKED form, read and write (§7)
	FormBlock                    // <Base>Block.go: the BLOCK form (§19)
	FormMessage                  // <Base>TableMessage.go: the MESSAGE form (§3.3)

	// FormsAll is every optional form.
	FormsAll = FormJson | FormCook | FormBlock | FormMessage
)

// GenerateOmitting is GenerateLineage without the files of the forms omit
// names. The files are the emitter's own: each optional form is emitted into
// files of its own, so leaving one out is leaving its files out, and what is
// left is exactly what GenerateLineage emits for the forms that remain.
func GenerateOmitting(u *ir.Unit, lineage map[string][]FixedLineageEntry, omit Form) (map[string][]byte, error) {
	out, forms, err := generateForms(u, lineage)
	if err != nil {
		return nil, err
	}
	for name, form := range forms {
		if form&omit != 0 {
			delete(out, name)
		}
	}
	return out, nil
}

// GenerateLineage is Generate with the LINEAGE the build holds for each fixed
// table: the locked layouts, OLDEST FIRST, the current one last
// (docs/FIXED-FORM-ALGORITHM.md §5.2). A nil map is a build with no lock, and
// every table then carries the one entry it can always compute: its own.
func GenerateLineage(u *ir.Unit, lineage map[string][]FixedLineageEntry) (map[string][]byte, error) {
	out, _, err := generateForms(u, lineage)
	return out, err
}

// generateForms is the whole output, and the optional form each file belongs
// to (0 for the core).
func generateForms(u *ir.Unit, lineage map[string][]FixedLineageEntry) (map[string][]byte, map[string]Form, error) {
	forms := map[string]Form{}
	if len(u.Tables) == 0 {
		return map[string][]byte{}, forms, nil
	}
	blocks := ir.Blocks(u)
	out, err := generateBlockFiles(u, blocks)
	if err != nil {
		return nil, nil, err
	}
	for name := range out {
		forms[name] = FormBlock
	}

	regional := len(variableTableNames(u)) > 0

	closure := ir.TableClosure(u)
	home := runtimeHome(u)
	anyKeyed := unitHasKeyedArray(u, closure)
	// the identity pair of an enum is emitted ONCE per unit, by the file that
	// declares it: a Go package is one namespace across its files, so a second
	// definition is a compile error rather than C++'s harmless re-inclusion
	// behind a guard
	usedEnums := closureEnums(u, closure)
	armSlots := unionArmSlots(u, closure)
	idOrdinal := wireIdOrdinals(u)
	// the shared runtime is emitted ONCE, into the package's own home file,
	// whether or not a schema file names the package (docs/SPEC-TABLES.md
	// §19.2). Extracted so the synthesized home and a file that happens to be
	// named for the package emit the same bytes.
	// the cook form's write half: the unit's writer runtime, and each file's
	// <Name>CookFrom entry points, carried into that file's <Base>Cook.go
	cookRuntime := ""
	emitRuntime := func(g *tableGen) {
		g.needsMath = true
		g.needsUnsafe() // the descriptor surface's reset column takes an unsafe.Pointer
		g.pf("// BuildVersion is the unit's build version (docs/SPEC-TABLES.md §20): the\n// one digest a block carries and BlockOpen compares, and the fact §3.3's\n// announcement carries before the first body so a peer's build is knowable.\nconst BuildVersion uint64 = 0x%016x\n\n", ir.BuildVersion(u))
		// the message form's TYPES are the core's: the descriptor's message
		// columns are typed by them, and the core declares the descriptor
		g.pf("%s", tableRuntimeForUnit(regional)+tableRefusalSource+tableWireRuntime(u)+tableMessageCoreTypes(u)+tableFixedRuntime)
		g.into(&g.message, func() { g.pf("%s", tableMessageRuntime(u)) })
		cookRuntime = tableCookWriteSource(u)
		g.emitUnitBlittableRows(blocks)
		if regional {
			g.emitRegionRuntime(blocks)
			g.emitRetainRuntime()
		}
		if anyKeyed {
			g.pf("%s", tableKeyedAccessor)
		}
		if len(armSlots) > 0 {
			g.pf("// One descriptor per union declaration, initialized after package data.\n")
			g.pf("var tableUnionArms = make([]TableUnionInfo, %d)\n\n", len(armSlots))
			if !regional {
				for _, file := range u.Files {
					for _, decl := range file.Decls {
						if un, ok := decl.(*ir.Union); ok {
							if _, used := armSlots[un.Name]; used {
								g.emitUnionRow(un)
							}
						}
					}
					for _, un := range file.TableUnions {
						if _, used := armSlots[un.Name]; used {
							g.emitUnionRow(un)
						}
					}
				}
			}
		}
	}
	runtimeWritten := false
	cookWriters := map[string]string{}
	for _, f := range u.Files {
		g := &tableGen{unit: u, file: f, home: f.Base == home, anyKeyed: anyKeyed, unionArmSlot: armSlots, regional: regional, idOrdinal: idOrdinal, lineage: lineage}
		members := fileMembers(f, closure)
		if g.home {
			emitRuntime(g)
			runtimeWritten = true
		}
		for _, un := range f.TableUnions {
			g.emitTableUnion(un)
		}
		for _, st := range members {
			if st.IsTable {
				g.owner = st
				g.emitTableStruct(st)
			}
		}
		for _, e := range fileEnums(f, usedEnums) {
			g.emitEnumIdentity(e)
		}
		for _, st := range members {
			g.owner = st
			g.emitTableReset(st)
			g.emitTimestampAccessors(st)
			g.emitTableMeasure(st)
			g.emitTableWrite(st)
			g.emitTableSave(st)
			g.emitTableRead(st)
			g.emitRetainMember(st)
			g.into(&g.cook, func() { g.emitCookWriteSurface(st) })
			g.into(&g.message, func() {
				g.emitMessageWrite(st)
				g.emitMessageRead(st)
				if regional {
					g.emitRegionMessage(st)
				}
			})
			if regional {
				g.emitRegionMember(st)
				for _, f := range st.Fields {
					if f.IsList() {
						g.emitListSurface(st, f)
					} else if f.IsMap() {
						g.emitMapSurface(st, f)
					}
				}
			}
		}
		g.emitFixedForm(members)
		if len(members) > 0 {
			g.pf("// ---- reflection descriptors (tables only, docs/SPEC-TABLES.md §8) ----\n\n")
			for _, st := range members {
				g.owner = st
				g.emitTableDescriptor(st)
			}
			g.emitUnionArms()
		}
		src, err := g.assemble()
		if err != nil {
			return nil, nil, err
		}
		out[f.Base+"Table.go"] = src
		if err := g.emitForm(out, forms, f.Base+"TableMessage.go", FormMessage, &g.message); err != nil {
			return nil, nil, err
		}
		cookWriters[f.Base] = g.cook.String()
	}
	// No file of the unit is named for the package, so the home is emitted FOR
	// the unit rather than for a file. The runtime lands in <Package>Table.go
	// either way — that is the whole rule, and it is what keeps an
	// earlier-sorting file from moving it (§19.2).
	if !runtimeWritten {
		g := &tableGen{unit: u, home: true, anyKeyed: anyKeyed, unionArmSlot: armSlots, regional: regional, idOrdinal: idOrdinal, lineage: lineage}
		emitRuntime(g)
		src, err := g.assemble()
		if err != nil {
			return nil, nil, err
		}
		out[home+"Table.go"] = src
		if err := g.emitForm(out, forms, home+"TableMessage.go", FormMessage, &g.message); err != nil {
			return nil, nil, err
		}
	}
	cooks, err := generateCookFiles(u, blocks, cookRuntime, cookWriters)
	if err != nil {
		return nil, nil, err
	}
	for name := range cooks {
		forms[name] = FormCook
	}
	maps.Copy(out, cooks)

	// the TEXT form (docs/SPEC-TABLES.md §16): one generic walk over the
	// descriptors, in its own file so a project that never reads or writes a
	// text does not compile it
	texts, err := generateJsonFiles(u, closure, home)
	if err != nil {
		return nil, nil, err
	}
	for name := range texts {
		forms[name] = FormJson
	}
	maps.Copy(out, texts)
	view, err := generateViewFile(u, closure, armSlots)
	if err != nil {
		return nil, nil, err
	}
	maps.Copy(out, view)
	return out, forms, nil
}

// emitForm assembles one optional form's buffer into its own file, when the
// form emitted anything for this file.
func (g *tableGen) emitForm(out map[string][]byte, forms map[string]Form, name string, form Form, b *strings.Builder) error {
	if b.Len() == 0 {
		return nil
	}
	src, err := g.assembleForm(name, form, b.String())
	if err != nil {
		return err
	}
	out[name] = src
	forms[name] = form
	return nil
}

// assembleForm is assemble for an optional form's own file: the same header,
// that form's declarations, and the imports they use.
func (g *tableGen) assembleForm(name string, form Form, body string) ([]byte, error) {
	var h strings.Builder
	if g.file != nil {
		fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.file.Base)
	} else {
		fmt.Fprintf(&h, "// Code generated by the schema compiler for package %s. DO NOT EDIT.\n", g.unit.Package)
	}
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	switch form {
	case FormMessage:
		fmt.Fprintf(&h, "// package %s — the MESSAGE FORM (docs/SPEC-TABLES.md §3.3): the\n", g.unit.Package)
		h.WriteString("// announcement, the vocabulary, the bit-packed batch codec and every fixed\n")
		h.WriteString("// table's <Name>MeasureMessages, <Name>SaveMessages and <Name>LoadMessages.\n")
		h.WriteString("// An OPTIONAL form (docs/SPEC-TABLES.md, GO's optional forms): the types it\n")
		h.WriteString("// works on are the core's, so the package compiles with this file left out.\n")
	}
	h.WriteString("\n")
	fmt.Fprintf(&h, "package %s\n\n", g.unit.Package)
	h.WriteString(goImports(usedImports(body, formImports)))
	h.WriteString(body)
	src, err := format.Source([]byte(repoPrefix(h.String())))
	if err != nil {
		return nil, fmt.Errorf("generated Go for %s does not parse — a compiler bug, not a schema error: %w", name, err)
	}
	return src, nil
}

// formImports is every import an optional form's file may need, in the order
// a core file spells them.
var formImports = []string{`"encoding/binary"`, `"unicode/utf8"`, `"sync"`, `"sync/atomic"`, `"slices"`, `"math"`, `"time"`, `"unsafe"`, `"github.com/mas-bandwidth/serialize.go"`}

// usedImports keeps the imports body refers to: a selector on an identifier
// the file does not declare (go/parser leaves it unresolved) names a package.
// A file of one form references only what it uses, so an import the core
// needed and the form does not is not spelled — Go refuses an unused import.
func usedImports(body string, candidates []string) []string {
	f, err := parser.ParseFile(token.NewFileSet(), "", "package p\n\n"+body, 0)
	if err != nil {
		return candidates // format.Source reports the real error
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Obj == nil {
				used[id.Name] = true
			}
		}
		return true
	})
	var keep []string
	for _, c := range candidates {
		path := strings.Trim(c, `"`)
		name := path[strings.LastIndex(path, "/")+1:]
		name = strings.TrimSuffix(name, ".go")
		if used[name] {
			keep = append(keep, c)
		}
	}
	return keep
}

// fileMembers is a file's closure members: its tables, then the plain `type`
// declarations the closure reaches.
func fileMembers(f *ir.File, closure map[string]bool) []*ir.Struct {
	var members []*ir.Struct
	members = append(members, f.Tables...)
	for _, d := range f.Decls {
		if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
			members = append(members, st)
		}
	}
	return members
}

func (g *tableGen) assemble() ([]byte, error) {
	usesSerialize := strings.Contains(g.types.String()+g.body.String(), "serialize.")
	var h strings.Builder
	// a home the unit has no file for is generated FOR THE UNIT, and says so —
	// there is no <Base>.schema to name
	if g.file != nil {
		fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.file.Base)
	} else {
		fmt.Fprintf(&h, "// Code generated by the schema compiler for package %s. DO NOT EDIT.\n", g.unit.Package)
	}
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — the TABLE wire (docs/SPEC-TABLES.md): evolution-tolerant, neutral\n", g.unit.Package)
	h.WriteString("// bytes.")
	if usesSerialize {
		h.WriteString(" Wide values use serialize.go storage.")
	}
	h.WriteString(" Tables version by field id, never by the\n")
	h.WriteString("// unit's protocol id.\n")
	if g.home {
		h.WriteString("//\n")
		h.WriteString("// Measure/Save/Load are NAME-FIRST free functions over a *T: <Name>Measure\n")
		h.WriteString("// gives the exact wire size, <Name>Save writes exactly that many bytes into\n")
		h.WriteString("// the caller's slice, <Name>Load overlays a value in place and reports every\n")
		h.WriteString("// tolerance event. Nothing here allocates: the caller owns the value, the\n")
		h.WriteString("// slice and the report.\n")
	}
	h.WriteString("\n")
	fmt.Fprintf(&h, "package %s\n\n", g.unit.Package)
	var std []string
	if g.home {
		std = append(std, `"encoding/binary"`, `"unicode/utf8"`)
	}
	if g.home && g.regional {
		std = append(std, `"sync"`, `"sync/atomic"`)
		if unitHasContainers(g.unit) {
			std = append(std, `"slices"`)
		}
	}
	if g.needsMath {
		std = append(std, `"math"`)
	}
	if g.needsTime {
		std = append(std, `"time"`)
	}
	if g.unsafeUsed {
		std = append(std, `"unsafe"`)
	}
	if usesSerialize {
		std = append(std, `"github.com/mas-bandwidth/serialize.go"`)
	}
	// the core's imports are its own: what an optional form needed went with it
	h.WriteString(goImports(usedImports(g.types.String()+g.body.String(), std)))
	h.WriteString(g.types.String())
	h.WriteString(g.body.String())
	src, err := format.Source([]byte(repoPrefix(h.String())))
	if err != nil {
		base := runtimeHome(g.unit)
		if g.file != nil {
			base = g.file.Base
		}
		return nil, fmt.Errorf("generated Go for %sTable.go does not parse — a compiler bug, not a schema error: %w", base, err)
	}
	return src, nil
}

// repoPrefix marks every bare reference to this repository's own paths that a
// generated comment cites — docs/... and internal/... — with the repository's
// name, so an adopter's path linter reads them as another repository's rather
// than its own (docs/USAGE.md Go notes).
var repoPrefixRe = regexp.MustCompile(`(docs|internal)/`)

func isPathWordByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// repoPrefix marks every bare reference to this repository's own paths that a
// generated comment cites — docs/... and internal/... — with the repository's
// name, so an adopter's path linter reads them as another repository's rather
// than its own. A path already inside a longer one (an import like
// github.com/mas-bandwidth/schema/v2/internal/ast) is left alone: it is
// preceded by a word byte or a slash, which a bare citation never is.
func repoPrefix(src string) string {
	idxs := repoPrefixRe.FindAllStringIndex(src, -1)
	if len(idxs) == 0 {
		return src
	}
	var b strings.Builder
	last := 0
	for _, loc := range idxs {
		start := loc[0]
		if start > 0 {
			c := src[start-1]
			if isPathWordByte(c) || c == '/' {
				continue
			}
		}
		b.WriteString(src[last:start])
		b.WriteString("mas-bandwidth/schema ")
		b.WriteString(src[start:loc[1]])
		last = loc[1]
	}
	b.WriteString(src[last:])
	return b.String()
}

// goImports renders an import block the way gofmt would have written it: a
// lone import takes the one-line form, which is what a hand-written file of
// this shape looks like. go/format does not rewrite the grouping, so the
// emitter picks it.
func goImports(paths []string) string {
	switch len(paths) {
	case 0:
		return ""
	case 1:
		return "import " + paths[0] + "\n\n"
	}
	var b strings.Builder
	b.WriteString("import (\n")
	for _, p := range paths {
		fmt.Fprintf(&b, "\t%s\n", p)
	}
	b.WriteString(")\n\n")
	return b.String()
}

// variableTableNames is the unit's variable-length tables, sorted — the tables
// whose table surface uses canonical region storage.
func variableTableNames(u *ir.Unit) []string {
	variable := ir.VariableTables(u)
	if len(variable) == 0 {
		return nil
	}
	names := make([]string, 0, len(variable))
	for name := range variable {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// closureEnums is every enum whose values ride in the unit's table closure.
func closureEnums(u *ir.Unit, closure map[string]bool) map[string]*ir.Enum {
	used := map[string]*ir.Enum{}
	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			// an enum-keyed array's KEY rides as a variant hash too, so its
			// enum needs the identity pair even when no field has that type
			if f.KeyEnumRef != nil {
				used[f.KeyEnumRef.Name] = f.KeyEnumRef
			}
			if f.Type.Kind != ir.TNamed {
				continue
			}
			if e, isEnum := f.Type.Ref.(*ir.Enum); isEnum {
				used[e.Name] = e
			}
		}
	}
	for name := range ir.TableClosureVocabulary(u) {
		un := u.Unions[name]
		if un == nil {
			un = u.TableUnions[name]
		}
		if un != nil {
			for _, v := range un.Variants {
				if v.F != nil {
					if e := enumRef(v.F); e != nil {
						used[e.Name] = e
					}
				}
			}
		}
	}
	return used
}

// fileEnums is the used enums this file DECLARES, in declaration order.
func fileEnums(f *ir.File, used map[string]*ir.Enum) []*ir.Enum {
	var out []*ir.Enum
	for _, d := range f.Decls {
		if e, ok := d.(*ir.Enum); ok {
			if live, ok := used[e.Name]; ok {
				out = append(out, live)
			}
		}
	}
	return out
}

// unitHasKeyedArray reports whether any closure member declares an enum-keyed
// array, which is what decides whether the unit carries the keyed accessor.
func unitHasKeyedArray(u *ir.Unit, closure map[string]bool) bool {
	for name := range closure {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.KeyEnum != "" {
				return true
			}
		}
	}
	return false
}

// tableRuntime is the unit's shared table runtime, emitted into ONE file per
// unit (<Package>Table.go, the package's home). C++ emits it into every header
// behind an include guard; a Go package is one namespace across its files, so a
// second copy would be a redeclaration error instead.
func tableRuntime() string {
	return `// TableReport is the table-wire read report — the permissive contract's
// ledger. Silence (all zero) means the data matched this reader's schema
// exactly. Err (docs/SPEC-TABLES.md §6.5) is the STRICT read's typed reason
// for malformed, clamped, kind-mismatched, unknown form, wire-damaged and
// build-version-mismatched. Unknown, Widened and Retained alone are not one.
type TableReport struct {
 Retained, RetainLost int32
	Verdict      TableOpenVerdict // unsupported form is distinct from damage
	Reason       string // why the form was refused; empty for a file read
	Widened      int32 // a compatible wider kind preserved the value
	Unknown      int32 // unknown field ids skipped (newer data)
	KindMismatch int32 // known id, changed type — skipped, never misdecoded
	Clamped      int32 // out-of-range values clamped to declared bounds
	// Duplicate counts repeated text keys. Fixed-class wire fields do not raise it.
	// A body carrying an id twice is legal input
	// whose last occurrence wins, silently. A wire read always leaves it zero.
	Duplicate int32
	// MissingRequired counts required fields (docs/SPEC-TABLES.md §2.3) the
	// record did not carry: a record an older writer produced before the field
	// became required. The refusal names the field in Reason.
	MissingRequired int32
	Malformed bool // framing damage; decode stopped, partial result kept
	// BuildVersionPeer and BuildVersionOwn are the two build versions a strict
	// announcement refusal carries (docs/SPEC-TABLES.md §3.3): the peer's
	// announcement and this unit's own. They are zero on every other read.
	BuildVersionPeer uint64
	BuildVersionOwn  uint64
	// LayoutHash is THE FILE'S hash, and it is set by ONE refusal:
	// layout_newer reports it "AND NOTHING ELSE" (§5.3). layout_unsupported
	// reports it too — the operator's other answer, a layout this build served
	// and has retired.
	LayoutHash uint64
}

// ---- reflection (tables only, docs/SPEC-TABLES.md §8) ----
//
// Static field descriptors for every type in the table closure: name, the
// TEXT form's key, wire id and kind, storage offset, bounds, ranges, the
// enum/union vocabulary and its wire ids, and branch guards — enough to walk,
// print, diff, edit or bind any table value at runtime with no schema files on
// hand. <Name>TableType() returns the descriptor.
//
// The descriptors carry MEMORY facts — Offset, ElemSize, CountOffset,
// PresentOffset and TableTypeInfo.Size — exactly as the C++ reference does,
// because Go can state them: unsafe.Offsetof and unsafe.Sizeof are constant
// expressions of the compiler's own layout model, so the generated numbers are
// this build's own rather than a guess about it. They are what lets one
// generic walk drive the text form for every table.

// THE SHARED EMPTY DOC (docs/SPEC-TABLES.md §8.1): a declaration with no ///
// block carries a doc column naming this one definition, so absence costs a
// unit no string data and a printer concatenates doc columns with no nil test.
// Go gives a string no address identity, so what the rule is READ OFF here is
// the emitted text: the generated file defines the empty doc once and every
// unannotated row names that definition, rather than each row carrying an
// inline "" of its own.
const TableDocNone = ""

// TableFieldInfo is one field's descriptor.
type TableFieldInfo struct {
 CookOffset, CookElemSize, CookCountOffset, CookPresentOffset uint32
 List, Map bool // unbounded by-value containers
 ElemAlign uint32
	Name     string // schema field name, e.g. "health"
	Json     string // the TEXT form's key: the json = "key" attribute, else Name (§16.3)
	TypeName string // schema element type, e.g. "float32", "Grade"
	DeclaredTypeName string // complete schema spelling including arrays, bounds and pointers
	Id       uint64 // table-wire field id (name hash; the was alias's hash after a rename)
	Pointer bool
 TargetId uint64
 Kind     uint8  // table-wire kind; for arrays/strings/bytes, the ELEMENT kind
	IsArray  bool   // fixed or counted array (bytes included)
	Counted  bool   // a <Name>Count/<Name>Length int32 companion exists
	Optional bool   // a ?T field: a <Name>Present bool decides whether it rides

	ArrayBound    int32  // array capacity / string max length; 0 for plain scalars
	Offset        uint32 // unsafe.Offsetof the storage member
	ElemSize      uint32 // unsafe.Sizeof the member (element size for arrays)
	CountOffset   uint32 // unsafe.Offsetof the count/length companion, or 0xffffffff
	PresentOffset uint32 // unsafe.Offsetof the Present companion, or 0xffffffff

	FracBits uint8 // fixed-point binary scale
 WideRange bool
 WideMin, WideMax [2]uint64 // exact two-complement raw bounds, low lane first
 HasRange bool    // a declared [min, max] (int or float)
	RangeMin float64 // NOTE: int64 ranges beyond 2^53 lose precision here
	RangeMax float64

	// EnumMax bounds the vocabulary: enums, the highest valid value (None = 0
	// is always valid); unions, the arm count (tag range [0, EnumMax]); a
	// FLAGS field, the highest declared BIT INDEX; else -1.
	EnumMax int64
	// EnumName is the vocabulary's names, indexed the way EnumMax bounds: an
	// enum's value -> name, a union's tag -> arm name, a FLAGS field's bit
	// index -> variant name. nil for every other kind.
	EnumName func(value uint64) string
	// VariantId is the TABLE-WIRE id of one variant (docs/SPEC-TABLES.md §5): for
	// an enum, the hash of the variant's name; for a union, the hash of the
	// arm's name. Hash zero is an ordinary id; ordinal zero is None. nil
	// for every other kind — a FLAGS field's variants have no per-variant wire
	// id (§4), so a nil here beside a non-nil EnumName is what says "flags".
	VariantId func(value uint64) uint64

	// an ENUM-KEYED array (docs/SPEC-TABLES.md §2.4, §8): the array has one slot
	// per variant of KeyTypeName, indexed by the variant's value, and its
	// slots ride under variant ids rather than positions. KeyName and KeyId
	// are the key's vocabulary — walk [0, ArrayBound) to print slots by name.
	// All three are nil/"" on every other field.
	KeyTypeName string
	KeyName     func(value uint64) string
	KeyId       func(value uint64) uint64

	// Arms is a union field's shape: the tag and the arms indexed by it, behind
	// a function so a descriptor graph needs no initialization order. nil for
	// every other kind.
	Arms func() *TableUnionInfo

	Guard string // branch guard, e.g. "at_rest" or "!at_rest"; "" if unguarded

	// Table is the nested table's descriptor, or nil. Held as a factory rather
	// than a value so a descriptor graph needs no initialization order: Go
	// refuses an initialization cycle across package-level variables, and a
	// table may name one declared later.
	Table func() *TableTypeInfo

	// what a PERSON wrote about the field (docs/SPEC-TABLES.md §8.1): the ///
	// block above it, verbatim (SPEC §4.1). It is TableDocNone when there is
	// none, never nil. Its tags (SPEC §4.2) follow in declared order, and an
	// untagged field is 0 beside a nil list. Static package data, allocating
	// nothing.
	Doc     string
	NumTags int32
	Tags    []string
}

// TableUnionArmInfo is one arm of a union field: where its payload sits inside
// the union's storage and what its payload looks like. The arm's NAME and its
// table-wire id come from the field's EnumName/VariantId functions at the same
// tag, so nothing is spelled twice (docs/SPEC-TABLES.md §8).
type TableUnionArmInfo struct {
 Field TableFieldInfo // arm shape; offsets are relative to union storage
 Void bool // a selected payload-free arm
 Reset func(storage unsafe.Pointer) // establish selected payload defaults
	Offset uint32               // unsafe.Offsetof the arm's payload within the union storage
	Table  func() *TableTypeInfo // the arm payload's descriptor
}

// TableUnionInfo is a union field's shape: the tag, and the arms indexed by
// it. Arms run [0, EnumMax]; index 0 is the EMPTY arm and carries no payload.
type TableUnionInfo struct {
	CookTagSize uint32
	TagOffset uint32 // unsafe.Offsetof the tag within the union storage
	TagSize   uint32 // unsafe.Sizeof the tag
	Arms      []TableUnionArmInfo
}

// TableTypeInfo is one type's descriptor.
type TableTypeInfo struct {
	CookSize, CookAlign uint32
	Name      string // schema type name
	Size      uint32 // unsafe.Sizeof the storage struct
 Id uint64
 Variable bool
 NodeType func(uint64)*TableTypeInfo
 SaveBody func(*TableWriter, unsafe.Pointer) bool
 LoadBody func(TableReader, unsafe.Pointer) bool
 SaveMessageBody func(*TableMessageWriter,unsafe.Pointer) bool
 LoadMessageBody func(TableMessageReader,unsafe.Pointer)(TableMessageReader,bool)
	NumFields int32
	Fields    []TableFieldInfo
	// Reset puts one instance back at its declared defaults, in place. A
	// generic walker that fills a value has to be able to establish the
	// defaults an absent field takes, and it holds no type to spell — this is
	// the one thing the descriptors could not express without it.
	Reset func(storage unsafe.Pointer)

	// the declaration's own doc and tags, on the same terms as a field's
	// (docs/SPEC-TABLES.md §8.1, SPEC §4.1, §4.2)
	Doc     string
	NumTags int32
	Tags    []string
}

// The helpers below began as the TEXT form's, and the core uses them too: the
// table and view sources read storage through the same width switch and
// evaluate the same guards, so they are declared here, once, and the text
// form's own file only adds its walk (docs/SPEC-TABLES.md, "GO's optional forms").

type tableJsonGraphNode struct { refs int; label uint64; written bool }

// ---- storage access: the descriptors give an offset and a width, and the
// ---- storage is the HOST's, so every load and store goes through a width
// ---- switch rather than a copy into the low bytes of a wider word

func tableJsonGetRaw(storage unsafe.Pointer, width uint32) uint64 {
	switch width {
	case 1:
		return uint64(*(*uint8)(storage))
	case 2:
		return uint64(*(*uint16)(storage))
	case 4:
		return uint64(*(*uint32)(storage))
	case 8:
		return *(*uint64)(storage)
	}
	return 0
}

func tableJsonSetRaw(storage unsafe.Pointer, width uint32, value uint64) {
	switch width {
	case 1:
		*(*uint8)(storage) = uint8(value)
	case 2:
		*(*uint16)(storage) = uint16(value)
	case 4:
		*(*uint32)(storage) = uint32(value)
	case 8:
		*(*uint64)(storage) = value
	}
}

func tableJsonGetSigned(storage unsafe.Pointer, width uint32) int64 {
	raw := tableJsonGetRaw(storage, width)
	if width < 8 {
		sign := uint64(1) << (width*8 - 1)
		if raw&sign != 0 {
			raw |= ^((sign << 1) - 1)
		}
	}
	return int64(raw)
}

type tableMapKey struct { text []byte; raw uint64 }
func tableMapKeyValid(k tableMapKey,f *TableFieldInfo)bool {return f.Kind!=12||k.raw==0&&len(k.text)<=int(f.ArrayBound)}
func tableMapStoredKey(p unsafe.Pointer,f *TableFieldInfo)tableMapKey {key:=tableMapKey{};if f.Kind==12 {n:=*(*int32)(unsafe.Add(p,f.CountOffset));if n<0||n>f.ArrayBound{return tableMapKey{text:nil,raw:1}};key.text=unsafe.Slice((*byte)(unsafe.Add(p,f.Offset)),int(n))}else{key.raw=tableJsonGetRaw(unsafe.Add(p,f.Offset),f.ElemSize);if f.Kind>=2&&f.Kind<=5 {key.raw=uint64(tableJsonGetSigned(unsafe.Add(p,f.Offset),f.ElemSize))}};return key}
func tableMapStoreKey(p unsafe.Pointer,f *TableFieldInfo,k tableMapKey) {if f.Kind==12 {clear(unsafe.Slice((*byte)(unsafe.Add(p,f.Offset)),int(f.ArrayBound)+1));copy(unsafe.Slice((*byte)(unsafe.Add(p,f.Offset)),len(k.text)),k.text);*(*int32)(unsafe.Add(p,f.CountOffset))=int32(len(k.text))}else{tableJsonSetRaw(unsafe.Add(p,f.Offset),f.ElemSize,k.raw)}}
func tableMapKeyOrder(a,b tableMapKey,f *TableFieldInfo)int {if f.Kind==12 {for i:=0;i<len(a.text)&&i<len(b.text);i++ {if a.text[i]<b.text[i]{return -1};if a.text[i]>b.text[i]{return 1}};if len(a.text)<len(b.text){return -1};if len(a.text)>len(b.text){return 1};return 0};if f.Kind>=2&&f.Kind<=5 {if int64(a.raw)<int64(b.raw){return -1};if int64(a.raw)>int64(b.raw){return 1};return 0};if a.raw<b.raw{return -1};if a.raw>b.raw{return 1};return 0}

// A guarded group rides only when its guard reads true — the wire's own elision
// (§4), carried into the text so a text and a wire written from one instance
// say the same thing. The guard is spelled as its branch condition over bool
// fields of the SAME type ("at_rest", "!at_rest", "active && has_target"), so
// evaluating it is a walk of the same descriptor. Nothing is inferred in the
// other direction: reading places every key it can name, and the guard is a
// plain bool key (§16.2).
func tableJsonGuardHolds(base unsafe.Pointer, info *TableTypeInfo, guard string) bool {
	p := 0
	for {
		for p < len(guard) && (guard[p] == ' ' || guard[p] == '&') {
			p++
		}
		if p >= len(guard) {
			return true
		}
		want := true
		if guard[p] == '!' {
			want = false
			p++
		}
		start := p
		for p < len(guard) && guard[p] != ' ' && guard[p] != '&' {
			p++
		}
		name := guard[start:p]
		value := false
		for i := range info.Fields {
			f := &info.Fields[i]
			if f.Name == name {
				value = tableJsonGetRaw(unsafe.Add(base, uintptr(f.Offset)), f.ElemSize) != 0
				break
			}
		}
		if value != want {
			return false
		}
	}
}
`
}

// tableKeyedAccessor is the enum-keyed array's one indexing helper
// (docs/SPEC-TABLES.md §2.4). Emitted only into a unit that declares a keyed array,
// so a unit without one is byte-identical to what it was.
//
// WHAT GO SPELLS DIFFERENTLY, stated where a reader meets it. C++ has an
// operator[] on a storage template and C# an indexer on a generic class; Go
// has neither operator overloading nor generic array extents, so an enum-keyed
// array's STORAGE IS the plain array the schema means — [E.Max]T, the key k at
// index k-1, which is also exactly what the packet emitter writes for the same
// declaration on the type wire. The extent is still derived and named nowhere
// else: it is the generated E.Max constant and no other number.
//
// The SHIFT and the None refusal live here, in one function, so no call site
// in any language spells either (§2.4). A None key is a program error and this
// panics in EVERY build — the C++ abort and the C# throw, in Go's spelling —
// because the storage holds no slot for None and indexing one byte before an
// array is not a thing a config read may do.
const tableKeyedAccessor = `// TableKeyed indexes an enum-keyed array's slots BY KEY: the storage shifts
// left, so the key k lives at index k-1 and nothing is stored for None
// (docs/SPEC-TABLES.md §2.4). It hands back a pointer, so the same call reads and
// fills a slot.
//
// NONE IS THE NULL KEY: it names no slot, it never rides on the wire, a stored
// key of 0 is malformed, and INDEXING BY IT IS A PROGRAM ERROR IN EVERY BUILD.
// The panic stands in release too: the alternative is reading the element
// before the array.
//
// The generated codecs walk the array by STORAGE INDEX and never call this;
// it is the surface a CALLER writes, so no call site spells the shift:
//
//	*TableKeyed(hull.Turrets[:], int(WeaponMissile)) = turret
//
// THE GUARD IS SYMMETRIC, and §2.4's argument for the None compare is what
// makes it so: the storage holds one slot per NAMED variant and nothing else,
// so a key of 0 indexes the element BEFORE the array and a key past E.Max
// indexes past its end. Both are the same program error, both are refused in
// EVERY BUILD, and neither compare is removable — a build that dropped them
// would read memory that is not a slot.
func TableKeyed[T any](slots []T, key int) *T {
	if key <= 0 || key > len(slots) {
		panic("an enum-keyed array is indexed by a NAMED variant of its key enum: " +
			"None keys no slot, and neither does a value past E.Max")
	}
	return &slots[key-1]
}

`

// timestampFields is a table's timestamp fields, in declaration order.
func timestampFields(st *ir.Struct) []*ir.Field {
	var out []*ir.Field
	for _, f := range st.Fields {
		if f.Type.Kind == ir.TInt && f.Type.Timestamp {
			out = append(out, f)
		}
	}
	return out
}

// emitTimestampAccessors is the Go VALUE of a timestamp (docs/SPEC-TABLES.md
// §2.11): a time.Time, in UTC on read, through two methods beside the field.
//
// The field itself is the int64 count and has to be: a table's storage is the
// blittable record the cook, the block and the fixed form lay out at declared
// widths (§19, §20), and a time.Time carries a pointer. So the time.Time is the
// accessor pair, and the REFUSAL a time outside Time.UnixNano's domain owes
// ("by name, on write") is the setter's: the count it would store does not
// exist, so nothing is stored and the error names the field. Every int64 the
// field can hold is a time, so nothing downstream of the setter can refuse.
func (g *tableGen) emitTimestampAccessors(st *ir.Struct) {
	fields := timestampFields(st)
	if len(fields) == 0 || st.IsMapEntry() {
		return
	}
	g.needsTime = true
	recv := g.storageName(st.Name)
	for _, f := range fields {
		name := member(f)
		g.pf("// %sTime is %s.%s as a time.Time, in UTC: the field counts nanoseconds since\n", name, st.Name, name)
		g.pf("// the Unix epoch, and 0 is the zero time, 1970-01-01T00:00:00Z (SPEC.md §4.13).\n")
		g.pf("func (v *%s) %sTime() time.Time { return tableTimestampTime(v.%s) }\n\n", recv, name, name)
		g.pf("// Set%sTime stores a time.Time in %s.%s. A time outside Time.UnixNano's\n", name, st.Name, name)
		g.pf("// domain, the years 1678 through 2262, has no count: it is REFUSED with a\n")
		g.pf("// TableTimestampError naming the field and the field is left as it was. Go's\n")
		g.pf("// own zero Time{} is the zero time and stores 0.\n")
		g.pf("func (v *%s) Set%sTime(t time.Time) error {\n", recv, name)
		g.pf("\tn, ok := tableTimestampNanos(t)\n\tif !ok {\n\t\treturn TableTimestampError{Field: %q, Time: t}\n\t}\n", st.Name+"."+name)
		g.pf("\tv.%s = n\n\treturn nil\n}\n\n", name)
	}
}

// ---- the optional shared Go runtime (docs/SPEC-TABLES.md §8.4) ----
//
// Standalone is the default and stays byte-identical to every build before this
// option existed. Shared makes a unit's home file import tables/goruntime for
// the declarations the emitter writes identically for EVERY unit shape, instead
// of carrying a copy of them. There is one source: the package's text is the
// emitter's own text for those declarations (sharedRuntime derives it from the
// emitter's output for the probe units below), so the package and the inline copy
// cannot drift, and a test fails when the checked-in package and the emitter
// disagree.

// SharedRuntimeImport is the import path of the shared Go runtime package.
const SharedRuntimeImport = "github.com/mas-bandwidth/schema/v2/tables/goruntime"

// SharedRuntimeFile is the one file of the shared package, relative to
// tables/goruntime.
const SharedRuntimeFile = "goruntime.go"

// Runtime says where a Go unit's table-independent machinery lives.
type Runtime uint8

const (
	// RuntimeStandalone emits the machinery into the unit (the default).
	RuntimeStandalone Runtime = iota
	// RuntimeShared imports it from SharedRuntimeImport.
	RuntimeShared
)

// ParseRuntime reads the generator option `runtime`: absent, empty or
// "standalone" is standalone; "shared" is shared; anything else is refused by
// name (docs/SPEC-TABLES.md §8.4).
func ParseRuntime(v string) (Runtime, error) {
	switch v {
	case "", "standalone":
		return RuntimeStandalone, nil
	case "shared":
		return RuntimeShared, nil
	}
	return RuntimeStandalone, fmt.Errorf("go option runtime=%q is not a runtime: use standalone (the default) or shared (docs/SPEC-TABLES.md §8.4)", v)
}

// GenerateRuntime is GenerateLineage with the runtime choice. RuntimeStandalone
// returns exactly GenerateLineage's bytes.
func GenerateRuntime(u *ir.Unit, lineage map[string][]FixedLineageEntry, rt Runtime) (map[string][]byte, error) {
	out, err := GenerateLineage(u, lineage)
	if err != nil || rt == RuntimeStandalone || len(out) == 0 {
		return out, err
	}
	sr, err := loadSharedRuntime()
	if err != nil {
		return nil, err
	}
	name := runtimeHome(u) + "Table.go"
	src, err := sr.apply(out[name])
	if err != nil {
		return nil, fmt.Errorf("shared runtime for %s: %w", name, err)
	}
	out[name] = src
	return out, nil
}

// SharedRuntimeFiles is the shared package's source, keyed by file name.
func SharedRuntimeFiles() (map[string][]byte, error) {
	sr, err := loadSharedRuntime()
	if err != nil {
		return nil, err
	}
	return map[string][]byte{SharedRuntimeFile: sr.pkg}, nil
}

// sharedProbes are the unit shapes whose home files decide what is shared: a
// declaration is shared only when every probe's home file carries it
// byte-for-byte. Together they switch on every conditional the runtime emitter
// has: fixed-only and variable units, containers, wide text, float64, keyed
// arrays and unions.
var sharedProbes = []string{
	"package probe\nfixed table A { n int32 }\n",
	`package probe
enum Team { Red, Blue }
flags Caps { Jump, Fly }
type Point { x int32 }
union Value { tally int32
 label string(8)
 point Point
 ping }
fixed table Gunner { reaction float64 = 0.5 }
fixed table B {
 name string(16)
 tag bytes(4)
 caps Caps
 value Value
 slots [Team]Gunner
 maybe ?Gunner
 samples [..3]float32
}
`,
	"package probe\ntable A { n int32\n name string(8) }\n",
	`package probe
fixed table Child { n int32 = 7 }
enum Key { first }
union Choice { one Child
 ping }
table Root { numbers []int32
 children []Child
 pointers []*Child
 head *Child
 pick Choice
 pairs map[uint8]Child
 slots [Key]Child }
`,
	"package probe\nfixed table W { words wstring(8) }\n",
	"package probe\ntable W { words wstring(8)\n n float64 }\n",
}

// rtDecl is one top-level declaration of a probe's home file.
type rtDecl struct {
	key     string // the declared names joined; a method is "(T).m"
	names   []string
	recv    string // the receiver's base type, or ""
	text    string // the source slice, doc comment included
	start   int    // byte offsets of text in the file
	end     int
	idents  map[string]bool // identifiers the body uses, selectors' fields excluded
	members map[string]bool // selector fields and composite-literal keys it uses
	fields  []string        // unexported struct fields and method name this declares
	typ     bool
	skip    bool // generic, var or init: never shared
	order   int
}

type sharedRuntime struct {
	pkg     []byte
	ordered []string // keys of the shared declarations, package order
	text    map[string]string
	shim    string
	version uint64
}

var (
	sharedRuntimeOnce sync.Once
	sharedRuntimeVal  *sharedRuntime
	sharedRuntimeErr  error
)

func loadSharedRuntime() (*sharedRuntime, error) {
	sharedRuntimeOnce.Do(func() { sharedRuntimeVal, sharedRuntimeErr = buildSharedRuntime() })
	return sharedRuntimeVal, sharedRuntimeErr
}

func rtParseDecls(path string, src []byte) ([]rtDecl, *ast.File, error) {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, path, src, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}
	off := func(p token.Pos) int { return fs.Position(p).Offset }
	var out []rtDecl
	for i, decl := range f.Decls {
		d := rtDecl{idents: map[string]bool{}, members: map[string]bool{}, order: i}
		var doc *ast.CommentGroup
		switch x := decl.(type) {
		case *ast.FuncDecl:
			doc = x.Doc
			d.names = []string{x.Name.Name}
			d.key = x.Name.Name
			if x.Recv != nil && len(x.Recv.List) == 1 {
				t := x.Recv.List[0].Type
				if st, ok := t.(*ast.StarExpr); ok {
					t = st.X
				}
				if ix, ok := t.(*ast.IndexExpr); ok {
					t = ix.X
					d.skip = true
				}
				if id, ok := t.(*ast.Ident); ok {
					d.recv = id.Name
					d.key = "(" + id.Name + ")." + x.Name.Name
					if !ast.IsExported(x.Name.Name) {
						d.fields = append(d.fields, x.Name.Name)
					}
				}
			}
			if x.Type.TypeParams != nil || x.Name.Name == "init" {
				d.skip = true
			}
		case *ast.GenDecl:
			if x.Tok == token.IMPORT {
				continue
			}
			doc = x.Doc
			if x.Tok == token.VAR {
				d.skip = true
			}
			for _, sp := range x.Specs {
				switch s := sp.(type) {
				case *ast.TypeSpec:
					d.names = append(d.names, s.Name.Name)
					d.typ = true
					if s.TypeParams != nil {
						d.skip = true
					}
					if st, ok := s.Type.(*ast.StructType); ok {
						for _, fl := range st.Fields.List {
							for _, n := range fl.Names {
								if !ast.IsExported(n.Name) {
									d.fields = append(d.fields, n.Name)
								}
							}
							if len(fl.Names) == 0 {
								d.skip = true // embedded: members promote across types
							}
						}
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						d.names = append(d.names, n.Name)
					}
				}
			}
			d.key = strings.Join(d.names, ",")
		}
		d.start, d.end = off(decl.Pos()), off(decl.End())
		if doc != nil {
			d.start = off(doc.Pos())
		}
		d.text = string(src[d.start:d.end])
		ast.Inspect(decl, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				d.members[x.Sel.Name] = true
				ast.Inspect(x.X, func(m ast.Node) bool {
					if id, ok := m.(*ast.Ident); ok {
						d.idents[id.Name] = true
					}
					return true
				})
				return false
			case *ast.KeyValueExpr:
				if id, ok := x.Key.(*ast.Ident); ok {
					d.members[id.Name] = true
					d.idents[id.Name] = true
				}
			case *ast.Field:
				// a declared field or parameter name is not a use
				ast.Inspect(x.Type, func(m ast.Node) bool {
					if id, ok := m.(*ast.Ident); ok {
						d.idents[id.Name] = true
					}
					return true
				})
				return false
			case *ast.Ident:
				d.idents[x.Name] = true
			}
			return true
		})
		out = append(out, d)
	}
	return out, f, nil
}

func rtProbeUnit(src string) (*ir.Unit, error) {
	f, perrs := schemaparser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		return nil, perrs[0]
	}
	u, cerrs := schemacheck.Unit([]schemacheck.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		return nil, cerrs[0]
	}
	return u, nil
}

func lowerFirst(name string) bool {
	r := []rune(name)
	return len(r) > 0 && unicode.IsLower(r[0])
}

func buildSharedRuntime() (*sharedRuntime, error) {
	type probe struct {
		home   []rtDecl
		others []rtDecl
		file   *ast.File
		src    []byte
	}
	var probes []probe
	for _, src := range sharedProbes {
		u, err := rtProbeUnit(src)
		if err != nil {
			return nil, fmt.Errorf("shared runtime probe: %w", err)
		}
		files, err := GenerateLineage(u, nil)
		if err != nil {
			return nil, fmt.Errorf("shared runtime probe: %w", err)
		}
		var p probe
		names := make([]string, 0, len(files))
		for n := range files {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			ds, f, err := rtParseDecls(n, files[n])
			if err != nil {
				return nil, fmt.Errorf("shared runtime probe %s: %w", n, err)
			}
			if n == runtimeHome(u)+"Table.go" {
				p.home, p.file, p.src = ds, f, files[n]
			} else {
				p.others = append(p.others, ds...)
			}
		}
		if p.home == nil {
			return nil, fmt.Errorf("shared runtime probe has no home file")
		}
		probes = append(probes, p)
	}
	// identical in every probe
	seen := map[string]map[string]int{}
	byKey := map[string]rtDecl{}
	for _, p := range probes {
		for _, d := range p.home {
			if seen[d.key] == nil {
				seen[d.key] = map[string]int{}
				byKey[d.key] = d
			}
			seen[d.key][d.text]++
		}
	}
	cand := map[string]bool{}
	for k, m := range seen {
		if len(m) == 1 && m[byKey[k].text] == len(probes) && !byKey[k].skip {
			cand[k] = true
		}
	}
	// the universe: every package-level name any probe declares, and every
	// unexported member each type declares
	universe := map[string]bool{}
	methodsOf := map[string]map[string]bool{} // type -> method keys, all files
	memberNames := map[string]map[string]bool{}
	nameKey := map[string]string{}
	for _, p := range probes {
		for _, d := range append(slices.Clone(p.home), p.others...) {
			if d.recv == "" {
				for _, n := range d.names {
					universe[n] = true
				}
			} else {
				if methodsOf[d.recv] == nil {
					methodsOf[d.recv] = map[string]bool{}
				}
				methodsOf[d.recv][d.key] = true
			}
			if d.typ {
				if memberNames[d.names[0]] == nil {
					memberNames[d.names[0]] = map[string]bool{}
				}
				for _, f := range d.fields {
					memberNames[d.names[0]][f] = true
				}
			} else if d.recv != "" {
				if memberNames[d.recv] == nil {
					memberNames[d.recv] = map[string]bool{}
				}
				for _, f := range d.fields {
					memberNames[d.recv][f] = true
				}
			}
		}
	}
	for k := range cand {
		for _, n := range byKey[k].names {
			nameKey[n] = k
		}
	}
	for changed := true; changed; {
		changed = false
		drop := func(k string) {
			if cand[k] {
				delete(cand, k)
				changed = true
			}
		}
		inCand := map[string]bool{}
		for k := range cand {
			for _, n := range byKey[k].names {
				if byKey[k].recv == "" {
					inCand[n] = true
				}
			}
		}
		// (a) a shared declaration names only shared declarations
		for k := range cand {
			d := byKey[k]
			own := map[string]bool{}
			for _, n := range d.names {
				own[n] = true
			}
			for id := range d.idents {
				if universe[id] && !inCand[id] && !own[id] {
					drop(k)
					break
				}
			}
		}
		// (b) a method rides with its type, and a type with every method
		for k := range cand {
			d := byKey[k]
			if d.recv != "" && !inCand[d.recv] {
				drop(k)
			}
			if d.typ {
				for mk := range methodsOf[d.names[0]] {
					if !cand[mk] {
						drop(k)
					}
				}
			}
		}
		// (c) code outside the shared set must not reach an unexported member
		nonShared := map[string]bool{} // members used by code outside the shared set
		for _, p := range probes {
			for _, d := range append(slices.Clone(p.home), p.others...) {
				if !cand[d.key] {
					for m := range d.members {
						nonShared[m] = true
					}
				}
			}
		}
		for k := range cand {
			d := byKey[k]
			if !d.typ {
				continue
			}
			for f := range memberNames[d.names[0]] {
				if nonShared[f] {
					drop(k)
					break
				}
			}
		}
	}
	// the home file's own order, from the first probe that carries each
	var ordered []string
	have := map[string]bool{}
	for _, d := range probes[0].home {
		if cand[d.key] && !have[d.key] {
			ordered = append(ordered, d.key)
			have[d.key] = true
		}
	}
	for k := range cand {
		if !have[k] {
			return nil, fmt.Errorf("shared runtime: %s is shared but absent from the first probe", k)
		}
	}
	// exported spellings
	rename := map[string]string{}
	for _, k := range ordered {
		d := byKey[k]
		if d.recv != "" {
			continue
		}
		for _, n := range d.names {
			if lowerFirst(n) {
				up := strings.ToUpper(n[:1]) + n[1:]
				if universe[up] {
					return nil, fmt.Errorf("shared runtime: cannot export %s as %s: the name is taken", n, up)
				}
				rename[n] = up
			}
		}
	}
	for n := range rename {
		for _, m := range memberNames {
			if m[n] {
				return nil, fmt.Errorf("shared runtime: %s is both a package name and a member name", n)
			}
		}
	}
	// imports the shared text uses
	imp := map[string]string{} // local name -> quoted path
	for _, is := range probes[0].file.Imports {
		path, _ := strconv.Unquote(is.Path.Value)
		name := path[strings.LastIndex(path, "/")+1:]
		if is.Name != nil {
			name = is.Name.Name
		}
		imp[name] = is.Path.Value
	}
	var body strings.Builder
	for _, k := range ordered {
		body.WriteString(byKey[k].text)
		body.WriteString("\n\n")
	}
	used := map[string]bool{}
	if pf, err := parser.ParseFile(token.NewFileSet(), "", "package x\n"+body.String(), 0); err == nil {
		ast.Inspect(pf, func(n ast.Node) bool {
			if sx, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sx.X.(*ast.Ident); ok && imp[id.Name] != "" {
					used[id.Name] = true
				}
			}
			return true
		})
	} else {
		return nil, fmt.Errorf("shared runtime text does not parse: %w", err)
	}
	var paths []string
	for n := range used {
		paths = append(paths, imp[n])
	}
	sort.Strings(paths)
	text := rtRename("package goruntime\n\n"+goImports(paths)+body.String(), rename)
	sum := sha256.Sum256([]byte(text))
	version := binary.BigEndian.Uint64(sum[:8])
	var pkg strings.Builder
	pkg.WriteString("// Code generated by the schema compiler (internal/codegen/gotable) from the table runtime it\n")
	pkg.WriteString("// emits into every standalone Go unit. DO NOT EDIT.\n")
	pkg.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	pkg.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	pkg.WriteString("// AGPL-3.0, its output is not.\n")
	pkg.WriteString("\n")
	pkg.WriteString("// Package goruntime is the optional shared Go runtime of the schema table wire\n")
	pkg.WriteString("// (docs/SPEC-TABLES.md §8.4): the declarations the compiler writes identically\n")
	pkg.WriteString("// into every standalone unit, once. A unit generated with the Go option\n")
	pkg.WriteString("// runtime=shared aliases these names instead of carrying a copy. The package\n")
	pkg.WriteString("// versions with the compiler: Version is a digest of this source, and a unit\n")
	pkg.WriteString("// asserts it at compile time, so a unit and a runtime from different compilers\n")
	pkg.WriteString("// is a build error, never a run-time surprise.\n")
	pkg.WriteString(text)
	fmt.Fprintf(&pkg, "// Version identifies this runtime. A generated unit asserts it.\nconst Version uint64 = 0x%016x\n", version)
	pkgSrc, err := format.Source([]byte(pkg.String()))
	if err != nil {
		return nil, fmt.Errorf("shared runtime package does not format: %w", err)
	}
	// the shim: what a unit declares in place of the copy
	var shim strings.Builder
	shim.WriteString("// ---- the shared runtime (docs/SPEC-TABLES.md §8.4): this unit was generated with\n")
	shim.WriteString("// the Go option runtime=shared, so the table-independent machinery is imported ----\n\n")
	fmt.Fprintf(&shim, "// A unit and its runtime come from one compiler: either difference overflows.\nconst _ = uint64(%s.Version - 0x%016x)\nconst _ = uint64(0x%016x - %s.Version)\n\n", sharedRuntimePackage, version, version, sharedRuntimePackage)
	for _, k := range ordered {
		d := byKey[k]
		if d.recv != "" {
			continue
		}
		s, err := rtShim(d.text, rename)
		if err != nil {
			return nil, err
		}
		shim.WriteString(s)
	}
	sr := &sharedRuntime{pkg: pkgSrc, ordered: ordered, text: map[string]string{}, shim: shim.String(), version: version}
	for _, k := range ordered {
		sr.text[k] = byKey[k].text
	}
	return sr, nil
}

const sharedRuntimePackage = "goruntime"

// rtRename spells every package-level name in rename by its exported name. A
// bijection on identifiers preserves every binding, a field or method name
// never changes (buildSharedRuntime checks the two sets are disjoint), and the
// result is formatted.
func rtRename(src string, rename map[string]string) string {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "", src, parser.ParseComments)
	if err != nil {
		panic(err)
	}
	type edit struct {
		at       int
		from, to string
	}
	var edits []edit
	skip := map[*ast.Ident]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			skip[x.Sel] = true
		case *ast.FuncDecl:
			if x.Recv != nil {
				skip[x.Name] = true
			}
		case *ast.Field:
			for _, nm := range x.Names {
				skip[nm] = true
			}
		}
		return true
	})
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && !skip[id] {
			if to, ok := rename[id.Name]; ok {
				edits = append(edits, edit{fs.Position(id.Pos()).Offset, id.Name, to})
			}
		}
		return true
	})
	sort.Slice(edits, func(i, j int) bool { return edits[i].at > edits[j].at })
	b := []byte(src)
	for _, e := range edits {
		b = append(b[:e.at], append([]byte(e.to), b[e.at+len(e.from):]...)...)
	}
	return string(b)
}

// rtShim is the declaration a unit writes in place of one shared package-level
// declaration: a type alias, an untyped-preserving const, or a forwarding
// function (small enough to inline).
func rtShim(text string, rename map[string]string) (string, error) {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "", "package x\n"+text, parser.ParseComments)
	if err != nil {
		return "", err
	}
	target := func(n string) string {
		if to, ok := rename[n]; ok {
			n = to
		}
		return sharedRuntimePackage + "." + n
	}
	var b strings.Builder
	for _, decl := range f.Decls {
		switch x := decl.(type) {
		case *ast.GenDecl:
			for _, sp := range x.Specs {
				switch s := sp.(type) {
				case *ast.TypeSpec:
					fmt.Fprintf(&b, "type %s = %s\n", s.Name.Name, target(s.Name.Name))
				case *ast.ValueSpec:
					if x.Tok != token.CONST {
						return "", fmt.Errorf("shared runtime: %s is not a const", s.Names[0].Name)
					}
					for _, nm := range s.Names {
						fmt.Fprintf(&b, "const %s = %s\n", nm.Name, target(nm.Name))
					}
				}
			}
		case *ast.FuncDecl:
			var sig bytes.Buffer
			ft := *x.Type
			var args []string
			params := &ast.FieldList{}
			n := 0
			for _, fl := range x.Type.Params.List {
				nf := *fl
				nf.Names = nil
				cnt := len(fl.Names)
				if cnt == 0 {
					cnt = 1
				}
				for i := 0; i < cnt; i++ {
					name := fmt.Sprintf("a%d", n)
					n++
					nf.Names = append(nf.Names, ast.NewIdent(name))
					if _, ok := fl.Type.(*ast.Ellipsis); ok {
						args = append(args, name+"...")
					} else {
						args = append(args, name)
					}
				}
				params.List = append(params.List, &nf)
			}
			ft.Params = params
			if err := format.Node(&sig, token.NewFileSet(), &ast.FuncDecl{Name: x.Name, Type: &ft}); err != nil {
				return "", err
			}
			call := fmt.Sprintf("%s(%s)", target(x.Name.Name), strings.Join(args, ", "))
			if x.Type.Results != nil && len(x.Type.Results.List) > 0 {
				call = "return " + call
			}
			fmt.Fprintf(&b, "%s { %s }\n", sig.String(), call)
		}
	}
	return b.String() + "\n", nil
}

// apply replaces a standalone home file's shared declarations with the import
// and the shim. A declaration the unit spells differently from the package is
// an error, never a silent second copy.
func (sr *sharedRuntime) apply(src []byte) ([]byte, error) {
	decls, f, err := rtParseDecls("home", src)
	if err != nil {
		return nil, err
	}
	fs := token.NewFileSet()
	if f, err = parser.ParseFile(fs, "home", src, parser.ParseComments); err != nil {
		return nil, err
	}
	type cut struct{ a, b int }
	var cuts []cut
	for _, d := range decls {
		want, ok := sr.text[d.key]
		if !ok {
			continue
		}
		if want != d.text {
			return nil, fmt.Errorf("declaration %s differs from the shared runtime's (add a probe for this unit shape to sharedProbes)", d.key)
		}
		cuts = append(cuts, cut{d.start, d.end})
	}
	var impStart, impEnd int
	for _, decl := range f.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			impStart, impEnd = fs.Position(gd.Pos()).Offset, fs.Position(gd.End()).Offset
		}
	}
	var out bytes.Buffer
	// the unit's body without the shared declarations; the import block is
	// rebuilt after, from what the remaining text uses
	at := impEnd
	var rest bytes.Buffer
	for _, c := range cuts {
		if c.a < at {
			continue
		}
		rest.Write(src[at:c.a])
		at = c.b
	}
	rest.Write(src[at:])
	imports := map[string]string{}
	for _, is := range f.Imports {
		path, _ := strconv.Unquote(is.Path.Value)
		name := path[strings.LastIndex(path, "/")+1:]
		if is.Name != nil {
			name = is.Name.Name
		}
		imports[name] = is.Path.Value
	}
	usedNames := map[string]bool{}
	if pf, err := parser.ParseFile(token.NewFileSet(), "", "package x\n"+rest.String(), 0); err == nil {
		ast.Inspect(pf, func(n ast.Node) bool {
			if sx, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sx.X.(*ast.Ident); ok && imports[id.Name] != "" {
					usedNames[id.Name] = true
				}
			}
			return true
		})
	} else {
		return nil, fmt.Errorf("remaining text does not parse: %w", err)
	}
	var paths []string
	for n := range usedNames {
		paths = append(paths, imports[n])
	}
	paths = append(paths, strconv.Quote(SharedRuntimeImport))
	sort.Slice(paths, func(i, j int) bool {
		// standard library first, then the rest, each sorted
		si, sj := !strings.Contains(paths[i], "."), !strings.Contains(paths[j], ".")
		if si != sj {
			return si
		}
		return paths[i] < paths[j]
	})
	out.Write(src[:impStart])
	out.WriteString(goImports(paths))
	out.WriteString(sr.shim)
	out.Write(rest.Bytes())
	return format.Source(out.Bytes())
}
