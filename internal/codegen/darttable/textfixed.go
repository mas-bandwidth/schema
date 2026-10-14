package darttable

// textFixedMembers joins textNumberMembers inside TableTextReport. JSON Q
// values spell WHOLE UNITS; storage and bounds are raw integers (units*2^F).
// F is the checked IR's fractional width, 0..127 (SPEC-TABLES §16.2).
const textFixedMembers = `
  static BigInt? _fixedDecimal(String token,int fracBits) {
    final match=_numberMatch(token);if(match==null)return null;
    final whole=match[2]!,fraction=match[3]??'';final digits=whole+fraction;
    var first=0;while(first<digits.length&&digits.codeUnitAt(first)==48){first++;}
    if(first==digits.length)return BigInt.zero;
    var last=digits.length;while(digits.codeUnitAt(last-1)==48){last--;}
    final significant=digits.substring(first,last);
    final spelling=match[4]??'0';var at=0,exponent=0;
    final negativeExponent=spelling.startsWith('-');
    if(negativeExponent||spelling.startsWith('+'))at++;
    final cap=token.length+4096;
    for(;at<spelling.length;at++)if(exponent<cap){exponent=exponent*10+spelling.codeUnitAt(at)-48;if(exponent>cap)exponent=cap;}
    if(negativeExponent)exponent=-exponent;
    final point=whole.length+exponent-first,scale=point-significant.length;
    BigInt raw;
    if(scale<0){
      final places=-scale;
      // With decimal trailing zeros removed, a dyadic value needs at least
      // this many fractional bits. Check exactness BEFORE overflow clamping.
      if(places>fracBits)return null;
      final fives=BigInt.from(5).pow(places);
      final tail=significant.substring(significant.length>places?significant.length-places:0);
      if(BigInt.parse(tail).remainder(fives)!=BigInt.zero)return null;
      if(point>400){raw=BigInt.one<<2048;}else{
        raw=(BigInt.parse(significant)~/fives)<<(fracBits-places);
      }
    }else{
      if(point>400){raw=BigInt.one<<2048;}else{
        raw=(BigInt.parse(significant)*BigInt.from(10).pow(scale))<<fracBits;
      }
    }
    return match[1]=='-'?-raw:raw;
  }
  static BigInt? fixedValue(dynamic value,int fracBits) {
    if(value is int)return BigInt.from(value)<<fracBits;
    if(value is BigInt)return value<<fracBits;
    if(value is ({String number}))return _fixedDecimal(value.number,fracBits);
    if(value is double){
      if(!value.isFinite)return null;
      final data=ByteData(8)..setFloat64(0,value,Endian.little);
      final bits=BigInt.from(data.getUint64(0,Endian.little)).toUnsigned(64);
      final exponent=((bits>>52)&BigInt.from(2047)).toInt();
      var mantissa=bits&((BigInt.one<<52)-BigInt.one);
      if(exponent!=0)mantissa|=BigInt.one<<52;
      if(mantissa==BigInt.zero)return BigInt.zero;
      final shift=(exponent==0?-1074:exponent-1023-52)+fracBits;
      BigInt raw;
      if(shift>=0){raw=mantissa<<shift;}else{
        if(-shift>53||mantissa.remainder(BigInt.one<<(-shift))!=BigInt.zero)return null;
        raw=mantissa>>(-shift);
      }
      return (bits>>63)!=BigInt.zero?-raw:raw;
    }
    return null;
  }
  static BigInt fixedField(dynamic value,TableTextReport r,int fracBits,BigInt lo,BigInt hi,BigInt fallback) {
    final raw=fixedValue(value,fracBits);
    if(raw==null){r.kindMismatch++;return fallback;}
    if(raw<lo){r.clamped++;return lo;}
    if(raw>hi){r.clamped++;return hi;}
    return raw;
  }
  static String fixedText(BigInt raw,int fracBits) {
    final magnitude=raw.abs(),sign=raw.isNegative?'-':'';
    final whole=magnitude>>fracBits;
    final fraction=magnitude&((BigInt.one<<fracBits)-BigInt.one);
    if(fraction==BigInt.zero)return '$sign$whole.0';
    var tail=(fraction*BigInt.from(5).pow(fracBits)).toString().padLeft(fracBits,'0');
    var end=tail.length;while(tail.codeUnitAt(end-1)==48){end--;}
    return '$sign$whole.'+tail.substring(0,end);
  }
`
