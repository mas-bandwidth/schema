package javatable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
	"strings"
)

// messageBatchSurface keeps fixed-form generated types independent of the
// optional message vocabulary. Java's unit-scoped verbs live on TableVocabulary.
func messageBatchSurface(u *ir.Unit) string {
	roots := fixedRoots(u)
	seen := map[string]bool{}
	var order []*ir.Struct
	for _, st := range roots {
		fixedCollectTypes(st, seen, &order)
	}
	var out strings.Builder
	descriptor := func(name, source string) {
		source = strings.ReplaceAll(source, "public static final TableVocabulary.MessageType messageType", "static final TableVocabulary.MessageType type")
		for _, st := range order {
			source = strings.ReplaceAll(source, fixedClass(st.Name)+".messageType", javaName(st.Name)+"Message.type")
		}
		for _, un := range fixedUnions(order) {
			source = strings.ReplaceAll(source, fixedClass(un.Name)+".messageType", javaName(un.Name)+"Message.type")
		}
		fmt.Fprintf(&out, "    private static final class %sMessage {\n%s    }\n", javaName(name), source)
	}
	for _, un := range fixedUnions(order) {
		tag := int64(ir.StorageBitsFor(un.Max) / 8)
		var widest int64
		for _, v := range un.Variants {
			if v.F != nil && fixedFieldBytes(v.F) > widest {
				widest = fixedFieldBytes(v.F)
			}
		}
		g := &fixedGen{unit: u}
		g.emitMessageUnion(un, tag, tag+widest)
		descriptor(un.Name, g.b.String())
	}
	for _, st := range order {
		g := &fixedGen{unit: u}
		g.emitMessageBatch(st, false)
		descriptor(st.Name, g.b.String())
	}
	for _, st := range roots {
		g := &fixedGen{unit: u}
		g.emitMessageBatch(st, true)
		source := g.b.String()
		source = source[strings.Index(source, "    /** SPEC-TABLES §3.3: M above"):]
		cls, holder := fixedClass(st.Name), javaName(st.Name)+"Message"
		source = strings.NewReplacer("Value[]", cls+".Value[]", "writeBody(", cls+".writeBody(", "scatter(", cls+".scatter(", "bodyBytes", cls+".bodyBytes", "messageDefaults", holder+".messageDefaults", "messageType", holder+".type", "measureMessages(", javaName(st.Name)+"MeasureMessages(", "saveMessages(", javaName(st.Name)+"SaveMessages(", "loadMessages(", javaName(st.Name)+"LoadMessages(").Replace(source)
		out.WriteString(source)
	}
	return out.String()
}

