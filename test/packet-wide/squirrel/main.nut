// test/packet-wide/squirrel/main.nut — the SQUIRREL leg of the packet wide-text
// check (test/packet-wide/README.md; SPEC §4.12).
//
// usage: sq main.nut corpus [generated dir]      one wire per stdin line
//        sq main.nut contracts [generated dir]   the composition contracts
//
// The working directory is the repository root. The generated units are
// build/packet-wide/squirrel (WideText) and its shapes/ (Shapes) unless a
// directory is named, which is how the negative control points the same
// driver at a sabotaged copy. The serialize runtime is the vendored pin.
//
// corpus: each line is `<bound> <hex wire>`; the answer is `REFUSE`, or `OK
// <bits read> <code units> <bits written> <hex rewrite>`, the harness's line
// (test/packet-text/harness). A crash writes to stderr, which the harness
// reads as a failure.

local argv = vargv;
if (argv.len() < 1) throw "usage: main.nut corpus|contracts [dir]";
local mode = argv[0];
local dir = argv.len() >= 2 ? argv[1] : "build/packet-wide/squirrel";
local serialize = dofile("internal/codegen/squirrel/serialize/serialize.nut", true);
local W = dofile(dir + "/schema_module.nut", true).require("WideText");

local HEX = "0123456789abcdef";
local hexOf = function(b, n) {
  if (n == 0) return "-";
  local s = "";
  for (local i = 0; i < n; i++) s += format("%c%c", HEX[(b[i] >> 4) & 15], HEX[b[i] & 15]);
  return s;
};
local nibble = function(c) {
  if (c >= '0' && c <= '9') return c - '0';
  if (c >= 'a' && c <= 'f') return c - 'a' + 10;
  if (c >= 'A' && c <= 'F') return c - 'A' + 10;
  throw "main.nut: not hex";
};
local unhex = function(s) {
  if (s == "-") return blob(0);
  local b = blob(s.len() / 2);
  for (local i = 0; i < b.len(); i++) b[i] = (nibble(s[2 * i]) << 4) | nibble(s[2 * i + 1]);
  return b;
};
local check = function(ok, what) { if (!ok) throw "contract: " + what; };

// encode answers the bytes and the bits; a refused write answers null
local encode = function(M, name, v) {
  local w = serialize.WriteStream(blob(1024));
  if (!M["Write" + name](w, v)) return null;
  local bits = w.bitsProcessed();
  w.flush();
  local data = w.data();
  return { bytes = data, bits = bits };
};
local decode = function(M, name, v, bytes) {
  local r = serialize.ReadStream(bytes);
  return M["Read" + name](r, v) ? r.bitsProcessed() : -1;
};

