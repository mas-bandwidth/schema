package darttable

// putWide writes only the UTF-16LE payload; its caller emits outer L. Kind
// 33 forbids zero units and unpaired surrogates (SPEC-TABLES §3). Like other
// write-side contracts, validity is asserted only in checked builds.
const wideTextWriterMembers = `
  static bool validWide(String value) {
    final units=value.codeUnits;
    for(var i=0;i<units.length;i++){
      final unit=units[i];if(unit==0)return false;
      if(unit>=0xd800&&unit<=0xdbff){
        if(i+1>=units.length||units[i+1]<0xdc00||units[i+1]>0xdfff)return false;
        i++;
      }else if(unit>=0xdc00&&unit<=0xdfff){return false;}
    }
    return true;
  }
  void putWide(String value) {
    assert(validWide(value));
    for(final unit in value.codeUnits){putByte(unit&255);putByte(unit>>8);}
  }
`

// readWide takes L in BYTES and the checked IR's bound in UTF-16 CODE UNITS.
// It owns the malformed flag. A failed framing preflight consumes nothing;
// framed content (even damaged content) consumes exactly L. The caller owns
// destination reset and its enclosing resume boundary. Validate the full
// payload before clamping, including the tail that will not be retained.
const wideTextReaderMembers = `
  String? readWide(int byteLength,int maxUnits) {
    if(!has(byteLength)||byteLength>limit-pos){report.malformed=true;return null;}
    final start=pos;pos+=byteLength;
    if(byteLength.isOdd){report.malformed=true;return null;}
    final units=List<int>.generate(byteLength~/2,(i)=>data[start+i*2]|(data[start+i*2+1]<<8));
    for(var i=0;i<units.length;i++){
      final unit=units[i];
      if(unit==0){report.malformed=true;return null;}
      if(unit>=0xd800&&unit<=0xdbff){
        if(i+1>=units.length||units[i+1]<0xdc00||units[i+1]>0xdfff){report.malformed=true;return null;}
        i++;
      }else if(unit>=0xdc00&&unit<=0xdfff){report.malformed=true;return null;}
    }
    var keep=units.length;
    if(keep>maxUnits){
      keep=maxUnits;
      if(keep>0&&units[keep-1]>=0xd800&&units[keep-1]<=0xdbff)keep--;
      report.clamped++;
    }
    return String.fromCharCodes(units.take(keep));
  }
`
