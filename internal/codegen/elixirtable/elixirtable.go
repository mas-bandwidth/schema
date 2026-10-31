// Package elixirtable emits the Elixir TABLE surface (docs/SPEC-TABLES.md):
// the BLOCK form's read and build sides in <Base>Block.ex (§19, §19.1) and the
// COOKED form's read and write sides in <Base>Cook.ex and <ns>.Table (§7, §7.6),
// with their shared runtime modules and the unit's BuildVersion module, emitted
// only when the unit declares tables.
//
// THE ACCELERATOR TIER. A block is POINTED AT and a cook is OPENED; neither
// parses a wire, so both reach every fixed table of the unit. Beside them the
// VARIABLE FORM is emitted for the whole variable class: the id-table wire's
// codec, the reflection descriptors and the text form of docs/SPEC-TABLES.md
// §3, §4, §8 and §16 in their id-table form — pointers to tables and the flat
// node table, blobs (*bytes, *string), unbounded arrays of scalar, string and
// enum elements and arrays of pointers, the remaining scalars, and nested
// tables by value. A unit that is only a map or a union is refused by name
// (§11). A unit that also carries unbounded arrays still generates: the roots
// that reach a map, a union, or an enum-keyed array are uncarried and answer
// an error, and the driver writes <case>.absent rather than a wrong byte.
package elixirtable

