// Squirrel message-form runtime. The batch is one low-bit-first stream (section 3.3).
local _M = vargv[0];

local setSlot = function(o, k, v) { if (k in o) o[k] = v; else o[k] <- v; };
local reset = function(r) {
  if (r == null) return;
  setSlot(r, "unknown", 0); setSlot(r, "kindMismatch", 0); setSlot(r, "widened", 0); setSlot(r, "clamped", 0);
  setSlot(r, "malformed", false); setSlot(r, "refused", false); setSlot(r, "reason", null);
};

local refuse = function(r, why) {
  if (r != null) { r.refused = true; r.reason = why; }
  return -1;
};

_M.RefuseTable <- function(name, report) {
  reset(report);
  return refuse(report, "message_form_plain_table:" + name);
};

local equalBytes = function(a, b) {
  if (a.len() != b.len()) return false;
  for (local i = 0; i < a.len(); i++) if ((a[i] & 0xff) != (b[i] & 0xff)) return false;
  return true;
};

local readU32 = function(b, at) {
  return (b[at]&0xff)|((b[at+1]&0xff)<<8)|((b[at+2]&0xff)<<16)|((b[at+3]&0xff)<<24);
};
local readU64 = function(b, at) {
  local lo = (b[at]&0xff) | ((b[at+1]&0xff)<<8) | ((b[at+2]&0xff)<<16) | ((b[at+3]&0xff)<<24);
  local hi = (b[at+4]&0xff) | ((b[at+5]&0xff)<<8) | ((b[at+6]&0xff)<<16) | ((b[at+7]&0xff)<<24);
  return lo | (hi << 32);
};
local readLeb = function(b, state) {
  local value = 0, shift = 0, start = state.at;
  for (local i=0; i<10; i++) {
    if (state.at >= state.end) { state.bad=true; state.at=start; return 0; }
    local x=b[state.at]&0xff; state.at++;
    if (i==9 && x>1) { state.bad=true; state.at=start; return 0; }
    if (i<9) value = value | ((x&0x7f)<<shift);
    if ((x&0x80)==0) { if(i>0 && x==0){state.bad=true;state.at=start;return 0;} return value; }
    shift += 7;
  }
  state.bad=true; state.at=start; return 0;
};
local messageNarrowF32, messageWidenF32, f32;
local parseShape;
parseShape = function(b, state, kind) {
  local s={Kind=kind,Bits=-1,Packing=0,Base=0,Min=0,Max=0,ElemKind=0,Element=null,QMin=0.0,QDelta=0.0,QCount=0};
  if(kind==1){s.Bits=1;return s;}
  if(kind>=2&&kind<=11){s.Bits=kind==10?32:(kind==11?64:(kind<=5?(8<<(kind-2)):(8<<(kind-6))));}
  if(kind>=18&&kind<=29){s.Bits=(kind==18||kind==19||kind==24||kind==29)?128:((kind==20||kind==25)?8:((kind==21||kind==26)?16:((kind==22||kind==27)?32:64)));}
  if((kind>=2&&kind<=10)||(kind>=18&&kind<=29)){
    if(state.at>=state.end){state.bad=true;return s;}
    s.Packing=b[state.at]&0xff;state.at++;
    if(s.Packing==0)return s;
    if(s.Packing==1&&kind!=10){local bits=readLeb(b,state);if(state.bad||bits>s.Bits){state.bad=true;return s;}s.Bits=bits;if(kind>=18){if(state.at+16>state.end){state.bad=true;return s;}s.Base=readU64(b,state.at);state.at+=16;}else{local raw=readLeb(b,state);if(state.bad)return s;s.Base=(kind>=2&&kind<=5)?((raw>>1)^(-(raw&1))):raw;}return s;}
    if(s.Packing==2&&kind==10){
      if(state.at+12>state.end){state.bad=true;return s;}
      local lo=messageWidenF32(readU32(b,state.at)),hi=messageWidenF32(readU32(b,state.at+4)),res=messageWidenF32(readU32(b,state.at+8));state.at+=12;
      // ir.TableMessageQuantization, in float32: a triple with min not below
      // max, res not above zero, or a delta or step count that is not finite
      // is refused; a step count below 1 is 1, and one past the largest
      // float below 2^32 is that float
      if(!(lo<hi)||!(res>0)){state.bad=true;return s;}
      local delta=f32(hi-lo),steps=f32(delta/res);
      if(delta-delta!=0||steps-steps!=0){state.bad=true;return s;}
      if(!(steps>=1))steps=1.0;else if(steps>4294967040.0)steps=4294967040.0;
      s.QMin=lo;s.QDelta=delta;s.QCount=ceil(steps).tointeger();s.Bits=0;local q=s.QCount;while(q>0){s.Bits++;q=q>>1;}return s;
    }
    state.bad=true;return s;
  }
  if(kind==11)return s;
  if(kind==12||kind==33){s.Max=readLeb(b,state);if(state.bad||s.Max>2147483647){state.bad=true;return s;}s.Bits=0;local m=s.Max;while(m>0){s.Bits++;m=m>>1;}return s;}
  if(kind==14||kind==16){if(kind==14)s.Min=readLeb(b,state);s.Max=readLeb(b,state);if(state.bad||s.Max>4294967295||s.Min>s.Max||state.at>=state.end){state.bad=true;return s;}s.Bits=0;local m=s.Max-s.Min;while(m>0){s.Bits++;m=m>>1;}s.ElemKind=b[state.at]&0xff;state.at++;if(s.ElemKind>33||s.ElemKind==12||s.ElemKind==33){state.bad=true;return s;}s.Element=parseShape(b,state,s.ElemKind);return s;}
  if(kind==0||kind==13||kind==15||kind==17||kind==30||kind==31||kind==32){return s;}
  state.bad=true;return s;
};
local parseVocabulary = function(words) {
  local state={b=words,at=0,end=words.len(),bad=false}, ids=[], entries=[], nodeSeen=false;
  while(state.at<state.end&&!state.bad){
    if(state.at+9>state.end){state.bad=true;break;}
    local start=state.at,id=readU64(words,state.at);state.at+=8;
    local kind=words[state.at]&0xff;state.at++;
    if(kind>33||id==0xfffffffffffffffd||id==0xfffffffffffffffe){state.bad=true;break;}
    if(id==0xffffffffffffffff){if(kind!=0||nodeSeen){state.bad=true;break;}nodeSeen=true;}
    local shape=parseShape(words,state,kind);if(state.bad)break;
    local entry={Id=id,Kind=kind,Shape=shape};
    if(kind==14||kind==16)entry.Element<-{Kind=shape.ElemKind,Shape=shape.Element};
    foreach(old in entries){if(old.Id==id&&old.Kind==kind&&old.End-old.Start==state.at-start){local same=true;for(local j=0;j<state.at-start;j++)if(words[old.Start+j]!=words[start+j]){same=false;break;}if(same){state.bad=true;break;}}}
    if(state.bad)break;
    entry.Start<-start;entry.End<-state.at;entries.append(entry);ids.append(id);
    if(ids.len()>4096)state.bad=true;
  }
  if(state.at!=state.end)state.bad=true;

  return [ids,entries,!state.bad];
};
local skipFileKind;
skipFileKind = function(b,state,kind) {
  local width=0;
  switch(kind){case 1:case 2:case 6:case 20:case 25:width=1;break;case 3:case 7:case 21:case 26:width=2;break;case 4:case 8:case 10:case 22:case 27:width=4;break;case 5:case 9:case 11:case 23:case 28:width=8;break;case 18:case 19:case 24:case 29:width=16;break;}
  if(width>0){if(state.at+width>state.end){state.bad=true;return;}state.at+=width;return;}
  if(kind==17 || kind==30){readLeb(b,state);return;}
  if(kind==12 || kind==13 || kind==14 || kind==16 || kind==31 || kind==32 || kind==33){local n=readLeb(b,state);if(state.bad||n>state.end-state.at){state.bad=true;return;}state.at+=n;return;}
  if(kind==15){local tag=readLeb(b,state);if(state.bad||tag==0)return;if(state.at>=state.end){state.bad=true;return;}local arm=b[state.at]&0xff;state.at++;skipFileKind(b,state,arm);return;}
  state.bad=true;
};
local refuseAnnouncement = function(v, report, why) {
  refuse(report, why);
  if(v!=null&&typeof v=="table")setSlot(v,"Refused",true);
  return false;
};
_M.AnnounceRead <- function(T, v, bytes, report) {
  reset(report);
  if (v == null || typeof v != "table") return refuse(report, "vocabulary_storage_required");
  if (("Announced" in v) && v.Announced || ("Refused" in v) && v.Refused) return refuse(report, "second_announcement");
  // THE FORM IS READ BEFORE THE LENGTH, as the engine does: a form byte this
  // reader does not carry is newer_form even when the file is too short to
  // hold the rest of an announcement (docs/SPEC-TABLES.md §3.3).
  if (bytes == null || bytes.len()<1) { if(report!=null)report.malformed=true; if("Refused" in v)v.Refused=true;else setSlot(v,"Refused",true); return false; }
  if ((bytes[0]&0xff)!=1) return refuseAnnouncement(v,report,(bytes[0]&0xff)==2 ? "message_form_as_file" : "newer_form");
  if (bytes.len()<9) { if(report!=null)report.malformed=true; if("Refused" in v)v.Refused=true;else setSlot(v,"Refused",true); return false; }
  // THE RECEIVER'S TWO BOUNDS (docs/SPEC-TABLES.md §3.3): MaxEntries caps the
  // entry count, MaxBytes the vocabulary field's own byte length, and zero
  // means the conforming default in each case.
  local maxEntries = (("MaxEntries" in v) && v.MaxEntries > 0) ? v.MaxEntries : 4096;
  local maxBytes = (("MaxBytes" in v) && v.MaxBytes > 0) ? v.MaxBytes : 65536;
  local total=bytes.len(), trailer=total-8, entries=readU64(bytes,trailer);
  if(entries>maxEntries)return refuseAnnouncement(v,report,"vocabulary_too_large");
  if(entries>(trailer-1)/8){if(report!=null)report.malformed=true;setSlot(v,"Refused",true);return false;}
  local bodyEnd=trailer-entries*8, ids=[];
  for(local i=0;i<entries;i++)ids.append(readU64(bytes,bodyEnd+i*8));
  local state={b=bytes,at=1,end=bodyEnd,bad=false}, versionCount=0, wordsCount=0, words=null, build=0;
  for(;;){
    local ref=readLeb(bytes,state);if(state.bad){break;}if(ref==0)break;
    if(ref>ids.len()||state.at>=state.end){state.bad=true;break;}
    local id=ids[ref-1],kind=bytes[state.at]&0xff;state.at++;
    if(id==0xfffffffffffffffe){if(kind!=9||state.at+8>state.end){state.bad=true;break;}build=readU64(bytes,state.at);state.at+=8;versionCount++;}
    else if(id==0xfffffffffffffffd){
      if(kind!=14){state.bad=true;break;}
      local n=readLeb(bytes,state);if(state.bad||n>state.end-state.at){state.bad=true;break;}
      local fieldEnd=state.at+n;
      if(state.at>=fieldEnd||(bytes[state.at]&0xff)!=6){state.bad=true;break;}state.at++;
      local wn=readLeb(bytes,state);if(state.bad||wn>fieldEnd-state.at||state.at+wn!=fieldEnd){state.bad=true;break;}
      if(wn>maxBytes)return refuseAnnouncement(v,report,"vocabulary_too_large");
      words=blob(wn);for(local j=0;j<wn;j++)words[j]=bytes[state.at+j]&0xff;
      state.at=fieldEnd;wordsCount++;
    } else {
      if(report!=null)report.unknown++;
      skipFileKind(bytes,state,kind);if(state.bad)break;
    }
  }
  if(state.bad||state.at!=bodyEnd||versionCount!=1||wordsCount!=1){if(report!=null)report.malformed=true;setSlot(v,"Refused",true);return false;}
  local parsed=parseVocabulary(words);
  if(!parsed[2]){if(report!=null)report.malformed=true;setSlot(v,"Refused",true);return false;}
  setSlot(v,"Ids",parsed[0]);setSlot(v,"Entries",parsed[1]);v.Count<-v.Ids.len();local refs=0, cap=v.Count;while(cap>0){refs++;cap=cap>>1;}v.RefBits<-refs;if(v.RefBits==0)v.RefBits=1;
  v.BuildVersion<-build;setSlot(v,"Announced",true);return true;
};

