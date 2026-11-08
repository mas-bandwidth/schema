package darttable

// Timestamp text is parsed before DateTime construction: Dart's permissive date
// normalisation must not accept invalid calendar components. BigInt protects the
// nanosecond conversion at both int64 endpoints; extra fraction digits truncate.
const timestampTextRuntime = `
int? _timestampNanos(String s) {
  int digits(int at,int count) {
    if(at<0||at+count>s.length)return -1;
    var value=0;
    for(var i=at;i<at+count;i++){final c=s.codeUnitAt(i)-48;if(c<0||c>9)return -1;value=value*10+c;}
    return value;
  }
  if(s.length<20||s[4]!='-'||s[7]!='-'||(s[10]!='T'&&s[10]!='t')||s[13]!=':'||s[16]!=':')return null;
  final y=digits(0,4),m=digits(5,2),d=digits(8,2),h=digits(11,2),minute=digits(14,2),sec=digits(17,2);
  if(y<1||m<1||m>12||d<1||d>31||h<0||h>23||minute<0||minute>59||sec<0||sec>59)return null;
  final date=DateTime.utc(y,m,d,h,minute,sec);
  if(date.year!=y||date.month!=m||date.day!=d)return null;
  var at=19,fraction=0,count=0;
  if(s[at]=='.'){
    at++;final start=at;
    while(at<s.length){final c=s.codeUnitAt(at)-48;if(c<0||c>9)break;if(count<9){fraction=fraction*10+c;count++;}at++;}
    if(at==start)return null;
  }
  while(count<9){fraction*=10;count++;}
  var offset=0;
  if(at==s.length-1&&(s[at]=='Z'||s[at]=='z')){at++;}
  else if(at==s.length-6&&(s[at]=='+'||s[at]=='-')&&s[at+3]==':'){
    final oh=digits(at+1,2),om=digits(at+4,2);
    if(oh<0||oh>23||om<0||om>59)return null;
    offset=(oh*3600+om*60)*(s[at]=='-'?-1:1);at+=6;
  }else{return null;}
  if(at!=s.length)return null;
  final seconds=date.microsecondsSinceEpoch~/1000000-offset;
  final value=BigInt.from(seconds)*BigInt.from(1000000000)+BigInt.from(fraction);
  if(value<-(BigInt.one<<63)||value>(BigInt.one<<63)-BigInt.one)return null;
  return value.toInt();
}

int _timestampField(dynamic value,TableTextReport report,[int previous=0]){
  if(value is! String){report.kindMismatch++;return previous;}
  final nanos=_timestampNanos(value);
  if(nanos==null)throw const FormatException('timestamp_not_rfc3339');
  return nanos;
}

String _timestampText(int nanos){
  var seconds=nanos~/1000000000;final fraction=nanos%1000000000;
  if(nanos<0&&fraction!=0)seconds--;
  final date=DateTime.fromMicrosecondsSinceEpoch(seconds*1000000,isUtc:true);
  final text=date.toIso8601String().substring(0,19);
  if(fraction==0)return '${text}Z';
  var digits=fraction.toString().padLeft(9,'0');
  while(digits.endsWith('0')){digits=digits.substring(0,digits.length-1);}
  return '$text.${digits}Z';
}
`
