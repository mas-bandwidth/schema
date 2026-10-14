package darttable

// textFlagsMembers is injected into TableTextReport. Flags spell the set bits
// as declared names, in bit order (SPEC-TABLES §16.2). The checked declaration
// supplies at most 64 unique names; native int holds the uint64 bit pattern.
const textFlagsMembers = `
  static int flagsField(dynamic value,TableTextReport r,List<String> names,int fallback) {
    if(value is! List){r.kindMismatch++;return fallback;}
    var mask=BigInt.zero;
    for(final item in value){
      if(item is! String){r.kindMismatch++;continue;}
      final bit=names.indexOf(item);
      if(bit<0){r.unknown++;continue;}
      mask|=BigInt.one<<bit;
    }
    return mask.toSigned(64).toInt();
  }
  static List<String> flagsNames(int mask,List<String> names) {
    final bits=BigInt.from(mask).toUnsigned(64);
    final selected=<String>[];
    for(var bit=0;bit<64;bit++)if((bits&(BigInt.one<<bit))!=BigInt.zero){
      if(bit>=names.length)throw StateError('flags bit $bit has no text spelling (docs/SPEC-TABLES §16.2)');
      selected.add(names[bit]);
    }
    return selected;
  }
  static void flagsText(_JOut out,int mask,List<String> names,int depth) {
    // Resolve every name before emitting: an unnamed bit cannot leave a
    // partial JSON array in the caller's output.
    final selected=flagsNames(mask,names);
    if(selected.isEmpty){out.raw('[]');return;}
    out.raw('[');var first=true;
    for(final name in selected){if(!first)out.raw(',');first=false;out.line(depth+1);out.raw(_jsonString(name));}
    out.line(depth);out.raw(']');
  }
`
