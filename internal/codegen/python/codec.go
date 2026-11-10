package python

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A kindEmitter is one field kind's read, write, validate and zero emitters.
// Every carried kind lives in its own file and registers from init(), so a
// later packet card adds a file and one registration line
// (docs/PORT-PYTHON.md, THE PACKET CORE CUT).
type kindEmitter struct {
	name     string
	read     func(c *emitCtx, f *ir.Field, target string)
	write    func(c *emitCtx, f *ir.Field, expr string)
	validate func(c *emitCtx, f *ir.Field, expr, path string)
	zero     func(c *emitCtx, f *ir.Field, target string)
}

var kindEmitters = map[string]*kindEmitter{}

func registerKind(k *kindEmitter) { kindEmitters[k.name] = k }

// emitCtx is one generated method's body while an emitter appends to it.
type emitCtx struct {
	m      *pyModule
	b      *strings.Builder
	indent string
	locals int
}

func (c *emitCtx) line(format string, args ...any) {
	fmt.Fprintf(c.b, "%s%s\n", c.indent, fmt.Sprintf(format, args...))
}

func (c *emitCtx) local() string {
	c.locals++
	return fmt.Sprintf("_s_v%d", c.locals)
}

// fieldKind is the registry key of a field, or "" when no emitter carries it.
func fieldKind(f *ir.Field) string {
	if f.Array != ir.ArrayNone || f.Type.Pointer {
		return ""
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt:
		if f.Type.Width <= 64 {
			return "int"
		}
		return ""
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			return "enum"
		case *ir.Flags:
			return "flags"
		case *ir.Struct:
			return "type"
		case *ir.Union:
			return "union"
		}
	}
	return ""
}

// blockingKind names what a field is when the codec does not carry it.
func blockingKind(f *ir.Field) string {
	if f.Array != ir.ArrayNone {
		return "array"
	}
	if f.Type.Pointer {
		return "pointer"
	}
	switch f.Type.Kind {
	case ir.TInt:
		if f.Type.Width > 64 {
			return fmt.Sprintf("int%d", f.Type.Width)
		}
	case ir.TBits:
		return "bits"
	case ir.TFixed:
		return "fixed"
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TString:
		return "string"
	case ir.TBytes:
		return "bytes"
	case ir.TMap:
		return "map"
	case ir.TNamed:
		return "named"
	}
	return "unknown"
}

// firstBlockerItem returns the first wire item the codec does not carry, as
// the kind's name, or "" when every item is carried.
func firstBlockerItem(items []ir.Item) string {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			if fieldKind(item.F) == "" {
				return blockingKind(item.F)
			}
		case *ir.Branch:
			if k := firstBlockerItem(item.Then); k != "" {
				return k
			}
			if k := firstBlockerItem(item.Else); k != "" {
				return k
			}
		case *ir.ConstItem:
			return "const"
		case *ir.ReservedItem:
			return "reserved"
		case *ir.AlignItem:
			return "align"
		}
	}
	return ""
}

// checkCarried refuses the unit at generate time, by kind and field, when any
// declaration needs a wire emitter this card does not carry.
func checkCarried(u *ir.Unit) error {
	for _, f := range u.Files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ir.Struct:
				items := d.Items
				if len(items) == 0 {
					for _, f := range d.Fields {
						items = append(items, &ir.FieldItem{F: f})
					}
				}
				for _, item := range items {
					if item, ok := item.(*ir.FieldItem); ok && fieldKind(item.F) == "" {
						return uncarried(f.Base, d.Name, "field", item.F.Name, blockingKind(item.F))
					}
				}
				if k := firstBlockerItem(items); k != "" {
					return uncarried(f.Base, d.Name, "wire", "", k)
				}
			case *ir.Union:
				for _, v := range d.Variants {
					if v.Void() {
						continue
					}
					if fieldKind(v.F) == "" {
						return uncarried(f.Base, d.Name, "arm", v.Name, blockingKind(v.F))
					}
				}
			}
		}
	}
	return nil
}

