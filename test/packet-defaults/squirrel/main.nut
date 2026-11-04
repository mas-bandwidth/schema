// test/packet-defaults/squirrel/main.nut — the SQUIRREL leg of the packet
// value-defaults fixture (test/packet-defaults/README.md; SPEC §4.2, §4.8, §5).
//
// usage: sq main.nut [golden-dir] [defaults-dir] [plain-dir]

local argv = vargv;
local goldenDir = argv.len() >= 1 ? argv[0] : "testdata/wire/packet-defaults";
local defaultsDir = argv.len() >= 2 ? argv[1] : "build/packet-defaults/squirrel/defaults";
local plainDir = argv.len() >= 3 ? argv[2] : "build/packet-defaults/squirrel/plain";

local serialize = dofile("internal/codegen/squirrel/serialize/serialize.nut", true);
local d = dofile(defaultsDir + "/schema_module.nut", true).require("Defaults");
local p = dofile(plainDir + "/schema_module.nut", true).require("Plain");

local failed = 0;
local check = function(ok, what) {
  if (!ok) {
    print("FAILED: " + what + "\n");
    failed++;
  }
};

local readBytes = function(path) {
  local f = file(path, "rb");
  local b = f.len() == 0 ? blob(0) : f.readblob(f.len());
  f.close();
  return b;
};

local readText = function(path) {
  local b = readBytes(path);
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%c", b[i] & 0xff);
  return strip(s);
};

local bytes = function(n, list) {
  local b = blob(n);
  for (local i = 0; i < n; i++) b[i] = 0;
  for (local i = 0; i < list.len(); i++) b[i] = list[i];
  return b;
};

local filled = function(n, x) {
  local b = blob(n);
  for (local i = 0; i < n; i++) b[i] = x;
  return b;
};

local HEX = "0123456789abcdef";
local hexOf = function(b) {
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%c%c", HEX[(b[i] >> 4) & 15], HEX[b[i] & 15]);
  return s;
};
local nibble = function(c) {
  if (c >= '0' && c <= '9') return c - '0';
  if (c >= 'a' && c <= 'f') return c - 'a' + 10;
  if (c >= 'A' && c <= 'F') return c - 'A' + 10;
  throw "bad hex byte";
};
local unhex = function(s) {
  local n = s.len() / 2;
  local b = blob(n);
  for (local i = 0; i < n; i++) b.writen((nibble(s[2 * i]) << 4) | nibble(s[2 * i + 1]), 'b');
  b.seek(0);
  return b;
};

local deepEqual = null;
deepEqual = function(a, b) {
  if (typeof a != typeof b) return false;
  if (typeof a == "blob") {
    if (a.len() != b.len()) return false;
    for (local i = 0; i < a.len(); i++) if (a[i] != b[i]) return false;
    return true;
  }
  if (typeof a == "array") {
    if (a.len() != b.len()) return false;
    for (local i = 0; i < a.len(); i++) if (!deepEqual(a[i], b[i])) return false;
    return true;
  }
  if (typeof a == "instance") {
    if (a.getclass() != b.getclass()) return false;
    foreach (k, mv in a.getclass()) {
      if (typeof mv == "function") continue;
      if (!deepEqual(a[k], b[k])) return false;
    }
    return true;
  }
  return a == b;
};

local zero = function() {
  local v = d.Sample();
  v.Name = filled(6, 0); v.NameLength = 0;
  v.Token = filled(4, 0); v.TokenLength = 0;
  v.Caps = 0;
  v.EmptyName = filled(3, 0); v.EmptyNameLength = 0;
  v.EmptyToken = filled(2, 0); v.EmptyTokenLength = 0;
  v.EmptyCaps = 0;
  return v;
};

local sample = function() {
  local v = zero();
  v.Name = bytes(6, [0xc3, 0xa9, 0xf0, 0x90, 0x80, 0x80]); v.NameLength = 6;
  v.Token = bytes(4, [0x5c, 0x6e, 0x5c, 0x74]); v.TokenLength = 4;
  v.Caps = 5;
  return v;
};

