package rusttable

// THE ITERATIVE DESTRUCTION OF A POINTER GRAPH (docs/SPEC-TABLES.md §3.1, §4;
// docs/SECURITY.md): a value the Rust leg reads from a conforming file can be
// dropped on any thread however long its pointer chain is. The wire puts no
// bound on a chain's length and a pointer does not nest, so a node record is
// walked from depth 0. The load and save walks already hold their own stack;
// this makes the value's own destruction hold one too.
//
// A table type that can reach a value of its own type through a pointer field
// takes every pointer it owns out of the value, one at a time, and pushes it on
// a local work list. Its Drop walks that list, unlinking each node before it
// lets the node drop, so the compiler's field-by-field glue never recurses once
// per node. The unlink descends through arrays, maps, unions and by-value
// bodies because a pointer a node owns can sit in any of them; it stops at a
// pointer edge (the target's own unlink handles the rest) and at a by-value
// table (a table on the same chain is itself cyclic and has its own unlink).

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// tableDropSet is every table type that can reach a value of its own type
// through a pointer field. The walk covers every edge a value owns: a pointer
// field, a by-value table, a map entry, a union arm and the value structures
// inside them. A type in the set gets the iterative unlink and Drop.
func tableDropSet(u *ir.Unit) map[string]bool {
	nodes := map[string]*ir.Struct{}
	for name, st := range u.Tables {
		if st != nil && !st.IsMapEntry() {
			nodes[name] = st
		}
	}
	edges := map[string]map[string]bool{}
	for name, st := range nodes {
		out := map[string]bool{}
		seenStruct := map[*ir.Struct]bool{}
		seenUnion := map[*ir.Union]bool{}
		dropStructEdges(st, out, seenStruct, seenUnion)
		edges[name] = out
	}
	drops := map[string]bool{}
	for name := range nodes {
		if dropReaches(edges, name) {
			drops[name] = true
		}
	}
	return drops
}

// dropStructEdges flattens one value structure into the table types it can
// reach: a pointer target is an edge and stops the descent, a by-value table is
// an edge and stops too (its own fields are its own edges), and a type, map
// entry or union is descended because it is part of the value that is dropped.
func dropStructEdges(st *ir.Struct, out map[string]bool, seenStruct map[*ir.Struct]bool, seenUnion map[*ir.Union]bool) {
	if st == nil || seenStruct[st] {
		return
	}
	seenStruct[st] = true
	for _, f := range st.Fields {
		dropFieldEdges(f, out, seenStruct, seenUnion)
	}
}

func dropFieldEdges(f *ir.Field, out map[string]bool, seenStruct map[*ir.Struct]bool, seenUnion map[*ir.Union]bool) {
	if f == nil {
		return
	}
	if f.IsMap() {
		dropStructEdges(f.MapEntry, out, seenStruct, seenUnion)
		return
	}
	if un := unionOf(f); un != nil {
		if seenUnion[un] {
			return
		}
		seenUnion[un] = true
		for _, v := range un.Variants {
			dropFieldEdges(v.F, out, seenStruct, seenUnion)
		}
		return
	}
	if f.Type.Pointer {
		if f.Type.Blob() {
			return
		}
		if ref, ok := f.Type.Ref.(*ir.Struct); ok {
			out[ref.Name] = true
		}
		return
	}
	if classOf(f) != vTable {
		return
	}
	ref, ok := f.Type.Ref.(*ir.Struct)
	if !ok {
		return
	}
	if ref.IsTable && !ref.IsMapEntry() {
		out[ref.Name] = true
		return
	}
	dropStructEdges(ref, out, seenStruct, seenUnion)
}