func uncarried(file, owner, what, name, kind string) error {
	subject := owner
	if name != "" {
		subject += "'s " + what + " " + name
	} else if what != "" {
		subject += "'s " + what
	}
	return fmt.Errorf("python: %s.schema: %s uses %s, which the Python target does not carry yet (docs/PORT-PYTHON.md, THE CARD MAP: P4 the packet wire)", file, subject, kind)
}

// emitCodecStruct appends a type's read, write, validate and zero methods.
func (m *pyModule) emitCodecStruct(d *ir.Struct) {
	m.needSchema("InvalidValue")
	m.needSchema("Refused")
	m.needSchema("wire_input")
	m.needSerialize("ReadStream")
	m.needSerialize("Ref")
	m.needSerialize("WriteStream")
	m.need("Self")

	m.line("    @classmethod")
	m.line("    def read(cls, data: bytes | bytearray | memoryview) -> Self:")
	m.line("        stream = ReadStream(wire_input(data))")
	m.line("        value = cls()")
	m.line("        if not value._s_read(stream):")
	m.line("            raise Refused(stream.refusal() or \"rejected\")")
	m.line("        if not stream.serialize_end():")
	m.line("            raise Refused(stream.refusal() or \"rejected\")")
	m.line("        return value")
	m.line("")
	m.line("    def write(self) -> bytes:")
	m.line("        self._s_validate(%q)", d.Name)
	m.line("        stream = WriteStream(%d)", wireCapacity(m, d.Name))
	m.line("        self._s_write(stream)")
	m.line("        stream.flush()")
	m.line("        return stream.data()")
	m.line("")

	m.emitReadStruct(d)
	m.emitWriteStruct(d)
	m.emitValidateStruct(d)
	m.emitZeroStruct(d)
}

func (m *pyModule) emitReadStruct(d *ir.Struct) {
	var b strings.Builder
	c := &emitCtx{m: m, b: &b, indent: "        "}
	m.readItems(c, d.Items)
	m.line("    def _s_read(self, stream: ReadStream) -> bool:")
	if len(d.Items) == 0 {
		m.line("        return True")
	} else {
		m.printf("%s", b.String())
		m.line("        return True")
	}
	m.line("")
}

func (m *pyModule) emitWriteStruct(d *ir.Struct) {
	var b strings.Builder
	c := &emitCtx{m: m, b: &b, indent: "        "}
	m.writeItems(c, d.Items)
	m.line("    def _s_write(self, stream: WriteStream) -> None:")
	if len(d.Items) == 0 {
		m.line("        pass")
	} else {
		m.printf("%s", b.String())
	}
	m.line("")
}

func (m *pyModule) emitValidateStruct(d *ir.Struct) {
	var b strings.Builder
	c := &emitCtx{m: m, b: &b, indent: "        "}
	m.validateItems(c, d.Items)
	m.line("    def _s_validate(self, path: str) -> None:")
	if len(d.Items) == 0 {
		m.line("        pass")
	} else {
		m.printf("%s", b.String())
	}
	m.line("")
}

func (m *pyModule) emitZeroStruct(d *ir.Struct) {
	var b strings.Builder
	c := &emitCtx{m: m, b: &b, indent: "        "}
	m.zeroItems(c, d.Items)
	m.line("    def _s_zero(self) -> None:")
	if len(d.Items) == 0 {
		m.line("        pass")
	} else {
		m.printf("%s", b.String())
	}
	m.line("")
}

