// write.go is the type wire's WRITE side (docs/PORT-LUA.md, the type-wire
// write card): for every struct it emits a Write<Name>(stream, v) that calls
// the vendored serialize port's WriteStream
// (internal/codegen/lua/runtime/, schema-lua-serialize-vendor). There is no
// private bit writer here; every bit on the wire is the port's. A value past
// its bound on write is a named refusal (docs/PORT-LUA.md, THE TYPE MAPPING).
package lua

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	RegisterPass(writePass)
}

func writePass(m *Module) error {
	var b strings.Builder
	for _, d := range m.File.Decls {
		st, ok := d.(*ir.Struct)
		if !ok {
			continue
		}
		emitWriteStruct(m, &b, st)
	}
	if b.Len() > 0 {
		serialize := m.Require("serialize")
		_ = serialize
		m.Body.WriteString(b.String())
	}
	return nil
}

// emitWriteStruct emits Write<Name>(stream, v) for one struct.
func emitWriteStruct(m *Module, b *strings.Builder, st *ir.Struct) {
	fmt.Fprintf(b, "function _M.Write%s(stream, v)\n", st.Name)
	for _, f := range st.Fields {
		emitWriteField(m, b, "v."+ir.GoExportName(f.Name), f, 1)
	}
	b.WriteString("end\n\n")
}

func emitWriteField(m *Module, b *strings.Builder, val string, f *ir.Field, indent int) {
	pad := strings.Repeat("  ", indent)
	switch f.Array {
	case ir.ArrayFixed:
		for i := 0; i < int(f.ArrayBound); i++ {
			emitWriteField(m, b, fmt.Sprintf("%s[%d]", val, i+1), &ir.Field{Type: f.Type}, indent)
		}
		return
	case ir.ArrayCounted:
		// the count rides first, then the elements the value carries
		bits := arrayCountBits(f)
		fmt.Fprintf(b, "%sserializeInt(stream, %sCount, %d)\n", pad, val, bits)
		fmt.Fprintf(b, "%sfor _i = 1, %sCount do\n", pad, val)
		emitWriteField(m, b, fmt.Sprintf("%s[_i]", val), &ir.Field{Type: f.Type}, indent+1)
		fmt.Fprintf(b, "%send\n", pad)
		return
	}
	emitWriteValue(m, b, val, f.Type, indent)
}

// emitWriteValue writes the value at the field's kind.
func emitWriteValue(m *Module, b *strings.Builder, val string, t ir.FieldType, indent int) {
	pad := strings.Repeat("  ", indent)
	switch t.Kind {
	case ir.TBool:
		fmt.Fprintf(b, "%sserializeBool(stream, %s)\n", pad, val)
	case ir.TInt:
		if t.Width == 128 {
			fmt.Fprintf(b, "%sserializeInt128(stream, %s)\n", pad, val)
			return
		}
		fn := "serializeInt"
		if t.Width == 64 && !t.Signed {
			fn = "serializeUint64"
		} else if t.Width == 32 {
			fn = "serializeInt32"
		} else if t.Width == 16 {
			fn = "serializeInt16"
		} else if t.Width == 8 {
			fn = "serializeInt8"
		}
		fmt.Fprintf(b, "%s%s(stream, %s)\n", pad, fn, val)
	case ir.TBits:
		fmt.Fprintf(b, "%sserializeBits(stream, %s, %d)\n", pad, val, t.Width)
	case ir.TFloat32:
		fmt.Fprintf(b, "%sserializeFloat(stream, %s)\n", pad, val)
	case ir.TFloat64:
		fmt.Fprintf(b, "%sserializeDouble(stream, %s)\n", pad, val)
	case ir.TString:
		fmt.Fprintf(b, "%sserializeString(stream, %s)\n", pad, val)
	case ir.TBytes:
		fmt.Fprintf(b, "%sserializeBytes(stream, %s)\n", pad, val)
	case ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Struct:
			fmt.Fprintf(b, "%s_M.Write%s(stream, %s)\n", pad, t.Name, val)
		case *ir.Union:
			emitWriteUnion(m, b, val, t.Ref.(*ir.Union), indent)
		case *ir.Enum:
			fmt.Fprintf(b, "%sserializeInt(stream, %s, %d)\n", pad, val, t.Ref.(*ir.Enum).StorageBits)
		case *ir.Flags:
			fmt.Fprintf(b, "%sserializeBits(stream, %s, %d)\n", pad, val, t.Ref.(*ir.Flags).WireBits)
		}
	}
}

// arrayCountBits is the width of a counted array's `NameCount` field: the
// smallest unsigned width fitting the declared bound (ir.StorageBitsFor).
func arrayCountBits(f *ir.Field) int {
	return ir.StorageBitsFor(f.ArrayBound)
}

func emitWriteUnion(m *Module, b *strings.Builder, val string, un *ir.Union, indent int) {
	pad := strings.Repeat("  ", indent)
	fmt.Fprintf(b, "%sserializeInt(stream, %s.Type, %d)\n", pad, val, un.StorageBits)
	for i, arm := range un.Variants {
		key := ir.GoExportName(arm.Name)
		if arm.Void() {
			continue
		}
		fmt.Fprintf(b, "%sif %s.Type == %d then\n", pad, val, i+1)
		emitWriteValue(m, b, fmt.Sprintf("%s.%s", val, key), arm.F.Type, indent+1)
		fmt.Fprintf(b, "%send\n", pad)
	}
}
