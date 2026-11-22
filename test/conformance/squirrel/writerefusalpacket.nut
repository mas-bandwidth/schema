// test/conformance/squirrel/writerefusalpacket.nut — the SQUIRREL leg's probe of
// the `write-refusal-packet` surface (docs/SPEC.md §5.1;
// test/conformance/write-refusal-packet/rows.txt). One row per line of the
// shared list: a fresh PacketWriteRefusal, ONE op, the generated packet Write
// into a 256-byte buffer, and one line out, "<name> refused" or "<name> wrote".
//
// HOW A REFUSAL SURFACES on this leg's packet Write (SPEC §5.1, the Squirrel
// line): rows 2-6 and 8 return false in every build; row 1 refuses in every
// build too, but for an int32 or int64 range the runtime's serializeInt raises
// its write-misuse trap ("serialize: write misuse: a value outside its range
// ..."). So a false return is a refusal on every row, and that one trap is a
// refusal only on a row-1 op (a ranged int32). Any other raise, or any other
// return, is not an answer: the probe prints it as such, and the Go side reads
// the row as unanswered, so a crash or a typo never passes as a refusal.
//
// The rows file arrives as inputs.nut's "rows" blob (internal/codegen/squirrel/sqrun).

local inputs = dofile("inputs.nut", true);
local serialize = dofile("serialize.nut", true);
local P = dofile("schema_module.nut", true).require("PacketWriteRefusal");

// RAISES: the ops whose field is a ranged int32 (SPEC §5.1 row 1), the only
// ones where the trap below is this leg's refusal.
local RAISES = { scalar = true, fixed_constant = true, reserved_zero = true };
local TRAP = "serialize: write misuse: a value outside its range or its bit width";

local text = "";
foreach (c in inputs["rows"]) text += format("%c", c & 0xff);

local nibble = function(c) {
  if (c >= '0' && c <= '9') return c - '0';
  if (c >= 'a' && c <= 'f') return c - 'a' + 10;
  throw "rows: bad hex digit " + c;
};

// real parses a float argument; nan, inf and -inf are spelled out.
local real = function(arg) {
  if (arg == "nan") return 0.0 / 0.0;
  if (arg == "inf") return 1.0 / 0.0;
  if (arg == "-inf") return -1.0 / 0.0;
  return arg.tofloat();
};

// apply makes the row's one change to a fresh, lawful value.
local apply = function(v, op, arg) {
  switch (op) {
    case "scalar": v.Scalar = arg.tointeger(); break;
    case "narrow": v.Narrow = arg.tointeger(); break;
    case "grade": v.Grade = arg.tointeger(); break;
    case "shot_tag": v.Shot.Type = arg.tointeger(); break;
    case "window_count": v.WindowCount = arg.tointeger(); break;
    case "text_length": v.TextLength = arg.tointeger(); break;
    case "text_bytes":
      if (arg.len() % 2 != 0) throw "rows: odd hex " + arg;
      for (local i = 0; i < arg.len(); i += 2) v.Text[i / 2] = nibble(arg[i]) * 16 + nibble(arg[i + 1]);
      v.TextLength = arg.len() / 2;
      break;
    case "text_fill": {
      local n = arg.tointeger();
      for (local i = 0; i < n; i++) v.Text[i] = 'a';
      v.TextLength = n;
      break;
    }
    case "fixed_constant": v.FixedConstant = arg.tointeger(); break;
    case "reserved_zero": v.ReservedZero = arg.tointeger(); break;
    case "ratio": v.Ratio = real(arg); break;
    default: throw "rows: unknown op " + op;
  }
};

foreach (line in split(text, "\n")) {
  line = strip(line);
  if (line.len() == 0 || line[0] == '#') continue;
  local f = [];
  foreach (x in split(line, " \t")) if (x.len() > 0) f.append(x);
  if (f.len() != 4) throw "rows: bad line " + line;
  local name = f[0], op = f[2], arg = f[3];
  local v = P.PacketWriteRefusal();
  apply(v, op, arg);
  local stream = serialize.WriteStream(blob(256));
  local ok;
  try {
    ok = P.WritePacketWriteRefusal(stream, v);
  } catch (e) {
    if ((op in RAISES) && typeof e == "string" && e.find(TRAP) == 0) {
      print(name + " refused\n");
    } else {
      print(name + " raised, not this row's refusal: " + e + "\n");
    }
    continue;
  }
  if (typeof ok == "bool" && !ok) print(name + " refused\n");
  else if (typeof ok == "bool" && ok) print(name + " wrote\n");
  else print(name + " returned " + typeof ok + ", neither true nor false\n");
}