// dropReaches reports whether a table reaches itself over the value edges.
func dropReaches(edges map[string]map[string]bool, from string) bool {
	todo := make([]string, 0, len(edges[from]))
	for next := range edges[from] {
		todo = append(todo, next)
	}
	seen := map[string]bool{}
	for len(todo) > 0 {
		n := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if n == from {
			return true
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		for next := range edges[n] {
			todo = append(todo, next)
		}
	}
	return false
}

// emitVariableDrop emits the iterative unlink and Drop for one table type the
// set names.
func (g *gen) emitVariableDrop(b *strings.Builder, st *ir.Struct, drops map[string]bool) {
	fmt.Fprintf(b, "/// %s can reach a value of its own type through a pointer field: it takes\n", st.Name)
	fmt.Fprintf(b, "/// its owned pointers out one at a time, so destroying a chain costs no stack\n")
	fmt.Fprintf(b, "/// per node (docs/SPEC-TABLES.md §3.1, §4).\n")
	fmt.Fprintf(b, "#[allow(unused_variables)]\n")
	fmt.Fprintf(b, "impl TableDropUnlink for %s {\n", st.Name)
	fmt.Fprintf(b, "    fn table_unlink(&mut self, work: &mut Vec<Rc<dyn TableDropUnlink>>) {\n")
	g.emitDropStructFields(b, st, "self", "        ", drops)
	fmt.Fprintf(b, "    }\n}\n\n")
	fmt.Fprintf(b, "impl Drop for %s {\n", st.Name)
	fmt.Fprintf(b, "    fn drop(&mut self) {\n")
	fmt.Fprintf(b, "        let mut work: Vec<Rc<dyn TableDropUnlink>> = Vec::new();\n")
	fmt.Fprintf(b, "        self.table_unlink(&mut work);\n")
	fmt.Fprintf(b, "        while let Some(mut node) = work.pop() {\n")
	fmt.Fprintf(b, "            if let Some(inner) = Rc::get_mut(&mut node) {\n")
	fmt.Fprintf(b, "                inner.table_unlink(&mut work);\n")
	fmt.Fprintf(b, "            }\n")
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "    }\n}\n\n")
}

// emitDropStructFields writes the unlink of every field of one value structure.
func (g *gen) emitDropStructFields(b *strings.Builder, st *ir.Struct, base, ind string, drops map[string]bool) bool {
	before := b.Len()
	for _, f := range st.Fields {
		g.emitDropField(b, f, base+"."+ir.RustSnake(f.Name), ind, drops)
	}
	return b.Len() > before
}

// emitDropField writes the unlink of one field, descending through the value
// structure it holds until it meets a pointer or a by-value table.
func (g *gen) emitDropField(b *strings.Builder, f *ir.Field, x, ind string, drops map[string]bool) bool {
	before := b.Len()
	if f.IsMap() {
		if f.Type.Optional {
			fmt.Fprintf(b, "%sif let Some(entries) = %s.as_mut() {\n", ind, x)
			g.emitDropMapEntries(b, f, "entries", ind+"    ", drops)
			fmt.Fprintf(b, "%s}\n", ind)
		} else {
			g.emitDropMapEntries(b, f, x, ind, drops)
		}
		return b.Len() > before
	}
	if un := unionOf(f); un != nil {
		switch {
		case f.Array != ir.ArrayNone || f.KeyEnum != "":
			if f.Type.Optional {
				fmt.Fprintf(b, "%sif let Some(elems) = %s.as_mut() {\n", ind, x)
				fmt.Fprintf(b, "%s    for e in elems.iter_mut() {\n", ind)
				g.emitDropUnion(b, un, "e", ind+"        ", drops)
				fmt.Fprintf(b, "%s    }\n%s}\n", ind, ind)
			} else {
				fmt.Fprintf(b, "%sfor e in %s.iter_mut() {\n", ind, x)
				g.emitDropUnion(b, un, "e", ind+"    ", drops)
				fmt.Fprintf(b, "%s}\n", ind)
			}
		case f.Type.Optional:
			fmt.Fprintf(b, "%sif let Some(e) = %s.as_mut() {\n", ind, x)
			g.emitDropUnion(b, un, "e", ind+"    ", drops)
			fmt.Fprintf(b, "%s}\n", ind)
		default:
			g.emitDropUnion(b, un, "&mut "+x, ind, drops)
		}
		return b.Len() > before
	}
	if f.Type.Pointer {
		if f.Type.Blob() {
			return false
		}
		ref, ok := f.Type.Ref.(*ir.Struct)
		if !ok || !drops[ref.Name] {
			return false
		}
		switch {
		case f.Array != ir.ArrayNone || f.KeyEnum != "":
			if f.Type.Optional {
				fmt.Fprintf(b, "%sif let Some(elems) = %s.as_mut() {\n", ind, x)
				g.emitDropPointerSlots(b, "elems", ind+"    ")
				fmt.Fprintf(b, "%s}\n", ind)
			} else {
				g.emitDropPointerSlots(b, x, ind)
			}
		case !f.Type.Optional:
			fmt.Fprintf(b, "%sif let Some(n) = %s.take() {\n", ind, x)
			fmt.Fprintf(b, "%s    if Rc::strong_count(&n) == 1 {\n", ind)
			fmt.Fprintf(b, "%s        work.push(n);\n", ind)
			fmt.Fprintf(b, "%s    } else {\n", ind)
			fmt.Fprintf(b, "%s        %s = Some(n);\n", ind, x)
			fmt.Fprintf(b, "%s    }\n%s}\n", ind, ind)
		}
		return b.Len() > before
	}
	if classOf(f) != vTable {
		return false
	}
	ref, ok := f.Type.Ref.(*ir.Struct)
	if !ok {
		return false
	}
	if ref.IsTable && !ref.IsMapEntry() {
		if !drops[ref.Name] {
			return false
		}
		switch {
		case f.Array != ir.ArrayNone || f.KeyEnum != "":
			if f.Type.Optional {
				fmt.Fprintf(b, "%sif let Some(elems) = %s.as_mut() {\n", ind, x)
				fmt.Fprintf(b, "%s    for e in elems.iter_mut() { TableDropUnlink::table_unlink(e, work); }\n", ind)
				fmt.Fprintf(b, "%s}\n", ind)
			} else {
				fmt.Fprintf(b, "%sfor e in %s.iter_mut() { TableDropUnlink::table_unlink(e, work); }\n", ind, x)
			}
		case f.Type.Optional:
			fmt.Fprintf(b, "%sif let Some(e) = %s.as_mut() { TableDropUnlink::table_unlink(e, work); }\n", ind, x)
		default:
			fmt.Fprintf(b, "%sTableDropUnlink::table_unlink(&mut %s, work);\n", ind, x)
		}
		return b.Len() > before
	}
	switch {
	case f.Array != ir.ArrayNone || f.KeyEnum != "":
		if f.Type.Optional {
			fmt.Fprintf(b, "%sif let Some(elems) = %s.as_mut() {\n", ind, x)
			fmt.Fprintf(b, "%s    for e in elems.iter_mut() {\n", ind)
			g.emitDropStructFields(b, ref, "e", ind+"        ", drops)
			fmt.Fprintf(b, "%s    }\n%s}\n", ind, ind)
		} else {
			fmt.Fprintf(b, "%sfor e in %s.iter_mut() {\n", ind, x)
			g.emitDropStructFields(b, ref, "e", ind+"    ", drops)
			fmt.Fprintf(b, "%s}\n", ind)
		}
	case f.Type.Optional:
		fmt.Fprintf(b, "%sif let Some(e) = %s.as_mut() {\n", ind, x)
		g.emitDropStructFields(b, ref, "e", ind+"    ", drops)
		fmt.Fprintf(b, "%s}\n", ind)
	default:
		g.emitDropStructFields(b, ref, x, ind, drops)
	}
	return b.Len() > before
}

