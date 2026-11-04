// test/packet-text/squirrel/main.nut — the SQUIRREL leg of the packet UTF-8
// sweep (test/packet-text/README.md; SPEC §4.7).
//
// usage: sq main.nut [generated dir]
//
// One hexadecimal wire per stdin line ("-" is the empty wire); one verdict per
// output line: "REFUSE" or "OK <bits> <payload> <bits> <canonical>". All output
// is buffered until the end so any runtime throw leaves stdout empty and fails
// the harness row-count check with stderr attached.

local argv = vargv;
local dir = argv.len() >= 1 ? argv[0] : "build/packet-text/squirrel";
local serialize = dofile("internal/codegen/squirrel/serialize/serialize.nut", true);
local Narrow = dofile(dir + "/schema_module.nut", true).require("Narrow");

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
  throw "bad hex byte";
};
local unhex = function(s) {
  if (s == "-") return blob(0);
  local n = s.len() / 2;
  local b = blob(n);
  for (local i = 0; i < n; i++) b.writen((nibble(s[2 * i]) << 4) | nibble(s[2 * i + 1]), 'b');
  b.seek(0);
  return b;
};

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
  local wire = unhex(line);
  local value = Narrow.Narrow();
  for (local k = 0; k < value.Text.len(); k++) value.Text[k] = 0x7f;
  local r = serialize.ReadStream(wire);
  if (!Narrow.ReadNarrow(r, value)) {
    out.append("REFUSE\n");
    continue;
  }
  local w = serialize.WriteStream(blob(Narrow.NarrowMaxBytes));
  if (!Narrow.WriteNarrow(w, value)) throw "main.nut: accepted value did not write";
  local bits = w.bitsProcessed();
  w.flush();
  local data = w.data();
  if (r.bitsProcessed() != bits) throw "main.nut: read consumed bits != write bits";
  out.append("OK " + r.bitsProcessed() + " " + hexOf(value.Text, value.TextLength) + " " + bits + " " + hexOf(data, data.len()) + "\n");
}
foreach (s in out) print(s);
