// test/conformance/squirrel/messagefuzz.nut — Form-2 differential message-fuzz driver.
local corpus = "build/conformance-squirrel";
local override = getenv("SCHEMA_SQUIRREL_CONFORMANCE");
if (override != null && override.len() > 0) corpus = override;

local input = ::stdin;
local out = ::stdout;

local take = function(n) {
  local b = blob(n);
  try {
    local words = n >> 3;
    for (local i = 0; i < words; i++) b.writen(input.readn('l'), 'l');
    for (local i = words << 3; i < n; i++) b.writen(input.readn('b'), 'b');
  } catch (e) { return null; }
  b.seek(0);
  return b;
};

local u16At = function(b, at) { return (b[at] & 0xff) | ((b[at + 1] & 0xff) << 8); };
local u32At = function(b, at) {
  return (b[at] & 0xff) | ((b[at + 1] & 0xff) << 8) |
         ((b[at + 2] & 0xff) << 16) | ((b[at + 3] & 0xff) << 24);
};

local sliceBlob = function(src, off, len) {
  local dst = blob(len);
  for (local i = 0; i < len; i++) dst.writen(src[off + i] & 0xff, 'b');
  dst.seek(0);
  return dst;
};

local nameOf = function() {
  local h = take(2);
  if (h == null) throw "messagefuzz.nut: short name";
  local n = u16At(h, 0);
  local b = take(n);
  if (b == null) throw "messagefuzz.nut: short name";
  local s = "";
  for (local i = 0; i < n; i++) s += format("%c", b[i] & 0xff);
  return s;
};

local putU8 = function(b, v) { b.writen(v & 0xff, 'b'); };
local putLE = function(b, v, n) {
  for (local i = 0; i < n; i++) b.writen((v >> (8 * i)) & 0xff, 'b');
};

local exists = function(path) {
  try { local f = file(path, "rb"); f.close(); return true; }
  catch (e) { return false; }
};

local shq = function(s) {
  local out = "'";
  for (local i = 0; i < s.len(); i++) {
    if ((s[i] & 0xff) == 39) out += "'\''";
    else out += format("%c", s[i] & 0xff);
  }
  return out + "'";
};

local baseName = function(path) {
  local cut = -1;
  local at;
  while ((at = path.find("/", cut + 1)) != null) cut = at;
  return path.slice(cut + 1);
};

local tableModuleName = function(dir) {
  local tmp = corpus + "/.msgfuzz_ls.txt";
  system("ls " + shq(dir) + "/*Tables.nut > " + shq(tmp) + " 2>/dev/null");
  if (!exists(tmp)) return null;
  local f = file(tmp, "rb");
  local len = f.len();
  if (len <= 0) { f.close(); return null; }
  local b = f.readblob(len);
  f.close();
  local s = "";
  for (local i = 0; i < len; i++) {
    local c = b[i] & 0xff;
    if (c == 10 || c == 13) break;
    s += format("%c", c);
  }
  if (s.len() == 0) return null;
  local bn = baseName(s);
  if (bn.len() <= 4) return null;
  return bn.slice(0, bn.len() - 4);
};

local unitCache = {};
local openUnit = function(unit) {
  if (unit in unitCache) return unitCache[unit];
  local dir = corpus + "/" + unit;
  if (!exists(dir + "/schema_table_load.nut")) {
    unitCache[unit] <- null;
    return null;
  }
  local module = tableModuleName(dir);
  if (module == null) {
    unitCache[unit] <- null;
    return null;
  }
  local schema = dofile(dir + "/schema_module.nut", true);
  local T = schema.require(module);
  if (!("Announce" in T) || !("AnnounceRead" in T)) {
    unitCache[unit] <- null;
    return null;
  }
  local annSz = T.AnnounceMeasure();
  local annBlob = blob(annSz);
  if (T.Announce(annBlob) != annSz) throw unit + ": own Announce failed";
  annBlob.seek(0);
  local ownVocab = {};
  local ar = {};
  if (!T.AnnounceRead(ownVocab, annBlob, ar)) throw unit + ": own AnnounceRead failed";
  local u = { T = T, ownVocab = ownVocab };
  unitCache[unit] <- u;
  return u;
};