// emitMessageBatch adds the fixed root's plural batch verbs (SPEC-TABLES §3.3).
// The caller supplies the record image, as with the fixed reader, so load never
// allocates a body or a buffer. The vocabulary's static descriptors are code.
func (g *fixedGen) emitMessageBatch(st *ir.Struct, root bool) {
	g.emitByteConstant("messageDefaults", fixedDefaultImage(st), nil)
	g.pf("    public static final TableVocabulary.MessageType messageType = new TableVocabulary.MessageType(messageDefaults, new TableVocabulary.MessageField[] {\n")
	var off int64
	for _, f := range st.Fields {
		e := ir.TableFieldEntry(f)
		payload := off
		if f.Type.Optional {
			payload++
		}
		slots, _ := g.fixedSlots(f)
		count := int64(0)
		stride := fixedElementBytes(f)
		max := slots
		if f.Array == ir.ArrayCounted {
			count = 4
			payload += 4
		}
		if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
			count = 4
			max = int64(f.Type.Size)
			stride = 1
			payload += 4
		}
		child := "null"
		enums := "null"
		if f.Type.Kind == ir.TNamed {
			switch v := f.Type.Ref.(type) {
			case *ir.Struct:
				child = fixedClass(v.Name) + ".messageType"
			case *ir.Union:
				child = fixedClass(v.Name) + ".messageType"
			case *ir.Enum:
				var ids []string
				// Names ride their vocabulary reference, not their storage ordinal (§3.3).
				for i := range v.Variants {
					ids = append(ids, fmt.Sprintf("0x%016xL", ir.TableWireId(v.VariantWireName(i))))
				}
				enums = "new long[] {0L," + strings.Join(ids, ",") + "}"
			}
		}
		optional := int64(-1)
		if f.Type.Optional {
			optional = off
		}
		g.pf("        new TableVocabulary.MessageField(0x%016xL,%d,%d,%d,%d,%d,%d,%d,%d,%d,%s,%s),\n", e.Id, ir.TableVocabularySlots(g.unit)[e.Key()], e.Kind, e.Shape.Elem, off, payload, optional, count, max, stride, child, enums)
		off += fixedFieldBytes(f)
	}
	g.pf("    });\n")
	if !root {
		return
	}
	g.pf(`    /** SPEC-TABLES §3.3: M above 256 is batch_too_large at measure time. */
    public static int measureMessages(Value[] values, int count, byte[] image, TableVocabulary.Report report) {
        if (!TableVocabulary.batchWriteOpen(values == null ? 0 : values.length, count, null, report)) return -1;
        for (int k=0;k<count;k++) { writeBody(image,0,values[k]); if (!TableVocabulary.messageWrite(messageType,image,0,report)) return -1; }
        TableVocabulary.alignWrite(report); return report.error ? -1 : (int)(report.bit/8);
    }
    /** SPEC-TABLES §3.3: one array of one root writes one batch. */
    public static int saveMessages(Value[] values, int count, byte[] buffer, byte[] image, TableVocabulary.Report report) {
        if (buffer == null || !TableVocabulary.batchWriteOpen(values == null ? 0 : values.length,count,buffer,report)) return -1;
        for(int k=0;k<count;k++) {writeBody(image,0,values[k]);if(!TableVocabulary.messageWrite(messageType,image,0,report))return -1;}
        TableVocabulary.alignWrite(report); return report.error ? -1 : (int)(report.bit/8);
    }
    /** SPEC-TABLES §3.3: capacity refused before decoding; body k damage returns k-1. */
    public static int loadMessages(Value[] values, int capacity, byte[] data, TableVocabulary vocabulary, byte[] image, TableVocabulary.Report report) {
        int count=TableVocabulary.batchReadOpen(values == null ? 0 : values.length,capacity,data,vocabulary,report);
        if(count<0)return report.count;
        for(int k=0;k<count;k++) {
            System.arraycopy(messageDefaults,0,image,0,bodyBytes);
            boolean ok=TableVocabulary.messageRead(messageType,image,0,vocabulary,report,0);
            scatter(image,0,values[k],report.fixed);
            if(!ok){report.malformed=true;report.count=k;return k;}
        }
        report.count=count;
        if(!TableVocabulary.alignRead(report)||report.bit != (long)data.length*8)report.malformed=true;
        return count;
    }
`)
}

