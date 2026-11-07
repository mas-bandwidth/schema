package gotable

// Exact conversion follows the C++ reference's decimal normalization and
// binary-fraction extraction. Four 32-bit limbs avoid a host-wide integer or
// an allocating big-number library in the generated walk.
const tableJSONWideSource = `
type tableJSONWide [2]uint64

func tableJSONWideSigned(kind uint8) bool { return kind == 18 || kind >= 20 && kind <= 24 }
func (v tableJSONWide) negative() bool { return v[1] >> 63 != 0 }
func (v tableJSONWide) zero() bool { return v[0] == 0 && v[1] == 0 }
func (v tableJSONWide) cmp(b tableJSONWide, signed bool) int {
 if signed && v.negative() != b.negative() { if v.negative() { return -1 }; return 1 }
 if v[1] < b[1] || v[1] == b[1] && v[0] < b[0] { return -1 }
 if v != b { return 1 };return 0
}
func (v tableJSONWide) shl(n int) tableJSONWide {
 if n <= 0 { return v };if n >= 128 { return tableJSONWide{} }
 if n >= 64 { return tableJSONWide{0,v[0] << (n-64)} }
 return tableJSONWide{v[0]<<n, v[1]<<n | v[0]>>(64-n)}
}
func (v tableJSONWide) shr(n int) tableJSONWide {
 if n <= 0 { return v };if n >= 128 { return tableJSONWide{} }
 if n >= 64 { return tableJSONWide{v[1] >> (n-64),0} }
 return tableJSONWide{v[0]>>n | v[1]<<(64-n),v[1]>>n}
}
func (v tableJSONWide) neg() tableJSONWide {
 v[0] = ^v[0]+1;v[1] = ^v[1];if v[0]==0 {v[1]++};return v
}
func (v *tableJSONWide) mulAdd(m,a uint32) uint32 {
 limbs:=[4]uint64{v[0]&0xffffffff,v[0]>>32,v[1]&0xffffffff,v[1]>>32}
 carry:=uint64(a)
 for i:=range limbs { p:=limbs[i]*uint64(m)+carry;limbs[i]=p&0xffffffff;carry=p>>32 }
 v[0]=limbs[0]|limbs[1]<<32;v[1]=limbs[2]|limbs[3]<<32;return uint32(carry)
}
func (v *tableJSONWide) div(d uint32) uint32 {
 limbs:=[4]uint64{v[0]&0xffffffff,v[0]>>32,v[1]&0xffffffff,v[1]>>32}
 rem:=uint64(0)
 for i:=3;i>=0;i-- {cur:=rem<<32|limbs[i];limbs[i]=cur/uint64(d);rem=cur%uint64(d)}
 v[0]=limbs[0]|limbs[1]<<32;v[1]=limbs[2]|limbs[3]<<32;return uint32(rem)
}
func tableJSONWideLoad(storage unsafe.Pointer,width uint32,signed bool) tableJSONWide {
 if width==16 { return *(*tableJSONWide)(storage) }
 if signed { n:=tableJSONGetSigned(storage,width);v:=tableJSONWide{uint64(n),0};if n<0 {v[1]=^uint64(0)};return v }
 return tableJSONWide{tableJSONGetRaw(storage,width),0}
}
func tableJSONWideStore(storage unsafe.Pointer,width uint32,v tableJSONWide) {
 if width==16 { *(*tableJSONWide)(storage)=v } else { tableJSONSetRaw(storage,width,v[0]) }
}
func (v tableJSONWide) fraction(n int) tableJSONWide {
 if n<64 {v[1]=0;v[0]&=uint64(1)<<n-1} else {v[1]&=uint64(1)<<(n-64)-1};return v
}
func tableJSONWriteWide(out *tableJSONOut,storage unsafe.Pointer,f *TableFieldInfo) {
 signed:=tableJSONWideSigned(f.Kind);v:=tableJSONWideLoad(storage,f.ElemSize,signed)
 if signed && v.negative() {out.put('-');v=v.neg()}
 frac:=int(f.FracBits);whole:=v.shr(frac)
 var digits [40]byte;n:=0
 for { digits[n]='0'+byte(whole.div(10));n++;if whole.zero(){break} }
 for i:=n-1;i>=0;i-- {out.put(digits[i])}
 if f.Kind<20 {return};out.put('.')
 fraction:=v.fraction(frac)
 if fraction.zero() {out.put('0');return}
 for !fraction.zero() {
  carry:=fraction.mulAdd(10,0);digit:=fraction.shr(frac)[0]
  if frac>64 {digit|=uint64(carry)<<(128-frac)}
  out.put('0'+byte(digit));fraction=fraction.fraction(frac)
 }
}

func tableJSONReadWide(in *tableJSONIn,token []byte,storage unsafe.Pointer,f *TableFieldInfo) bool {
 signed:=tableJSONWideSigned(f.Kind);frac:=int(f.FracBits)
 i:=0;negative:=false
 if len(token)>0 && (token[0]=='-' || token[0]=='+') {negative=token[0]=='-';i++}
 intStart:=i
 for i<len(token) && token[i]>='0' && token[i]<='9' {i++}
 intLen:=i-intStart;fracStart:=i;fracLen:=0
 if i<len(token) && token[i]=='.' {i++;fracStart=i;for i<len(token) && token[i]>='0' && token[i]<='9' {i++};fracLen=i-fracStart}
 exp:=0
 if i<len(token) && (token[i]=='e' || token[i]=='E') {
  i++;neg:=false;if i<len(token) && (token[i]=='-' || token[i]=='+') {neg=token[i]=='-';i++}
  for i<len(token) && token[i]>='0' && token[i]<='9' {if exp<100000 {exp=exp*10+int(token[i]-'0')};i++}
  if neg {exp=-exp}
 }
 digit:=func(k int) byte {if k<intLen {return token[intStart+k]};return token[fracStart+k-intLen]}
 start,end,point:=0,intLen+fracLen,intLen+exp
 for start<end && digit(start)=='0' {start++;point--}
 for end>start && digit(end-1)=='0' {end--}
 var raw tableJSONWide;saturated:=false
 signedMax:=tableJSONWide{^uint64(0),^uint64(0)>>1};signedMin:=tableJSONWide{0,uint64(1)<<63};unsignedMax:=tableJSONWide{^uint64(0),^uint64(0)}
 switch {
 case start==end:
 case point>40:
  saturated=true;if !negative {if signed {raw=signedMax}else{raw=unsignedMax}}else if signed {raw=signedMin}
 case point < -40:
  in.report.KindMismatch++;return true
 default:
    // §16.2: Q I.F has I+F=128 bits, so a nonzero digit past the 128th
    // decimal place is inexact (a shape error), never a truncation.
    var fd [128]byte;fn:=0
    for z:=point;z<0;z++ {if fn<128 {fd[fn]=0};fn++}
    for k:=max(point,0)+start;k<end;k++ {if fn>=128 {if digit(k)!='0'{in.report.KindMismatch++;return true}}else{fd[fn]=digit(k)-'0';fn++}}
  var fraction tableJSONWide
  for range frac {
   carry:=byte(0)
   for k:=fn-1;k>=0;k-- {d:=fd[k]*2+carry;fd[k]=d%10;carry=d/10}
   fraction=fraction.shl(1);fraction[0]|=uint64(carry)
  }
  for k := range fn {if fd[k]!=0 {in.report.KindMismatch++;return true}}
  var whole tableJSONWide
  for k:=start;k<start+point && !saturated;k++ {d:=uint32(0);if k<end {d=uint32(digit(k)-'0')};if whole.mulAdd(10,d)!=0{saturated=true}}
  if !saturated && frac>0 && !whole.shr(128-frac).zero() {saturated=true}
  if !saturated {raw=whole.shl(frac);raw[0]|=fraction[0];raw[1]|=fraction[1]}
  if signed {
   if !saturated && !negative && raw.negative() {saturated=true}
   if !saturated && negative && raw.cmp(signedMin,false)>0 {saturated=true}
   if saturated {if negative {raw=signedMin}else{raw=signedMax}}else if negative {raw=raw.neg()}
  } else {
   if saturated {raw=unsignedMax};if negative && !raw.zero() {raw=tableJSONWide{};saturated=true}
  }
 }
 if saturated {in.report.Clamped++}
 if f.WideRange {
  lo,hi:=tableJSONWide(f.WideMin),tableJSONWide(f.WideMax)
  if raw.cmp(lo,signed)<0 {raw=lo;in.report.Clamped++}else if raw.cmp(hi,signed)>0 {raw=hi;in.report.Clamped++}
 }
 tableJSONWideStore(storage,f.ElemSize,raw);return true
}
`