_M.BuildVersion <- function(bytes) {
  // The announcement's version is the eight-byte payload after the form,
  // version reference and kind (section 3.3).
  local lo = 0, hi = 0;
  for (local i = 0; i < 4; i++) lo = lo | ((bytes[3+i] & 0xff) << (8*i));
  for (local i = 0; i < 4; i++) hi = hi | ((bytes[7+i] & 0xff) << (8*i));
  return lo | (hi << 32);
};

local newWriter = function(buffer, measuring) {
  // owner and field name the refusal a deprecated variant makes
  // (docs/SPEC-TABLES.md §2.15)
  return { b = buffer, bits = 0, measuring = measuring, bad = false,
    owner = null, field = null, reason = null };
};
local put = function(w, value, n) {
  if (n < 0 || n > 64) { w.bad = true; return; }
  if (n == 0) return;
  local start = w.bits; w.bits += n;
  if (w.measuring) return;
  if (w.bits > w.b.len() * 8) { w.bad = true; return; }
  for (local i = 0; i < n; i++) {
    local at = (start + i) / 8, bit = (start + i) % 8;
    local mask = 1 << bit;
    if (((value >> i) & 1) != 0) w.b[at] = (w.b[at] | mask) & 0xff;
    else w.b[at] = (w.b[at] & (~mask)) & 0xff;
  }
};
local get = function(r, n) {
  if (n < 0 || n > 64 || r.bits + n > r.b.len() * 8) { r.bad = true; return 0; }
  local value = 0;
  for (local i = 0; i < n; i++) {
    local at = (r.bits + i) / 8, bit = (r.bits + i) % 8;
    if (((r.b[at] >> bit) & 1) != 0) value = value | (1 << i);
  }
  r.bits += n;
  return value;
};
local skip = function(r, n) {
  if (n < 0 || r.bits + n > r.b.len() * 8) { r.bad = true; return false; }
  r.bits += n; return true;
};
local alignWrite = function(w) { local n = (-w.bits) & 7; put(w, 0, n); };
local alignRead = function(r) {
  local n = (-r.bits) & 7, v = get(r, n);
  return !r.bad && v == 0;
};
// Float32 message scalars narrow and widen by the same bit surgery as the
// file-form runtime; castf2i/casti2f reinterpret binary64 values only.
local messageRoundShift = function(full, shift) {
  if (shift <= 0) return full << (-shift);
  local q = full >>> shift, rem = full & ((1 << shift) - 1), half = 1 << (shift - 1);
  if (rem > half || (rem == half && (q & 1) != 0)) q++;
  return q;
};
messageNarrowF32 = function(x) {
  local b=castf2i(x), sign=(b >>> 63)&1, exp=(b >>> 52)&0x7ff, mant=b&0xfffffffffffff;
  if(exp==0x7ff){if(mant==0)return (sign<<31)|0x7f800000;local payload=(mant>>>29)&0x007fffff;if(payload==0)payload=0x00400000;return (sign<<31)|0x7f800000|payload;}
  if(exp==0)return sign<<31;
  local e=exp-1023;
  if(e < -126){local shift=-e-97;if(shift>=54)return sign<<31;local full=0x10000000000000|mant,q=messageRoundShift(full,shift);if(q>=0x800000)return (sign<<31)|0x00800000;return (sign<<31)|(q&0x007fffff);}
  local q=messageRoundShift(0x10000000000000|mant,29);if(q>=0x1000000){q=q>>>1;e++;}
  if(e>127)return (sign<<31)|0x7f800000;
  return (sign<<31)|((e+127)<<23)|(q&0x007fffff);
};
messageWidenF32 = function(bits) {
  bits=bits.tointeger()&0xffffffff;local sign=bits&0x80000000,exp=(bits>>>23)&0xff,mant=bits&0x007fffff,high=0;
  if(sign!=0)high=0x8000000000000000;
  if(exp==0xff){high=high|0x7ff0000000000000;if(mant!=0)high=high|(mant<<29);}
  else if(exp==0){if(mant!=0){local e=-126;while((mant&0x800000)==0){mant=mant<<1;e--;}mant=mant&0x007fffff;high=high|((e+1023)<<52)|(mant<<29);}}
  else high=high|((exp-127+1023)<<52)|(mant<<29);
  return casti2f(high);
};
// f32 rounds a double to the nearest float32 and back, exactly
f32 = function(x) { return messageWidenF32(messageNarrowF32(x.tofloat())); };
// THE ONE RULE (§3.3): a finite float32 read into a COMPRESSED float32 field
// clamps to the declared range and counts, a -0 lands as +0, and a float32
// widened into a float64 field clamps nothing (the Go engine's clampFloat)
local messageClampFloat = function(r, fd, v) {
  if (!fd.f32 || fd.fmin == null) return v;
  if (v < fd.fmin) { r.clamped++; return f32(fd.fmin); }
  if (v > fd.fmax) { r.clamped++; return f32(fd.fmax); }
  if (v == 0.0) return 0.0;
  return v;
};
local bitCount = function(n) {
  local bits = 0;
  while (n > 0) { bits++; n = n >> 1; }
  return bits;
};
local skipRemoteEntry;
local skipRemoteBody;
// THE UNKNOWN SKIP WALK IS BOUNDED AT DEPTH 128 (docs/SPEC-TABLES.md §3.3,
// message_skip_depth): every reader-side skip starts at 0, a body's fields are
// skipped at the body's own depth, and a nested body, a union arm or a walked
// element is one level down; past 128 the batch is malformed, and 128 itself
// still skips (internal/codegen/darttable/messageform_batch.go, skipEntry).
skipRemoteEntry = function(T,v,r,e,depth) {
  if(depth>128){r.bad=true;return;}
  local sh=e.Shape,k=sh.Kind;
  if(k==0||k==32)return;
  if(k==30){local ref=get(r,v.RefBits);if(r.bad||ref==0)return;if(ref>v.Count){r.bad=true;return;}local en=v.Entries[ref-1];if(en==null||en.Shape.Kind!=0||(en.Id==0xfffffffffffffffd||en.Id==0xfffffffffffffffe||en.Id==0xffffffffffffffff))r.bad=true;return;}
  if(k==13){skipRemoteBody(T,v,r,depth+1);return;}
  if(k==15){local ref=get(r,v.RefBits);if(r.bad||ref==0)return;if(ref>v.Count){r.bad=true;return;}local arm=v.Entries[ref-1];if(arm==null||arm.Shape.Kind==0||(arm.Id==0xfffffffffffffffd||arm.Id==0xfffffffffffffffe||arm.Id==0xffffffffffffffff)){r.bad=true;return;}skipRemoteEntry(T,v,r,arm,depth+1);return;}
  if(k==12||k==33){local n=get(r,sh.Bits);if(r.bad||n>sh.Max){r.bad=true;return;}if(k==12&&!alignRead(r)){r.bad=true;return;}skip(r,n*(k==12?8:16));return;}
  if(k==31){if(!alignRead(r)){r.bad=true;return;}local n=get(r,32);if(r.bad)return;skip(r,n*8);return;}
  if(k==14||k==16){local n=get(r,sh.Bits)+sh.Min;if(r.bad||n>sh.Max){r.bad=true;return;}if(k==14&&e.Element.Kind==6&&!alignRead(r)){r.bad=true;return;}for(local i=0;i<n;i++){if(k==16){local key=get(r,v.RefBits);if(r.bad||key==0||key>v.Count){r.bad=true;return;}local ke=v.Entries[key-1];if(ke.Shape.Kind!=0||(ke.Id==0xfffffffffffffffd||ke.Id==0xfffffffffffffffe||ke.Id==0xffffffffffffffff)){r.bad=true;return;}}skipRemoteEntry(T,v,r,e.Element,depth+1);if(r.bad)return;}return;}
  if(k==17){r.bad=true;return;}
  if(sh.Bits<0){r.bad=true;return;}skip(r,sh.Bits);
};
skipRemoteBody = function(T,v,r,depth) {
  if(depth>128){r.bad=true;return;}
  for(;;){local ref=get(r,v.RefBits);if(r.bad||ref==0)return;if(ref>v.Entries.len()){r.bad=true;return;}local entry=v.Entries[ref-1];if(entry.Id==0xfffffffffffffffd||entry.Id==0xfffffffffffffffe||entry.Id==0xffffffffffffffff){r.bad=true;return;}skipRemoteEntry(T,v,r,entry,depth);if(r.bad)return;}
};
local findSlot = function(T, id) {
  for (local i = 0; i < T.TableMessageIds.len(); i++) if (T.TableMessageIds[i] == id) return i + 1;
  return 0;
};
local idAt = function(v, ref) {
  if (v == null || ref <= 0 || ref > v.Ids.len()) return null;
  return v.Ids[ref - 1];
};
local findField = function(T, id) {
  foreach (name, d in T.Descriptors) foreach (f in d.fields) if (f.id == id) return f;
  return null;
};
local numberBits = function(fd) {
  if (fd.shape == "bool") return 1;
  if (fd.shape == "flags") return fd.flagbits;
  if (fd.lo != null && fd.hi != null) return bitCount(fd.hi - fd.lo);
  if (fd.bits > 0) return fd.bits;
  return fd.width * 8;
};
// WIDE TEXT ON THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3): kind 33's
// content rule, SURROGATES PAIRED and no zero unit (wideValid in
// internal/tablewire/text.go), and its clamp, which never splits a pair
// (wideBoundary). Here ill-formed text is damage and TERMINAL: a bit stream
// has no defined position to continue from.
local wideValid = function(units) {
  local n = units.len();
  for (local i = 0; i < n; i++) {
    local u = units[i];
    if (u == 0) return false;
    if (u >= 0xD800 && u <= 0xDBFF) {
      if (i + 1 >= n || units[i + 1] < 0xDC00 || units[i + 1] > 0xDFFF) return false;
      i++;
    } else if (u >= 0xDC00 && u <= 0xDFFF) return false;
  }
  return true;
};
local wideClamp = function(units, bound) {
  local cut = bound;
  if (cut > 0 && units[cut - 1] >= 0xD800 && units[cut - 1] <= 0xDBFF) cut--;
  return units.slice(0, cut);
};

