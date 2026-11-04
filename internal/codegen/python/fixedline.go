package python

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE IDENTITY LANE, STRAIGHT-LINE (docs/FIXED-FORM-ALGORITHM.md §4.2; D1 by
// measurement, docs/PORT-PYTHON.md). A record this build wrote is read by
// emitted code: one function per struct, table and union a fixed root
// reaches, every field at its constant offset, each run of plain integers
// fused into one struct.unpack_from. It holds every check the runtime's plan
// holds, by the same names: a bool or present byte past 1, an ordinal or tag
// past the set, a range, a count or length past its bound, ill-formed text, a
// non-finite float. Another lineage entry's records take the runtime's plan.

// plainInt is a field the identity reader can fuse: an unranged integer, flags
// mask or fixed-point raw value of 1, 2, 4 or 8 bytes, not optional, no array.
func plainInt(f *ir.Field) bool {
	if f.Array != ir.ArrayNone || f.KeyEnum != "" || f.Type.Optional {
		return false
	}
	switch f.Type.Kind {
	case ir.TInt, ir.TFixed, ir.TBits:
	case ir.TNamed:
		if _, ok := f.Type.Ref.(*ir.Flags); !ok {
			return false
		}
	default:
		return false
	}
	if lo, hi := rawBounds(f); lo != "None" || hi != "None" {
		return false
	}
	_, ok := intFormat(f)
	return ok
}

// intFormat is the struct format of an integer field's storage.
func intFormat(f *ir.Field) (string, bool) {
	size := ir.TableFixedElementBytes(f)
	signed := f.Type.Signed && (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed)
	if f.Type.Kind == ir.TInt && f.Type.Width == 128 {
		return "", false
	}
	switch size {
	case 1:
		return map[bool]string{true: "b", false: "B"}[signed], true
	case 2:
		return map[bool]string{true: "h", false: "H"}[signed], true
	case 4:
		return map[bool]string{true: "i", false: "I"}[signed], true
	case 8:
		return map[bool]string{true: "q", false: "Q"}[signed], true
	}
	return "", false
}

// line is one identity reader being emitted: its body and a temporary
// counter.
type line struct {
	g    *gen
	b    strings.Builder
	temp int
}

func (l *line) pf(indent, format string, args ...any) {
	l.b.WriteString(indent)
	fmt.Fprintf(&l.b, format, args...)
	l.b.WriteString("\n")
}

func (l *line) tmp() string {
	l.temp++
	return fmt.Sprintf("x%d", l.temp)
}

func refuse(reason, path string) string {
	return fmt.Sprintf("raise _s_rt.Refused(%q, %q)", reason, path)
}

// elem reads one element of f (the field itself when it is no array) at off
// into a fresh temporary, and answers the temporary.
func (l *line) elem(f *ir.Field, off, path, ind string) string {
	x := l.tmp()
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct, *ir.Union:
			l.pf(ind, "%s = %s(buf, %s)", x, l.g.fn("_s_fl_", f.Type.Name), off)
			return x
		case *ir.Enum:
			size := ir.TableFixedElementBytes(f)
			l.pf(ind, "%s = _s_su(%q, buf, %s)[0]", x, "<"+map[int64]string{1: "B", 2: "H", 4: "I", 8: "Q"}[size], off)
			l.pf(ind, "if %s > %d: %s", x, len(r.Variants), refuse("out_of_range", path))
			return x
		}
	}
	switch f.Type.Kind {
	case ir.TBool:
		l.pf(ind, "%s = buf[%s]", x, off)
		l.pf(ind, "if %s > 1: %s", x, refuse("wire_damaged", path))
		l.pf(ind, "%s = %s == 1", x, x)
		return x
	case ir.TFloat32, ir.TFloat64:
		f32 := f.Type.Kind == ir.TFloat32
		bits, mask, fl := "<Q", "0x7FF0000000000000", "<d"
		if f32 {
			bits, mask, fl = "<I", "0x7F800000", "<f"
		}
		l.pf(ind, "if _s_su(%q, buf, %s)[0] & %s == %s: %s", bits, off, mask, mask, refuse("non_finite_float", path))
		l.pf(ind, "%s = _s_su(%q, buf, %s)[0]", x, fl, off)
		if f.HasFloatRange {
			lo, hi := pyFloat(f.FMin), pyFloat(f.FMax)
			if f32 {
				lo, hi = pyFloat32(f.FMin), pyFloat32(f.FMax)
			}
			l.pf(ind, "if %s < %s or %s > %s: %s", x, lo, x, hi, refuse("out_of_range", path))
		}
		return x
	}
	lo, hi := rawBounds(f)
	if format, ok := intFormat(f); ok {
		l.pf(ind, "%s = _s_su(%q, buf, %s)[0]", x, "<"+format, off)
	} else {
		// a 128-bit value: the low 64-bit half, then the high
		l.pf(ind, "%s = _s_su(\"<Q\", buf, %s)[0] | (_s_su(\"<Q\", buf, %s + 8)[0] << 64)", x, off, off)
		if f.Type.Signed {
			l.pf(ind, "if %s >= 1 << 127: %s -= 1 << 128", x, x)
		}
	}
	switch {
	case lo != "None" && hi != "None":
		l.pf(ind, "if %s < %s or %s > %s: %s", x, lo, x, hi, refuse("out_of_range", path))
	case lo != "None":
		l.pf(ind, "if %s < %s: %s", x, lo, refuse("out_of_range", path))
	case hi != "None":
		l.pf(ind, "if %s > %s: %s", x, hi, refuse("out_of_range", path))
	}
	return x
}

