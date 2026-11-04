package squirreltable

import (
	"fmt"
	"sort"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE STORAGE LAYOUT (docs/SPEC-TABLES.md §6.5, §11): the C ABI sizes the
// region measure (runtime/load.nut, LoadMeasure) sums, beside the descriptors
// its walk reads. Per table: its record size, the element size and alignment
// of each list and map field by wire id, the node types and blob kinds its
// pointers reach (ir.PointerReachable, ir.PointerReachableBlobs). Per union:
// the same element facts for each arm by arm id. `scale` is the most storage
// one wire byte can command (ir.TableStorageScale), so the load measures a
// wire against the storage cap before it builds anything only where the wire
// is long enough to pass it.
func emitStorageLayout(m *Module) {
	u := m.Unit
	elems := func(fields map[uint64]*ir.Field) string {
		ids := make([]uint64, 0, len(fields))
		for id := range fields {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		s := "{"
		for _, id := range ids {
			size, align := storageElem(u, fields[id])
			s += fmt.Sprintf(" [%s] = [%d, %d],", squirrel.HexLit(id), size, align)
		}
		return s + " }"
	}
	contained := func(f *ir.Field) bool { return f.IsMap() || f.Array != ir.ArrayNone }
	unions := map[string]*ir.Union{}
	m.Printf("// THE STORAGE LAYOUT (docs/SPEC-TABLES.md §6.5, §11): the C ABI sizes the\n")
	m.Printf("// region measure sums, and the storage one wire byte can command at most.\n")
	m.Printf("_M.StorageLayout <- {\n")
	m.Printf("  regionAlign = %d,\n", ir.TableRegionAlign(u))
	m.Printf("  tables = {\n")
	for _, st := range m.Closure {
		size := ir.RecordLayout(u, st).Size
		fields := map[uint64]*ir.Field{}
		for _, f := range st.Fields {
			if contained(f) {
				fields[ir.TableFieldWireId(f)] = f
			}
			if un := unionOf(f); un != nil {
				unions[un.Name] = un
			}
		}
		nodes := ""
		for _, n := range ir.PointerReachable(st) {
			nodes += fmt.Sprintf(" [%s] = %s,", squirrel.HexLit(ir.TableWireId(n.WireName())), squirrel.StringLit([]byte(n.Name)))
		}
		bblob, sblob := ir.PointerReachableBlobs(st)
		m.Printf("    %s = { size = %d, elems = %s, nodes = {%s }, sblob = %v, bblob = %v },\n",
			squirrel.TableKey(st.Name), size, elems(fields), nodes, sblob, bblob)
	}
	m.Printf("  },\n")
	// a union arm may hold a union in turn
	for grew := true; grew; {
		grew = false
		for _, un := range unions {
			for _, v := range un.Variants {
				if v.Void() {
					continue
				}
				if inner := unionOf(v.F); inner != nil && unions[inner.Name] == nil {
					unions[inner.Name] = inner
					grew = true
				}
			}
		}
	}
	names := make([]string, 0, len(unions))
	for n := range unions {
		names = append(names, n)
	}
	sort.Strings(names)
	m.Printf("  unions = {\n")
	for _, n := range names {
		fields := map[uint64]*ir.Field{}
		for _, v := range unions[n].Variants {
			if !v.Void() && contained(v.F) {
				fields[ir.TableWireId(v.WireName())] = v.F
			}
		}
		m.Printf("    %s = %s,\n", squirrel.TableKey(n), elems(fields))
	}
	m.Printf("  },\n")
	m.Printf("  scale = %d,\n", ir.TableStorageScale(u))
	m.Printf("};\n\n")
}

// storageElem is one list element's or map entry's storage and alignment
// (ir.ListElementLayout; a map's generated entry record).
func storageElem(u *ir.Unit, f *ir.Field) (int64, int64) {
	if f.IsMap() {
		l := ir.RecordLayout(u, f.MapEntry)
		return l.Size, l.Align
	}
	return ir.ListElementLayout(u, f)
}