func (g *gen) emitDropMapEntries(b *strings.Builder, f *ir.Field, entries, ind string, drops map[string]bool) {
	fmt.Fprintf(b, "%sfor e in %s.iter_mut() {\n", ind, entries)
	g.emitDropStructFields(b, f.MapEntry, "e", ind+"    ", drops)
	fmt.Fprintf(b, "%s}\n", ind)
}

func (g *gen) emitDropPointerSlots(b *strings.Builder, slots, ind string) {
	fmt.Fprintf(b, "%sfor slot in %s.iter_mut() {\n", ind, slots)
	fmt.Fprintf(b, "%s    if let Some(n) = slot.take() {\n", ind)
	fmt.Fprintf(b, "%s        if Rc::strong_count(&n) == 1 {\n", ind)
	fmt.Fprintf(b, "%s            work.push(n);\n", ind)
	fmt.Fprintf(b, "%s        } else {\n", ind)
	fmt.Fprintf(b, "%s            *slot = Some(n);\n", ind)
	fmt.Fprintf(b, "%s        }\n%s    }\n%s}\n", ind, ind, ind)
}

// emitDropUnion writes one match over a union value, unlinking the arms that
// hold a pointer. expr is already a &mut union.
func (g *gen) emitDropUnion(b *strings.Builder, un *ir.Union, expr, ind string, drops map[string]bool) {
	var arms strings.Builder
	for _, v := range un.Variants {
		if v.Void() || v.F == nil {
			continue
		}
		var body strings.Builder
		if !g.emitDropField(&body, v.F, "(*arm)", ind+"        ", drops) {
			continue
		}
		fmt.Fprintf(&arms, "%s    %s::%s(arm) => {\n", ind, un.Name, rustVariant(v.Name))
		arms.WriteString(body.String())
		fmt.Fprintf(&arms, "%s    }\n", ind)
	}
	if arms.Len() == 0 {
		return
	}
	fmt.Fprintf(b, "%smatch %s {\n", ind, expr)
	b.WriteString(arms.String())
	fmt.Fprintf(b, "%s    _ => {}\n%s}\n", ind, ind)
}