if (mode == "contracts") {
  // WRITER REFUSALS: a used length past the bound, negative, not an integer
  local v = W.WideSeven();
  foreach (n in [-1, 8, 0.5, null]) {
    v.TextLength = n;
    local refused = false;
    try { refused = encode(W, "WideSeven", v) == null; } catch (e) { refused = true; }
    check(refused, "WriteWideSeven accepted a used length of " + n);
  }
  // READ-SIDE PAIRING: a lone high surrogate is written (the writer checks the
  // unit range only) and refused on read; 0xFFFF reads back and the reused
  // tail past the used length is left untouched
  v.TextLength = 1; v.Text[0] = 0xD800;
  local e = encode(W, "WideSeven", v);
  check(e != null, "a lone surrogate unit did not write");
  check(decode(W, "WideSeven", W.WideSeven(), e.bytes) < 0, "a lone high surrogate read");
  v.Text[0] = 0xFFFF;
  e = encode(W, "WideSeven", v);
  local back = W.WideSeven();
  for (local i = 0; i < 7; i++) back.Text[i] = 0x7F7F;
  check(decode(W, "WideSeven", back, e.bytes) == e.bits, "0xFFFF did not read back at its bits");
  check(back.TextLength == 1 && back.Text[0] == 0xFFFF, "0xFFFF read back wrong");
  for (local i = 1; i < 7; i++) check(back.Text[i] == 0x7F7F, "the reused tail was written at " + i);

  local P = dofile(dir + "/shapes/schema_module.nut", true).require("Shapes");
  // AN UNALIGNED CONDITIONAL: one bit, then the length, then the units
  local c = P.Conditional(); c.Enabled = true; c.TextLength = 2; c.Text[0] = 0xD800; c.Text[1] = 0xDC00;
  e = encode(P, "Conditional", c);
  check(e.bits == 68, "Conditional wrote " + e.bits + " bits, want 68");
  check((e.bytes[0] & 15) == 5, "Conditional's first nibble is not enabled + length 2");
  local co = P.Conditional();
  check(decode(P, "Conditional", co, e.bytes) == 68, "Conditional did not read its bits");
  check(co.Enabled && co.TextLength == 2 && co.Text[0] == 0xD800 && co.Text[1] == 0xDC00, "Conditional read back wrong");
  // BRANCH ZEROING: a disabled branch reads back as the type's born value
  c.Enabled = false;
  e = encode(P, "Conditional", c);
  check(decode(P, "Conditional", co, e.bytes) >= 0, "the disabled Conditional did not read");
  check(!co.Enabled && co.TextLength == 0, "the disabled branch was not zeroed");
  // FIXED AND COUNTED ARRAYS, THE BORN COUNT, A REPEATED UNION TAG
  local box = P.Box();
  check(box.CountedCount == 1, "a [1..3] array is not born at 1");
  box.Items[0].ValueLength = 1; box.Items[0].Value[0] = 0xFFFF;
  box.Counted[0].ValueLength = 1; box.Counted[0].Value[0] = 65;
  box.Choice.Type = P.ChoiceType.Text; box.Choice.Text.ValueLength = 1; box.Choice.Text.Value[0] = 66;
  e = encode(P, "Box", box);
  check(e != null, "Box did not write");
  local bo = P.Box(); bo.Counted[1].Value[0] = 0x7F7F;
  for (local k = 0; k < 2; k++) {
    bo.Choice.Text.Value[3] = 0x7F7F;
    check(decode(P, "Box", bo, e.bytes) == e.bits, "Box did not read its bits (pass " + k + ")");
    check(bo.Items[0].ValueLength == 1 && bo.Items[0].Value[0] == 0xFFFF, "Box.items[0] read back wrong");
    check(bo.Items[1].ValueLength == 0, "Box.items[1] read back wrong");
    check(bo.CountedCount == 1 && bo.Counted[0].ValueLength == 1 && bo.Counted[0].Value[0] == 65, "Box.counted read back wrong");
    check(bo.Counted[1].Value[0] == 0x7F7F, "an element past the count was written");
    check(bo.Choice.Type == P.ChoiceType.Text && bo.Choice.Text.ValueLength == 1 && bo.Choice.Text.Value[0] == 66, "Box.choice read back wrong");
  }
  print("packet wide squirrel contracts: ok\n");
  return;
}

if (mode != "corpus") throw "main.nut: no mode " + mode;

// the whole of stdin, then one answer a line, in order. A pipe's eos() is true
// before the first read on the pin, so the end is the read that fails.
local input = "";
local chunk = [];
for (;;) {
  local c;
  try { c = stdin.readn('b'); } catch (e) { break; }
  chunk.append(format("%c", c & 0xff));
  if (chunk.len() >= 4096) { foreach (x in chunk) input += x; chunk = []; }
}
foreach (x in chunk) input += x;

local out = [];
local start = 0;
for (local i = 0; i <= input.len(); i++) {
  if (i < input.len() && input[i] != '\n') continue;
  local line = input.slice(start, i);
  start = i + 1;
  if (line.len() > 0 && line[line.len() - 1] == '\r') line = line.slice(0, line.len() - 1);
  if (line.len() == 0) continue;
  local sp = line.find(" ");
  if (sp == null) throw "main.nut: a line needs a bound and a wire";
  local bound = line.slice(0, sp);
  local name = bound == "7" ? "WideSeven" : bound == "4" ? "WideFour" : null;
  if (name == null) throw "main.nut: unexpected bound " + bound;
  local v = W[name]();
  for (local k = 0; k < v.Text.len(); k++) v.Text[k] = 0x7F7F;
  local r = serialize.ReadStream(unhex(line.slice(sp + 1)));
  if (!W["Read" + name](r, v)) { out.append("REFUSE\n"); continue; }
  local e = encode(W, name, v);
  if (e == null) throw "main.nut: a value that read did not write";
  local units = "";
  for (local k = 0; k < v.TextLength; k++) units += format("%04x", v.Text[k]);
  if (units.len() == 0) units = "-";
  out.append("OK " + r.bitsProcessed() + " " + units + " " + e.bits + " " + hexOf(e.bytes, e.bytes.len()) + "\n");
}
foreach (s in out) print(s);