func (m *pyModule) emitCodecUnion(d *ir.Union) {
	m.needSchema("InvalidValue")
	m.needSchema("Refused")
	m.needSchema("wire_input")
	m.needSerialize("ReadStream")
	m.needSerialize("Ref")
	m.needSerialize("WriteStream")
	m.need("Self")

	m.line("    @classmethod")
	m.line("    def read(cls, data: bytes | bytearray | memoryview) -> Self:")
	m.line("        stream = ReadStream(wire_input(data))")
	m.line("        value = cls()")
	m.line("        if not value._s_read(stream):")
	m.line("            raise Refused(stream.refusal() or \"rejected\")")
	m.line("        if not stream.serialize_end():")
	m.line("            raise Refused(stream.refusal() or \"rejected\")")
	m.line("        return value")
	m.line("")
	m.line("    def write(self) -> bytes:")
	m.line("        self._s_validate(%q)", d.Name)
	m.line("        stream = WriteStream(%d)", wireCapacity(m, d.Name))
	m.line("        self._s_write(stream)")
	m.line("        stream.flush()")
	m.line("        return stream.data()")
	m.line("")
	m.emitReadUnion(d)
	m.emitWriteUnion(d)
	m.emitValidateUnion(d)
	m.emitZeroUnion(d)
}

func (m *pyModule) emitReadUnion(d *ir.Union) {
	m.line("    def _s_read(self, stream: ReadStream) -> bool:")
	if d.Max == 0 {
		m.line("        self.type = 0")
		m.line("        return True")
		m.line("")
		return
	}
	m.readTag(d, "        ", "self.type")
	for i, v := range d.Variants {
		m.line("        if self.type == %d:", i+1)
		if v.Void() {
			m.line("            return True")
			continue
		}
		m.line("            self.%s = %s", v.Name, m.unionArmDefault(v))
		m.line("            return self.%s._s_read(stream)", v.Name)
	}
	m.line("        return True")
	m.line("")
}

func (m *pyModule) emitWriteUnion(d *ir.Union) {
	m.line("    def _s_write(self, stream: WriteStream) -> None:")
	if d.Max == 0 {
		m.line("        pass")
		m.line("")
		return
	}
	m.writeTag(d, "        ", "self.type")
	for i, v := range d.Variants {
		if v.Void() {
			continue
		}
		m.line("        if self.type == %d:", i+1)
		c := &emitCtx{m: m, b: new(strings.Builder), indent: "            "}
		emitKindWrite(c, v.F, "self."+v.Name)
		m.printf("%s", c.b.String())
	}
	m.line("")
}

func (m *pyModule) emitValidateUnion(d *ir.Union) {
	m.line("    def _s_validate(self, path: str) -> None:")
	m.validateTag(d, "        ", "self.type")
	for i, v := range d.Variants {
		if v.Void() {
			continue
		}
		m.line("        if self.type == %d:", i+1)
		c := &emitCtx{m: m, b: new(strings.Builder), indent: "            "}
		emitKindValidate(c, v.F, "self."+v.Name, "path")
		if c.b.Len() > 0 {
			m.printf("%s", c.b.String())
		} else {
			m.line("            pass")
		}
	}
	m.line("")
}

func (m *pyModule) emitZeroUnion(d *ir.Union) {
	m.line("    def _s_zero(self) -> None:")
	m.line("        self.type = 0")
	for _, v := range unionArms(d) {
		if fieldKind(v.F) == "" {
			continue
		}
		c := &emitCtx{m: m, b: new(strings.Builder), indent: "        "}
		emitKindZero(c, v.F, "self."+v.Name)
		m.printf("%s", c.b.String())
	}
	m.line("")
}

func (m *pyModule) readTag(d *ir.Union, indent, target string) {
	c := &emitCtx{m: m, b: new(strings.Builder), indent: indent}
	emitRangeRead(c, 0, d.Max, target)
	m.printf("%s", c.b.String())
}

func (m *pyModule) writeTag(d *ir.Union, indent, expr string) {
	c := &emitCtx{m: m, b: new(strings.Builder), indent: indent}
	if d.Max > 0 {
		emitRangeWrite(c, expr, 0, d.Max)
	}
	m.printf("%s", c.b.String())
}

func (m *pyModule) validateTag(d *ir.Union, indent, expr string) {
	c := &emitCtx{m: m, b: new(strings.Builder), indent: indent}
	m.needSchema("validate_int")
	if d.Max > 0 {
		c.line("validate_int(%s, path + \".type\", 0, %d)", expr, d.Max)
	}
	m.printf("%s", c.b.String())
}

