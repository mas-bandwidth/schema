package ruby

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

type kindEmitter struct {
	name     string
	read     func(c *emitCtx, f *ir.Field, target string)
	write    func(c *emitCtx, f *ir.Field, expr string)
	validate func(c *emitCtx, f *ir.Field, expr, path string)
	zero     func(c *emitCtx, f *ir.Field, target string)
}

var kindEmitters = map[string]*kindEmitter{}

func registerKind(k *kindEmitter) { kindEmitters[k.name] = k }

type itemEmitter struct {
	name     string
	read     func(m *rbModule, c *emitCtx, item ir.Item)
	write    func(m *rbModule, c *emitCtx, item ir.Item)
	validate func(m *rbModule, c *emitCtx, item ir.Item)
	zero     func(m *rbModule, c *emitCtx, item ir.Item)
}

var itemEmitters = map[string]*itemEmitter{}

func registerItem(e *itemEmitter) { itemEmitters[e.name] = e }

func itemKind(item ir.Item) string {
	switch item.(type) {
	case *ir.Branch:
		return "branch"
	case *ir.ConstItem:
		return "const"
	case *ir.ReservedItem:
		return "reserved"
	case *ir.AlignItem:
		return "align"
	}
	return ""
}

func emitItemRead(m *rbModule, c *emitCtx, item ir.Item) {
	if e := itemEmitters[itemKind(item)]; e != nil {
		e.read(m, c, item)
	}
}

func emitItemWrite(m *rbModule, c *emitCtx, item ir.Item) {
	if e := itemEmitters[itemKind(item)]; e != nil {
		e.write(m, c, item)
	}
}

func emitItemValidate(m *rbModule, c *emitCtx, item ir.Item) {
	if e := itemEmitters[itemKind(item)]; e != nil {
		e.validate(m, c, item)
	}
}

func emitItemZero(m *rbModule, c *emitCtx, item ir.Item) {
	if e := itemEmitters[itemKind(item)]; e != nil {
		e.zero(m, c, item)
	}
}

// emitCtx collects straight-line body statements and method-top bound Ref
// locals (docs/PORT-RUBY.md, D1: straight-line generated code with locals
// bound).
type emitCtx struct {
	m      *rbModule
	b      *strings.Builder
	indent string
	binds  map[string]string
	locals int
}