import (
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// BuildVersionModule is the shared module a unit with tables grows beside the
// block and cook runtimes. It is a file basename as well as a module name, so
// it is claimed the way every other generated spelling is: a schema file named
// for it would collide (docs/SPEC-TABLES.md §11).
const BuildVersionModule = "BuildVersion"

// Generate returns filename -> file contents for the unit's table surface:
// the cook runtime when anything is cookable, the build version, one Cook
// module per file that has one, and the block modules. Empty when the unit
// declares no table: a table-free unit's Elixir output is byte-identical with
// this backend in the chain or out of it.
func Generate(u *ir.Unit) (map[string][]byte, error) { return GenerateLineage(u, nil) }

// GenerateLineage is [Generate] with the LOCK'S LINEAGE handed in: per fixed
// table, the locked layouts OLDEST FIRST, the current one last, each carrying
// §5.2's facts (docs/FIXED-FORM-ALGORITHM.md §5.9 #1). THE BACKEND OPENS NO
// FILE: `lockfile.Open`, `lockfile.Lineage` and `lockfile.Floor` are the
// CALLER's three calls, so the disk is read in one place and a test can play
// the lock in one line. A nil lineage is the NO-LOCK case — a unit that was
// never locked promises nothing, so every table serves its own layout and
// refuses every other hash by name.
func GenerateLineage(u *ir.Unit, lineage map[string][]FixedLineageEntry) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return nil, nil
	}
	// §15's WIDE-KIND REFUSAL IS THE FORM-1 ACCELERATORS' AND NOT THE WIRE'S
	// (ir.WideTableKinds): the block form and the cooked form are what must
	// name a kind's storage column and its reflection descriptor, and the fixed
	// form names no kind at all on its emitted path — a 128-bit field is a
	// sixteen-byte store and a `fixed(I, F)` is its raw scaled integer, and the
	// kind byte a reader compares rides in the LAYOUT the emitter already
	// writes.
	//
	// THE VARIABLE FORM CARRIES THE WIDE KINDS (docs/SPEC-TABLES.md §3) and is
	// emitted for every unit, the cook writer on its Table module with it, so
	// there is always something left to emit: `fixedForm`, the answer to "is
	// there a codec beside the accelerators", is true for this leg whatever its
	// fixed roots are, and only the accelerators stand down.
	scope := ir.WideTableKinds(u, "Elixir", true)
	if scope.Unit {
		return nil, scope.Refusal
	}
	out := map[string][]byte{}
	closure := ir.TableClosure(u)
	blocks := ir.Blocks(u)
	ns := ir.GoExportName(u.Package)

	// THE VARIABLE FORM'S BOUNDARY (docs/SPEC-TABLES.md §11) is the compiler's
	// now: maps (§2.8), optional fields and arrays (§2.3), enum-keyed arrays
	// (§2.4, §3.2), the fixed-point and 128-bit kinds (§3), and union arms of
	// every field type (§2.6) are carried by the variable form, so the backend
	// refuses none of them; the `was` rows this leg does not carry (§5) are
	// refused by name in compiler/target_elixir.go.

	// A DECLARATION LOWERS TO A MODULE under the unit's namespace, so one named
	// for a generated file's module would merge two unrelated modules. The
	// unit-level runtime names are the CHECKER's claim (internal/tablenames,
	// docs/SPEC-TABLES.md §11); these two are derived from a schema FILE's own
	// basename, which no unit-level registry can hold, so they are refused here
	// — beside the same refusal the packet emitter already makes for <Base>.
	for decl := range u.DeclFile {
		for _, suffix := range []string{"Block", "Cook", FixedModuleSuffix} {
			for _, f := range u.Files {
				if decl == ir.GoExportName(f.Base)+suffix {
					return nil, fmt.Errorf("declaration %s collides with the module the Elixir table backend writes for schema file %s.schema (%s.%s%s); rename one of them",
						decl, f.Base, ns, ir.GoExportName(f.Base), suffix)
				}
			}
		}
	}

	if !scope.Accelerators {
		if anyCookable(u, closure) {
			out[CookRuntimeModule+".ex"] = cookRuntimeModule(u, ns)
		}

		for _, f := range u.Files {
			g := &gen{unit: u, ns: ns, file: f, closure: closure}
			if body := g.cookModule(); body != nil {
				out[f.Base+"Cook.ex"] = body
			}
		}

		// the BLOCK form: nothing declares it, every fixed table has one, and
		// it lives in its own module so a consumer that never opens a block
		// pays only for a module it never calls (docs/SPEC-TABLES.md §19).
		if blocks != nil {
			blockOut, err := generateBlocks(u, ns, blocks)
			if err != nil {
				return nil, err
			}
			maps.Copy(out, blockOut)
		}
	}

	// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3: the FIXED
	// tables' wire. The variable form (form byte 1) is emitted below for the
	// variable class; this is the fixed tables' own, laid out by the plan.
	fixed, err := generateFixed(u, ns, lineage)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, fixed)

	// THE VARIABLE FORM (docs/SPEC-TABLES.md §3), form byte 1: the id-table
	// wire the card constructs read and write byte for byte as the Go, C, C++
	// and C# backends do. It is emitted for every unit whose closure holds a
	// table, beside the fixed form. A mixed unit (graphdemo, listdemo) gets
	// both. A fixed-only unit (tblp1, tblp3) gets it too: its conformance wire
	// is form byte 1, and an optional and a by-value nest are one framing
	// (§2.3), so those instances answer rather than write <case>.absent.
	maps.Copy(out, generateVariableForm(u, ns))

	// the id-table wire's shared framing (docs/SPEC-TABLES.md §3, §5): the
	// primitives every variable table's codecs call, emitted once per unit the
	// way the two accelerators' runtimes are.
	out[TableRuntimeModule+".ex"] = tableRuntimeModule(u, ns)
	out[BuildVersionModule+".ex"] = buildVersionModule(u, ns)
	out[UnitViewModule+".ex"] = unitViewModule(u, ns)

	// THE MESSAGE FORM'S ANNOUNCEMENT (docs/SPEC-TABLES.md §3.3): the surface a
	// `fixed table` root owes. The form is a fixed table's and nobody else's, so
	// the announcement surface is emitted for fixed roots, beside BuildVersion —
	// the announcement is a compile-time constant of the unit.
	if len(fixedRoots(u)) > 0 {
		out[TableMessageModule+".ex"] = messageformModule(u, ns)
	}
	return out, nil
}

