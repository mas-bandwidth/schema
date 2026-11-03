package elixir

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- nested types past the inline depth ----
//
// A nested type's items are emitted INSIDE its holder's body, so a holder's
// read and write are one run of groups with no call between them. Inlined
// without bound, a chain of single-field types would put the whole chain
// below each type into that type's own functions, and a module would grow
// with the square of the depth. So a nested type is inlined only while its
// own depth is at most wireInlineDepth; past it the holder calls the type's
// private helper, which inlines the next levels in turn. Every function then
// holds a bounded slice of the chain, and a module grows linearly with depth.
// The wire is the same bits either way: a helper reads and writes exactly the
// items the inlined body would, from the position the holder reached.

// wireInlineDepth is the deepest nested type a holder's body inlines.
const wireInlineDepth = 8

// dataWord and numBitsWord find a read body's uses of its parameters.
var (
	dataWord    = regexp.MustCompile(`\bdata\b`)
	numBitsWord = regexp.MustCompile(`\bnum_bits\b`)
)

// nestedOutOfLine reports a nested type its holders reach through a helper.
func (g *gen) nestedOutOfLine(st *ir.Struct) bool {
	return g.wireDepth(st) > wireInlineDepth
}

// wireDepth is how many levels of nested types a type's inlined body spans: 1
// for a type of plain fields, one more for each level of nested type or union
// arm below it. An array's element sits in a loop helper of its own, so it
// adds no depth to its holder.
func (g *gen) wireDepth(st *ir.Struct) int {
	if d, ok := g.depths[st]; ok {
		return d
	}
	if g.depths == nil {
		g.depths = map[*ir.Struct]int{}
	}
	g.depths[st] = 1 // a type reached again through itself adds no depth
	d := 1 + g.itemsDepth(st.Items)
	g.depths[st] = d
	return d
}

func (g *gen) itemsDepth(items []ir.Item) int {
	depth := 0
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			f := item.F
			if f.Array != ir.ArrayNone || f.Type.Kind != ir.TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Struct:
				depth = max(depth, g.wireDepth(ref))
			case *ir.Union:
				for _, vr := range ref.Variants {
					if !vr.Void() {
						depth = max(depth, g.wireDepth(vr.Ref))
					}
				}
			}
		case *ir.Branch:
			depth = max(depth, g.itemsDepth(item.Then), g.itemsDepth(item.Else))
		}
	}
	return depth
}

// emitWriteNested writes the nested type ref bound to expr through its
// helper: the group closes, the helper appends the type's items at the
// offset the caller reached, and the offset past it is the wire's.
func (g *gen) emitWriteNested(ref *ir.Struct, expr, ind string) {
	helper := g.writeNestedHelper(ref)
	g.flushW(ind) // the helper opens its own group
	g.syncSB(ind) // and reads the offset the caller was tracking statically
	g.ensureScratch(ind, true)
	g.callAssign("{data, scratch, scratch_bits}",
		fmt.Sprintf("%s(%s, %s, scratch, scratch_bits)", helper, expr, g.dataArg()), ind)
	g.dataBound = true
	g.sbKnown, g.scZero = false, false
	g.scBound, g.sbBound = true, true
}

// writeNestedHelper materializes (once per file) the write helper of a nested
// type and returns its name.
func (g *gen) writeNestedHelper(ref *ir.Struct) string {
	name := "wn_" + ir.RustSnake(ref.Name)
	if _, done := g.helpers[name]; done {
		return name
	}
	g.helpers[name] = "" // claim before recursing (helpers may nest)
	saved := g.fn
	savedSB := g.sbSave()
	savedBind, savedDisp, savedUsed := g.bindW, g.bindDisp, g.bindUsed
	savedSeg := g.lastSeg
	g.fn = strings.Builder{}
	// entered with the caller's group closed, at an offset the caller passes
	g.pendW, g.segW, g.segN = 0, nil, 0
	g.sbKnown, g.scZero = false, false
	g.scBound, g.sbBound = true, true
	g.dataBound = true
	g.bindW, g.bindDisp, g.bindUsed = nil, map[string]string{}, map[string]bool{}
	g.lastSeg = segRecord{}
	g.pf("  defp %s(value, data, scratch, scratch_bits) do\n", name)
	g.withOwner(ref.Name, func() { g.emitWriteItems(ref.Items, "value", "    ") })
	g.flushW("    ") // the helper's tail is a barrier: the residual is < 8 bits
	g.syncSB("    ")
	g.pf("    {data, scratch, scratch_bits}\n")
	g.pf("  end\n\n")
	g.helpers[name] = g.fn.String()
	g.fn = saved
	g.sbRestore(savedSB)
	g.bindW, g.bindDisp, g.bindUsed = savedBind, savedDisp, savedUsed
	g.lastSeg = savedSeg
	g.helperOrder = append(g.helperOrder, name)
	return name
}

// emitReadNested reads the nested type ref into local lv through its helper,
// which checks its own bounds and answers the position past it.
func (g *gen) emitReadNested(ref *ir.Struct, lv, ind string) {
	helper := g.readNestedHelper(ref)
	g.rdBreak()
	g.callAssign(fmt.Sprintf("{bits_read, %s}", lv),
		fmt.Sprintf("%s(data, num_bits, %s)", helper, g.rpos(0)), ind)
	g.rdDynamic(false)
}

// readNestedHelper materializes (once per file) the read helper of a nested
// type and returns its name. A refusal inside it throws to the read surface
// that called it, as an inlined body's would.
func (g *gen) readNestedHelper(ref *ir.Struct) string {
	name := "rn_" + ir.RustSnake(ref.Name)
	if _, done := g.helpers[name]; done {
		return name
	}
	g.helpers[name] = ""
	saved := g.fn
	savedOff, savedAvail, savedRun := g.rdOff, g.rdAvail, g.rdRun
	savedPos := g.rdSave()
	g.fn = strings.Builder{}
	g.rdBreak()        // the helper opens its own group
	g.rdDynamic(false) // at a position the caller passes
	g.withOwner(ref.Name, func() {
		g.emitReadItems(ref.Items, "v", "    ", false)
		g.emitBuildStruct(ref, "v", "value", "    ")
	})
	g.pf("    {%s, value}\n", g.rpos(0))
	g.pf("  end\n\n")
	body := g.fn.String()
	data, numBits := "data", "num_bits"
	if !dataWord.MatchString(body) {
		data = "_data"
	}
	if !numBitsWord.MatchString(body) {
		numBits = "_num_bits"
	}
	g.helpers[name] = fmt.Sprintf("  defp %s(%s, %s, bits_read) do\n", name, data, numBits) + body
	g.fn = saved
	g.rdOff, g.rdAvail, g.rdRun = savedOff, savedAvail, savedRun
	g.rdRestore(savedPos)
	g.helperOrder = append(g.helperOrder, name)
	return name
}