func newEmitCtx(m *rbModule, indent string) *emitCtx {
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

func (c *emitCtx) ref(name, zeroExpr string) string {
	return c.bind(name, "Serialize::Ref.new("+zeroExpr+")")
}

func (c *emitCtx) refTyped(name, zeroExpr, rbsType string) string {
	return c.bind(name, fmt.Sprintf("Serialize::Ref.new(%s) #: Serialize::Ref[%s]", zeroExpr, rbsType))
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

func fieldKind(f *ir.Field) string {
	if f.Type.Pointer {
		return ""
	}
	if f.Array != ir.ArrayNone {
		if f.Array != ir.ArrayFixed && f.Array != ir.ArrayCounted {
			return ""
		}
		if fieldKind(arrayElem(f)) == "" {
			return ""
		}
		return "array"
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TString, ir.TBytes:
		if f.Type.Unbounded || f.Type.Size < 1 {
			return ""
		}
		if f.Type.Kind == ir.TString {
			return "string"
		}
		return "bytes"
	case ir.TInt:
		if f.Type.Width <= 64 {
			return "int"
		}
		if f.Type.Width == 128 {
			return "int128"
		}
		return ""
	case ir.TBits:
		return "bits"
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TFixed:
		return "fixed"
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

// FieldKind returns the packet kind key of f, or "" if not carried.
func FieldKind(f *ir.Field) string { return fieldKind(f) }

func blockingKind(f *ir.Field) string {
	if f.Type.Pointer {
		return "pointer"
	}
	switch {
	case f.Array == ir.ArrayList:
		return "list"
	case f.Array != ir.ArrayNone:
		return blockingKind(arrayElem(f))
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

// BlockingKind names what blocks f when FieldKind(f) == "".
func BlockingKind(f *ir.Field) string { return blockingKind(f) }

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
		case *ir.ConstItem, *ir.ReservedItem, *ir.AlignItem:
		}
	}
	return ""
}

func checkCarried(u *ir.Unit) error {
	for _, f := range u.Files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ir.Struct:
				items := d.Items
				if len(items) == 0 {
					for _, fld := range d.Fields {
						items = append(items, &ir.FieldItem{F: fld})
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
	return fmt.Errorf("ruby: %s.schema: %s uses %s, which the Ruby target does not carry yet (docs/PORT-RUBY.md, THE CARD MAP: R4 the packet wire)", file, subject, kind)
}

func (m *rbModule) emitCodecStruct(d *ir.Struct) {
	m.emitEntryPoints(d.Name)
	m.emitReadStruct(d)
	m.emitWriteStruct(d)
	m.emitValidateStruct(d)
	m.emitZeroStruct(d)
}

func (m *rbModule) emitCodecUnion(d *ir.Union) {
	m.emitEntryPoints(d.Name)
	m.emitReadUnion(d)
	m.emitWriteUnion(d)
	m.emitValidateUnion(d)
	m.emitZeroUnion(d)
}

func (m *rbModule) emitEntryPoints(name string) {
	m.line("    def self.read_bits(data, bit_offset: 0)")
	m.line("      stream, rem = ::%s._s_open_read_stream(data, bit_offset)", m.mod)
	m.line("      value = %s.new", name)
	m.line("      raise Refused.new(stream.refusal || \"rejected\") unless value._s_read(stream)")
	m.line("      [value, stream.bits_processed - rem]")
	m.line("    end")
	m.line("")
	m.line("    def self.read(data)")
	m.line("      pair = %s.read_bits(data)", name)
	m.line("      pair[0]")
	m.line("    end")
	m.line("")
	m.line("    def write")
	m.line("      _s_validate(%q)", name)
	m.line("      stream = WriteStream.new(%d)", wireCapacity(m, name))
	m.line("      begin")
	m.line("        _s_write(stream)")
	m.line("      rescue Serialize::WriteMisuse => _e")
	m.line("        raise InvalidValue.new(%q, _e.message)", name)
	m.line("      end")
	m.line("      stream.flush")
	m.line("      stream.data")
	m.line("    end")
	m.line("")
}

func structItems(d *ir.Struct) []ir.Item {
	if len(d.Items) > 0 || len(d.Fields) == 0 {
		return d.Items
	}
	items := make([]ir.Item, len(d.Fields))
	for i, f := range d.Fields {
		items[i] = &ir.FieldItem{F: f}
	}
	return items
}

func (m *rbModule) emitReadStruct(d *ir.Struct) {
	c := newEmitCtx(m, "      ")
	m.readItems(c, structItems(d))
	body := c.finish()
	m.line("    def _s_read(stream)")
	if body == "" {
		m.line("      _ = stream")
	} else {
		m.printf("%s", body)
	}
	m.line("      true")
	m.line("    end")
	m.line("")
}

func (m *rbModule) emitWriteStruct(d *ir.Struct) {
	c := newEmitCtx(m, "      ")
	m.writeItems(c, structItems(d))
	body := c.finish()
	m.line("    def _s_write(stream)")
	if body == "" {
		m.line("      _ = stream")
	} else {
		m.printf("%s", body)
	}
	m.line("      nil")
	m.line("    end")
	m.line("")
}

func (m *rbModule) emitValidateStruct(d *ir.Struct) {
	c := newEmitCtx(m, "      ")
	m.validateItems(c, structItems(d))
	body := c.finish()
	m.line("    def _s_validate(path)")
	if body == "" {
		m.line("      _ = path")
	} else {
		m.printf("%s", body)
	}
	m.line("      nil")
	m.line("    end")
	m.line("")
}

func (m *rbModule) emitZeroStruct(d *ir.Struct) {
	c := newEmitCtx(m, "      ")
	m.zeroItems(c, structItems(d))
	body := c.finish()
	m.line("    def _s_zero")
	if body != "" {
		m.printf("%s", body)
	}
	m.line("      nil")
	m.line("    end")
	m.line("")
}

func (m *rbModule) emitReadUnion(d *ir.Union) {
	m.line("    def _s_read(stream)")
	if d.Max == 0 {
		m.line("      _ = stream")
		m.line("      self.type = 0")
		m.line("      true")
		m.line("    end")
		m.line("")
		return
	}
	c := newEmitCtx(m, "      ")
	emitRangeRead(c, 0, d.Max, "self.type")
	for i, v := range d.Variants {
		cond := "if"
		if i > 0 {
			cond = "elsif"
		}
		c.line("%s self.type == %d", cond, i+1)
		if v.Void() {
			c.line("  return true")
			continue
		}
		c.line("  %s", fieldWrite("self", v.Name, m.unionArmDefault(v)))
		sub := c.sub("        ")
		emitKindRead(sub, v.F, fieldRead("self", v.Name))
		c.locals = sub.locals
		c.b.WriteString(sub.b.String())
		c.line("  return true")
	}
	if len(d.Variants) > 0 {
		c.line("end")
	}
	c.line("true")
	m.printf("%s    end\n\n", c.finish())
}

func (m *rbModule) emitWriteUnion(d *ir.Union) {
	m.line("    def _s_write(stream)")
	if d.Max == 0 {
		m.line("      _ = stream")
		m.line("      nil")
		m.line("    end")
		m.line("")
		return
	}
	c := newEmitCtx(m, "      ")
	emitRangeWrite(c, "self.type", 0, d.Max)
	first := true
	for i, v := range d.Variants {
		if v.Void() {
			continue
		}
		sub := c.sub("        ")
		emitKindWrite(sub, v.F, fieldRead("self", v.Name))
		c.locals = sub.locals
		if sub.b.Len() == 0 {
			continue
		}
		cond := "if"
		if !first {
			cond = "elsif"
		}
		first = false
		c.line("%s self.type == %d", cond, i+1)
		c.b.WriteString(sub.b.String())
	}
	if !first {
		c.line("end")
	}
	body := c.finish()
	if body != "" {
		m.printf("%s", body)
	}
	m.line("      nil")
	m.line("    end")
	m.line("")
}

func (m *rbModule) emitValidateUnion(d *ir.Union) {
	m.line("    def _s_validate(path)")
	c := newEmitCtx(m, "      ")
	emitIntValidate(c, "self.type", "\"#{path}.type\"", "0", strconv.FormatInt(d.Max, 10))
	first := true
	for i, v := range d.Variants {
		if v.Void() {
			continue
		}
		sub := c.sub("        ")
		emitKindValidate(sub, v.F, fieldRead("self", v.Name), "path")
		c.locals = sub.locals
		if sub.b.Len() == 0 {
			continue
		}
		cond := "if"
		if !first {
			cond = "elsif"
		}
		first = false
		c.line("%s self.type == %d", cond, i+1)
		c.b.WriteString(sub.b.String())
	}
	if !first {
		c.line("end")
	}
	m.printf("%s      nil\n    end\n", c.finish())
}

func (m *rbModule) emitZeroUnion(d *ir.Union) {
	m.line("    def _s_zero")
	c := newEmitCtx(m, "      ")
	c.line("self.type = 0")
	for _, v := range unionArms(d) {
		if fieldKind(v.F) == "" {
			continue
		}
		emitKindZero(c, v.F, fieldRead("self", v.Name))
	}
	m.printf("%s      nil\n    end\n", c.finish())
}

func (m *rbModule) readItems(c *emitCtx, items []ir.Item) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitKindRead(c, item.F, fieldRead("self", item.F.Name))
		default:
			emitItemRead(m, c, item)
		}
	}
}

func (m *rbModule) writeItems(c *emitCtx, items []ir.Item) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitKindWrite(c, item.F, fieldRead("self", item.F.Name))
		default:
			emitItemWrite(m, c, item)
		}
	}
}