// buildVersionModule carries the unit's BUILD VERSION and nothing else (§20):
// one digest answering "which build?" and not "which form?", so it belongs to
// neither accelerator. Both of them compare against it.
func buildVersionModule(u *ir.Unit, ns string) []byte {
	var b strings.Builder
	b.WriteString(header(BuildVersionModule, u.Package, "the unit's build version (docs/SPEC-TABLES.md §20)"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "defmodule %s.BuildVersion do\n", ns)
	b.WriteString("  @moduledoc \"\"\"\n")
	b.WriteString("  The unit's BUILD VERSION: one digest over every fact a pointed-at form\n")
	b.WriteString("  depends on (docs/SPEC-TABLES.md §20). A block image or a cooked file whose\n")
	b.WriteString("  header carries a different one is refused, because its bytes were laid out\n")
	b.WriteString("  by a build this one does not agree with.\n")
	b.WriteString("  \"\"\"\n\n")
	fmt.Fprintf(&b, "  @build_version 0x%016X\n\n", ir.BuildVersion(u))
	b.WriteString("  def build_version, do: @build_version\n")
	b.WriteString("end\n")
	return []byte(b.String())
}

// gen carries one schema file's emission state: the unit, its module
// namespace, the file, the table closure, and the body being written.
type gen struct {
	unit    *ir.Unit
	ns      string
	file    *ir.File
	closure map[string]bool

	body   strings.Builder
	indent string
}

func (g *gen) pf(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	if g.indent != "" && s != "" {
		trailing := strings.HasSuffix(s, "\n")
		if trailing {
			s = s[:len(s)-1]
		}
		// a BLANK line takes no indent: trailing whitespace is what `mix
		// format` refuses, and the gate that runs it is the point
		lines := strings.Split(s, "\n")
		for i, line := range lines {
			if line != "" {
				lines[i] = g.indent + line
			}
		}
		s = strings.Join(lines, "\n")
		if trailing {
			s += "\n"
		}
	}
	g.body.WriteString(s)
}

// header is the generated-file banner every module carries.
func header(base, pkg, what string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", base)
	b.WriteString("# SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	b.WriteString("# your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	b.WriteString("# AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&b, "# package %s — %s\n", pkg, what)
	return b.String()
}

func moduleBase(base string) string { return ir.GoExportName(base) }

// exLineLimit is the line length Elixir 1.20.4's `mix format` wraps past, and
// the emitters wrap at the same column so the source they write is the source
// the formatter would write (issue #547).
const exLineLimit = 98

// exInt spells a decimal integer the way `mix format` spells it: six digits or
// more are grouped in threes by underscores.
func exInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	if len(s) < 6 {
		return sign + s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('_')
		}
		b.WriteRune(c)
	}
	return sign + b.String()
}

// exWidth is a line's width in columns as the formatter counts them: runes.
func exWidth(s string) int { return len([]rune(s)) }

// exDefDo writes `def head, do: body` on one line at indent, and when that
// passes the line limit, the formatter's wrap: the head, then `do:` on the
// next line two columns in.
func exDefDo(indent, head, body string) string {
	if one := indent + head + ", do: " + body; exWidth(one) <= exLineLimit {
		return one + "\n"
	}
	return indent + head + ",\n" + indent + "  do: " + body + "\n"
}

// exComma is the separator after element i of n in a multi-line list or map:
// a comma, and none after the last, which the formatter drops.
func exComma(i, n int) string {
	if i == n-1 {
		return ""
	}
	return ","
}

// exBlock joins the expressions of one block the way the formatter separates
// them: an expression that spans more than one line gets a blank line on each
// side, and single-line neighbours stay together. Each item is one expression
// with any comment lines that lead it, and ends in a newline.
func exBlock(items []string) string {
	multi := func(s string) bool {
		code := 0
		for line := range strings.SplitSeq(strings.TrimSuffix(s, "\n"), "\n") {
			if t := strings.TrimSpace(line); t != "" && !strings.HasPrefix(t, "#") {
				code++
			}
		}
		return code > 1
	}
	var b strings.Builder
	for i, it := range items {
		if i > 0 && (multi(it) || multi(items[i-1])) {
			b.WriteString("\n")
		}
		b.WriteString(it)
	}
	return b.String()
}

func isStruct(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	_, ok := f.Type.Ref.(*ir.Struct)
	return ok
}

// orderTables returns a file's tables with every same-file table preceding its
// by-value users. Stable: declaration order survives wherever no dependency
// forces otherwise.
func orderTables(tables []*ir.Struct) []*ir.Struct {
	n := len(tables)
	byName := map[string]int{}
	for i, st := range tables {
		byName[st.Name] = i
	}
	adj := make([][]int, n)
	indeg := make([]int, n)
	for i, st := range tables {
		for _, f := range st.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			if ref, ok := f.Type.Ref.(*ir.Struct); ok && ref.IsTable {
				if j, ok := byName[ref.Name]; ok && j != i {
					adj[j] = append(adj[j], i)
					indeg[i]++
				}
			}
		}
	}
	order := make([]*ir.Struct, 0, n)
	done := make([]bool, n)
	for len(order) < n {
		pick := -1
		for i := range n {
			if !done[i] && indeg[i] == 0 {
				pick = i
				break
			}
		}
		if pick == -1 {
			for i := range n {
				if !done[i] {
					pick = i
					break
				}
			}
		}
		done[pick] = true
		order = append(order, tables[pick])
		for _, t := range adj[pick] {
			indeg[t]--
		}
	}
	return order
}