local defaultValue = function(fd, value, owner) {
  if (("optional" in fd) && fd.optional) return !owner[fd.member + "Present"];
  if (fd.shape == "str") return value == fd.def;
  if (fd.shape == "wstr") return value.len() == 0;
  if (fd.shape == "bytes") {
    if (value.len() != fd.def.len()) return false;
    for (local i = 0; i < value.len(); i++) if (value[i] != fd.def[i]) return false;
    return true;
  }
  if (fd.shape == "arr") {
    if ((("list" in fd) && fd.list) || (("counted" in fd) && fd.counted)) return value.len() == 0;
    if (fd.elem.shape == "tab" || fd.elem.shape == "union") return false;
    // a float element compares its BITS (docs/SPEC-TABLES.md §3.3): a -0
    // element under a +0 default rides the array
    if (fd.elem.shape == "float") {
      foreach (x in value) {
        if (fd.elem.f32) { if (messageNarrowF32(x.tofloat()) != messageNarrowF32(fd.elem.def.tofloat())) return false; }
        else if (castf2i(x.tofloat()) != castf2i(fd.elem.def.tofloat())) return false;
      }
      return true;
    }
    for (local i = 0; i < value.len(); i++) if (value[i] != fd.elem.def) return false;
    return true;
  }
  if (fd.shape == "union") return value.Type == 0;
  if (fd.shape == "ptr" || fd.shape == "sblob" || fd.shape == "bblob") return value == null;
  if (fd.shape == "tab") return false;
  // ELISION COMPARES A FLOAT'S BITS (docs/SPEC-TABLES.md §3.3): -0 under a +0
  // default rides, and a float32 compares at float32
  if (fd.shape == "float") {
    // a COMPRESSED float compares by value at float32, as the engine's
    // cellIsDefault does: its -0 is +0 (§3.3), so it elides as +0 does
    if (fd.f32 && fd.fmin != null) return f32(value) == f32(fd.def);
    if (fd.f32) return messageNarrowF32(value.tofloat()) == messageNarrowF32(fd.def.tofloat());
    return castf2i(value.tofloat()) == castf2i(fd.def.tofloat());
  }
  return value == fd.def;
};