local ch = take(4);
if (ch == null) return;
local rcount = u32At(ch, 0);
local roster = array(rcount, null);
local avail = blob(rcount);
for (local i = 0; i < rcount; i++) {
  local unit = nameOf();
  local root = nameOf();
  local fb = take(2);
  if (fb == null) throw "messagefuzz.nut: short roster";
  local form = fb[0] & 0xff;
  local retain = fb[1] & 0xff;
  local entry = null;
  if (form == 5 && retain == 0) {
    local u = openUnit(unit);
    if (u != null && ("Descriptors" in u.T) && (root in u.T.Descriptors) &&
        u.T.Descriptors[root].fixed && ((root + "LoadMessages") in u.T)) {
      local storage = array(u.T.TableMessageBatchMax);
      for (local j = 0; j < storage.len(); j++) storage[j] = u.T[root]();
      entry = { u = u, root = root, storage = storage };
    }
  }
  roster[i] = entry;
  putU8(avail, entry != null ? 1 : 0);
}
out.writeblob(avail);
out.flush();

while (true) {
  local mh = take(8);
  if (mh == null) break;
  local idx = u32At(mh, 0);
  local sz = u32At(mh, 4);
  local frame = take(sz);
  if (frame == null) throw "messagefuzz.nut: short mutant";
  local e = roster[idx];
  if (e == null) throw "messagefuzz.nut: absent roster entry";
  local T = e.u.T;
  local root = e.root;

  local loaded = false;
  local malformed = false;
  local refused = false;
  local unknown = 0;
  local widened = 0;
  local saved = null;

  if (sz < 4) {
    malformed = true;
    refused = true;
  } else {
    local alen = u32At(frame, 0);
    if (alen < 0 || 4 + alen > sz) {
      malformed = true;
      refused = true;
    } else {
      local vocab = e.u.ownVocab;
      local annOk = true;
      if (alen > 0) {
        vocab = {};
        local ar = {};
        local annBlob = sliceBlob(frame, 4, alen);
        if (!T.AnnounceRead(vocab, annBlob, ar) || (("bodyDamaged" in ar) && ar.bodyDamaged) || (("damaged" in ar) && ar.damaged) || (("refused" in ar) && ar.refused)) {
          annOk = false;
          malformed = (("bodyDamaged" in ar) && ar.bodyDamaged) || (("damaged" in ar) && ar.damaged);
          refused = true;
        }
      }
      if (annOk) {
        local batchLen = sz - 4 - alen;
        local batchBlob = sliceBlob(frame, 4 + alen, batchLen);
        local initLim = batchLen >= 2 ? ((batchBlob[1] & 0xff) + 1) : 1;
        if (initLim > e.storage.len()) initLim = e.storage.len();
        for (local j = 0; j < initLim; j++) e.storage[j] = T[root]();
        local lr = {};
        local n = T[root + "LoadMessages"](e.storage, vocab, batchBlob, lr);
        if ("unknown" in lr) unknown = lr.unknown;
        if ("widened" in lr) widened = lr.widened;
        local dmg = (("bodyDamaged" in lr) && lr.bodyDamaged) || (("damaged" in lr) && lr.damaged);
        local ref = (("refused" in lr) && lr.refused) || (("nonFiniteFloat" in lr) && lr.nonFiniteFloat != null && lr.nonFiniteFloat != "");
        if (n >= 1 && !dmg && !ref) {
          local values = e.storage.slice(0, n);
          local outSz = T[root + "MeasureMessages"](values, {});
          if (outSz >= 0) {
            local outBlob = blob(outSz);
            if (T[root + "SaveMessages"](values, outBlob, {}) == outSz) {
              loaded = true;
              saved = outBlob;
            } else {
              refused = true;
            }
          } else {
            refused = true;
          }
        } else {
          malformed = dmg;
          refused = true;
        }
      }
    }
  }

  local repLen = 47 + (saved != null ? saved.len() : 0);
  local rep = blob(repLen);
  putU8(rep, loaded ? 1 : 0);
  putLE(rep, unknown, 4);
  putLE(rep, 0, 4);
  putLE(rep, widened, 4);
  putLE(rep, 0, 4);
  putLE(rep, 0, 4);
  putU8(rep, malformed ? 1 : 0);
  putU8(rep, refused ? 1 : 0);
  putLE(rep, -1, 8);
  putLE(rep, 0, 4);
  putLE(rep, 0, 4);
  if (saved == null) {
    putLE(rep, -1, 8);
  } else {
    putLE(rep, saved.len(), 8);
    saved.seek(0);
    if (saved.len() > 0) rep.writeblob(saved);
  }
  rep.seek(0);
  out.writeblob(rep);
  out.flush();
}