// THE DATA LITERALS THE VARIABLE FORM WRITES (@tables, @unions, @node_types,
// @known_ids, @uncarried) are laid out in `mix format`'s shape as they are
// emitted (issue #547). The descriptor builders spell each literal as one
// flat term (maps, lists, tuples, binaries and leaves; whitespace between
// tokens is free) and exTerm lays that term out by the formatter's rules for
// those shapes and nothing else: a term that fits the line stays on it; a map
// or a list breaks one entry per line two columns in, its closer back at the
// opening line's indent, with no trailing comma; a binary fills greedily two
// columns in and a tuple one column inside its brace; an entry that does not
// fit opens a map or list value beside its key and moves any other value to
// the next line two columns in. Numbers are spelled as the formatter spells
// them (exNumber). It is not a formatter: it knows no code, only the literal
// shapes these emitters write, and the raw-format gate
// (tables-elixir-emit-format-clean) is what holds it to Elixir 1.20.4.
type exTerm struct {
	leaf        string // set for a leaf
	open, close string // "%{" "}", "[" "]", "{" "}", "<<" ">>"
	items       []exItem
}

// exItem is one entry of a container: key is "name: " or `"k" => `, or empty.
type exItem struct {
	key string
	val *exTerm
}

// exParseLiteral reads the flat term a descriptor builder spells. It panics on
// a term outside those shapes: that is an emitter bug, and the generator's own
// tests reach every builder.
func exParseLiteral(s string) *exTerm {
	p := &exParser{s: s}
	t := p.term()
	p.space()
	if p.i != len(p.s) {
		panic(fmt.Sprintf("elixirtable: literal has trailing text at %d: %q", p.i, p.s[p.i:]))
	}
	return t
}

type exParser struct {
	s string
	i int
}

func (p *exParser) space() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\n') {
		p.i++
	}
}

func (p *exParser) term() *exTerm {
	p.space()
	for _, c := range [][2]string{{"%{", "}"}, {"<<", ">>"}, {"[", "]"}, {"{", "}"}} {
		if strings.HasPrefix(p.s[p.i:], c[0]) {
			p.i += len(c[0])
			return p.container(c[0], c[1])
		}
	}
	return &exTerm{leaf: exNumber(p.leaf())}
}

// leaf reads a string literal or a bare token up to the next separator.
func (p *exParser) leaf() string {
	start := p.i
	if p.i < len(p.s) && p.s[p.i] == '"' {
		p.i++
		for p.i < len(p.s) && p.s[p.i] != '"' {
			if p.s[p.i] == '\\' {
				p.i++
			}
			p.i++
		}
		p.i++
		return p.s[start:p.i]
	}
	for p.i < len(p.s) && !strings.ContainsRune(", \n}]>", rune(p.s[p.i])) {
		p.i++
	}
	if p.i == start {
		panic(fmt.Sprintf("elixirtable: literal has no term at %d: %q", p.i, p.s[p.i:]))
	}
	return p.s[start:p.i]
}

func exIdentByte(c byte) bool {
	return c == '_' || c == '?' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func (p *exParser) container(open, close string) *exTerm {
	t := &exTerm{open: open, close: close}
	for {
		p.space()
		if strings.HasPrefix(p.s[p.i:], close) {
			p.i += len(close)
			return t
		}
		key := ""
		// a keyword key: an identifier, then ": "
		j := p.i
		for j < len(p.s) && exIdentByte(p.s[j]) {
			j++
		}
		if j > p.i && j+1 < len(p.s) && p.s[j] == ':' && p.s[j+1] == ' ' {
			key = p.s[p.i:j] + ": "
			p.i = j + 2
		}
		v := p.term()
		p.space()
		if key == "" && strings.HasPrefix(p.s[p.i:], "=>") {
			p.i += 2
			key = v.flat() + " => "
			v = p.term()
			p.space()
		}
		t.items = append(t.items, exItem{key: key, val: v})
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
		}
	}
}

