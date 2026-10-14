package javatable

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookWriterFile spells §7.6 over the same ABI layouts as the read surface.
// Its first pass numbers the identity graph and validates every stored slot;
// the caller's buffer is touched only after that pass and capacity checks.
func cookValueType(u *ir.Unit, st *ir.Struct) (string, bool) {
	fixed := st.FixedDeclared || !ir.VariableTables(u)[st.Name]
	if !fixed {
		return st.Name + "Variable.Value", false
	}
	for _, n := range fixedNames(u) {
		if n == fixedClass(st.Name) {
			return fixedValue(st.Name), true
		}
	}
	return "TableCookWriter." + st.Name + "Value", true
}

// A carried list is writable even while the accelerator reader lacks list accessors.
func cookWriterUnit(u *ir.Unit) *cookUnit {
	c := &cookUnit{members: map[string]*ir.MemberLayout{}, skipped: map[string]string{}}
	for _, st := range u.Tables {
		seen := map[string]bool{}
		var supported func(*ir.Struct) bool
		supported = func(s *ir.Struct) bool {
			if seen[s.Name] {
				return true
			}
			seen[s.Name] = true
			for _, f := range s.Fields {
				if f.IsMap() {
					return false
				}
				if f.Type.Kind == ir.TNamed {
					switch ref := f.Type.Ref.(type) {
					case *ir.Union:
						return false
					case *ir.Struct:
						if !supported(ref) {
							return false
						}
					}
				}
			}
			return true
		}
		if !supported(st) {
			continue
		}
		c.tables = append(c.tables, st)
		cookWalk(u, st.Name, func(n string, ml *ir.MemberLayout) { c.members[n] = ml })
	}
	for n := range c.members {
		c.order = append(c.order, n)
	}
	sort.Strings(c.order)
	return c
}

func cookHasExtent(u *ir.Unit, st *ir.Struct) bool {
	for _, f := range st.Fields {
		if f.IsList() {
			return true
		}
		if !f.Type.Pointer && f.Type.Kind == ir.TNamed {
			if ref, ok := f.Type.Ref.(*ir.Struct); ok && cookHasExtent(u, ref) {
				return true
			}
		}
	}
	return false
}

func cookRecordSize(u *ir.Unit, st *ir.Struct) int64 {
	size := ir.RecordLayout(u, st).Size
	if cookHasExtent(u, st) {
		size = (size + 7) & -8
	}
	return size
}