// tableMessageBatchRuntime implements the batch framing and the bitpacked
// body walk, SPEC-TABLES §3.3. Descriptors and defaults are generated constants;
// state and image storage belong to the caller, so no read allocates storage.
const tableMessageBatchRuntime = `
    public static final class MessageType {
        final byte[] defaults; final MessageField[] fields; final int tagBytes;
        MessageType(byte[] d, MessageField[] f) { this(d,f,0); }
        MessageType(byte[] d, MessageField[] f,int tag) { defaults=d;fields=f;tagBytes=tag; }
    }
    public static final class MessageField {
        final long id; final int slot,kind,element,offset,payload,present,count,max,stride;
        final MessageType child; final long[] names;
        MessageField(long i,int sl,int k,int el,int o,int p,int pr,int c,int m,int s,MessageType ch,long[] ns) {
            id=i;slot=sl;kind=k;element=el;offset=o;payload=p;present=pr;count=c;max=m;stride=s;child=ch;names=ns;
        }
    }
    private static TableVocabulary own() {
        TableVocabulary v=new TableVocabulary();v.reset(4096);
        announceRead(v,tableAnnouncement,new Report());
        return v;
    }
    static final class Own { static final TableVocabulary vocabulary=own(); }
    static boolean refuseBatch(Report r,String reason) { r.refused=true;r.reason=reason;return false; }
    public static boolean batchWriteOpen(int capacity,int count,byte[] data,Report r) {
        r.bit=16;r.buffer=data;r.error=false;r.count=0;
        if(count<1||count>256)return refuseBatch(r,"batch_too_large");
        if(count>capacity||data!=null&&data.length<2){r.error=true;return false;}
        if(data!=null){data[0]=2;data[1]=(byte)(count-1);}return true;
    }
    public static int batchReadOpen(int size,int capacity,byte[] data,TableVocabulary v,Report r) {
        r.bit=16;r.buffer=data;r.error=false;r.count=0;
        if(data==null||data.length==0){r.malformed=true;return -1;}
        if((data[0]&255)!=2){refuseBatch(r,"newer_form");return -1;}
        if(v==null||!v.announced||v.refused){refuseBatch(r,"no_vocabulary");return -1;}
        if(data.length<2){r.malformed=true;return -1;}
        int n=(data[1]&255)+1;
        if(n>capacity||n>size){r.count=n;refuseBatch(r,"batch_too_large");return -1;}
        return n;
    }
    static Entry entry(TableVocabulary v,long ref) {return ref>0&&ref<=v.count?v.entries[(int)ref-1]:null;}
    static int slot(TableVocabulary v,long id,int kind) {
        for(int i=0;i<v.count;i++)if(v.entries[i].id==id&&v.entries[i].kind==kind)return i+1;
        return 0;
    }
    static void put(Report r,long value,int bits) {
        if(bits<0||bits>64){r.error=true;return;}
        if(r.buffer==null){r.bit+=bits;return;}
        for(int i=0;i<bits;i++) {
            long p=r.bit++;
            if(r.buffer!=null) {
                if(p>=((long)r.buffer.length)*8){r.error=true;return;}
                int at=(int)(p>>3),mask=1<<(int)(p&7);
                r.buffer[at]=(byte)((r.buffer[at]&~mask)|(((value>>>i)&1)==0?0:mask));
            }
        }
    }
    static long get(Report r,int bits) {
        if(bits<0||bits>64||bits>(long)r.buffer.length*8-r.bit){r.error=true;return 0;}
        long v=0;for(int i=0;i<bits;i++){v|=(long)((r.buffer[(int)(r.bit>>3)] >>> (int)(r.bit&7))&1)<<i;r.bit++;}return v;
    }
    public static void alignWrite(Report r){put(r,0,(int)((-r.bit)&7));}
    public static boolean alignRead(Report r){return get(r,(int)((-r.bit)&7))==0&&!r.error;}
    static int countBits(Shape s){long n=s.max-s.min;return n==0?0:64-Long.numberOfLeadingZeros(n);}
    static long raw(byte[] image,int at,int n){long v=0;for(int i=0;i<n&&i<8;i++)v|=(long)(image[at+i]&255)<<(8*i);return v;}
    static void store(byte[] image,int at,int n,long v){for(int i=0;i<n&&i<8;i++)image[at+i]=(byte)(v>>>(8*i));}
    static int width(int k) {
        switch(k){case 1:case 2:case 6:return 1;case 3:case 7:return 2;case 4:case 8:case 10:return 4;case 5:case 9:case 11:return 8;default:return 0;}
    }
    static long scalarRead(Report r,Shape s) {
        int bits=s.kind==1?1:s.packing!=0?(int)s.bits:width(s.kind)*8;
        if(bits==0&&s.packing!=1){r.error=true;return 0;}
        long raw=get(r,bits);
        if(s.packing==2){float ratio=(float)Math.min(raw,s.steps)/(float)s.steps;return Float.floatToRawIntBits(ratio*s.qDelta+s.qMin)&0xffffffffL;}
        return raw+(s.packing==1?s.baseLo:0);
    }
    static void scalarWrite(Report r,Shape s,long value) {
        int bits=s.kind==1?1:s.packing!=0?(int)s.bits:width(s.kind)*8;
        if(bits==0&&s.packing!=1){r.error=true;return;}
        if(s.packing==2){float ratio=(Float.intBitsToFloat((int)value)-s.qMin)/s.qDelta;if(!(ratio>=0))ratio=0;else if(!(ratio<=1))ratio=1;float scaled=ratio*(float)s.steps;value=Math.min((long)Math.floor((double)(scaled+0.5f)),s.steps);}
        put(r,value-(s.packing==1?s.baseLo:0),bits);
    }
    static boolean changed(MessageType t,byte[] image,int base,int i) {
        MessageField f=t.fields[i];int end=i+1<t.fields.length?t.fields[i+1].offset:t.defaults.length;
        if(f.present>=0)return image[base+f.present]!=0;
        if(f.kind==14&&f.child!=null&&f.count==0&&f.max>0)return true;
        if(f.kind==13&&f.child!=null){for(int j=0;j<f.child.fields.length;j++)if(changed(f.child,image,base+f.payload,j))return true;return false;}
        for(int p=f.offset;p<end;p++)if(image[base+p]!=t.defaults[p])return true;
        return false;
    }
    static boolean messageWrite(MessageType t,byte[] image,int base,Report r) {
        if(t.tagBytes!=0) {
            int tag=(int)raw(image,base,t.tagBytes);
            if(tag==0){put(r,0,Own.vocabulary.refBits);return !r.error;}
            if(tag<1||tag>t.fields.length){r.error=true;return false;}
            MessageField f=t.fields[tag-1];int ref=f.slot;Entry e=entry(Own.vocabulary,ref);
            if(e==null){r.error=true;return false;}put(r,ref,Own.vocabulary.refBits);
            return writeValue(f,e.shape,e.element,image,base+f.payload,r);
        }
        for(int i=0;i<t.fields.length;i++) {
            MessageField f=t.fields[i];if(!changed(t,image,base,i))continue;
            int ref=f.slot;Entry e=entry(Own.vocabulary,ref);
            if(e==null){r.error=true;return false;}
            put(r,ref,Own.vocabulary.refBits);
            if(!writeValue(f,e.shape,e.element,image,base+f.payload,r))return false;
        }
        put(r,0,Own.vocabulary.refBits);return !r.error;
    }
    static boolean writeValue(MessageField f,Shape shape,Shape elem,byte[] image,int at,Report r) {
        if(shape.kind==12||shape.kind==14) {
            int n=f.count!=0?(int)raw(image,at-4,4):f.max;
            if(n<shape.min||n>shape.max||n>f.max){r.error=true;return false;}
            put(r,n-shape.min,countBits(shape));
            if(shape.kind==12||elem!=null&&elem.kind==6)alignWrite(r);
            for(int j=0;j<n;j++) {
                if(shape.kind==12)put(r,image[at+j]&255,8);
                else if(!writeElement(f,elem,image,at+j*f.stride,r))return false;
            }
            return !r.error;
        }
        return writeElement(f,shape,image,at,r);
    }
    static boolean writeElement(MessageField f,Shape s,byte[] image,int at,Report r) {
        if(s.kind==32)return true;
        if((s.kind==13||s.kind==15)&&f.child!=null)return messageWrite(f.child,image,at,r);
        if(s.kind==30&&f.names!=null) {
            long n=raw(image,at,f.stride);if(n<0||n>=f.names.length){r.error=true;return false;}
            int ref=n==0?0:slot(Own.vocabulary,f.names[(int)n],0);if(n!=0&&ref==0){r.error=true;return false;}
            put(r,ref,Own.vocabulary.refBits);return !r.error;
        }
        if(width(s.kind)==0){r.error=true;return false;}
        long value=raw(image,at,f.stride);
        if(s.kind>=2&&s.kind<=5&&f.stride<8)value=(value<<(64-f.stride*8))>>(64-f.stride*8);
        scalarWrite(r,s,value);return !r.error;
    }
    static boolean messageRead(MessageType t,byte[] image,int base,TableVocabulary v,Report r,int depth) {
        if(depth>64){r.error=true;return false;}
        if(t.tagBytes!=0) {
            long ref=get(r,v.refBits);if(r.error)return false;
            if(ref==0){store(image,base,t.tagBytes,0);return true;}
            Entry e=entry(v,ref);if(e==null){r.error=true;return false;}
            for(int i=0;i<t.fields.length;i++){MessageField f=t.fields[i];if(f.id==e.id&&f.kind==e.kind){store(image,base,t.tagBytes,i+1);return readValue(f,e.shape,e.element,image,base+f.payload,v,r,depth+1);}}
            r.unknown++;return skip(e.shape,e.element,v,r,depth+1);
        }
        while(!r.error) {
            long ref=get(r,v.refBits);if(r.error)return false;if(ref==0)return true;
            Entry e=entry(v,ref);if(e==null||Long.compareUnsigned(e.id,-3L)>=0){r.error=true;return false;}
            MessageField f=null;for(MessageField field:t.fields)if(field.id==e.id){f=field;break;}
            if(f==null){r.unknown++;if(!skip(e.shape,e.element,v,r,depth+1))return false;continue;}
            if(f.kind!=e.kind||f.element!=(e.element==null?0:e.element.kind)){r.fixed.kindMismatch++;if(!skip(e.shape,e.element,v,r,depth+1))return false;continue;}
            if(!readValue(f,e.shape,e.element,image,base+f.payload,v,r,depth+1))return false;
            if(f.present>=0)image[base+f.present]=1;
        }
        return false;
    }
    static boolean readValue(MessageField f,Shape s,Shape elem,byte[] image,int at,TableVocabulary v,Report r,int depth) {
        if(s.kind==12||s.kind==14) {
            long n=get(r,countBits(s))+s.min;
            if(r.error||n<s.min||n>s.max){r.error=true;return false;}
            if(s.kind==12||elem!=null&&elem.kind==6)if(!alignRead(r)){r.error=true;return false;}
            if(s.kind==12&&!utf8(r.buffer,(int)(r.bit/8),(int)n)){r.error=true;return false;}
            int keep=(int)Math.min(n,f.max);
            if(f.count!=0)store(image,at-4,4,keep);
            for(long j=0;j<n;j++) {
                if(s.kind==12){long b=get(r,8);if(j<keep)image[at+(int)j]=(byte)b;}
                else if(j<keep){if(!readElement(f,elem,image,at+(int)j*f.stride,v,r,depth))return false;}
                else if(!skip(elem,null,v,r,depth+1))return false;
                if(r.error)return false;
            }
            return true;
        }
        return readElement(f,s,image,at,v,r,depth);
    }
    static boolean readElement(MessageField f,Shape s,byte[] image,int at,TableVocabulary v,Report r,int depth) {
        if(s.kind==32)return true;
        if((s.kind==13||s.kind==15)&&f.child!=null){System.arraycopy(f.child.defaults,0,image,at,f.child.defaults.length);return messageRead(f.child,image,at,v,r,depth);}
        if(s.kind==30&&f.names!=null) {
            long ref=get(r,v.refBits);if(r.error)return false;long value=0;
            if(ref!=0){Entry name=entry(v,ref);if(name==null||name.kind!=0){r.error=true;return false;}
                boolean found=false;for(int i=1;i<f.names.length;i++)if(f.names[i]==name.id){value=i;found=true;break;}if(!found)r.unknown++;
            }
            store(image,at,f.stride,value);return !r.error;
        }
        long value=scalarRead(r,s);if(r.error)return false;store(image,at,f.stride,value);return true;
    }
    // The text rule is unchanged: NUL and invalid UTF-8 are framing damage (§3.3).
    static boolean utf8(byte[] b,int at,int n) {
        if(n<0||at<0||n>b.length-at)return false;
        int end=at+n;
        while(at<end){int a=b[at++]&255;if(a==0)return false;if(a<128)continue;
            int more=a>=194&&a<=223?1:a>=224&&a<=239?2:a>=240&&a<=244?3:-1;
            if(more<0||more>end-at)return false;
            int first=b[at]&255;
            if(a==224&&first<160||a==237&&first>=160||a==240&&first<144||a==244&&first>=144)return false;
            for(int i=0;i<more;i++)if((b[at++]&192)!=128)return false;
        }
        return true;
    }
    // A fixed-width skipped array is one bounded bit advance, never a count loop (§3.3).
    static boolean skipBits(Report r,long bits){if(bits<0||bits>(long)r.buffer.length*8-r.bit){r.error=true;return false;}r.bit+=bits;return true;}
    static boolean skip(Shape s,Shape elem,TableVocabulary v,Report r,int depth) {
        if(s==null||depth>64){r.error=true;return false;}
        if(s.kind==0||s.kind==32)return true;
        if(s.kind==13) {
            while(true){long ref=get(r,v.refBits);if(r.error)return false;if(ref==0)return true;Entry e=entry(v,ref);if(e==null)return false;if(!skip(e.shape,e.element,v,r,depth+1))return false;}
        }
        if(s.kind==15){long ref=get(r,v.refBits);if(r.error)return false;if(ref==0)return true;Entry e=entry(v,ref);return e!=null&&skip(e.shape,e.element,v,r,depth+1);}
        if(s.kind==30){long ref=get(r,v.refBits);Entry name=entry(v,ref);return !r.error&&(ref==0||name!=null&&name.kind==0);}
        if(s.kind==12||s.kind==14){long n=get(r,countBits(s))+s.min;if(r.error||n>s.max)return false;
            if(s.kind==12||elem!=null&&elem.kind==6)if(!alignRead(r))return false;
            if(s.kind==12){if(!utf8(r.buffer,(int)(r.bit/8),(int)n))return false;return skipBits(r,n*8);}
            if(elem==null)return false;
            if(elem.kind==0||elem.kind==32)return true;
            if(width(elem.kind)>0){int bits=elem.kind==1?1:elem.packing!=0?(int)elem.bits:width(elem.kind)*8;return skipBits(r,n*bits);}
            for(long i=0;i<n;i++){if(!skip(elem,null,v,r,depth+1))return false;if(r.error)return false;}return true;}
        scalarRead(r,s);return !r.error;
    }
`

