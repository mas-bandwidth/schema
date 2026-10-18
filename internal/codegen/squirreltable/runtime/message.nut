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
local shapeEnd;
shapeEnd = function(b, state, kind) {
  if ((kind>=2 && kind<=10) || (kind>=18 && kind<=29)) {
    if (state.at>=state.end) { state.bad=true; return; }
    local packing=b[state.at]&0xff; state.at++;
    if (packing==0) return;
    if (packing==1 && kind!=10) {
      local bits=readLeb(b,state);
      if(state.bad || bits>128){state.bad=true;return;}
      if(kind>=18){if(state.at+16>state.end){state.bad=true;return;}state.at+=16;}
      else readLeb(b,state);
      return;
    }
    if(packing==2 && kind==10){if(state.at+12>state.end){state.bad=true;return;}state.at+=12;return;}
    state.bad=true; return;
  }
  if(kind==12 || kind==33){readLeb(b,state);return;}
  if(kind==14 || kind==16){
    if(kind==14) readLeb(b,state);
    readLeb(b,state);
    if(state.at>=state.end){state.bad=true;return;}
    local elem=b[state.at]&0xff;state.at++;
    if(elem>33 || elem==12 || elem==33){state.bad=true;return;}
    shapeEnd(b,state,elem);return;
  }
};
local parseVocabulary = function(words) {
  local state={b=words,at=0,end=words.len(),bad=false}, ids=[];
  while(state.at<state.end && !state.bad){
    if(state.at+9>state.end){state.bad=true;break;}
    local id=readU64(words,state.at);state.at+=8;
    local kind=words[state.at]&0xff;state.at++;
    if(kind>33 || id==0xfffffffffffffffd || id==0xfffffffffffffffe){state.bad=true;break;}
    shapeEnd(words,state,kind);
    if(state.bad)break;
    // A field id may appear in multiple table contexts with distinct shapes.
    // Keep each vocabulary slot: the body reference names its slot, not its id alone.
    ids.append(id);
    if(ids.len()>4096)state.bad=true;
  }
  if(state.at!=state.end)state.bad=true;
  return [ids,!state.bad];
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
_M.AnnounceRead <- function(T, v, bytes, report) {
  reset(report);
  if (v == null || typeof v != "table") return refuse(report, "vocabulary_storage_required");
  if (("Announced" in v) && v.Announced || ("Refused" in v) && v.Refused) return refuse(report, "second_announcement");
  if (bytes != null && equalBytes(bytes, T.TableMessageAnnouncement)) {
    local ownIds=[]; foreach(id in T.TableMessageIds) ownIds.append(id);
    v.Ids <- ownIds; v.Count <- v.Ids.len(); v.RefBits <- T.TableMessageRefBitsHere;
    v.BuildVersion <- _M.BuildVersion(bytes); v.Announced <- true; return true;
  }
  if (bytes == null || bytes.len()<9) { if(report!=null)report.malformed=true; if("Refused" in v)v.Refused=true;else v.Refused<-true; return false; }
  if ((bytes[0]&0xff)!=1) return refuse(report, (bytes[0]&0xff)==2 ? "message_form_as_file" : "newer_form");
  local total=bytes.len(), trailer=total-8, entries=readU64(bytes,trailer);
  if(entries>4096 || entries>(trailer-1)/8){if(report!=null)report.malformed=true;v.Refused<-true;return false;}
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
      if(wn>65536)return refuse(report,"vocabulary_too_large");
      words=blob(wn);for(local j=0;j<wn;j++)words[j]=bytes[state.at+j]&0xff;
      state.at=fieldEnd;wordsCount++;
    } else {
      if(report!=null)report.unknown++;
      skipFileKind(bytes,state,kind);if(state.bad)break;
    }
  }
  if(state.bad||state.at!=bodyEnd||versionCount!=1||wordsCount!=1){if(report!=null)report.malformed=true;v.Refused<-true;return false;}
  local parsed=parseVocabulary(words);
  if(!parsed[1]){if(report!=null)report.malformed=true;v.Refused<-true;return false;}
  v.Ids<-parsed[0];v.Count<-v.Ids.len();local refs=0, cap=v.Count;while(cap>0){refs++;cap=cap>>1;}v.RefBits<-refs;if(v.RefBits==0)v.RefBits=1;
  v.BuildVersion<-build;v.Announced<-true;return true;
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
  return { b = buffer, bits = 0, measuring = measuring, bad = false };
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
local bitCount = function(n) {
  local bits = 0;
  while (n > 0) { bits++; n = n >> 1; }
  return bits;
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
  if (fd.shape == "flags") return fd.variants != null ? fd.variants.len() : 0;
  if (fd.lo != null && fd.hi != null) return bitCount(fd.hi - fd.lo);
  if (fd.bits > 0) return fd.bits;
  return fd.width * 8;
};
local defaultValue = function(fd, value, owner) {
  if (fd.optional) return !owner[fd.member + "Present"];
  if (fd.shape == "str") return value == fd.def;
  if (fd.shape == "bytes") {
    if (value.len() != fd.def.len()) return false;
    for (local i = 0; i < value.len(); i++) if (value[i] != fd.def[i]) return false;
    return true;
  }
  if (fd.shape == "arr") {
    if (fd.list || fd.counted) return value.len() == 0;
    if (fd.elem.shape == "tab" || fd.elem.shape == "union") return false;
    for (local i = 0; i < value.len(); i++) if (value[i] != fd.elem.def) return false;
    return true;
  }
  if (fd.shape == "union") return value.Type == 0;
  if (fd.shape == "ptr" || fd.shape == "sblob" || fd.shape == "bblob") return value == null;
  if (fd.shape == "tab") return false;
  return value == fd.def;
};

