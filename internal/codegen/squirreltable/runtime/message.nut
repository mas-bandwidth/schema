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

_M.AnnounceRead <- function(T, v, bytes, report) {
  reset(report);
  if (v == null || typeof v != "table") return refuse(report, "vocabulary_storage_required");
  if (("Announced" in v) && v.Announced || ("Refused" in v) && v.Refused) return refuse(report, "second_announcement");
  if (bytes == null || !equalBytes(bytes, T.TableMessageAnnouncement)) {
    if (report != null) report.malformed <- true;
    if ("Refused" in v) v.Refused = true; else v.Refused <- true;
    return -1;
  }
  v.Count <- T.TableMessageEntriesHere;
  v.RefBits <- T.TableMessageRefBitsHere;
  v.BuildVersion <- _M.BuildVersion(T.TableMessageAnnouncement);
  v.Announced <- true;
  return true;
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
local idAt = function(T, ref) {
  if (ref <= 0 || ref > T.TableMessageIds.len()) return null;
  return T.TableMessageIds[ref - 1];
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

local readScalar = function(r, T, fd) {
  if (fd.shape == "bool") return get(r, 1) != 0;
  if (fd.shape == "enum") {
    local ref = get(r, T.TableMessageRefBitsHere), id = idAt(T, ref);
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

skipPayload = function(T, r, fd, depth) {
  if (depth > 128) { r.bad = true; return; }
  if (fd.shape == "str" || fd.shape == "bytes") {
    local n = get(r, bitCount(fd.bound)); if (r.bad || n > fd.bound) { r.bad = true; return; }
    if (!alignRead(r)) { r.bad = true; return; } skip(r, n * 8); return;
  }
  if (fd.shape == "tab") { local d = T.Descriptors[fd.table]; for (;;) { local ref=get(r,T.TableMessageRefBitsHere); if(r.bad||ref==0)return; local id=idAt(T,ref); local f=id==null?null:d.byId[id]; if(f==null){r.bad=true;return;} skipPayload(T,r,f,depth+1); } }
  if (fd.shape == "union") {
    local ref=get(r,T.TableMessageRefBitsHere); if(r.bad||ref==0)return;
    local id=idAt(T,ref), ud=T[fd.table+"Descriptor"], arm=null;
    if(id==null){r.bad=true;return;} foreach(a in ud.arms) if(a.id==id) arm=a;
    if(arm==null){r.bad=true;return;} if(arm.payload!=null) skipPayload(T,r,arm.payload,depth+1); return;
  }
  if (fd.shape == "arr") {
    local n = fd.list || fd.counted ? get(r, bitCount(fd.bound-fd.min)) + fd.min : fd.bound;
    for(local i=0;i<n;i++) skipPayload(T,r,fd.elem,depth+1); return;
  }
  if(fd.shape=="enum"){local ref=get(r,T.TableMessageRefBitsHere);if(ref>T.TableMessageEntriesHere)r.bad=true;return;}
  skip(r, numberBits(fd));
};

readPayload = function(T, r, fd, owner, member, depth) {
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
  if(fd.shape=="tab"){local child=T.Descriptors[fd.table].make();readBody(T,r,T.Descriptors[fd.table],child,depth+1);owner[member]=child;return;}
  if(fd.shape=="union"){
    local ref=get(r,T.TableMessageRefBitsHere);if(r.bad)return;
    local id=idAt(T,ref),ud=T[fd.table+"Descriptor"],u=ud.make(),arm=null;
    if(ref!=0&&id==null){r.bad=true;return;}
    foreach(a in ud.arms)if(a.id==id)arm=a;
    if(arm==null&&ref!=0){r.bad=true;return;}
    if(arm!=null){u.Type=arm.tag;if(arm.payload!=null)readPayload(T,r,arm.payload,u,arm.member,depth+1);}
    owner[member]=u;return;
  }
  if(fd.shape=="arr"){
    local n=fd.list||fd.counted?get(r,bitCount(fd.bound-fd.min))+fd.min:fd.bound;
    if(r.bad||n<fd.min||n>fd.bound){r.bad=true;return;}
    if(n>fd.bound){r.bad=true;return;}
    local a=[];
    for(local i=0;i<n;i++){local before=r.bits;local holder={value=null};readPayload(T,r,fd.elem,holder,"value",depth+1);a.append(holder.value);if(r.bad||r.bits==before){if(numberBits(fd.elem)==0)continue;r.bad=true;return;}}
    owner[member]=a;return;
  }
  owner[member]=readScalar(r,T,fd);
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

readBody = function(T, r, d, value, depth) {
  if(depth>128){r.bad=true;return;}
  for(;;){
    local ref=get(r,T.TableMessageRefBitsHere);if(r.bad)return;
    if(ref==0)return;
    local id=idAt(T,ref);if(id==null){r.bad=true;return;}
    local fd=null;if(id in d.byId)fd=d.byId[id];
    if(fd==null){local other=findField(T,id);if(other==null){r.bad=true;return;}skipPayload(T,r,other,depth+1);continue;}
    if(fd.optional)value[fd.member+"Present"] = true;
    readPayload(T,r,fd,value,fd.member,depth+1);
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
  local r={b=bytes,bits=16,bad=false};
  for(local k=0;k<count;k++){
    local before=r.bits;readBody(T,r,d,storage[k],0);
    if(r.bad){if(report!=null)report.malformed<-true;return k;}
    if(r.bits==before){if(report!=null)report.malformed<-true;return k;}
  }
  if(!alignRead(r)||r.bits!=bytes.len()*8){if(report!=null)report.malformed<-true;return -1;}
  return count;
};
