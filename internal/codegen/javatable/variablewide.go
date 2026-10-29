package javatable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func variableWide(f *ir.Field) bool {
	return f.Type.Kind == ir.TFixed || f.Type.Kind == ir.TInt && f.Type.Width > 64
}
func variableWideType(f *ir.Field) string {
	if f.Type.Width <= 32 {
		return "int"
	}
	if f.Type.Width <= 64 {
		return "long"
	}
	if f.Type.Signed {
		return "Int128"
	}
	return "UInt128"
}
func variableWideDefault(f *ir.Field) string {
	raw := new(big.Int)
	if f.HasDefault && f.DefInt != nil {
		raw.Set(f.DefInt)
	}
	if f.Type.Width <= 32 {
		return fmt.Sprintf("0x%08x", new(big.Int).And(raw, new(big.Int).SetUint64(0xffffffff)))
	}
	bits := new(big.Int).And(raw, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1)))
	if f.Type.Width <= 64 {
		return fmt.Sprintf("0x%016xL", bits.Uint64())
	}
	hi := new(big.Int).Rsh(new(big.Int).Set(bits), 64).Uint64()
	return fmt.Sprintf("new %s(0x%016xL,0x%016xL)", variableWideType(f), hi, bits.Uint64())
}
func variableRawExpr(f *ir.Field, expr string) string {
	if f.Type.Width > 64 {
		return fmt.Sprintf("TableVariable.wide(%s.hi,%s.lo,%t)", expr, expr, f.Type.Signed)
	}
	if f.Type.Signed {
		return "java.math.BigInteger.valueOf(" + expr + ")"
	}
	if f.Type.Width <= 32 {
		return "java.math.BigInteger.valueOf(Integer.toUnsignedLong(" + expr + "))"
	}
	return "new java.math.BigInteger(Long.toUnsignedString(" + expr + "))"
}
func variableRawStorage(f *ir.Field, raw string) string {
	if f.Type.Width <= 32 {
		return raw + ".intValue()"
	}
	if f.Type.Width <= 64 {
		return raw + ".longValue()"
	}
	return fmt.Sprintf("new %s(%s.shiftRight(64).longValue(),%s.longValue())", variableWideType(f), raw, raw)
}
func variableRawBounds(f *ir.Field) (string, string) {
	lo, hi, ok := ir.TableRawRange(f)
	if !ok {
		lo = new(big.Int)
		hi = new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width))
		if f.Type.Signed {
			hi.Rsh(hi, 1)
			lo.Neg(new(big.Int).Set(hi))
		}
		hi.Sub(hi, big.NewInt(1))
	}
	return fmt.Sprintf("new java.math.BigInteger(%q)", lo.String()), fmt.Sprintf("new java.math.BigInteger(%q)", hi.String())
}
func variableCompatibleKinds(kind int, expr string) string {
	var cond strings.Builder
	fmt.Fprintf(&cond, "%s==%d", expr, kind)
	for k := 1; k <= 33; k++ {
		if ir.TableKindWidens(k, kind) {
			fmt.Fprintf(&cond, "||%s==%d", expr, k)
		}
	}
	return cond.String()
}
func writeVariableWideRead(b *strings.Builder, f *ir.Field) {
	m := varMember(f)
	kind := ir.TableWireFieldKind(f)
	lo, hi := variableRawBounds(f)
	fmt.Fprintf(b, "                if(%s){java.math.BigInteger raw=TableVariable.readInteger(r,kind);if(raw!=null){raw=TableVariable.clampInteger(raw,%s,%s,report);v.%s=%s;if(kind!=%d)report.widened++;}}else{r.skip(kind);report.kindMismatch++;}\n", variableCompatibleKinds(kind, "kind"), lo, hi, m, variableRawStorage(f, "raw"), kind)
}
func writeVariableWideArrayRead(b *strings.Builder, f *ir.Field) {
	m := varMember(f)
	kind := ir.TableWireElemKind(f)
	lo, hi := variableRawBounds(f)
	if f.ArrayBound > 0 {
		// A count past the fixed bound keeps the bounded prefix and counts
		// clamped (docs/SPEC-TABLES.md §4); malformed is reserved for a count
		// the body cannot cover (§4), so the over-bound tail is never read
		// (the body length frames the unused bytes) and an unsigned count whose
		// high bit is set is still past the bound, clamped, not refused.
		pad := ""
		clear, place := "v."+m+".clear();", fmt.Sprintf("v.%s.add(%%s)", m)
		if f.Array == ir.ArrayFixed {
			// a FIXED array overwrites the slots it reaches and leaves every
			// later slot standing (docs/SPEC-TABLES.md §3.3)
			pad = fmt.Sprintf("while(v.%s.size()<%d)v.%s.add(%s);", m, f.ArrayBound, m, variableWideDefault(f))
			clear, place = "", fmt.Sprintf("TableVariable.put(v.%s,(int)i,%%s)", m)
		}
		fmt.Fprintf(b, "                if(kind==14){TableVariable.Reader ar=r.body();if(ar!=null&&ar.has(2)){int ek=ar.get8();long count=ar.count(r.limit);if(!ar.fail){if(!(%s)){report.kindMismatch++;}else{if(ek!=%d)report.widened++;long take=count;if(take<0||take>%dL){take=%dL;report.clamped++;}%sfor(long i=0;i<take;i++){java.math.BigInteger raw=TableVariable.readInteger(ar,ek);if(raw==null)break;raw=TableVariable.clampInteger(raw,%s,%s,report);%s;}%s}}}}else{r.skip(kind);report.kindMismatch++;}\n", variableCompatibleKinds(kind, "ek"), kind, f.ArrayBound, f.ArrayBound, clear, lo, hi, fmt.Sprintf(place, variableRawStorage(f, "raw")), pad)
		return
	}
	fmt.Fprintf(b, "                if(kind==14){TableVariable.Reader ar=r.body();if(ar!=null&&ar.has(2)){int ek=ar.get8();long count=ar.leb();if(!ar.fail){if(!(%s)){report.kindMismatch++;}else if(count<0||count>0x7fffffffL){report.refused=true;}else{if(ek!=%d)report.widened++;v.%s.clear();for(long i=0;i<count;i++){java.math.BigInteger raw=TableVariable.readInteger(ar,ek);if(raw==null)break;raw=TableVariable.clampInteger(raw,%s,%s,report);v.%s.add(%s);}}}}}else{r.skip(kind);report.kindMismatch++;}\n", variableCompatibleKinds(kind, "ek"), kind, m, lo, hi, m, variableRawStorage(f, "raw"))
}
func writeVariableWideJsonRead(b *strings.Builder, f *ir.Field, expr string, array bool) {
	lo, hi := variableRawBounds(f)
	fmt.Fprintf(b, "{java.math.BigInteger raw=TableJson.readWide(in,%t,%d,%s,%s);", f.Type.Signed, f.Type.FracBits, lo, hi)
	if array {
		if f.ArrayBound > 0 {
			fmt.Fprintf(b, "if(%s.size()<%d)%s.add(raw==null?%s:%s);else in.rep.clamped++;", expr, f.ArrayBound, expr, variableWideDefault(f), variableRawStorage(f, "raw"))
		} else {
			fmt.Fprintf(b, "%s.add(raw==null?%s:%s);", expr, variableWideDefault(f), variableRawStorage(f, "raw"))
		}
	} else {
		fmt.Fprintf(b, "if(raw!=null)%s=%s;", expr, variableRawStorage(f, "raw"))
	}
	b.WriteString("}\n")
}
func variableWideJson(f *ir.Field, expr string) string {
	return fmt.Sprintf("TableJson.wideText(%s,%d,%t)", variableRawExpr(f, expr), f.Type.FracBits, f.Type.Kind == ir.TFixed)
}