local writeBody;
local writePayload;
local readBody;
local readPayload;
local skipPayload;

local writeScalar = function(T, w, fd, value) {
  local kind = fd.kind, bits = numberBits(fd);
  if (kind == 1) { put(w, value ? 1 : 0, 1); return; }
  if (fd.shape == "enum") {
    local ref = 0;
    if (value != null) foreach (p in fd.variants) if (p[0] == value) ref = findSlot(T, p[1]);
    put(w, ref, T.TableMessageRefBitsHere); return;
  }
  if (fd.shape == "float") {
    if (fd.f32) put(w, castf2i(value) & 0xffffffff, 32);
    else put(w, castf2i(value), 64);
    return;
  }
  local raw = value;
  if (fd.shape == "int" && fd.lo != null) raw = value - fd.lo;
  put(w, raw, bits);
};

local readScalar = function(r, T, v, fd) {
  if (fd.shape == "bool") return get(r, 1) != 0;
  if (fd.shape == "enum") {
    local ref = get(r, v.RefBits), id = idAt(v, ref);
    if (ref == 0) return null;
    if (id == null) { r.bad = true; return null; }
    foreach (p in fd.variants) if (p[1] == id) return p[0];
    r.unknown = true; return null;
  }
  if (fd.shape == "float") {
    if (fd.f32) return casti2f(get(r, 32));
    return casti2f(get(r, 64));
  }
  local bits = numberBits(fd), x = get(r, bits);
  if (fd.shape == "int" && fd.lo != null) x += fd.lo;
  if (fd.shape == "int" && fd.signed && fd.width * 8 < 64 && (x & (1 << (fd.width*8-1))) != 0) x -= (1 << (fd.width*8));
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
    local n = value.len();
    if (n < fd.min || n > fd.bound) { w.bad = true; return; }
    if (fd.list || fd.counted) put(w, n - fd.min, bitCount(fd.bound - fd.min));
    local e = fd.elem;
    for (local i = 0; i < n; i++) writePayload(T, w, e, value[i], depth + 1);
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
  if (fd.shape == "tab") { local d = T.Descriptors[fd.table]; for (;;) { local ref=get(r,v.RefBits); if(r.bad||ref==0)return; local id=idAt(v,ref); local f=id==null?null:d.byId[id]; if(f==null){r.bad=true;return;} skipPayload(T,v,r,f,depth+1); } }
  if (fd.shape == "union") {
    local ref=get(r,v.RefBits); if(r.bad||ref==0)return;
    local id=idAt(v,ref), ud=T[fd.table+"Descriptor"], arm=null;
    if(id==null){r.bad=true;return;} foreach(a in ud.arms) if(a.id==id) arm=a;
    if(arm==null){r.bad=true;return;} if(arm.payload!=null) skipPayload(T,v,r,arm.payload,depth+1); return;
  }
  if (fd.shape == "arr") {
    local n = fd.list || fd.counted ? get(r, bitCount(fd.bound-fd.min)) + fd.min : fd.bound;
    for(local i=0;i<n;i++) skipPayload(T,v,r,fd.elem,depth+1); return;
  }
  if(fd.shape=="enum"){local ref=get(r,v.RefBits);if(ref>v.Count)r.bad=true;return;}
  skip(r, numberBits(fd));
};