func (m *pyModule) readItems(c *emitCtx, items []ir.Item) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			m.readField(c, item.F, "self."+item.F.Name)
		case *ir.Branch:
			cond := "self." + item.Cond
			if item.Neg {
				cond = "not " + cond
			}
			c.line("if %s:", cond)
			inner := *c
			inner.indent += "    "
			m.readItems(&inner, item.Then)
			c.locals = inner.locals
			m.zeroItems(&inner, item.Else)
			c.line("else:")
			other := *c
			other.indent += "    "
			m.readItems(&other, item.Else)
			c.locals = other.locals
			m.zeroItems(&other, item.Then)
		}
	}
}

func (m *pyModule) writeItems(c *emitCtx, items []ir.Item) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			m.writeField(c, item.F, "self."+item.F.Name)
		case *ir.Branch:
			cond := "self." + item.Cond
			if item.Neg {
				cond = "not " + cond
			}
			c.line("if %s:", cond)
			inner := *c
			inner.indent += "    "
			m.writeItems(&inner, item.Then)
			c.locals = inner.locals
			if item.Else != nil {
				c.line("else:")
				other := *c
				other.indent += "    "
				m.writeItems(&other, item.Else)
				c.locals = other.locals
			}
		}
	}
}

func (m *pyModule) validateItems(c *emitCtx, items []ir.Item) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			m.validateField(c, item.F, "self."+item.F.Name)
		case *ir.Branch:
			cond := "self." + item.Cond
			if item.Neg {
				cond = "not " + cond
			}
			c.line("if %s:", cond)
			inner := *c
			inner.indent += "    "
			m.validateItems(&inner, item.Then)
			c.locals = inner.locals
			if item.Else != nil {
				c.line("else:")
				other := *c
				other.indent += "    "
				m.validateItems(&other, item.Else)
				c.locals = other.locals
			}
		}
	}
}

func (m *pyModule) zeroItems(c *emitCtx, items []ir.Item) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			m.zeroField(c, item.F, "self."+item.F.Name)
		case *ir.Branch:
			m.zeroItems(c, item.Then)
			m.zeroItems(c, item.Else)
		}
	}
}

func (m *pyModule) readField(c *emitCtx, f *ir.Field, target string) {
	emitKindRead(c, f, target)
}

func (m *pyModule) writeField(c *emitCtx, f *ir.Field, expr string) {
	emitKindWrite(c, f, expr)
}

func (m *pyModule) validateField(c *emitCtx, f *ir.Field, expr string) {
	emitKindValidate(c, f, expr, "path")
}

func (m *pyModule) zeroField(c *emitCtx, f *ir.Field, target string) {
	emitKindZero(c, f, target)
}

func emitKindRead(c *emitCtx, f *ir.Field, target string) {
	k := kindEmitters[fieldKind(f)]
	if k == nil {
		return
	}
	k.read(c, f, target)
}

func emitKindWrite(c *emitCtx, f *ir.Field, expr string) {
	k := kindEmitters[fieldKind(f)]
	if k == nil {
		return
	}
	k.write(c, f, expr)
}

func emitKindValidate(c *emitCtx, f *ir.Field, expr, path string) {
	k := kindEmitters[fieldKind(f)]
	if k == nil {
		return
	}
	k.validate(c, f, expr, path)
}

func emitKindZero(c *emitCtx, f *ir.Field, target string) {
	k := kindEmitters[fieldKind(f)]
	if k == nil {
		return
	}
	k.zero(c, f, target)
}

// emitRangeRead reads a ranged int [min, max] into target.
func emitRangeRead(c *emitCtx, min, max int64, target string) {
	emitRangeReadBig(c, big.NewInt(min), big.NewInt(max), target)
}

