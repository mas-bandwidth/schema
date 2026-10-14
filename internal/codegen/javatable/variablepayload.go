package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A present optional and an active union arm write their complete payload,
// including defaults. Presence/arm identity already prevents elision (§2.3, §2.6).
func writeVariablePayload(b *strings.Builder, u *ir.Unit, owner string, f *ir.Field, expr, w string, depth int) {
	if f.KeyEnum != "" {
		fmt.Fprintf(b, "        %sVariable.writeKeyedPayload_%s(%s,graph,%s);\n", owner, varMember(f), w, expr)
		return
	}
	kind := ir.TableWireFieldKind(f)
	if f.Array != ir.ArrayNone {
		aw := fmt.Sprintf("payload%d", depth)
		count := expr + ".size()"
		if f.Array == ir.ArrayFixed {
			count = fmt.Sprint(f.ArrayBound)
		}
		fmt.Fprintf(b, "        { TableVariable.Writer %s=%s.sub(new byte[1<<20]);%s.put8(%d);%s.putLeb(%s);\n", aw, w, aw, ir.TableWireElemKind(f), aw, count)
		one := *f
		one.Array = ir.ArrayNone
		one.ArrayBound = 0
		one.KeyEnum = ""
		one.Type.Optional = false
		fmt.Fprintf(b, "        for(int index%d=0;index%d<%s;index%d++){%s element%d=index%d<%s.size()?%s.get(index%d):%s;\n", depth, depth, count, depth, variableValueType(&one), depth, depth, expr, expr, depth, variableDefault(&one))
		writeVariablePayload(b, u, owner, &one, fmt.Sprintf("element%d", depth), aw, depth+1)
		fmt.Fprintf(b, "        } %s.putLeb(%s.off);%s.putBytes(%s.buf,0,%s.off); }\n", w, aw, w, aw, aw)
		return
	}
	if f.Type.Pointer {
		fmt.Fprintf(b, "        %s.putLeb(graph.index(%s));\n", w, expr)
		return
	}
	switch f.Type.Kind {
	case ir.TBool:
		fmt.Fprintf(b, "        %s.put8(%s?1:0);\n", w, expr)
	case ir.TString:
		fmt.Fprintf(b, "        {byte[] text%d=%s.getBytes(StandardCharsets.UTF_8);%s.putLeb(text%d.length);%s.putBytes(text%d,0,text%d.length);}\n", depth, expr, w, depth, w, depth, depth)
	case ir.TWString:
		fmt.Fprintf(b, "        %s.putLeb(%s.length*2L);for(char unit%d:%s){%s.put8(unit%d);%s.put8(unit%d>>>8);}\n", w, expr, depth, expr, w, depth, w, depth)
	case ir.TBytes:
		fmt.Fprintf(b, "        %s.putLeb(%s.length+1+TableVariable.lebLen(%s.length));%s.put8(6);%s.putLeb(%s.length);%s.putBytes(%s,0,%s.length);\n", w, expr, expr, w, w, expr, w, expr, expr)
	case ir.TFloat32:
		fmt.Fprintf(b, "        %s.put32(Float.floatToRawIntBits(%s));\n", w, expr)
	case ir.TFloat64:
		fmt.Fprintf(b, "        %s.put64(Double.doubleToRawLongBits(%s));\n", w, expr)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			sw := fmt.Sprintf("payload%d", depth)
			fmt.Fprintf(b, "        {TableVariable.Writer %s=%s.sub(new byte[1<<20]);%sVariable.writeFields(%s,graph,%s);%s.put8(0);%s.putLeb(%s.off);%s.putBytes(%s.buf,0,%s.off);}\n", sw, w, ref.Name, sw, expr, sw, w, sw, w, sw, sw)
		case *ir.Union:
			fmt.Fprintf(b, "        %sVariable.writePayload(%s,graph,%s);\n", ref.Name, w, expr)
		case *ir.Enum:
			fmt.Fprintf(b, "        switch(%s){\n", expr)
			for i := range ref.Variants {
				fmt.Fprintf(b, "        case %d:%s.putLeb(%s.ref(0x%016xL));break;\n", i+1, w, w, ir.TableWireId(ref.VariantWireName(i)))
			}
			fmt.Fprintf(b, "        default:%s.putLeb(0);break;}\n", w)
		case *ir.Flags:
			fmt.Fprintf(b, "        %s.put64(%s);\n", w, expr)
		}
	default:
		width := ir.TableKindWidth(kind)
		if width == 16 {
			fmt.Fprintf(b, "        %s.put64(%s.lo);%s.put64(%s.hi);\n", w, expr, w, expr)
		} else {
			fmt.Fprintf(b, "        %s.put%d(%s);\n", w, width*8, expr)
		}
	}
}