local writeBody;
local writePayload;
local readBody;
local readPayload;
local skipPayload;
local kindWidens;
local readBatch;

local writeScalar = function(T, w, fd, value) {
  local kind = fd.kind, bits = numberBits(fd);
  if (kind == 1) { put(w, value ? 1 : 0, 1); return; }
  if (fd.shape == "enum") {
    local ref = 0;
    if (value != null) foreach (p in fd.variants) {
      if (p[0] == value) {
        if (p.len() > 2 && p[2]) {
          // A STRICT WRITE REFUSES A VALUE HOLDING A DEPRECATED VARIANT
          // (docs/SPEC-TABLES.md §2.15): the value is the caller's.
          local where = (w.owner == null ? "" : w.owner + ".") + (w.field == null ? "" : w.field);
          w.reason = "invalid_value: " + where + " names the deprecated variant " + value;
          w.bad = true; return;
        }
        ref = findSlot(T, p[1]);
        break;
      }
    }
    put(w, ref, T.TableMessageRefBitsHere); return;
  }
  if (fd.shape == "float") {
    // A NaN OR AN INFINITY IS REFUSED BEFORE THE QUANTIZER RUNS, never
    // clamped (docs/SPEC-TABLES.md §3.3), and a float32 must stay finite at
    // float32: the write refuses, non_finite_float: <Owner>.<field>
    value = value.tofloat();
    local finite = fd.f32 ? (messageNarrowF32(value) & 0x7f800000) != 0x7f800000
                          : (castf2i(value) & 0x7ff0000000000000) != 0x7ff0000000000000;
    if (!finite) { w.reason = "non_finite_float: " + fd.nf; w.bad = true; return; }
    if (fd.f32 && fd.qcount > 0) {
      // ir.TableMessageQuantize, BIT FOR BIT: every operation rounds to
      // float32 as Go's explicit conversions do; a float32 add, subtract,
      // multiply or divide computed in a double and rounded once is exactly
      // the float32 operation
      local v=f32(value), norm=f32(f32(v-fd.qmin)/fd.qdelta);
      if(!(norm>=0))norm=0.0;else if(!(norm<=1))norm=1.0;
      local scaled=f32(norm*f32(fd.qcount.tofloat()));
      local index=floor(f32(scaled+0.5)).tointeger();
      if(index>fd.qcount)index=fd.qcount;
      put(w,index,fd.qbits);
    } else if (fd.f32) put(w, messageNarrowF32(value), 32);
    else put(w, castf2i(value), 64);
    return;
  }
  local raw = value;
  if (fd.shape == "int" && fd.lo != null) raw = value - fd.lo;
  put(w, raw, bits);
};