const variableWideRuntime = `
    static java.math.BigInteger wide(long hi,long lo,boolean signed) {
        java.math.BigInteger upper=signed?java.math.BigInteger.valueOf(hi):new java.math.BigInteger(Long.toUnsignedString(hi));
        return upper.shiftLeft(64).add(new java.math.BigInteger(Long.toUnsignedString(lo)));
    }
    static int integerWidth(int kind){switch(kind){case 2:case 6:case 20:case 25:return 1;case 3:case 7:case 21:case 26:return 2;case 4:case 8:case 22:case 27:return 4;case 5:case 9:case 23:case 28:return 8;case 18:case 19:case 24:case 29:return 16;default:return 0;}}
    static boolean signedKind(int kind){return kind>=2&&kind<=5||kind==18||kind>=20&&kind<=24;}
    static java.math.BigInteger readInteger(Reader r,int kind){
        int width=integerWidth(kind);if(width==0||!r.has(width)){r.report.malformed=true;r.fail=true;return null;}
        byte[] bytes=new byte[width];for(int i=0;i<width;i++)bytes[width-i-1]=(byte)r.get8();
        return signedKind(kind)?new java.math.BigInteger(bytes):new java.math.BigInteger(1,bytes);
    }
    static java.math.BigInteger clampInteger(java.math.BigInteger raw,java.math.BigInteger lo,java.math.BigInteger hi,Report report){
        if(raw.compareTo(lo)<0){report.clamped++;return lo;}if(raw.compareTo(hi)>0){report.clamped++;return hi;}return raw;
    }
`

