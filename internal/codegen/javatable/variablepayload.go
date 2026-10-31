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
		fmt.Fprintf(b, "        { int %s=%s.off;%s.put8(%d);%s.putLeb(%s);\n", aw, w, w, ir.TableWireElemKind(f), w, count)
		one := *f
		one.Array = ir.ArrayNone
		one.ArrayBound = 0
		one.KeyEnum = ""
		one.Type.Optional = false
		fmt.Fprintf(b, "        for(int index%d=0;index%d<%s;index%d++){%s element%d=index%d<%s.size()?%s.get(index%d):%s;\n", depth, depth, count, depth, variableValueType(&one), depth, depth, expr, expr, depth, variableDefault(&one))
		writeVariablePayload(b, u, owner, &one, fmt.Sprintf("element%d", depth), w, depth+1)
		fmt.Fprintf(b, "        } %s.frame(%s); }\n", w, aw)
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
		fmt.Fprintf(b, "        %s.putUtf8(%s);\n", w, expr)
	case ir.TWString:
		fmt.Fprintf(b, "        %s.putLeb(%s.length*2L);for(int i%d=0;i%d<%s.length;i%d++){char unit%d=%s[i%d];%s.put8(unit%d);%s.put8(unit%d>>>8);}\n", w, expr, depth, depth, expr, depth, depth, expr, depth, w, depth, w, depth)
	case ir.TBytes:
		fmt.Fprintf(b, "        %s.putLeb(%s.length+1+TableVariable.lebLen(%s.length));%s.put8(6);%s.putLeb(%s.length);%s.putBytes(%s,0,%s.length);\n", w, expr, expr, w, w, expr, w, expr, expr)
	case ir.TFloat32, ir.TFloat64:
		fmt.Fprintf(b, "        %s\n", variableFloatPut(f, w, expr))
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			sw := fmt.Sprintf("payload%d", depth)
			fmt.Fprintf(b, "        {int %s=%s.off;%sVariable.writeFields(%s,graph,%s);%s.put8(0);%s.frame(%s);}\n", sw, w, ref.Name, w, expr, w, w, sw)
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

// The retaining walk uses the same framing, with each child body at the path
// selected by its holder. Scalars have no tail and share the ordinary codec.
func writeVariableRetainedPayload(b *strings.Builder, u *ir.Unit, owner string, f *ir.Field, expr, w string, serial int, bodyDepth string) {
	if f.KeyEnum != "" {
		fmt.Fprintf(b, "%sVariable.writeKeyedPayloadRetain_%s(%s,graph,%s,retain,report,path,%s);\n", owner, varMember(f), w, expr, bodyDepth)
		return
	}
	if f.Array != ir.ArrayNone {
		aw := fmt.Sprintf("retainedPayload%d", serial)
		ix := fmt.Sprintf("retainedIndex%d", serial)
		el := fmt.Sprintf("retainedElement%d", serial)
		count := expr + ".size()"
		if f.Array == ir.ArrayFixed {
			count = fmt.Sprint(f.ArrayBound)
		}
		one := variableElement(f)
		fmt.Fprintf(b, "{TableVariable.Writer %s=%s.sub(new byte[1<<20]);%s.put8(%d);%s.putLeb(%s);for(int %s=0;%s<%s;%s++){%s %s=%s<%s.size()?%s.get(%s):%s;path[2*((%s)-1)+1]=%s;\n", aw, w, aw, ir.TableWireElemKind(f), aw, count, ix, ix, count, ix, variableValueType(one), el, ix, expr, expr, ix, variableDefault(one), bodyDepth, ix)
		writeVariableRetainedPayload(b, u, owner, one, el, aw, serial+1, bodyDepth)
		fmt.Fprintf(b, "}%s.putLeb(%s.off);%s.putBytes(%s.buf,0,%s.off); }\n", w, aw, w, aw, aw)
		return
	}
	if !f.Type.Pointer && isStruct(f) {
		sw := fmt.Sprintf("retainedBody%d", serial)
		fmt.Fprintf(b, "{TableVariable.Writer %s=%s.sub(new byte[1<<20]);%sRetain.writeBodyRetain(%s,graph,%s,retain,report,path,%s);%s.putLeb(%s.off);%s.putBytes(%s.buf,0,%s.off); }\n", sw, w, f.Type.Name, sw, expr, bodyDepth, w, sw, w, sw, sw)
		return
	}
	if un := variableUnion(f); un != nil {
		fmt.Fprintf(b, "%sVariable.writePayloadRetain(%s,graph,%s,retain,report,path,%s);\n", un.Name, w, expr, bodyDepth)
		return
	}
	writeVariablePayload(b, u, owner, f, expr, w, serial)
}

// variableFloatPut is a float's write on the variable form: its RAW bits, -0
// and the subnormals exact, through the writer's finite test, so a NaN or an
// infinity refuses the save (docs/SPEC-TABLES.md §3.3).
func variableFloatPut(f *ir.Field, w, expr string) string {
	if f.Type.Kind == ir.TFloat32 {
		return w + ".putF32(" + expr + ");"
	}
	return w + ".putF64(" + expr + ");"
}