func emitRangeReadBig(c *emitCtx, min, max *big.Int, target string) {
	if min.Cmp(max) == 0 {
		c.line("%s = %s", target, intLiteral(min))
		return
	}
	local := c.local()
	c.m.needSerialize("Ref")
	c.line("%s = Ref(0)", local)
	c.line("if not stream.%s(%s, %s, %s):", rangeFn(min, max), local, intLiteral(min), intLiteral(max))
	c.line("    return False")
	c.line("%s = %s.value", target, local)
}

func emitRangeWrite(c *emitCtx, expr string, min, max int64) {
	emitRangeWriteBig(c, expr, big.NewInt(min), big.NewInt(max))
}

func emitRangeWriteBig(c *emitCtx, expr string, min, max *big.Int) {
	if min.Cmp(max) == 0 {
		return
	}
	c.m.needSerialize("Ref")
	c.line("stream.%s(Ref(%s), %s, %s)", rangeFn(min, max), expr, intLiteral(min), intLiteral(max))
}

func rangeFn(min, max *big.Int) string {
	int32Min := big.NewInt(-2147483648)
	int32Max := big.NewInt(2147483647)
	int64Min := big.NewInt(-9223372036854775808)
	int64Max := big.NewInt(9223372036854775807)
	switch {
	case min.Cmp(int32Min) >= 0 && max.Cmp(int32Max) <= 0:
		return "serialize_int"
	case min.Cmp(int64Min) >= 0 && max.Cmp(int64Max) <= 0:
		return "serialize_int64"
	default:
		return "serialize_int128"
	}
}

// wireCapacity is the write buffer size in bytes: the type's maximum wire
// width, rounded up to a multiple of eight bytes (serialize's capacity rule).
func wireCapacity(m *pyModule, name string) int64 {
	bits := m.wireBits(name, map[string]bool{})
	return ((bits + 63) / 64) * 8
}

func (m *pyModule) wireBits(name string, seen map[string]bool) int64 {
	if seen[name] {
		return 0
	}
	seen[name] = true
	defer delete(seen, name)
	if st, ok := m.u.Structs[name]; ok {
		return wireItemsBits(m, st.Items, seen)
	}
	if un, ok := m.u.Unions[name]; ok {
		bits := rangeBits64(0, un.Max)
		var armMax int64
		for _, v := range un.Variants {
			if v.Void() {
				continue
			}
			if b := m.fieldBits(v.F, seen); b > armMax {
				armMax = b
			}
		}
		return bits + armMax
	}
	return 0
}

func wireItemsBits(m *pyModule, items []ir.Item, seen map[string]bool) int64 {
	var bits int64
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			bits += m.fieldBits(item.F, seen)
		case *ir.Branch:
			then := wireItemsBits(m, item.Then, seen)
			els := wireItemsBits(m, item.Else, seen)
			if els > then {
				then = els
			}
			bits += then
		}
	}
	return bits
}

func (m *pyModule) fieldBits(f *ir.Field, seen map[string]bool) int64 {
	if f.Array != ir.ArrayNone {
		return 0
	}
	switch f.Type.Kind {
	case ir.TBool:
		return 1
	case ir.TInt:
		if f.HasIntRange {
			return rangeBits64Big(f.IntMin, f.IntMax)
		}
		return int64(f.Type.Width)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			return rangeBits64(0, ref.Max)
		case *ir.Flags:
			return int64(ref.WireBits)
		case *ir.Struct:
			return m.wireBits(ref.Name, seen)
		case *ir.Union:
			return m.wireBits(ref.Name, seen)
		}
	}
	return 0
}

func rangeBits64(min, max int64) int64 {
	if max <= min {
		return 0
	}
	return int64(new(big.Int).Sub(big.NewInt(max), big.NewInt(min)).BitLen())
}

func rangeBits64Big(min, max *big.Int) int64 {
	if min == nil || max == nil || max.Cmp(min) <= 0 {
		return 0
	}
	return int64(new(big.Int).Sub(max, min).BitLen())
}