func (m *rbModule) validateItems(c *emitCtx, items []ir.Item) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitKindValidate(c, item.F, fieldRead("self", item.F.Name), "path")
		default:
			emitItemValidate(m, c, item)
		}
	}
}

func (m *rbModule) zeroItems(c *emitCtx, items []ir.Item) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitKindZero(c, item.F, fieldRead("self", item.F.Name))
		default:
			emitItemZero(m, c, item)
		}
	}
}

func emitKindRead(c *emitCtx, f *ir.Field, target string) {
	if k := kindEmitters[fieldKind(f)]; k != nil {
		k.read(c, f, target)
	}
}

func emitKindWrite(c *emitCtx, f *ir.Field, expr string) {
	if k := kindEmitters[fieldKind(f)]; k != nil {
		k.write(c, f, expr)
	}
}

func emitKindValidate(c *emitCtx, f *ir.Field, expr, path string) {
	if k := kindEmitters[fieldKind(f)]; k != nil {
		k.validate(c, f, expr, path)
	}
}

func emitKindZero(c *emitCtx, f *ir.Field, target string) {
	if k := kindEmitters[fieldKind(f)]; k != nil {
		k.zero(c, f, target)
	}
}

func emitRangeRead(c *emitCtx, min, max int64, target string) {
	emitRangeReadBig(c, big.NewInt(min), big.NewInt(max), target)
}