local short = function() {
  local v = zero();
  v.Name = bytes(6, [65]); v.NameLength = 1;
  v.Token = bytes(4, [0, 255]); v.TokenLength = 2;
  v.Caps = 2;
  return v;
};

local dirty = function() {
  local v = zero();
  v.Name = filled(6, 100); v.NameLength = 6;
  v.Token = filled(4, 161); v.TokenLength = 4;
  v.Caps = 7;
  v.EmptyName = filled(3, 111); v.EmptyNameLength = 3;
  v.EmptyToken = filled(2, 177); v.EmptyTokenLength = 2;
  v.EmptyCaps = 7;
  return v;
};

local overlay = function(initial, sent) {
  for (local i = 0; i < sent.NameLength; i++) initial.Name[i] = sent.Name[i];
  initial.NameLength = sent.NameLength;
  for (local i = 0; i < sent.TokenLength; i++) initial.Token[i] = sent.Token[i];
  initial.TokenLength = sent.TokenLength;
  initial.Caps = sent.Caps;
  initial.EmptyNameLength = 0;
  initial.EmptyTokenLength = 0;
  initial.EmptyCaps = 0;
  return initial;
};

local write = function(mod, name, value) {
  local stream = serialize.WriteStream(blob(4096));
  check(mod["Write" + name](stream, value), "runtime write " + name);
  local bits = stream.bitsProcessed();
  stream.flush();
  return { bytes = stream.data(), bits = bits };
};

local checkRead = function(name, wire, bits, initial, want) {
  local stream = serialize.ReadStream(wire);
  check(d["Read" + name](stream, initial), "runtime read " + name);
  check(stream.bitsProcessed() == bits, "read consumed bits " + name);
  if (name == "Choice") {
    check(initial.Type == want.Type, "Choice Type");
    if (want.Type == d.ChoiceType.Sample) {
      check(deepEqual(initial.Sample, want.Sample), name + ": Sample values and backing storage");
    } else if (want.Type == d.ChoiceType.Conditional) {
      check(deepEqual(initial.Conditional, want.Conditional), name + ": Conditional values and backing storage");
    }
  } else {
    check(deepEqual(initial, want), name + ": values and backing storage");
  }
};

local pin = function(name, fileName, value, initialFn, wantFn) {
  local wantBytes = readBytes(goldenDir + "/" + fileName + ".bin");
  local wantBits = readText(goldenDir + "/" + fileName + ".bits").tointeger();
  local w = write(d, name, value);
  check(deepEqual(w.bytes, wantBytes) && w.bits == wantBits, "C++ pin " + fileName);
  checkRead(name, wantBytes, wantBits, initialFn(), wantFn());
};

// Keep this marker exact: the negative control sabotages constructor bytes.
check(deepEqual(d.Sample(), sample()), "packet-default constructor bytes");

local empty = d.EmptyOnly();
check(empty.NameLength == 0 && empty.TokenLength == 0 && empty.Caps == 0, "explicit empty defaults");
check(deepEqual(empty.Name, filled(2, 0)) && deepEqual(empty.Token, filled(1, 0)), "explicit empty defaults leave zero buffers");

local prefix = d.Prefix();
check(deepEqual(prefix.Name, bytes(5, [0xc3, 0xa9])) && prefix.NameLength == 2, "a short string default, zero tail");
check(deepEqual(prefix.Token, bytes(5, [0x5c, 0x6e])) && prefix.TokenLength == 2, "a short bytes default, zero tail");

local wide = d.WideMask();
check(wide.High == 0x8000000000000000 && wide.All == 0xffffffffffffffff, "the bit-63 and full 64-bit masks");
local wideWire = unhex("0000000000000080ffffffffffffffff");
local ww = write(d, "WideMask", wide);
check(deepEqual(ww.bytes, wideWire) && ww.bits == 128, "WideMask bytes " + hexOf(ww.bytes));

local split = d.SplitMask();
split.Lead = 5; split.Mask = 0x100000000; split.Tail = 2;
local sw = write(d, "SplitMask", split);
local splitWire = unhex("0500000028");
check(deepEqual(sw.bytes, splitWire) && sw.bits == 38, "SplitMask bytes " + hexOf(sw.bytes));