func cookWriterFile(u *ir.Unit, c *cookUnit) []byte {
	var b strings.Builder
	b.WriteString(javaCookWriter)
	for _, name := range c.order {
		st := cookMember(u, name)
		typ, fixed := cookValueType(u, st)
		variants := []bool{false}
		if fixed {
			for _, v := range cardVariableTables(u) {
				if v.Name == name {
					variants = append(variants, true)
					break
				}
			}
		}
		for _, variableContext := range variants {
			method := name
			typ, fixed := typ, fixed
			if variableContext {
				typ = name + "Variable.Value"
				fixed = false
			}
			if !fixed {
				method = name + "Variable"
			}
			if strings.HasPrefix(typ, "TableCookWriter.") {
				g := &fixedGen{unit: u}
				g.emitValueClass(st)
				value := strings.ReplaceAll(g.b.String(), "class Value {", "class "+name+"Value {")
				value = strings.ReplaceAll(value, "public Value()", "public "+name+"Value()")
				b.WriteString(value)
			}
			fmt.Fprintf(&b, "    static boolean write%s(TableCookWriter w, Object object, int at, boolean live) {\n        if (!(object instanceof %s)) return false;\n        %s v = (%s) object;\n", method, typ, typ, typ)
			ml := c.members[name]
			for _, fl := range ml.Fields {
				f := fl.Field
				m := varMember(f)
				if fixed {
					m = javaName(f.Name)
				}
				expr := "v." + m
				pieces := ir.FieldPieces(u, f, fl.Offset)
				live := "live"
				if f.Guard != "" {
					guard := regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`).ReplaceAllStringFunc(f.Guard, func(n string) string { return "v." + javaName(n) })
					live += " && (" + guard + ")"
				}
				if f.Type.Optional {
					live += " && v." + m + "Present"
				}
				if f.Type.Optional {
					fmt.Fprintf(&b, "        w.put(at+%d, v.%sPresent ? 1 : 0, 1);\n", pieces[len(pieces)-1].Offset, m)
				}
				if f.IsList() {
					size, alignment := ir.ListElementLayout(u, f)
					fmt.Fprintf(&b, "        if (%s==null || (!(%s)&&!%s.isEmpty())) return false;\n        { int start=w.carve(%s.size(),%d,%d); if(start<0)return false; if(!%s.isEmpty()){w.put(at+%d,(long)start-(at+%d),8);w.put(at+%d,%s.size(),4);}\n        for(int i=0;i<%s.size();i++){\n", expr, live, expr, expr, size, alignment, expr, fl.Offset, fl.Offset, fl.Offset+8, expr, expr)
					one := *f
					one.Array = ir.ArrayNone
					one.ArrayBound = 0
					cookWriteElement(&b, u, c, &one, expr+".get(i)", "start+i*"+fmt.Sprint(size), size, false, live)
					b.WriteString("        }}\n")
					continue
				}
				array := f.Array != ir.ArrayNone || f.KeyEnum != ""
				if array {
					count := fmt.Sprint(f.ArrayBound)
					if !fixed {
						count = expr + ".size()"
					}
					fmt.Fprintf(&b, "        if (%s == null) return false;\n", expr)
					if f.Array == ir.ArrayCounted {
						used := expr + ".size()"
						if fixed {
							used = "v." + m + "Count"
						}
						ci := len(pieces) - 1
						if f.Type.Optional {
							ci--
						}
						fmt.Fprintf(&b, "        if (%s < 0 || %s > %d) return false; w.put(at+%d,%s,4);\n", used, used, f.ArrayBound, pieces[ci].Offset, used)
					}
					fmt.Fprintf(&b, "        if (%s > %d) return false;\n        for (int i=0;i<%s;i++) {\n", count, f.ArrayBound, count)
					el := expr + "[i]"
					if !fixed {
						el = expr + ".get(i)"
					}
					cookWriteElement(&b, u, c, f, el, fmt.Sprintf("at+%d+i*%d", fl.Offset, pieces[0].Size/f.ArrayBound), pieces[0].Size/f.ArrayBound, fixed, live+func() string {
						if f.Array == ir.ArrayCounted && fixed {
							return " && i<v." + m + "Count"
						}
						return ""
					}())
					b.WriteString("        }\n")
				} else {
					cookWriteElement(&b, u, c, f, expr, fmt.Sprintf("at+%d", fl.Offset), pieces[0].Size, fixed, live)
					if !f.Type.Pointer && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString) {
						length := expr + ".length"
						if f.Type.Kind == ir.TString && !fixed {
							length = "textLength(" + expr + ")"
						}
						if fixed {
							length = "v." + m + "Length"
						}
						fmt.Fprintf(&b, "        w.put(at+%d,%s,4);\n", pieces[1].Offset, length)
					}
				}
			}
			b.WriteString("        return true;\n    }\n")
		}
	}
	b.WriteString("}\n")
	return javaFile(u, "cook measurement and transactional write (docs/SPEC-TABLES.md §7.6).", b.String())
}

func cookWriteElement(b *strings.Builder, u *ir.Unit, c *cookUnit, f *ir.Field, e, at string, width int64, fixed bool, live string) {
	if f.Type.Pointer {
		fmt.Fprintf(b, "        if (!(%s) && %s != null) return false;\n", live, e)
		if f.Type.Blob() {
			id := "0x704be0d8faaffc58L"
			text := f.Type.Kind == ir.TString
			if !text {
				id = "0x2f2ec0474f1c4fe4L"
			}
			fmt.Fprintf(b, "        if (!w.blob(%s,%s,%s,%v)) return false;\n", e, at, id, text)
			return
		}
		name := f.Type.Name
		method := name
		if !fixed {
			method += "Variable"
		}
		ml := c.members[name]
		if ml == nil {
			b.WriteString("        return false;\n")
			return
		}
		fmt.Fprintf(b, "        if (!w.reference(%s,%s,%d,%d,0x%016xL,TableCookWriter::write%s)) return false;\n", e, at, cookRecordSize(u, cookMember(u, name)), ml.Align, ir.TableWireId(cookMember(u, name).WireName()), method)
		return
	}
	if f.Type.Kind == ir.TNamed {
		if st, ok := f.Type.Ref.(*ir.Struct); ok {
			method := st.Name
			if !fixed {
				method += "Variable"
			}
			fmt.Fprintf(b, "        if (!write%s(w,%s,%s,%s)) return false;\n", method, e, at, live)
			return
		}
	}
	switch f.Type.Kind {
	case ir.TString, ir.TBytes, ir.TWString:
		if f.Type.Kind == ir.TString && !fixed {
			clone := *f
			clone.Array = ir.ArrayNone
			clone.ArrayBound = 0
			parts := ir.FieldPieces(u, &clone, 0)
			fmt.Fprintf(b, "        if (%s==null || textLength(%s)<0 || textLength(%s)>%d) return false; w.text(%s,%s); putTextCount(w,%s+%d,%s);\n", e, e, e, f.Type.Size, e, at, at, parts[1].Offset, e)
			return
		}
		data := e
		if f.Type.Kind == ir.TString && !fixed {
			data = e + ".getBytes(java.nio.charset.StandardCharsets.UTF_8)"
		}
		length := data + ".length"
		if fixed {
			length = e + "Length"
		}
		fmt.Fprintf(b, "        if (%s == null || %s < 0 || %s > %d) return false;\n", e, length, length, f.Type.Size)
		unit := 1
		if f.Type.Kind == ir.TWString {
			unit = 2
		}
		fmt.Fprintf(b, "        for(int j=0;j<%s;j++) w.put(%s+j*%d,%s[j],%d);\n", length, at, unit, data, unit)
		clone := *f
		clone.Array = ir.ArrayNone
		clone.ArrayBound = 0
		parts := ir.FieldPieces(u, &clone, 0)
		fmt.Fprintf(b, "        w.put(%s+%d,%s,4);\n", at, parts[1].Offset, length)
	case ir.TBool:
		fmt.Fprintf(b, "        w.put(%s,%s ? 1 : 0,%d);\n", at, e, width)
	case ir.TFloat32:
		fmt.Fprintf(b, "        w.put(%s,Float.floatToRawIntBits(%s),%d);\n", at, e, width)
	case ir.TFloat64:
		fmt.Fprintf(b, "        w.put(%s,Double.doubleToRawLongBits(%s),%d);\n", at, e, width)
	default:
		if width > 8 {
			fmt.Fprintf(b, "        if (%s==null)return false; w.put(%s,w.order==BIG?%s.hi:%s.lo,8); w.put(%s+8,w.order==BIG?%s.lo:%s.hi,8);\n", e, at, e, e, at, e, e)
		} else {
			fmt.Fprintf(b, "        w.put(%s,%s,%d);\n", at, e, width)
		}
	}
}

const javaCookWriter = `public final class TableCookWriter {
    private TableCookWriter() {}
    public static final int LITTLE = 1, BIG = 2;
    public record MeasureResult(long size, String reason) {}
    interface Write { boolean run(TableCookWriter w, Object value, int at,boolean live); }
    private static final class Node {
        Object value; long id; int size, recordSize, align, offset; Write write;
        Node(Object value,int size,int align,long id,Write write) {this.value=value;this.size=size;this.recordSize=size;this.align=align;this.id=id;this.write=write;}
    }
    private final java.util.ArrayList<Node> nodes = new java.util.ArrayList<>();
    private final java.util.IdentityHashMap<Object,Node> seen = new java.util.IdentityHashMap<>();
    private final java.util.Set<Object> open = java.util.Collections.newSetFromMap(new java.util.IdentityHashMap<>());
    private byte[] output;
    private int order, alignment=8, dataBytes;
    private int tail;
    int total;
    private static long align(long n,long a) { return (n+a-1)&-a; }
    private boolean add(Object value,int size,int alignment,long id,Write write) {
        if(value==null) return false;
        Node old=seen.get(value);
        if(old!=null) return old.id==id&&!open.contains(value);
        Node n=new Node(value,size,alignment,id,write); seen.put(value,n); nodes.add(n);
        this.alignment=Math.max(this.alignment,alignment);
        int saved=tail;tail=size;open.add(value); boolean ok=write.run(this,value,0,true);open.remove(value);n.size=tail;tail=saved;return ok;
    }
    boolean reference(Object value,int slot,int size,int alignment,long id,Write write) {
        if(value==null) return true;
        if(output==null) return add(value,size,alignment,id,write);
        Node n=seen.get(value); if(n==null||n.id!=id)return false;
        put(slot,64L+n.offset-slot,8); return true;
    }
    boolean blob(Object value,int slot,long id,boolean text) {
        if(value==null)return true;
        int length=text?textLength((String)value):((byte[])value).length;
        if(length<0||length>Integer.MAX_VALUE-9)return false;
        if(output==null) return add(value,8+length+(text?1:0),8,id,(w,v,at,live)->true);
        Node n=seen.get(value); if(n==null)return false;
        put(slot,64L+n.offset-slot,8); put(64+n.offset,length,4);
        if(text) text((String)value,64+n.offset+8);
        else System.arraycopy((byte[])value,0,output,64+n.offset+8,length);
        return true;
    }
    static int textLength(String value) {
        if(value==null)return -1;
        long length=0;
        for(int i=0;i<value.length();i++) {
            char c=value.charAt(i);
            if(Character.isHighSurrogate(c)&&i+1<value.length()&&Character.isLowSurrogate(value.charAt(i+1))) {length+=4;i++;}
            else length+=c<128||Character.isSurrogate(c)?1:c<2048?2:3;
        }
        return length>Integer.MAX_VALUE?-1:(int)length;
    }
    void text(String value,int at) {
        if(output==null)return;
        int p=at;
        for(int i=0;i<value.length();i++) {
            int c=value.charAt(i);
            if(Character.isHighSurrogate((char)c)&&i+1<value.length()&&Character.isLowSurrogate(value.charAt(i+1))) {c=Character.toCodePoint((char)c,value.charAt(++i));}
            else if(Character.isSurrogate((char)c)) c='?';
            if(c<128)put(p++,c,1);
            else if(c<2048) {put(p++,0xc0|(c>>6),1);put(p++,0x80|(c&63),1);}
            else if(c<65536) {put(p++,0xe0|(c>>12),1);put(p++,0x80|((c>>6)&63),1);put(p++,0x80|(c&63),1);}
            else {put(p++,0xf0|(c>>18),1);put(p++,0x80|((c>>12)&63),1);put(p++,0x80|((c>>6)&63),1);put(p++,0x80|(c&63),1);}
        }
    }
    static void putTextCount(TableCookWriter w,int at,String value) {w.put(at,textLength(value),4);}
    int carve(int count,int width,int alignment) {
        if(count==0)return tail;
        long start=align(tail,alignment),end=start+(long)count*width;
        if(count<0||end>Integer.MAX_VALUE)return -1;
        tail=(int)end;return (int)start;
    }
    void put(int at,long value,int width) {
        if(output==null)return;
        for(int i=0;i<width;i++) output[at+i]=(byte)(value >>> (8*(order==BIG?width-1-i:i)));
    }
    static TableCookWriter plan(Object value,int size,int alignment,long id,Write write) {
        TableCookWriter w=new TableCookWriter();
        if(!w.add(value,size,alignment,id,write))return null;
        long end=0;
        for(Node n:w.nodes) {end=align(end,n.align);if(end>Integer.MAX_VALUE)return null;n.offset=(int)end;end+=n.size;}
        end=align(end,w.alignment);long total=64+end+16L*w.nodes.size();
        if(total>Integer.MAX_VALUE)return null;
        w.dataBytes=(int)end;w.total=(int)total;return w;
    }
    boolean finish(byte[] buffer,int order) {
        if(buffer==null||buffer.length<total||(order!=LITTLE&&order!=BIG))return false;
        this.order=order;output=buffer;java.util.Arrays.fill(buffer,0,total,(byte)0);
        put(0,0x4b4f4f434d484353L,8);put(8,BuildVersion.value,8);put(16,order,8);put(24,dataBytes,8);put(32,16L*nodes.size(),8);put(40,alignment,8);
        for(int i=0;i<nodes.size();i++) {Node n=nodes.get(i);tail=64+n.offset+n.recordSize;if(!n.write.run(this,n.value,64+n.offset,true))return false;put(64+dataBytes+i*16,n.offset,8);put(72+dataBytes+i*16,n.id,8);}
        return true;
    }
`