local readScalar = function(r, T, v, fd, shape) {
  if (fd.shape == "bool") return get(r, 1) != 0;
  if (fd.shape == "enum") {
    local ref = get(r, v.RefBits), id = idAt(v, ref);
    if (ref == 0) return null;
    if (id == null) { r.bad = true; return null; }
    local entry=v.Entries[ref-1];
    if(entry.Shape.Kind!=0||(entry.Id==0xfffffffffffffffd||entry.Id==0xfffffffffffffffe||entry.Id==0xffffffffffffffff)){r.bad=true;return null;}
    local name = null, dep = false;
    foreach (p in fd.variants) {
      if (p[1] == id) { name = p[0]; dep = p.len() > 2 && p[2]; break; }
    }
    if (name == null) { r.unknown++; return null; }
    // a DEPRECATED VARIANT READS AS THE FIELD'S DECLARED DEFAULT, never
    // unknown (docs/SPEC-TABLES.md §2.15); an element's fd is its element
    // descriptor, whose default is the element default
    return dep ? fd.def : name;
  }
  if (fd.shape == "float") {
    // A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS (docs/SPEC-TABLES.md
    // §3.3), before the bits become a value: the first name wins and the
    // batch refuses whole (LoadMessages). A quantized float's index always
    // names a finite value.
    if (shape.Kind==11) {local raw=get(r,64);if(r.bad)return 0.0;if((raw&0x7ff0000000000000)==0x7ff0000000000000){if(r.nonFinite==null)r.nonFinite=fd.nf;return 0.0;}return casti2f(raw);}
    if (shape.Kind==10 && shape.Packing!=2) {local raw=get(r,32);if(r.bad)return 0.0;if((raw&0x7f800000)==0x7f800000){if(r.nonFinite==null)r.nonFinite=fd.nf;return 0.0;}return messageClampFloat(r,fd,messageWidenF32(raw));}
    // ir.TableMessageDequantize, bit for bit: an index above the step count is
    // damage and never clamped (§3.3, SPEC.md §4.3), every product rounds to
    // float32, and the float lands under the reader's own declared range
    if (shape.Kind==10 && shape.Packing==2) {local index=get(r,shape.Bits);if(r.bad)return 0.0;if(index>shape.QCount){r.bad=true;return 0.0;}local n=f32(f32(index.tofloat())/f32(shape.QCount.tofloat()));local scaled=f32(n*shape.QDelta);return messageClampFloat(r,fd,f32(scaled+shape.QMin));}
    if (shape.Kind==10) return messageWidenF32(get(r, 32));
    return casti2f(get(r, 64));
  }
  local bits = shape.Bits, x = get(r, bits);
  if (shape.Packing == 1) x += shape.Base;
  else {
    if (shape.Kind>=2 && shape.Kind<=5) {local sw=8<<(shape.Kind-2);if(sw<64 && (x&(1<<(sw-1)))!=0)x-=(1<<sw);}
  }
  if (fd.shape == "int" && fd.lo != null && fd.hi != null) {
    if (fd.kind == 9) {
      local MIN = 0x8000000000000000;
      if ((x ^ MIN) < (fd.lo ^ MIN)) { x = fd.lo; r.clamped++; }
      else if ((x ^ MIN) > (fd.hi ^ MIN)) { x = fd.hi; r.clamped++; }
    } else if(x<fd.lo){x=fd.lo;r.clamped++;}else if(x>fd.hi){x=fd.hi;r.clamped++;}
  }
  return x;
};