const variableWideJsonRuntime = `
    static String wideText(java.math.BigInteger raw,int fraction,boolean fixed){
        if(!fixed)return raw.toString();String text=new java.math.BigDecimal(raw).divide(new java.math.BigDecimal(java.math.BigInteger.ONE.shiftLeft(fraction))).stripTrailingZeros().toPlainString();
        return text.indexOf('.')<0?text+".0":text;
    }
    // Exact raw-scale conversion, with the oracle's bounded decimal band.
    // A huge exponent is classified before integer allocation (§15, §16.2).
    static java.math.BigInteger readWide(In in,boolean signed,int fraction,java.math.BigInteger lo,java.math.BigInteger hi){
        int c=in.peek();if(c!='-'&&(c<'0'||c>'9')){in.skipValue();in.rep.kindMismatch++;return null;}
        String token=in.scanNumber(new boolean[1]);if(in.bad||token==null)return null;
        java.math.BigInteger domainHi=java.math.BigInteger.ONE.shiftLeft(signed?127:128).subtract(java.math.BigInteger.ONE);
        java.math.BigInteger domainLo=signed?java.math.BigInteger.ONE.shiftLeft(127).negate():java.math.BigInteger.ZERO;
        boolean nonzero=false;for(int i=0;i<token.length()&&token.charAt(i)!='e'&&token.charAt(i)!='E';i++)if(token.charAt(i)>='1'&&token.charAt(i)<='9')nonzero=true;
        java.math.BigInteger raw=java.math.BigInteger.ZERO;
        if(nonzero){
            try{
                java.math.BigDecimal value=new java.math.BigDecimal(token);long point=(long)value.precision()-value.scale();
                if(point>40){in.rep.clamped++;raw=token.charAt(0)=='-'?domainLo:domainHi;}
                else if(point< -40){in.rep.kindMismatch++;return null;}
                else{try{raw=value.multiply(new java.math.BigDecimal(java.math.BigInteger.ONE.shiftLeft(fraction))).toBigIntegerExact();}catch(ArithmeticException finer){in.rep.kindMismatch++;return null;}raw=TableVariable.clampInteger(raw,domainLo,domainHi,in.rep);}
            }catch(NumberFormatException exponent){int e=Math.max(token.indexOf('e'),token.indexOf('E'));if(e>=0&&e+1<token.length()&&token.charAt(e+1)=='-'){in.rep.kindMismatch++;return null;}in.rep.clamped++;raw=token.charAt(0)=='-'?domainLo:domainHi;}
        }
        return TableVariable.clampInteger(raw,lo,hi,in.rep);
    }
`