// exNumber spells a numeric leaf as the formatter does: a decimal integer of
// six digits or more grouped by underscores (exInt), a float's integer part
// the same way, hex digits upper case. Anything else is returned unchanged.
func exNumber(s string) string {
	body := strings.TrimPrefix(s, "-")
	sign := s[:len(s)-len(body)]
	if body == "" || body[0] < '0' || body[0] > '9' || strings.Contains(body, "_") {
		return s
	}
	if strings.HasPrefix(body, "0x") {
		return sign + "0x" + strings.ToUpper(body[2:])
	}
	if strings.ContainsAny(body, "eE") {
		return s
	}
	intPart, frac := body, ""
	if k := strings.IndexByte(body, '.'); k >= 0 {
		intPart, frac = body[:k], body[k:]
	}
	if intPart == "" || strings.Trim(intPart, "0123456789") != "" {
		return s
	}
	if len(intPart) < 6 {
		return s
	}
	// grouped in threes from the right, the way exInt groups: an unsigned
	// 64-bit bound is past int64, so the digits are grouped as text
	var b strings.Builder
	for i := range len(intPart) {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte('_')
		}
		b.WriteByte(intPart[i])
	}
	return sign + b.String() + frac
}

func (t *exTerm) flat() string {
	if t.open == "" {
		return t.leaf
	}
	parts := make([]string, len(t.items))
	for i, it := range t.items {
		parts[i] = it.key + it.val.flat()
	}
	return t.open + strings.Join(parts, ", ") + t.close
}

func exFits(col, tail int, s string) bool { return col+exWidth(s)+tail <= exLineLimit }

// render lays the term out starting at column at, its broken lines indented
// from column ind, with tail columns following its closer on that line.
func (t *exTerm) render(at, ind, tail int) string {
	one := t.flat()
	if t.open == "" || len(t.items) == 0 || exFits(at, tail, one) {
		return one
	}
	if t.open == "<<" || t.open == "{" {
		cont := ind + 2
		if t.open == "{" {
			cont = at + 1
		}
		var b strings.Builder
		first := t.items[0].key + t.items[0].val.flat()
		b.WriteString(t.open + first)
		line := at + len(t.open) + exWidth(first)
		for i := 1; i < len(t.items); i++ {
			s := t.items[i].key + t.items[i].val.flat()
			after := 1
			if i == len(t.items)-1 {
				after = len(t.close) + tail
			}
			if line+2+exWidth(s)+after > exLineLimit {
				b.WriteString(",\n" + strings.Repeat(" ", cont) + s)
				line = cont + exWidth(s)
				continue
			}
			b.WriteString(", " + s)
			line += 2 + exWidth(s)
		}
		b.WriteString(t.close)
		return b.String()
	}
	pad := strings.Repeat(" ", ind+2)
	var b strings.Builder
	b.WriteString(t.open + "\n")
	for i, it := range t.items {
		sep := exComma(i, len(t.items))
		b.WriteString(pad + it.render(ind+2, len(sep)) + sep + "\n")
	}
	b.WriteString(strings.Repeat(" ", ind) + t.close)
	return b.String()
}

// render lays one entry out at column ind: on one line where it fits; a map or
// list value opened beside its key; any other value on the next line, two
// columns in.
func (it exItem) render(ind, tail int) string {
	if one := it.key + it.val.flat(); exFits(ind, tail, one) || it.key == "" && it.val.open == "" {
		return one
	}
	if it.key == "" {
		return it.val.render(ind, ind, tail)
	}
	if it.val.open == "%{" || it.val.open == "[" {
		return it.key + it.val.render(ind+exWidth(it.key), ind, tail)
	}
	return strings.TrimSuffix(it.key, " ") + "\n" + strings.Repeat(" ", ind+2) + it.val.render(ind+2, ind+2, tail)
}

// exAttribute writes `@name literal` at module indent, the literal laid out by
// exTerm's rules.
func exAttribute(name, literal string) string {
	head := "  @" + name + " "
	return head + exParseLiteral(literal).render(exWidth(head), 2, 0) + "\n"
}
