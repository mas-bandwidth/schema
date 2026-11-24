// test/packet-void/squirrel/main.nut — the SQUIRREL leg of test/packet-void
// (SPEC §4.8): answers every case of testdata/wire/packet-void/cases.txt, one
// "case <name> ok|FAILED: <why>" line each, for tools/casegate. The pins are
// the Go oracle's bytes, never this writer's.
//
// usage (from the repository root, for the runtime's path):
//   sq test/packet-void/squirrel/main.nut <pins dir> <generated dir>
//
// sq exits 0 after an uncaught throw, so every case runs under try/catch and
// a run that dies early leaves cases unanswered, which casegate refuses.
local pinsDir = vargv.len() >= 1 ? vargv[0] : "testdata/wire/packet-void";
local generatedDir = vargv.len() >= 2 ? vargv[1] : "build/packet-void/squirrel";

local serialize = dofile("internal/codegen/squirrel/serialize/serialize.nut", true);
local V = dofile(generatedDir + "/schema_module.nut", true).require("Void");

local TYPES = { Mixed = true, Late = true, Tags = true, Single = true, Empty = true, Frame = true, Batch = true };

local readBytes = function(path) {
  local f = file(path, "rb");
  local b = f.len() == 0 ? blob(0) : f.readblob(f.len());
  f.close();
  return b;
};

local readText = function(path) {
  local data = readBytes(path);
  local s = "";
  for (local i = 0; i < data.len(); i++) s += format("%c", data[i] & 0xff);
  return s;
};

local hexOf = function(b) {
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%02x", b[i] & 0xff);
  return s;
};

local sameBytes = function(x, y) {
  if (x.len() != y.len()) return false;
  for (local i = 0; i < x.len(); i++) if ((x[i] & 0xff) != (y[i] & 0xff)) return false;
  return true;
};

// same compares the value's wire-meaningful fields: the selected arm and the
// counted elements; an unselected arm and storage past a count are
// unspecified (SPEC §4.8).
local sameArm = function(a, b) {
  if (a.EntriesCount != b.EntriesCount || a.Marker != b.Marker) return false;
  for (local i = 0; i < a.EntriesCount; i++) if (a.Entries[i].Retries != b.Entries[i].Retries) return false;
  return true;
};

local sameMixed = function(a, b) {
  if (a.Type != b.Type) return false;
  return a.Type != V.MixedType.Payload || sameArm(a.Payload, b.Payload);
};

local same = function(typeName, a, b) {
  if (typeName == "Mixed") return sameMixed(a, b);
  if (typeName == "Late") {
    if (a.Type != b.Type) return false;
    return a.Type != V.LateType.Payload || sameArm(a.Payload, b.Payload);
  }
  if (typeName == "Frame") return a.Lead == b.Lead && a.Tail == b.Tail && sameMixed(a.Choice, b.Choice);
  if (typeName == "Batch") {
    if (a.ValuesCount != b.ValuesCount) return false;
    for (local i = 0; i < a.ValuesCount; i++) if (!sameMixed(a.Values[i], b.Values[i])) return false;
    return true;
  }
  return a.Type == b.Type;
};

local mixed = function(tag) {
  local v = V.Mixed();
  v.Type = tag;
  return v;
};

// The value of each accept case, by name, and its type; null for a name this
// driver does not know, which is FAILED and never written with another case's
// value.
local build = function(name) {
  switch (name) {
    case "mixed-none": return { value = mixed(V.MixedType.None), type = "Mixed" };
    case "mixed-ping": return { value = mixed(V.MixedType.Ping), type = "Mixed" };
    case "mixed-payload": return { value = mixed(V.MixedType.Payload), type = "Mixed" };
    case "late-payload":
    case "late-pong": {
      local v = V.Late();
      v.Type = name == "late-payload" ? V.LateType.Payload : V.LateType.Pong;
      return { value = v, type = "Late" };
    }
    case "tags-none":
    case "tags-ping":
    case "tags-pong": {
      local v = V.Tags();
      v.Type = { ["tags-none"] = V.TagsType.None, ["tags-ping"] = V.TagsType.Ping, ["tags-pong"] = V.TagsType.Pong }[name];
      return { value = v, type = "Tags" };
    }
    case "single-none":
    case "single-ack": {
      local v = V.Single();
      v.Type = name == "single-ack" ? V.SingleType.Ack : V.SingleType.None;
      return { value = v, type = "Single" };
    }
    case "empty-none": return { value = V.Empty(), type = "Empty" };
    case "frame-ping": {
      local v = V.Frame();
      v.Lead = 5;
      v.Choice = mixed(V.MixedType.Ping);
      v.Tail = 6;
      return { value = v, type = "Frame" };
    }
    case "batch-empty":
    case "batch-ping":
    case "batch-ping-payload": {
      local v = V.Batch();
      if (name != "batch-empty") {
        v.Values[0] = mixed(V.MixedType.Ping);
        v.ValuesCount = 1;
      }
      if (name == "batch-ping-payload") {
        v.Values[1] = mixed(V.MixedType.Payload);
        v.ValuesCount = 2;
      }
      return { value = v, type = "Batch" };
    }
  }
  return null;
};

// dirtyArm fills the storage a read reuses, so a read that does not start the
// payload arm at its declared defaults shows.
local dirtyArm = function(arm) {
  arm.EntriesCount = 2;
  arm.Entries[0].Retries = 9;
  arm.Entries[1].Retries = 9;
  arm.Marker = 0;
};

