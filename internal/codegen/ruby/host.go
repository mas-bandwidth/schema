package ruby

import (
	"github.com/mas-bandwidth/schema/v3/ir"
)

// Host renders one schema file's packet-side declarations for the table
// emitter, which hosts the packet classes a table closure reaches
// (docs/PORT-RUBY.md, THE RUNTIME LAYOUT).
type Host struct {
	m *rbModule
}

// NewHost starts a packet-side module body for one schema file inside mod.
func NewHost(u *ir.Unit, f *ir.File, mod string) *Host {
	return &Host{m: &rbModule{
		u:      u,
		f:      f,
		mod:    mod,
		hosted: true,
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

// Body returns the emitted declarations indented for reopening module <Mod>.
func (h *Host) Body() string { return h.m.body.String() }
