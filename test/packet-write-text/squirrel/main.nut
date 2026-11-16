// test/packet-write-text/squirrel/main.nut — the SQUIRREL leg of the packet
// Write text gate (docs/SPEC.md §5.1 contract row 8; §4.7 string(N)).
//
// The packet writer holds the used bytes of string(N) to no zero byte and
// well-formed UTF-8 by a refusal in every build: Write returns false. Every
// case of the shared list (test/packet-write-text/cases.txt) is written; each
// refused case must return false, and each written case must write, read back
// to the same used bytes, and write again byte-exact.
//
// usage: sq main.nut [generated-dir]

local argv = vargv;
local generatedDir = argv.len() >= 1 ? argv[0] : "build/packet-write-text/squirrel";
local casesPath = "test/packet-write-text/cases.txt";

local serialize = dofile("internal/codegen/squirrel/serialize/serialize.nut", true);
local Text = dofile(generatedDir + "/schema_module.nut", true).require("Text");

local failed = 0;
local check = function(ok, what) {
  if (!ok) {
    print("FAILED: packet write text " + what + "\n");
    failed++;
  }
};

local readText = function(path) {
  local f = file(path, "rb");
  local s = "";
  while (!f.eos()) s += format("%c", f.readn('b') & 0xff);
  f.close();
  return s;
};

local nibble = function(c) {
  if (c >= '0' && c <= '9') return c - '0';
  if (c >= 'a' && c <= 'f') return c - 'a' + 10;
  if (c >= 'A' && c <= 'F') return c - 'A' + 10;
  throw "bad hex digit in " + casesPath;
};

// usedBytes parses "61 ff 62" (or "-" for none) into a list of byte values.
local usedBytes = function(hex) {
  local out = [];
  if (hex == "-") return out;
  foreach (tok in split(hex, " ")) {
    if (tok.len() != 2) throw "bad hex byte " + tok + " in " + casesPath;
    out.append((nibble(tok[0]) << 4) | nibble(tok[1]));
  }
  return out;
};

local packetOf = function(list) {
  local value = Text.TextPacket();
  for (local i = 0; i < list.len(); i++) value.Text[i] = list[i];
  value.TextLength = list.len();
  return value;
};

local write = function(value) {
  local stream = serialize.WriteStream(blob(64));
  local ok = Text.WriteTextPacket(stream, value);
  if (!ok) return null;
  stream.flush();
  return stream.data();
};

local sameBlob = function(a, b) {
  if (a.len() != b.len()) return false;
  for (local i = 0; i < a.len(); i++) if (a[i] != b[i]) return false;
  return true;
};

local refused = function(name, list) {
  check(write(packetOf(list)) == null, name + " writer accepted ill-formed text");
};

local written = function(name, list) {
  local wire = write(packetOf(list));
  if (wire == null) {
    check(false, name + " writer refused well-formed text");
    return;
  }
  local out = Text.TextPacket();
  if (!Text.ReadTextPacket(serialize.ReadStream(wire), out)) {
    check(false, name + " reader refused the written packet");
    return;
  }
  check(out.TextLength == list.len(), name + " read back " + out.TextLength + " used bytes, want " + list.len());
  local same = out.TextLength == list.len();
  for (local i = 0; same && i < list.len(); i++) same = out.Text[i] == list[i];
  check(same, name + " read back other used bytes");
  local again = write(out);
  check(again != null && sameBlob(again, wire), name + " rewrite is not byte exact");
};

local cases = 0;
foreach (raw in split(readText(casesPath), "\n")) {
  local line = strip(raw);
  if (line.len() == 0 || line[0] == '#') continue;
  local parts = split(line, "|");
  if (parts.len() != 3) throw "bad case line in " + casesPath + ": " + line;
  local name = strip(parts[0]);
  local list = usedBytes(strip(parts[1]));
  local verdict = strip(parts[2]);
  if (verdict == "refused") refused(name, list);
  else if (verdict == "written") written(name, list);
  else throw "bad verdict " + verdict + " in " + casesPath;
  cases++;
}

check(cases > 0, "found no cases in " + casesPath);
if (failed > 0) throw "packet write text squirrel: " + failed + " check(s) failed";
print("packet write text Squirrel: " + cases + " cases, ill-formed text refused, well-formed text written and read back byte-exact\n");