// emitMessageUnion emits the chosen arm's reference and payload, §3.3's union
// framing, using the fixed value's tag and pre-allocated arm storage.
func (g *fixedGen) emitMessageUnion(un *ir.Union, tag, body int64) {
	g.pf("    public static final TableVocabulary.MessageType messageType = new TableVocabulary.MessageType(new byte[%d],new TableVocabulary.MessageField[] {\n", body)
	for _, v := range un.Variants {
		e := ir.TableArmEntry(v)
		child := "null"
		enums := "null"
		stride := int64(0)
		count := int64(0)
		max := int64(0)
		payload := tag
		if f := v.F; f != nil {
			stride = fixedElementBytes(f)
			max = f.ArrayBound
			if f.Array == ir.ArrayCounted {
				count = 4
				payload += 4
			}
			if f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString {
				count = 4
				payload += 4
				max = int64(f.Type.Size)
				stride = 1
			}
			switch target := f.Type.Ref.(type) {
			case *ir.Struct:
				child = fixedClass(target.Name) + ".messageType"
			case *ir.Union:
				child = fixedClass(target.Name) + ".messageType"
			case *ir.Enum:
				var ids []string
				for i := range target.Variants {
					ids = append(ids, fmt.Sprintf("0x%016xL", ir.TableWireId(target.VariantWireName(i))))
				}
				enums = "new long[] {0L," + strings.Join(ids, ",") + "}"
			}
		}
		g.pf("        new TableVocabulary.MessageField(0x%016xL,%d,%d,%d,0,%d,-1,%d,%d,%d,%s,%s),\n", e.Id, ir.TableVocabularySlots(g.unit)[e.Key()], e.Kind, e.Shape.Elem, payload, count, max, stride, child, enums)
	}
	g.pf("    },%d);\n", tag)
}