local wideInitial = d.WideMask();
wideInitial.High = 0; wideInitial.All = 0;
checkRead("WideMask", wideWire, 128, wideInitial, wide);
checkRead("SplitMask", splitWire, 38, d.SplitMask(), split);

local plain = p.Sample();
local s = sample();
plain.Name = s.Name; plain.NameLength = s.NameLength;
plain.Token = s.Token; plain.TokenLength = s.TokenLength;
plain.Caps = s.Caps;
local plainW = write(p, "Sample", plain);
local defaultW = write(d, "Sample", d.Sample());
check(deepEqual(plainW.bytes, defaultW.bytes) && plainW.bits == defaultW.bits, "the defaultless twin writes the same bytes");

pin("Sample", "sample-defaults", d.Sample(), zero, sample);

local batch = d.Batch();
check(batch.CountedCount == 1 && deepEqual(batch.Head, sample()), "Batch is born at its defaults");
foreach (elem in batch.Items) check(deepEqual(elem, sample()), "every fixed element at its defaults");
foreach (elem in batch.Counted) check(deepEqual(elem, sample()), "every backing element at its defaults");
local reusedBatch = function() {
  local b = d.Batch();
  b.Counted[1] = dirty();
  b.Counted[2] = short();
  return b;
};
pin("Batch", "batch-defaults", batch, reusedBatch, reusedBatch);

local z = d.ZeroCount();
check(z.ItemsCount == 0 && deepEqual(z.Items, [sample(), sample()]), "ZeroCount is born empty over defaulted elements");
pin("ZeroCount", "zero-count", z, function() {
  local v = d.ZeroCount();
  v.Items = [dirty(), short()]; v.ItemsCount = 2;
  return v;
}, function() {
  local v = d.ZeroCount();
  v.Items = [dirty(), short()]; v.ItemsCount = 0;
  return v;
});

check(d.Conditional().Enabled == true && deepEqual(d.Conditional().Value, sample()), "Conditional is born enabled at its defaults");
pin("Conditional", "conditional-on", d.Conditional(), function() {
  local v = d.Conditional();
  v.Enabled = false; v.Value = zero();
  return v;
}, function() { return d.Conditional(); });

local off = d.Conditional();
off.Enabled = false;
pin("Conditional", "conditional-off", off, function() {
  local v = d.Conditional();
  v.Value = dirty();
  return v;
}, function() {
  local v = d.Conditional();
  v.Enabled = false; v.Value = zero();
  return v;
});

local choice = function() {
  local v = d.Choice();
  v.Type = d.ChoiceType.Sample;
  return v;
};
pin("Choice", "choice-sample", choice(), function() {
  local v = d.Choice();
  v.Type = d.ChoiceType.Conditional;
  return v;
}, choice);

pin("Sample", "sample-short", short(), dirty, function() { return overlay(dirty(), short()); });
pin("Sample", "sample-empty", zero(), dirty, function() { return overlay(dirty(), zero()); });

foreach (sent in [short(), zero()]) {
  local value = choice();
  value.Sample = sent;
  local w = write(d, "Choice", value);
  local output = d.Choice();
  output.Type = d.ChoiceType.Conditional;
  for (local attempt = 0; attempt < 2; attempt++) {
    output.Sample = dirty();
    local want = choice();
    want.Sample = overlay(sample(), sent);
    checkRead("Choice", w.bytes, w.bits, output, want);
  }
}

local offChoice = d.Choice();
offChoice.Type = d.ChoiceType.Conditional;
offChoice.Conditional.Enabled = false;
local offW = write(d, "Choice", offChoice);
local output = choice();
for (local attempt = 0; attempt < 2; attempt++) {
  output.Conditional.Enabled = true;
  output.Conditional.Value = dirty();
  local want = d.Choice();
  want.Type = d.ChoiceType.Conditional;
  want.Conditional.Enabled = false;
  want.Conditional.Value = zero();
  checkRead("Choice", offW.bytes, offW.bits, output, want);
}

if (failed > 0) throw "packet defaults squirrel: " + failed + " check(s) failed";
print("packet defaults Squirrel: constructors, eight C++ goldens and reused storage OK\n");
