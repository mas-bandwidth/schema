package gotable

// Exact conversion follows the C++ reference's decimal normalization and
// binary-fraction extraction. Four 32-bit limbs avoid a host-wide integer or
// an allocating big-number library in the generated walk.
const tableJsonWideSource = `
type tableJsonWide [2]uint64

func tableJsonWideSigned(kind uint8) bool { return kind == 18 || kind >= 20 && kind <= 24 }
func (v tableJsonWide) negative() bool { return v[1] >> 63 != 0 }
func (v tableJsonWide) zero() bool { return v[0] == 0 && v[1] == 0 }
func (v tableJsonWide) cmp(b tableJsonWide, signed bool) int {
 if signed && v.negative() != b.negative() { if v.negative() { return -1 }; return 1 }
 if v[1] < b[1] || v[1] == b[1] && v[0] < b[0] { return -1 }
 if v != b { return 1 };return 0
}
func (v tableJsonWide) shl(n int) tableJsonWide {
 if n <= 0 { return v };if n >= 128 { return tableJsonWide{} }
 if n >= 64 { return tableJsonWide{0,v[0] << (n-64)} }
 return tableJsonWide{v[0]<<n, v[1]<<n | v[0]>>(64-n)}
}
func (v tableJsonWide) shr(n int) tableJsonWide {
 if n <= 0 { return v };if n >= 128 { return tableJsonWide{} }
 if n >= 64 { return tableJsonWide{v[1] >> (n-64),0} }
 return tableJsonWide{v[0]>>n | v[1]<<(64-n),v[1]>>n}
}
func (v tableJsonWide) neg() tableJsonWide {
 v[0] = ^v[0]+1;v[1] = ^v[1];if v[0]==0 {v[1]++};return v
}
func (v *tableJsonWide) mulAdd(m,a uint32) uint32 {
 limbs:=[4]uint64{v[0]&0xffffffff,v[0]>>32,v[1]&0xffffffff,v[1]>>32}
 carry:=uint64(a)
 for i:=range limbs { p:=limbs[i]*uint64(m)+carry;limbs[i]=p&0xffffffff;carry=p>>32 }
 v[0]=limbs[0]|limbs[1]<<32;v[1]=limbs[2]|limbs[3]<<32;return uint32(carry)
}
func (v *tableJsonWide) div(d uint32) uint32 {
 limbs:=[4]uint64{v[0]&0xffffffff,v[0]>>32,v[1]&0xffffffff,v[1]>>32}
 rem:=uint64(0)
 for i:=3;i>=0;i-- {cur:=rem<<32|limbs[i];limbs[i]=cur/uint64(d);rem=cur%uint64(d)}
 v[0]=limbs[0]|limbs[1]<<32;v[1]=limbs[2]|limbs[3]<<32;return uint32(rem)
}
func tableJsonWideLoad(storage unsafe.Pointer,width uint32,signed bool) tableJsonWide {
 if width==16 { return *(*tableJsonWide)(storage) }
 if signed { n:=tableJsonGetSigned(storage,width);v:=tableJsonWide{uint64(n),0};if n<0 {v[1]=^uint64(0)};return v }
 return tableJsonWide{tableJsonGetRaw(storage,width),0}
}
func tableJsonWideStore(storage unsafe.Pointer,width uint32,v tableJsonWide) {
 if width==16 { *(*tableJsonWide)(storage)=v } else { tableJsonSetRaw(storage,width,v[0]) }
}
func (v tableJsonWide) fraction(n int) tableJsonWide {
 if n<64 {v[1]=0;v[0]&=uint64(1)<<n-1} else {v[1]&=uint64(1)<<(n-64)-1};return v
}
func tableJsonWriteWide(out *tableJsonOut,storage unsafe.Pointer,f *TableFieldInfo) {
 signed:=tableJsonWideSigned(f.Kind);v:=tableJsonWideLoad(storage,f.ElemSize,signed)
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

func tableJsonReadWide(in *tableJsonIn,token []byte,storage unsafe.Pointer,f *TableFieldInfo) bool {
 signed:=tableJsonWideSigned(f.Kind);frac:=int(f.FracBits)
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
 var raw tableJsonWide;saturated:=false
 signedMax:=tableJsonWide{^uint64(0),^uint64(0)>>1};signedMin:=tableJsonWide{0,uint64(1)<<63};unsignedMax:=tableJsonWide{^uint64(0),^uint64(0)}
 switch {
 case start==end:
 case point>40:
  saturated=true;if !negative {if signed {raw=signedMax}else{raw=unsignedMax}}else if signed {raw=signedMin}
 case point < -40:
  in.report.KindMismatch++;return true
 default:
  var fd [tableJsonMaxNumber+48]byte;fn:=0
  for z:=point;z<0;z++ {fd[fn]=0;fn++}
  for k:=max(point,0)+start;k<end;k++ {fd[fn]=digit(k)-'0';fn++}
  var fraction tableJsonWide
  for b:=0;b<frac;b++ {
   carry:=byte(0)
   for k:=fn-1;k>=0;k-- {d:=fd[k]*2+carry;fd[k]=d%10;carry=d/10}
   fraction=fraction.shl(1);fraction[0]|=uint64(carry)
  }
  for k:=0;k<fn;k++ {if fd[k]!=0 {in.report.KindMismatch++;return true}}
  var whole tableJsonWide
  for k:=start;k<start+point && !saturated;k++ {d:=uint32(0);if k<end {d=uint32(digit(k)-'0')};if whole.mulAdd(10,d)!=0{saturated=true}}
  if !saturated && frac>0 && !whole.shr(128-frac).zero() {saturated=true}
  if !saturated {raw=whole.shl(frac);raw[0]|=fraction[0];raw[1]|=fraction[1]}
  if signed {
   if !saturated && !negative && raw.negative() {saturated=true}
   if !saturated && negative && raw.cmp(signedMin,false)>0 {saturated=true}
   if saturated {if negative {raw=signedMin}else{raw=signedMax}}else if negative {raw=raw.neg()}
  } else {
   if saturated {raw=unsignedMax};if negative && !raw.zero() {raw=tableJsonWide{};saturated=true}
  }
 }
 if saturated {in.report.Clamped++}
 if f.WideRange {
  lo,hi:=tableJsonWide(f.WideMin),tableJsonWide(f.WideMax)
  if raw.cmp(lo,signed)<0 {raw=lo;in.report.Clamped++}else if raw.cmp(hi,signed)>0 {raw=hi;in.report.Clamped++}
 }
 tableJsonWideStore(storage,f.ElemSize,raw);return true
}
`