func emitRangeReadBig(c *emitCtx, min, max *big.Int, target string) {
	if min.Cmp(max) == 0 {
		c.line("%s = %s", target, intLiteral(min))
		return
	}
	ri := c.ref("_s_ri", "0")
	c.line("return false unless stream.%s(%s, %s, %s)", rangeFn(min, max), ri, intLiteral(min), intLiteral(max))
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
	c.line("%s.value = %s", ri, expr)
	c.line("stream.%s(%s, %s, %s)", rangeFn(min, max), ri, intLiteral(min), intLiteral(max))
}

func emitIntValidate(c *emitCtx, expr, pathExpr, min, max string) {
	v := c.local()
	c.line("%s = %s #: untyped", v, expr)
	c.line("unless %s.is_a?(::Integer) && %s >= %s && %s <= %s", v, v, min, v, max)
	c.line("  ::%s._s_validate_int(%s, %s, %s, %s)", c.m.mod, v, pathExpr, min, max)
	c.line("end")
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

func wireCapacity(m *rbModule, name string) int64 {
	bits := m.wireBits(name, map[string]bool{})
	capBytes := max(((bits+63)/64)*8, 8)
	return capBytes
}

func (m *rbModule) wireBits(name string, seen map[string]bool) int64 {
	if seen[name] {
		return 0
	}
	seen[name] = true
	defer delete(seen, name)
	if st, ok := m.u.Structs[name]; ok {
		return wireItemsBits(m, structItems(st), seen)
	}
	if un, ok := m.u.Unions[name]; ok {
		var best int64
		for _, v := range un.Variants {
			if !v.Void() {
				if w := wireFieldBits(m, v.F, seen); w > best {
					best = w
				}
			}
		}
		return rangeBits64(0, un.Max) + best
	}
	return 64
}

func wireItemsBits(m *rbModule, items []ir.Item, seen map[string]bool) int64 {
	var total int64
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			total += wireFieldBits(m, item.F, seen)
		case *ir.Branch:
			tBits := wireItemsBits(m, item.Then, seen)
			eBits := wireItemsBits(m, item.Else, seen)
			if eBits > tBits {
				tBits = eBits
			}
			total += tBits
		case *ir.ConstItem:
			total += item.Bits
		case *ir.ReservedItem:
			total += item.Bits
		case *ir.AlignItem:
			total += 7
		}
	}
	return total
}

func wireFieldBits(m *rbModule, f *ir.Field, seen map[string]bool) int64 {
	if f.Array != ir.ArrayNone {
		elem := arrayElem(f)
		eb := wireFieldBits(m, elem, seen)
		if f.Array == ir.ArrayCounted {
			return rangeBits64(f.ArrayMin, f.ArrayBound) + f.ArrayBound*eb
		}
		return f.ArrayBound * eb
	}
	switch f.Type.Kind {
	case ir.TBool:
		return 1
	case ir.TInt:
		if f.HasIntRange {
			return int64(new(big.Int).Sub(f.IntMax, f.IntMin).BitLen())
		}
		return int64(f.Type.Width)
	case ir.TBits:
		return int64(f.Type.Width)
	case ir.TFixed:
		return fixedBits(f)
	case ir.TFloat32:
		if f.HasFloatRange {
			_, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
			return bits
		}
		return 32
	case ir.TFloat64:
		return 64
	case ir.TString, ir.TBytes:
		return rangeBits64(0, f.Type.Size) + 7 + 8*f.Type.Size
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
	return 64
}

func rangeBits64(min, max int64) int64 {
	if max <= min {
		return 0
	}
	return int64(new(big.Int).Sub(big.NewInt(max), big.NewInt(min)).BitLen())
}