local fresh = function(typeName) {
  local v = V[typeName]();
  if (typeName == "Mixed" || typeName == "Late") dirtyArm(v.Payload);
  if (typeName == "Batch") {
    dirtyArm(v.Values[0].Payload);
    dirtyArm(v.Values[1].Payload);
  }
  return v;
};

local write = function(typeName, value) {
  local stream = serialize.WriteStream(blob(256));
  if (!V["Write" + typeName](stream, value)) return null;
  local bits = stream.bitsProcessed();
  stream.flush();
  return { bytes = stream.data(), bits = bits };
};

// The refuse-write cases: the type's default value with the one change the
// case names. The writer's refusal is a false return; a thrown error is not
// the writer's refusal and is FAILED.
local buildRefusal = function(name) {
  switch (name) {
    case "tags-write-tag-3": {
      local v = V.Tags();
      v.Type = 3;
      return { value = v, type = "Tags" };
    }
    case "empty-write-tag-1": {
      local v = V.Empty();
      v.Type = 1;
      return { value = v, type = "Empty" };
    }
    case "mixed-write-tag-negative":
    case "mixed-write-tag-text":
    case "mixed-write-tag-3":
    case "mixed-write-payload-type":
    case "mixed-write-marker-8": {
      local v = V.Mixed();
      if (name == "mixed-write-tag-negative") v.Type = -1;
      if (name == "mixed-write-tag-text") v.Type = "ping";
      if (name == "mixed-write-tag-3") v.Type = 3;
      if (name == "mixed-write-payload-type") {
        v.Type = V.MixedType.Payload;
        v.Payload = 1;
      }
      if (name == "mixed-write-marker-8") {
        v.Type = V.MixedType.Payload;
        v.Payload.Marker = 8;
      }
      return { value = v, type = "Mixed" };
    }
  }
  return null;
};

local refuseWrite = function(name, typeName) {
  local c = buildRefusal(name);
  if (c == null) return "unknown refuse-write case";
  if (c.type != typeName) return "case type " + typeName + ", the driver builds " + c.type;
  local stream = serialize.WriteStream(blob(256));
  local res;
  try {
    res = V["Write" + typeName](stream, c.value);
  } catch (e) {
    return "not the writer's refusal: " + e;
  }
  if (res != false) return "write accepted";
  return null;
};

// selectPayload reads mixed-ping's pin, then selects the payload arm the
// Squirrel API's way: the union keeps its Payload instance, so selecting is
// setting the tag, and the arm must still carry its declared defaults.
local selectPayload = function() {
  local v = V.Mixed();
  if (!V.ReadMixed(serialize.ReadStream(readBytes(pinsDir + "/mixed-ping.bin")), v)) return "read mixed-ping refused";
  if (v.Type != V.MixedType.Ping) return "read mixed-ping: tag " + v.Type;
  v.Type = V.MixedType.Payload;
  return write("Mixed", v);
};

local answer = function(name, typeName, verdict, bits) {
  if (!(typeName in TYPES)) return "unknown type " + typeName;
  if (verdict == "refuse-write") return refuseWrite(name, typeName);
  if (verdict == "select") {
    if (name != "mixed-ping-select-payload") return "unknown select case";
    local w = selectPayload();
    if (typeof w == "string") return w;
    if (w == null) return "write refused";
    local pin = readBytes(pinsDir + "/" + name + ".bin");
    if (!sameBytes(w.bytes, pin)) return "write " + hexOf(w.bytes) + ", pin " + hexOf(pin);
    if (w.bits != bits.tointeger()) return "write has " + w.bits + " bits, want " + bits;
    return null;
  }
  local pin = readBytes(pinsDir + "/" + name + ".bin");
  if (verdict == "refuse") {
    if (V["Read" + typeName](serialize.ReadStream(pin), fresh(typeName))) return "read accepted";
    return null;
  }
  if (verdict != "accept") return "verdict " + verdict;

  local c = build(name);
  if (c == null) return "unknown accept case";
  if (c.type != typeName) return "case type " + typeName + ", the driver builds " + c.type;
  local want = bits.tointeger();

  local w = write(typeName, c.value);
  if (w == null) return "write refused";
  if (!sameBytes(w.bytes, pin)) return "write " + hexOf(w.bytes) + ", pin " + hexOf(pin);
  if (w.bits != want) return "write has " + w.bits + " bits, want " + bits;

  local back = fresh(typeName);
  if (!V["Read" + typeName](serialize.ReadStream(pin), back)) return "read refused";
  if (!same(typeName, back, c.value)) return "read value differs";
  local again = write(typeName, back);
  if (again == null || !sameBytes(again.bytes, pin)) return "write what was read differs";

  // The body read stops before the end check, so its cursor is the object's
  // bit count.
  local body = serialize.ReadStream(pin);
  if (!V["Read" + typeName + "Body"](body, fresh(typeName))) return "body read refused";
  if (body.bitsProcessed() != want) return "read has " + body.bitsProcessed() + " bits, want " + bits;
  return null;
};

foreach (line in split(readText(pinsDir + "/cases.txt"), "\n")) {
  local fields = [];
  foreach (f in split(line, " \t\r")) if (f.len() > 0) fields.append(f);
  if (fields.len() == 0 || fields[0][0] == '#') continue;
  if (fields.len() < 4) {
    print("case " + fields[0] + " FAILED: a case line needs <name> <type> <verdict> <bits>\n");
    continue;
  }
  local why;
  try {
    why = answer(fields[0], fields[1], fields[2], fields[3]);
  } catch (e) {
    why = "raised: " + e;
  }
  print(why == null ? "case " + fields[0] + " ok\n" : "case " + fields[0] + " FAILED: " + why + "\n");
}
