package python

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A kindEmitter is one field kind's read, write, validate and zero emitters.
// Every carried kind lives in its own file and registers from init(), so a
// new packet kind adds a file and one registration line
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
// Method-level bindings (bound stream operations, reusable Ref holders, type
// checks) are collected in binds and emitted once at the top of the method
// (docs/PORT-PYTHON.md, D1: straight-line generated code with locals bound).
type emitCtx struct {
	m      *pyModule
	b      *strings.Builder
	indent string
	binds  map[string]string
	locals int
}

func newEmitCtx(m *pyModule, indent string) *emitCtx {
	return &emitCtx{
		m:      m,
		b:      new(strings.Builder),
		indent: indent,
		binds:  map[string]string{},
	}
}

func (c *emitCtx) sub(indent string) *emitCtx {
	return &emitCtx{
		m:      c.m,
		b:      new(strings.Builder),
		indent: indent,
		binds:  c.binds,
		locals: c.locals,
	}
}

func (c *emitCtx) line(format string, args ...any) {
	fmt.Fprintf(c.b, "%s%s\n", c.indent, fmt.Sprintf(format, args...))
}

func (c *emitCtx) local() string {
	c.locals++
	return fmt.Sprintf("_s_v%d", c.locals)
}

func (c *emitCtx) bind(name, expr string) string {
	c.binds[name] = expr
	return name
}

func (c *emitCtx) ref(name, zero string) string {
	c.m.needSerialize("Ref")
	return c.bind(name, "Ref("+zero+")")
}

func (c *emitCtx) streamOp(op string) string {
	return c.bind("_s_"+op, "stream."+op)
}

func (c *emitCtx) finish() string {
	if len(c.binds) == 0 {
		return c.b.String()
	}
	keys := make([]string, 0, len(c.binds))
	for k := range c.binds {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&out, "%s%s = %s\n", c.indent, k, c.binds[k])
	}
	out.WriteString(c.b.String())
	return out.String()
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
// declaration needs a wire emitter the target does not carry yet.
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
	m.needSerialize("WriteMisuse")
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
	m.line("        try:")
	m.line("            self._s_write(stream)")
	m.line("        except WriteMisuse as _s_exc:")
	m.line("            raise InvalidValue(%q, str(_s_exc)) from _s_exc", d.Name)
	m.line("        stream.flush()")
	m.line("        return stream.data()")
	m.line("")

	m.emitReadStruct(d)
	m.emitWriteStruct(d)
	m.emitValidateStruct(d)
	m.emitZeroStruct(d)
}

func (m *pyModule) emitReadStruct(d *ir.Struct) {
	c := newEmitCtx(m, "        ")
	m.readItems(c, d.Items)
	body := c.finish()
	m.line("    def _s_read(self, stream: ReadStream) -> bool:")
	if body != "" {
		m.printf("%s", body)
	}
	m.line("        return True")
	m.line("")
}

func (m *pyModule) emitWriteStruct(d *ir.Struct) {
	c := newEmitCtx(m, "        ")
	m.writeItems(c, d.Items)
	body := c.finish()
	m.line("    def _s_write(self, stream: WriteStream) -> None:")
	if body == "" {
		m.line("        pass")
	} else {
		m.printf("%s", body)
	}
	m.line("")
}

func (m *pyModule) emitValidateStruct(d *ir.Struct) {
	c := newEmitCtx(m, "        ")
	m.validateItems(c, d.Items)
	body := c.finish()
	m.line("    def _s_validate(self, path: str) -> None:")
	if body == "" {
		m.line("        pass")
	} else {
		m.printf("%s", body)
	}
	m.line("")
}

func (m *pyModule) emitZeroStruct(d *ir.Struct) {
	c := newEmitCtx(m, "        ")
	m.zeroItems(c, d.Items)
	body := c.finish()
	m.line("    def _s_zero(self) -> None:")
	if body == "" {
		m.line("        pass")
	} else {
		m.printf("%s", body)
	}
	m.line("")
}

func (m *pyModule) emitCodecUnion(d *ir.Union) {
	m.needSchema("InvalidValue")
	m.needSchema("Refused")
	m.needSchema("wire_input")
	m.needSerialize("ReadStream")
	m.needSerialize("WriteMisuse")
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
	m.line("        try:")
	m.line("            self._s_write(stream)")
	m.line("        except WriteMisuse as _s_exc:")
	m.line("            raise InvalidValue(%q, str(_s_exc)) from _s_exc", d.Name)
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
	c := newEmitCtx(m, "        ")
	emitRangeRead(c, 0, d.Max, "self.type")
	for i, v := range d.Variants {
		cond := "if"
		if i > 0 {
			cond = "elif"
		}
		c.line("%s self.type == %d:", cond, i+1)
		if v.Void() {
			c.line("    return True")
			continue
		}
		c.line("    self.%s = %s", v.Name, m.unionArmDefault(v))
		sub := c.sub("            ")
		emitKindRead(sub, v.F, "self."+v.Name)
		c.locals = sub.locals
		c.b.WriteString(sub.b.String())
		c.line("    return True")
	}
	c.line("return True")
	m.printf("%s\n", c.finish())
}