writePayload = function(T, w, fd, value, depth) {
  if (depth > 128) { w.bad = true; return; }
  if (fd.shape == "str") {
    local n = value.len(); if (n > fd.bound) { w.bad = true; return; } put(w, n, bitCount(fd.bound)); alignWrite(w);
    for (local i = 0; i < n; i++) put(w, value[i] & 0xff, 8); return;
  }
  if (fd.shape == "bytes") {
    local n = value.len(); if (n > fd.bound) { w.bad = true; return; } put(w, n, bitCount(fd.bound)); alignWrite(w);
    for (local i = 0; i < n; i++) put(w, value[i] & 0xff, 8); return;
  }
  if (fd.shape == "wstr") {
    // the length at bits_required(0, N), NO align, then sixteen bits a code
    // unit (§3.3); the used length is checked, the content is not (§3)
    local n = value.len(); if (n > fd.bound) { w.bad = true; return; } put(w, n, bitCount(fd.bound));
    foreach (u in value) { if (typeof u != "integer" || u < 0 || u > 0xFFFF) { w.bad = true; return; } put(w, u, 16); }
    return;
  }
  if (fd.shape == "tab") { writeBody(T, w, T.Descriptors[fd.table], value, depth + 1); return; }
  if (fd.shape == "union") {
    local ud = T[fd.table + "Descriptor"], arm = null;
    foreach (a in ud.arms) if (a.tag == value.Type) arm = a;
    if (arm == null) { put(w, 0, T.TableMessageRefBitsHere); return; }
    local ref = findSlot(T, arm.id); put(w, ref, T.TableMessageRefBitsHere);
    if (arm.payload != null) writePayload(T, w, arm.payload, value[arm.member], depth + 1);
    return;
  }
  if (fd.shape == "arr") {
    // §3.3: kind 14 bytes count/alignment/raw octets; kind 16 count then key/element pairs.
    local n = value.len();
    if (n < fd.min || n > fd.bound) { w.bad = true; return; }
    local keyed = (fd.keyvariants != null), present = 0;
    if (keyed) { for(local i=0;i<n;i++) if(!defaultValue(fd.elem,value[i],value)) present++; put(w,present,bitCount(fd.bound-fd.min)); }
    else if (fd.list || fd.counted || fd.elem.shape == "bytes") put(w, n - fd.min, bitCount(fd.bound - fd.min));
    local e = fd.elem;
    if (!keyed && e.shape == "bytes") {
      alignWrite(w);
      for(local i=0;i<n;i++) put(w,value[i]&0xff,8);
      return;
    }
    for (local i = 0; i < n; i++) {
      if(keyed){if(defaultValue(e,value[i],value))continue;local key=findSlot(T,fd.keyvariants[i][1]);if(key==0){w.bad=true;return;}put(w,key,T.TableMessageRefBitsHere);}
      writePayload(T, w, e, value[i], depth + 1);
    }
    return;
  }
  writeScalar(T, w, fd, value);
};

