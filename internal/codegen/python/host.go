package python

import (
	"maps"
	"sort"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Host renders one schema file's PACKET-side declarations for a caller other
// than the packet emitter: the table emitter, which hosts the packet classes a
// table closure reaches (docs/PORT-PYTHON.md, THE TABLE FRAME). The text is the
// packet core's own class surface and codec, so a packet type carried by a
// table is the class a packet-only unit gets; the caller appends the table
// descriptor that names it.
type Host struct {
	m *pyModule
}

// HostImports is the names a Host's body needs, grouped by the module they come
// from, so a caller that writes its own header merges them rather than meeting
// an import it did not expect.
type HostImports struct {
	Typing    []string
	Schema    []string
	Serialize []string
	Siblings  map[string]string // schema file base -> local alias
}

// NewHost starts a packet-side module for one schema file.
func NewHost(u *ir.Unit, f *ir.File) *Host {
	return &Host{m: &pyModule{
		u:       u,
		f:       f,
		hosted:  true,
		imports: map[string]bool{},
		schema:  map[string]bool{},
		serial:  map[string]bool{},
		sibling: map[string]string{},
	}}
}

// Emit walks the file's declarations in emission order and emits the ones keep
// names. A nil keep emits every declaration.
func (h *Host) Emit(keep func(ir.Decl) bool) {
	for _, d := range ir.EmissionOrder(h.m.f) {
		if keep != nil && !keep(d) {
			continue
		}
		switch d := d.(type) {
		case *ir.Const:
			h.m.emitConst(d)
		case *ir.Enum:
			h.m.emitEnum(d)
		case *ir.Flags:
			h.m.emitFlags(d)
		case *ir.Struct:
			h.m.emitStruct(d)
		case *ir.Union:
			h.m.emitUnion(d)
		}
	}
}

// Body is the module body the emitters appended.
func (h *Host) Body() string { return h.m.body.String() }

// Imports is the body's import set.
func (h *Host) Imports() HostImports {
	out := HostImports{
		Typing:    sortedKeys(h.m.imports),
		Schema:    sortedKeys(h.m.schema),
		Serialize: sortedKeys(h.m.serial),
		Siblings:  make(map[string]string, len(h.m.sibling)),
	}
	maps.Copy(out.Siblings, h.m.sibling)
	return out
}

// SortedSiblingBases is the sibling map's bases in sorted order.
func SortedSiblingBases(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for base := range m {
		out = append(out, base)
	}
	sort.Strings(out)
	return out
}