func (m *pyModule) emitWriteUnion(d *ir.Union) {
	m.line("    def _s_write(self, stream: WriteStream) -> None:")
	if d.Max == 0 {
		m.line("        pass")
		m.line("")
		return
	}
	c := newEmitCtx(m, "        ")
	emitRangeWrite(c, "self.type", 0, d.Max)
	first := true
	for i, v := range d.Variants {
		if v.Void() {
			continue
		}
		sub := c.sub("            ")
		emitKindWrite(sub, v.F, "self."+v.Name)
		c.locals = sub.locals
		if sub.b.Len() == 0 {
			continue
		}
		cond := "if"
		if !first {
			cond = "elif"
		}
		first = false
		c.line("%s self.type == %d:", cond, i+1)
		c.b.WriteString(sub.b.String())
	}
	body := c.finish()
	if body == "" {
		m.line("        pass")
	} else {
		m.printf("%s", body)
	}
	m.line("")
}

func (m *pyModule) emitValidateUnion(d *ir.Union) {
	m.line("    def _s_validate(self, path: str) -> None:")
	c := newEmitCtx(m, "        ")
	emitIntValidate(c, "self.type", "path + \".type\"", "0", strconv.FormatInt(d.Max, 10))
	first := true
	for i, v := range d.Variants {
		if v.Void() {
			continue
		}
		sub := c.sub("            ")
		emitKindValidate(sub, v.F, "self."+v.Name, "path")
		c.locals = sub.locals
		if sub.b.Len() == 0 {
			continue
		}
		cond := "if"
		if !first {
			cond = "elif"
		}
		first = false
		c.line("%s self.type == %d:", cond, i+1)
		c.b.WriteString(sub.b.String())
	}
	m.printf("%s\n", c.finish())
}

func (m *pyModule) emitZeroUnion(d *ir.Union) {
	m.line("    def _s_zero(self) -> None:")
	c := newEmitCtx(m, "        ")
	c.line("self.type = 0")
	for _, v := range unionArms(d) {
		if fieldKind(v.F) == "" {
			continue
		}
		emitKindZero(c, v.F, "self."+v.Name)
	}
	m.printf("%s\n", c.finish())
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
			thenSub := c.sub(c.indent + "    ")
			m.readItems(thenSub, item.Then)
			m.zeroItems(thenSub, item.Else)
			c.locals = thenSub.locals

			elseSub := c.sub(c.indent + "    ")
			m.readItems(elseSub, item.Else)
			m.zeroItems(elseSub, item.Then)
			c.locals = elseSub.locals

			if thenSub.b.Len() == 0 && elseSub.b.Len() == 0 {
				continue
			}
			c.line("if %s:", cond)
			if thenSub.b.Len() == 0 {
				c.line("    pass")
			} else {
				c.b.WriteString(thenSub.b.String())
			}
			if elseSub.b.Len() > 0 {
				c.line("else:")
				c.b.WriteString(elseSub.b.String())
			}
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
			thenSub := c.sub(c.indent + "    ")
			m.writeItems(thenSub, item.Then)
			c.locals = thenSub.locals

			elseSub := c.sub(c.indent + "    ")
			if item.Else != nil {
				m.writeItems(elseSub, item.Else)
				c.locals = elseSub.locals
			}
			if thenSub.b.Len() == 0 && elseSub.b.Len() == 0 {
				continue
			}
			c.line("if %s:", cond)
			if thenSub.b.Len() == 0 {
				c.line("    pass")
			} else {
				c.b.WriteString(thenSub.b.String())
			}
			if elseSub.b.Len() > 0 {
				c.line("else:")
				c.b.WriteString(elseSub.b.String())
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
			thenSub := c.sub(c.indent + "    ")
			m.validateItems(thenSub, item.Then)
			c.locals = thenSub.locals

			elseSub := c.sub(c.indent + "    ")
			if item.Else != nil {
				m.validateItems(elseSub, item.Else)
				c.locals = elseSub.locals
			}
			if thenSub.b.Len() == 0 && elseSub.b.Len() == 0 {
				continue
			}
			c.line("if %s:", cond)
			if thenSub.b.Len() == 0 {
				c.line("    pass")
			} else {
				c.b.WriteString(thenSub.b.String())
			}
			if elseSub.b.Len() > 0 {
				c.line("else:")
				c.b.WriteString(elseSub.b.String())
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
	ri := c.ref("_s_ri", "0")
	op := c.streamOp(rangeFn(min, max))
	c.line("if not %s(%s, %s, %s):", op, ri, intLiteral(min), intLiteral(max))
	c.line("    return False")
	c.line("%s = %s.value", target, ri)
}

func emitRangeWrite(c *emitCtx, expr string, min, max int64) {
	emitRangeWriteBig(c, expr, big.NewInt(min), big.NewInt(max))
}

func emitRangeWriteBig(c *emitCtx, expr string, min, max *big.Int) {
	if min.Cmp(max) == 0 {
		return
	}
	ri := c.ref("_s_ri", "0")
	op := c.streamOp(rangeFn(min, max))
	c.line("%s.value = %s", ri, expr)
	c.line("%s(%s, %s, %s)", op, ri, intLiteral(min), intLiteral(max))
}

func emitIntValidate(c *emitCtx, expr, pathExpr, min, max string) {
	c.m.needSchema("validate_int")
	tp := c.bind("_s_type", "type")
	it := c.bind("_s_int", "int")
	v := c.local()
	c.line("%s = %s", v, expr)
	c.line("if %s(%s) is not %s or %s < %s or %s > %s:", tp, v, it, v, min, v, max)
	c.line("    validate_int(%s, %s, %s, %s)", v, pathExpr, min, max)
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