readPayload = function(T, v, r, fd, owner, member, depth) {
  if (depth > 128) { r.bad = true; return; }
  if (fd.shape == "str") {
    local n=get(r,bitCount(fd.bound));if(r.bad||n>fd.bound){r.bad=true;return;}
    if(!alignRead(r)){r.bad=true;return;}
    local s="";for(local i=0;i<n;i++)s+=format("%c",get(r,8)&0xff);
    owner[member]=s;return;
  }
  if (fd.shape == "bytes") {
    local n=get(r,bitCount(fd.bound));if(r.bad||n>fd.bound){r.bad=true;return;}
    if(!alignRead(r)){r.bad=true;return;}
    local b=blob(n);for(local i=0;i<n;i++)b[i]=get(r,8)&0xff;
    owner[member]=b;return;
  }
  if(fd.shape=="tab"){local child=T.Descriptors[fd.table].make();readBody(T,v,r,T.Descriptors[fd.table],child,depth+1);owner[member]=child;return;}
  if(fd.shape=="union"){
    local ref=get(r,v.RefBits);if(r.bad)return;
    local id=idAt(v,ref),ud=T[fd.table+"Descriptor"],u=ud.make(),arm=null;
    if(ref!=0&&id==null){r.bad=true;return;}
    foreach(a in ud.arms)if(a.id==id)arm=a;
    if(arm==null&&ref!=0){r.bad=true;return;}
    if(arm!=null){u.Type=arm.tag;if(arm.payload!=null)readPayload(T,v,r,arm.payload,u,arm.member,depth+1);}
    owner[member]=u;return;
  }
  if(fd.shape=="arr"){
    local n=fd.list||fd.counted?get(r,bitCount(fd.bound-fd.min))+fd.min:fd.bound;
    if(r.bad||n<fd.min||n>fd.bound){r.bad=true;return;}
    if(n>fd.bound){r.bad=true;return;}
    local a=[];
    for(local i=0;i<n;i++){local before=r.bits;local holder={value=null};readPayload(T,v,r,fd.elem,holder,"value",depth+1);a.append(holder.value);if(r.bad||r.bits==before){if(numberBits(fd.elem)==0)continue;r.bad=true;return;}}
    owner[member]=a;return;
  }
  owner[member]=readScalar(r,T,v,fd);
};

writeBody = function(T, w, d, value, depth) {
  if(depth>128){w.bad=true;return;}
  foreach(fd in d.fields){
    if(!(fd.member in value))continue;
    local val=value[fd.member];
    if(defaultValue(fd,val,value))continue;
    local ref=findSlot(T,fd.id);if(ref==0){w.bad=true;return;}
    put(w,ref,T.TableMessageRefBitsHere);
    writePayload(T,w,fd,val,depth+1);
    if(w.bad)return;
  }
  put(w,0,T.TableMessageRefBitsHere);
};

readBody = function(T, v, r, d, value, depth) {
  if(depth>128){r.bad=true;return;}
  for(;;){
    local ref=get(r,v.RefBits);if(r.bad)return;
    if(ref==0)return;
    local id=idAt(v,ref);if(id==null){r.bad=true;return;}
    local fd=null;if(id in d.byId)fd=d.byId[id];
    if(fd==null){local other=findField(T,id);if(other==null){r.bad=true;return;}skipPayload(T,v,r,other,depth+1);continue;}
    if(fd.optional)value[fd.member+"Present"] = true;
    readPayload(T,v,r,fd,value,fd.member,depth+1);
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

  foreach(v in values){writeBody(T,w,d,v,0);if(w.bad){if(report!=null)report.malformed<-true;return -1;}}
  local pad=(-w.bits)&7;put(w,0,pad);
  if(w.bad){if(report!=null)report.refused<-true;return -1;}
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
  local r={b=bytes,bits=16,bad=false};
  for(local k=0;k<count;k++){
    local before=r.bits;readBody(T,v,r,d,storage[k],0);
    if(r.bad){if(report!=null)report.malformed<-true;return k;}
    if(r.bits==before){if(report!=null)report.malformed<-true;return k;}
  }
  if(!alignRead(r)||r.bits!=bytes.len()*8){if(report!=null)report.malformed<-true;return -1;}
  return count;
};