// value reads a field's payload (the optional's present byte aside) at off
// and answers the expression holding it.
func (l *line) value(f *ir.Field, off, path, ind string, table bool) string {
	switch {
	case f.KeyEnum != "":
		xs := l.tmp()
		i := l.tmp()
		stride := ir.TableFixedElementBytes(f)
		l.pf(ind, "%s = []", xs)
		l.pf(ind, "for %s in range(%d):", i, f.ArrayBound)
		e := l.elem(f, fmt.Sprintf("%s + %s * %d", off, i, stride), path, ind+"    ")
		l.pf(ind+"    ", "%s.append(%s)", xs, e)
		if table {
			return "_s_fx.KeyedArray(" + xs + ")"
		}
		return xs
	case f.Array != ir.ArrayNone:
		xs := l.tmp()
		i := l.tmp()
		stride := ir.TableFixedElementBytes(f)
		count := fmt.Sprint(f.ArrayBound)
		base := off
		if f.Array == ir.ArrayCounted {
			n := l.tmp()
			l.pf(ind, "%s = _s_su(\"<i\", buf, %s)[0]", n, off)
			l.pf(ind, "if %s < 0 or %s > %d: %s", n, n, f.ArrayBound, refuse("count_over_bound", path))
			count, base = n, off+" + 4"
		}
		l.pf(ind, "%s = []", xs)
		l.pf(ind, "for %s in range(%s):", i, count)
		e := l.elem(f, fmt.Sprintf("%s + %s * %d", base, i, stride), path, ind+"    ")
		l.pf(ind+"    ", "%s.append(%s)", xs, e)
		return xs
	case f.Type.Kind == ir.TBytes:
		n := l.tmp()
		l.pf(ind, "%s = _s_su(\"<i\", buf, %s)[0]", n, off)
		l.pf(ind, "if %s < 0 or %s > %d: %s", n, n, f.Type.Size, refuse("count_over_bound", path))
		return fmt.Sprintf("bytes(buf[%s + 4 : %s + 4 + %s])", off, off, n)
	case f.Type.Kind == ir.TString:
		n, raw, x := l.tmp(), l.tmp(), l.tmp()
		l.pf(ind, "%s = _s_su(\"<i\", buf, %s)[0]", n, off)
		l.pf(ind, "if %s < 0 or %s > %d: %s", n, n, f.Type.Size, refuse("length_over_bound", path))
		l.pf(ind, "%s = buf[%s + 4 : %s + 4 + %s]", raw, off, off, n)
		l.pf(ind, "if b\"\\x00\" in %s: %s", raw, refuse("text_ill_formed", path))
		l.pf(ind, "try:")
		l.pf(ind+"    ", "%s = %s.decode(\"utf-8\")", x, raw)
		l.pf(ind, "except UnicodeDecodeError:")
		l.pf(ind+"    ", "%s from None", refuse("text_ill_formed", path))
		return x
	case f.Type.Kind == ir.TWString:
		n, x := l.tmp(), l.tmp()
		l.pf(ind, "%s = _s_su(\"<i\", buf, %s)[0]", n, off)
		l.pf(ind, "if %s < 0 or %s > %d: %s", n, n, f.Type.Size, refuse("length_over_bound", path))
		l.pf(ind, "try:")
		l.pf(ind+"    ", "%s = buf[%s + 4 : %s + 4 + 2 * %s].decode(\"utf-16-le\")", x, off, off, n)
		l.pf(ind, "except UnicodeDecodeError:")
		l.pf(ind+"    ", "%s from None", refuse("text_ill_formed", path))
		l.pf(ind, "if \"\\x00\" in %s: %s", x, refuse("text_ill_formed", path))
		return x
	}
	return l.elem(f, off, path, ind)
}