skipPayload = function(T, v, r, fd, depth) {
  if (depth > 128) { r.bad = true; return; }
  if (fd.shape == "str" || fd.shape == "bytes") {
    local n = get(r, bitCount(fd.bound)); if (r.bad || n > fd.bound) { r.bad = true; return; }
    if (!alignRead(r)) { r.bad = true; return; } skip(r, n * 8); return;
  }
  if (fd.shape == "wstr") {
    local n = get(r, bitCount(fd.bound)); if (r.bad || n > fd.bound) { r.bad = true; return; }
    skip(r, n * 16); return;
  }
  if (fd.shape == "tab") { local d = T.Descriptors[fd.table]; for (;;) { local ref=get(r,v.RefBits); if(r.bad||ref==0)return; local id=idAt(v,ref); local f=id==null?null:d.byId[id]; if(f==null){r.bad=true;return;} skipPayload(T,v,r,f,depth+1); } }
  if (fd.shape == "union") {
    local ref=get(r,v.RefBits); if(r.bad||ref==0)return;
    local id=idAt(v,ref), ud=T[fd.table+"Descriptor"], arm=null;
    if(id==null){r.bad=true;return;} foreach(a in ud.arms) if(a.id==id) arm=a;
    if(arm==null){r.bad=true;return;} if(arm.payload!=null) skipPayload(T,v,r,arm.payload,depth+1); return;
  }
  if (fd.shape == "arr") {
    // §3.3: consume byte alignment and raw kind-6 elements, or keyed refs before each element.
    local n = fd.list || fd.counted || fd.elem.shape == "bytes" || (fd.keyvariants != null) ? get(r, bitCount(fd.bound-fd.min)) + fd.min : fd.bound;
    if(fd.elem.shape == "bytes") { if(!alignRead(r)){r.bad=true;return;} skip(r,n*8); return; }
    for(local i=0;i<n;i++){if((fd.keyvariants != null)){local key=get(r,v.RefBits);if(r.bad||key==0||key>v.Count){r.bad=true;return;}local ke=v.Entries[key-1];if(ke.Shape.Kind!=0||(ke.Id==0xfffffffffffffffd||ke.Id==0xfffffffffffffffe||ke.Id==0xffffffffffffffff)){r.bad=true;return;}}skipPayload(T,v,r,fd.elem,depth+1);} return;
  }
  if(fd.shape=="enum"){local ref=get(r,v.RefBits);if(ref>v.Count)r.bad=true;return;}
  skip(r, numberBits(fd));
};

readPayload = function(T, v, r, fd, owner, member, depth, entry) {
  if (depth > 128) { r.bad = true; return; }
  if(entry.Kind!=fd.kind&&!kindWidens(entry.Kind,fd.kind)){r.kindMismatch++;skipRemoteEntry(T,v,r,entry,0);return;}
  if(entry.Kind!=fd.kind)r.widened++;
  if (fd.shape == "str") {
    local n=get(r,entry.Shape.Bits);if(r.bad||n>entry.Shape.Max){r.bad=true;return;}
    if(!alignRead(r)){r.bad=true;return;}
    local s="";if(n>fd.bound)r.clamped++;for(local i=0;i<n;i++){local c=get(r,8)&0xff;if(i<fd.bound)s+=format("%c",c);}
    owner[member]=s;return;
  }
  if (fd.shape == "bytes") {
    local n=get(r,entry.Shape.Bits);if(r.bad||n<entry.Shape.Min||n>entry.Shape.Max){r.bad=true;return;}
    if(!alignRead(r)){r.bad=true;return;}
    local kept=n<fd.bound?n:fd.bound;if(kept<n)r.clamped++;
    local b=blob(kept);for(local i=0;i<n;i++){local x=get(r,8)&0xff;if(i<kept)b[i]=x;}
    owner[member]=b;return;
  }
  if (fd.shape == "wstr") {
    local n=get(r,entry.Shape.Bits);if(r.bad||n>entry.Shape.Max||r.bits+n*16>r.b.len()*8){r.bad=true;return;}
    local units=array(n);for(local i=0;i<n;i++)units[i]=get(r,16);
    if(r.bad||!wideValid(units)){r.bad=true;return;}
    if(n>fd.bound){r.clamped++;units=wideClamp(units,fd.bound);}
    owner[member]=units;return;
  }
  if(fd.shape=="tab"){local child=T.Descriptors[fd.table].make();readBody(T,v,r,T.Descriptors[fd.table],child,depth+1);owner[member]=child;return;}
  if(fd.shape=="union"){
    local ref=get(r,v.RefBits);if(r.bad)return;
    local id=idAt(v,ref),ud=T[fd.table+"Descriptor"],u=ud.make(),arm=null;
    if(ref!=0&&id==null){r.bad=true;return;}
    if(ref!=0){local entry=v.Entries[ref-1];if(entry.Shape.Kind==0||(entry.Id==0xfffffffffffffffd||entry.Id==0xfffffffffffffffe||entry.Id==0xffffffffffffffff)){r.bad=true;return;}}
    foreach(a in ud.arms)if(a.id==id)arm=a;
    if(arm==null&&ref!=0){if(ref>v.Count){r.bad=true;return;}r.unknown++;skipRemoteEntry(T,v,r,v.Entries[ref-1],0);owner[member]=u;return;}
    if(arm!=null){u.Type=arm.tag;if(arm.payload!=null)readPayload(T,v,r,arm.payload,u,arm.member,depth+1,v.Entries[ref-1]);}
    owner[member]=u;return;
  }
  if(fd.shape=="arr"){
    // §3.3: kind 14 counts and aligns byte elements; kind 16 restores each element by key name.
    local shape=entry.Shape,n=get(r,shape.Bits)+shape.Min;
    if(r.bad||n<shape.Min||n>shape.Max){r.bad=true;return;}
    local kept=n<fd.bound?n:fd.bound;if(kept<n)r.clamped++;
    if(entry.Element.Kind==6&&!alignRead(r)){r.bad=true;return;}
    local keyed=(fd.keyvariants != null),a=keyed?owner[member]:[];if(!keyed){local size=((fd.list||fd.counted)?kept:fd.bound);for(local j=0;j<size;j++)a.append(fd.elem.def);}
    for(local i=0;i<n;i++){
      local index=i;
      if(keyed){local key=get(r,v.RefBits);if(r.bad||key==0||key>v.Count){r.bad=true;return;}local ke=v.Entries[key-1];if(ke.Shape.Kind!=0||(ke.Id==0xfffffffffffffffd||ke.Id==0xfffffffffffffffe||ke.Id==0xffffffffffffffff)){r.bad=true;return;}index=-1;for(local j=0;j<fd.keyvariants.len();j++)if(fd.keyvariants[j][1]==ke.Id)index=j;if(index<0){r.unknown++;skipRemoteEntry(T,v,r,entry.Element,0);if(r.bad)return;continue;}}
      if(entry.Element.Kind!=fd.elem.kind&&!kindWidens(entry.Element.Kind,fd.elem.kind)){r.kindMismatch++;skipRemoteEntry(T,v,r,entry.Element,0);if(r.bad)return;continue;}
      local holder={value=null};readPayload(T,v,r,fd.elem,holder,"value",depth+1,entry.Element);if(index<kept)a[index]=holder.value;if(r.bad)return;
    }
    owner[member]=a;return;
  }
  owner[member]=readScalar(r,T,v,fd,entry.Shape);
};

