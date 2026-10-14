package darttable

// textNumberMembers is injected into TableTextReport. A fractional token keeps
// its spelling in a record until its destination asks for a float; integer
// placement never passes through binary floating point (SPEC-TABLES §16.2).
const textNumberMembers = `
  static final _numberGrammar = RegExp(r'^(-?)(0|[1-9][0-9]*)(?:\.([0-9]+))?(?:[eE]([+-]?[0-9]+))?$');
  static RegExpMatch? _numberMatch(String token) {
    final match=_numberGrammar.firstMatch(token);
    return match!=null&&match.end==token.length?match:null;
  }
  static BigInt? _numberInteger(String token) {
    final match=_numberMatch(token);if(match==null)return null;
    final whole=match[2]!;final fraction=match[3]??'';
    final digits=whole+fraction;
    var first=0;while(first<digits.length&&digits.codeUnitAt(first)==48){first++;}
    if(first==digits.length)return BigInt.zero;
    var last=digits.length;while(digits.codeUnitAt(last-1)==48){last--;}
    final significant=digits.substring(first,last);
    final spelling=match[4]??'0';var at=0;var negativeExponent=false;
    if(spelling.startsWith('-')||spelling.startsWith('+')){negativeExponent=spelling.startsWith('-');at++;}
    // A larger exponent cannot cancel the token's own decimal extent. Avoid
    // allocating a power of ten whose size is controlled by an exponent.
    final cap=token.length+4096;var exponent=0;
    for(;at<spelling.length;at++){
      if(exponent<cap){exponent=exponent*10+spelling.codeUnitAt(at)-48;if(exponent>cap)exponent=cap;}
    }
    if(negativeExponent)exponent=-exponent;
    final point=whole.length+exponent-first;
    if(point<significant.length)return null;
    BigInt value;
    if(point>400){value=BigInt.one<<2048;}else{
      value=BigInt.parse(significant)*BigInt.from(10).pow(point-significant.length);
    }
    return match[1]=='-'?-value:value;
  }
  static dynamic number(String token) {
    if(_numberMatch(token)==null)throw FormatException('invalid JSON number',token);
    final integer=_numberInteger(token);
    if(integer==null)return (number:token);
    final lo=-(BigInt.one<<63),hi=(BigInt.one<<63)-BigInt.one;
    return integer>=lo&&integer<=hi?integer.toInt():integer;
  }
  static bool isNumber(dynamic value) => value is num || value is BigInt || value is ({String number});
  static BigInt? integerValue(dynamic value) {
    if(value is BigInt)return value;
    if(value is int)return BigInt.from(value);
    if(value is ({String number}))return _numberInteger(value.number);
    if(value is num&&value.isFinite&&value==value.truncateToDouble())return BigInt.from(value);
    return null;
  }
  static double? floatValue(dynamic value) {
    if(value is num)return value.toDouble();
    if(value is BigInt)return value.toDouble();
    if(value is ({String number}))return double.tryParse(value.number);
    return null;
  }
  static int integerField(dynamic value,TableTextReport r,int? lo,int? hi,bool unsigned,int fallback) {
    var integer=integerValue(value);
    if(integer==null){r.kindMismatch++;return fallback;}
    final floor=unsigned?BigInt.zero:-(BigInt.one<<63);
    final ceiling=(BigInt.one<<(unsigned?64:63))-BigInt.one;
    var moved=false;
    if(integer<floor){integer=floor;moved=true;}
    if(integer>ceiling){integer=ceiling;moved=true;}
    if(lo!=null){final bound=unsigned?BigInt.from(lo).toUnsigned(64):BigInt.from(lo);if(integer<bound){integer=bound;moved=true;}}
    if(hi!=null){final bound=unsigned?BigInt.from(hi).toUnsigned(64):BigInt.from(hi);if(integer>bound){integer=bound;moved=true;}}
    if(moved)r.clamped++;
    return integer.toSigned(64).toInt();
  }
`