// emitIdentityReaders emits _s_fl_<Name> for every struct, table and union of
// this file that a fixed root reaches.
func (g *gen) emitIdentityReaders(closure map[string]bool) {
	var names []string
	for name := range closure {
		if g.u.DeclFile[name] == g.f.Base {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if st, ok := g.u.Tables[name]; ok {
			g.emitIdentityStruct(st, true)
			continue
		}
		if st, ok := g.u.Structs[name]; ok {
			g.emitIdentityStruct(st, false)
			continue
		}
		if un, ok := g.u.Unions[name]; ok {
			g.emitIdentityUnion(un)
		}
	}
}

func (g *gen) emitIdentityStruct(st *ir.Struct, table bool) {
	l := &line{g: g}
	at := int64(0)
	fields := st.Fields
	for i := 0; i < len(fields); {
		f := fields[i]
		if plainInt(f) {
			format, attrs, start := "<", []string{}, at
			for i < len(fields) && plainInt(fields[i]) {
				ff, _ := intFormat(fields[i])
				format += ff
				attrs = append(attrs, "v."+fields[i].Name)
				at += ir.TableFixedFieldBytes(fields[i])
				i++
			}
			comma := ""
			if len(attrs) == 1 {
				comma = ","
			}
			l.pf("    ", "%s%s = _s_su(%q, buf, off + %d)", strings.Join(attrs, ", "), comma, format, start)
			continue
		}
		off := fmt.Sprintf("off + %d", at)
		path := st.Name + "." + f.Name
		if f.Type.Optional {
			p := l.tmp()
			l.pf("    ", "%s = buf[%s]", p, off)
			l.pf("    ", "if %s > 1: %s", p, refuse("wire_damaged", path))
			l.pf("    ", "if %s == 1:", p)
			x := l.value(f, off+" + 1", path, "        ", table)
			l.pf("        ", "v.%s = %s", f.Name, x)
		} else {
			x := l.value(f, off, path, "    ", table)
			l.pf("    ", "v.%s = %s", f.Name, x)
		}
		at += ir.TableFixedFieldBytes(f)
		i++
	}
	g.pf("def _s_fl_%s(buf: bytes, off: int) -> %s:\n", st.Name, st.Name)
	g.pf("    \"\"\"%s at off, as this build's own fixed form lays it (the identity lane).\"\"\"\n", st.Name)
	g.pf("    v = %s()\n", st.Name)
	g.pf("%s", l.b.String())
	g.pf("    return v\n\n\n")
}

func (g *gen) emitIdentityUnion(un *ir.Union) {
	l := &line{g: g}
	tag := ir.TableFixedUnionTagBytes(un)
	l.pf("    ", "t = _s_su(%q, buf, off)[0]", "<"+map[int64]string{1: "B", 2: "H", 4: "I", 8: "Q"}[tag])
	l.pf("    ", "if t > %d: %s", len(un.Variants), refuse("out_of_range", un.Name))
	l.pf("    ", "v.type = t")
	for i, vr := range un.Variants {
		if vr.F == nil {
			continue
		}
		l.pf("    ", "if t == %d:", i+1)
		x := l.value(vr.F, fmt.Sprintf("off + %d", tag), un.Name+"."+vr.Name, "        ", false)
		l.pf("        ", "v.%s = %s", vr.Name, x)
	}
	g.pf("def _s_fl_%s(buf: bytes, off: int) -> %s:\n", un.Name, un.Name)
	g.pf("    \"\"\"%s at off, as this build's own fixed form lays it (the identity lane).\"\"\"\n", un.Name)
	g.pf("    v = %s()\n", un.Name)
	g.pf("%s", l.b.String())
	g.pf("    return v\n\n\n")
}