writeBody = function(T, w, d, value, depth) {
  if(depth>128){w.bad=true;return;}
  w.owner = d.name;
  foreach(fd in d.fields){
    if(!(fd.member in value))continue;
    local val=value[fd.member];
    if(defaultValue(fd,val,value))continue;
    local ref=findSlot(T,fd.id);if(ref==0){w.bad=true;return;}
    put(w,ref,T.TableMessageRefBitsHere);
    w.field = fd.name;
    writePayload(T,w,fd,val,depth+1);
    if(w.bad)return;
  }
  put(w,0,T.TableMessageRefBitsHere);
};

kindWidens = function(kind, declared) {
  if(declared>=3&&declared<=5)return kind>=2&&kind<declared;
  if(declared==18)return kind>=2&&kind<=5;
  if(declared>=7&&declared<=9)return kind>=6&&kind<declared;
  if(declared==19)return kind>=6&&kind<=9;
  return declared==11&&kind==10;
};

readBody = function(T, v, r, d, value, depth) {
  if(depth>128){r.bad=true;return;}
  for(;;){
    local ref=get(r,v.RefBits);if(r.bad)return;
    if(ref==0)return;
    local id=idAt(v,ref);if(id==null||ref>v.Entries.len()){r.bad=true;return;}
    if(id==0xfffffffffffffffd||id==0xfffffffffffffffe||id==0xffffffffffffffff){r.bad=true;return;}
    local entry=v.Entries[ref-1],fd=null;if(id in d.byId)fd=d.byId[id];
    if(fd==null){r.unknown++;skipRemoteEntry(T,v,r,entry,0);if(r.bad)return;continue;}
    if(entry.Kind!=fd.kind&&!kindWidens(entry.Kind,fd.kind)){r.kindMismatch++;skipRemoteEntry(T,v,r,entry,0);if(r.bad)return;continue;}
    if(fd.optional)value[fd.member + "Present"] = true;
    readPayload(T,v,r,fd,value,fd.member,depth+1,entry);
    if(r.bad)return;
  }
};

local writeBatch = function(T, root, values, out, measuring, report) {
  reset(report);
  if(values.len()<1)return refuse(report,"empty_batch");
  if(values.len()>256)return refuse(report,"batch_too_large");
  local d=T.Descriptors[root];
  if(d==null||!d.fixed)return refuse(report,"message_form_plain_table:"+root);
  local w=newWriter(out,measuring);put(w,2,8);put(w,values.len()-1,8);

  foreach(v in values){writeBody(T,w,d,v,0);if(w.bad){if(report!=null){report.malformed<-true;report.refused<-true;if(w.reason!=null)report.reason<-w.reason;}return -1;}}
  local pad=(-w.bits)&7;put(w,0,pad);
  if(w.bad){if(report!=null){report.refused<-true;if(w.reason!=null)report.reason<-w.reason;}return -1;}
  return (w.bits+7)/8;
};

_M.MeasureMessages <- function(T, root, values, report) {
  return writeBatch(T,root,values,null,true,report);
};
_M.SaveMessages <- function(T, root, values, out, report) {
  return writeBatch(T,root,values,out,false,report);
};
_M.LoadMessages <- function(T, root, storage, v, bytes, report) {
  reset(report);
  if(v==null||!("Announced" in v)||!v.Announced)return refuse(report,"no_vocabulary");
  if(bytes==null||bytes.len()<2){if(report!=null)report.malformed<-true;return -1;}
  if((bytes[0]&0xff)!=2)return refuse(report,"newer_form");
  local count = (bytes[1]&0xff)+1;
  if(count>storage.len())return refuse(report,"batch_too_large");
  local d=T.Descriptors[root];
  if(d==null||!d.fixed)return refuse(report,"message_form_plain_table:"+root);
  if(storage.len()<count)return refuse(report,"batch_too_large");
  // THE BATCH AS AN ARRAY OF BYTES: indexing a blob is a native metamethod,
  // and the interpreter cannot grow its stack inside one, so a deep but legal
  // nesting would die in the bit reader rather than at the depth bound.
  local raw=array(bytes.len());for(local i=0;i<raw.len();i++)raw[i]=bytes[i]&0xff;
  local r={b=raw,bits=16,bad=false,unknown=0,kindMismatch=0,widened=0,clamped=0,nonFinite=null};
  local walked=readBatch(T,v,r,d,storage,count,bytes,report);
  // A NON-FINITE FLOAT IN ANY BODY REFUSES THE BATCH (docs/SPEC-TABLES.md
  // §3.3), outranking every other verdict: no body of it is delivered
  if(r.nonFinite!=null)return refuse(report,"non_finite_float: "+r.nonFinite);
  return walked;
};
readBatch = function(T, v, r, d, storage, count, bytes, report) {
  for(local k=0;k<count;k++){
    local before=r.bits;readBody(T,v,r,d,storage[k],0);
    if(report!=null){report.unknown+=r.unknown;report.kindMismatch+=r.kindMismatch;report.widened+=r.widened;report.clamped+=r.clamped;}
    r.unknown=0;r.kindMismatch=0;r.widened=0;r.clamped=0;
    if(r.bad){if(report!=null)report.malformed<-true;return k;}
    if(r.bits==before){if(report!=null)report.malformed<-true;return k;}
  }
  if(!alignRead(r)||r.bits!=bytes.len()*8){if(report!=null)report.malformed<-true;return -1;}
  return count;
};
