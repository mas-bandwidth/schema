// Package elixirtable emits the Elixir TABLE surface (docs/SPEC-TABLES.md):
// the BLOCK form's read side in <Base>Block.ex (§19) and the COOKED form's
// read side in <Base>Cook.ex (§7), with their shared runtime modules and the
// unit's BuildVersion module, emitted only when the unit declares tables.
//
// THE ACCELERATOR TIER. A block is POINTED AT and a cook is OPENED; neither
// parses a wire, so both reach every fixed table of the unit. The TABLE WIRE
// itself — the codecs, the reflection descriptors and the text form of
// docs/SPEC-TABLES.md §3, §4, §8 and §16 in their id-table form — is not
// emitted for Elixir. The port that once stood here wrote the form that
// preceded the id-table wire, which the current specification does not
// describe and the C++ reference does not open; it was removed rather than
// carried (schema#515 is the row that brings the wire to Elixir). ROADMAP.md
// marks the cells.
package elixirtable

import (
	"fmt"
	"maps"
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
	// THE FIXED FORM HAS LANDED HERE, so `fixedForm` is now this backend's OWN
	// answer rather than the `false` every leg passed while none of them
	// carried a form-3 codec. The answer is THIS LEG'S OWN ROOTS (fixedRoots,
	// which narrows on fixedSupported), because coverage is a port's own state
	// and every leg answers from its own roots for that reason.
	// A unit whose wide kinds cost it the accelerators and
	// that has no fixed form to put in their place still has nothing to emit
	// and is still refused WHOLE and by name.
	scope := ir.WideTableKinds(u, "Elixir", len(fixedRoots(u)) > 0)
	if scope.Unit {
		return nil, scope.Refusal
	}
	out := map[string][]byte{}
	closure := ir.TableClosure(u)
	blocks := ir.Blocks(u)
	ns := ir.GoExportName(u.Package)

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

	// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3: this backend's
	// FIRST table WIRE. Form 1 is still deferred to schema#515 and nothing here
	// reads or writes one.
	fixed, err := generateFixed(u, ns, lineage)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, fixed)

	out[BuildVersionModule+".ex"] = buildVersionModule(u, ns)
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
