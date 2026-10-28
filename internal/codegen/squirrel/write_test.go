package squirrel_test

import (
	"bytes"
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

// goldenNames is the C++-pinned corpus test/go/main.go writes before its
// goldenWire calls (SPEC §6.1, §4.3): the fourteen instances the write side
// must reproduce byte for byte.
var goldenNames = []string{
	"shipcreate_flags",
	"rigidbody_moving",
	"rigidbody_at_rest",
	"chat",
	"probe_header",
	"compressed_probe",
	"probebits",
	"probecollider",
	"testdata",
	"inputpacket",
	"probearray",
	"degenerate",
	"clauses",
	"joins",
}

// TestSquirrelTypeWireWritesTheGoldens generates examples/*.schema into a
// t.TempDir, builds each pinned instance in Squirrel, writes it through the
// emitted Write<Name>(stream, v) against the vendored serialize port's
// WriteStream, and byte-compares against testdata/wire/<name>.bin. The bytes
// are the C++ target's (SPEC §4.3, §6.1); the write side owns no private bit
// writer (docs/PORT-SQUIRREL.md, THE TYPE MAPPING and THE RUNTIME LAYOUT).
func TestSquirrelTypeWireWritesTheGoldens(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	files := squirrelTypeWireFiles(t, root)
	got := writeSquirrelTypeWire(t, files)
	for _, name := range goldenNames {
		want, err := os.ReadFile(filepath.Join(root, "testdata", "wire", name+".bin"))
		if err != nil {
			t.Fatalf("read wire golden %s: %v", name, err)
		}
		have, ok := got[name]
		if !ok {
			t.Errorf("%s: the probe printed no wire", name)
			continue
		}
		if !bytes.Equal(have, want) {
			t.Errorf("%s: wrote %x, want the C++-pinned %x", name, have, want)
		}
	}
}

// TestSquirrelTypeWireWriteNegativeControl patches the emitted write in
// memory so one bool writes two bits instead of one (the ShipCreate has_flags
// gate), matched by one anchor that occurs exactly once, and requires the
// byte comparison against the same golden to go red: the golden compare
// would not be worth its green if a one-bit error stayed invisible.
func TestSquirrelTypeWireWriteNegativeControl(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	files := squirrelTypeWireFiles(t, root)
	const anchor = "_h.value = value.HasFlags;\n  if (!stream.serializeBool(_h)) return false;"
	count := 0
	for _, data := range files {
		count += strings.Count(string(data), anchor)
	}
	if count != 1 {
		t.Fatalf("the sabotage anchor %q matches %d times, want exactly 1", anchor, count)
	}
	for name, data := range files {
		if strings.Contains(string(data), anchor) {
			files[name] = []byte(strings.Replace(string(data), anchor,
				"_h.value = value.HasFlags ? 1 : 0;\n  if (!stream.serializeBits(_h, 2)) return false;", 1))
		}
	}
	got := writeSquirrelTypeWire(t, files)
	want, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "shipcreate_flags.bin"))
	if err != nil {
		t.Fatal(err)
	}
	have, ok := got["shipcreate_flags"]
	if !ok {
		t.Fatalf("the sabotaged probe printed no shipcreate_flags wire")
	}
	if bytes.Equal(have, want) {
		t.Fatalf("the saboted two-bit bool still matched the golden %x", want)
	}
}

// squirrelTypeWireFiles loads examples/*.schema, generates the type surface,
// and adds the vendored serialize port beside the modules: the write side's
// bottom layer, emitted under its own names (docs/PORT-SQUIRREL.md, THE
// RUNTIME LAYOUT; internal/codegen/squirrel/serialize.go).
func squirrelTypeWireFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	u := loadUnit(t, filepath.Join(root, "examples"))
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, squirrel.Serialize())
	return files
}

// writeSquirrelTypeWire writes the files into a t.TempDir, runs the probe
// under the pinned interpreter (sqrun, docs/PORT-SQUIRREL.md, THE HARNESS),
// and decodes one `name hex` line per golden.
func writeSquirrelTypeWire(t *testing.T, files map[string][]byte) map[string][]byte {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(typeWireProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := sqrun.Run(t, dir, "main.nut", nil)
	got := map[string][]byte{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		name, text, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("probe line %q is not a golden name and hex", line)
		}
		data, err := hex.DecodeString(text)
		if err != nil {
			t.Fatalf("%s: probe bytes %q are not hex: %v", name, text, err)
		}
		got[name] = data
	}
	return got
}

// typeWireProbe writes the fourteen pinned instances typeWireCases builds and
// prints each written wire as one lowercase-hex line. Every write goes
// through a WriteStream over a blob; the stream's latched error is never
// swallowed (a false throws, naming the golden). A "one" or "seq" case writes
// its shapes back to back into one stream (degenerate's twelve); a "parts"
// case writes each shape into its own flushed stream and concatenates, the
// way every emitter produces the clauses and joins mid-byte goldens.
const typeWireProbe = typeWireCases + `
local emit = function(name, data) {
  print(name + " ");
  ::hexline(data);
};

local cat = function(parts) {
  local n = 0;
  foreach (p in parts) n += p.len();
  local out = blob(n);
  local at = 0;
  foreach (p in parts) {
    for (local i = 0; i < p.len(); i++) out[at + i] = p[i];
    at += p.len();
  }
  return out;
};

foreach (c in ::typeWireCases(false)) {
  if (c.mode == "parts") {
    local parts = [];
    foreach (it in c.items) {
      local ws = serialize.WriteStream(blob(2048));
      if (!it.m["Write" + it.t](ws, it.v)) throw c.name + " " + it.t + ": write failed: " + ws.error();
      ws.flush();
      parts.append(ws.data());
    }
    emit(c.name, cat(parts));
  } else {
    local ws = serialize.WriteStream(blob(2048));
    foreach (it in c.items) {
      if (!it.m["Write" + it.t](ws, it.v)) throw c.name + " " + it.t + ": write failed: " + ws.error();
    }
    ws.flush();
    emit(c.name, ws.data());
  }
}
`

// typeWireCases is the Squirrel prelude both type-wire probes share: it loads
// the generated modules and the vendored serialize port, and defines
// ::typeWireCases(want), the fourteen pinned instances test/go/main.go builds,
// in its order, as { name, mode, items = [{ m, t, v }] }. want = false is the
// value written; want = true is the value a read of the golden must produce:
// the same instance with the untaken side of every wire branch zero (SPEC §5),
// exactly as test/go/main.go states its read-back expectations (the at-rest
// RigidBody, every Joins shape) and, for CompressedProbe, the reconstructed
// step values main.go checks (float32 per-op arithmetic, SPEC §4.3).
const typeWireCases = `local schema = dofile("schema_module.nut", true);
local serialize = dofile("serialize.nut", true);
local in_ = dofile("inputs.nut", true); // the goldens, name -> blob; defines ::hexline

local Types = schema.require("Types");
local Wire = schema.require("Wire");
local Enums = schema.require("Enums");
local Degenerate = schema.require("Degenerate");
local Clauses = schema.require("Clauses");
local Joins = schema.require("Joins");

local setBytes = function(b, s) {
  for (local i = 0; i < s.len(); i++) b[i] = s[i] & 0xff;
};

// f32 rounds a double to the nearest float32, the reader's per-op arithmetic
local f32 = function(x) {
  local b = blob(4);
  b.writen(x, 'f');
  b.seek(0);
  return b.readn('f');
};

local seq = function(n) {
  local out = [];
  for (local i = 0; i < n; i++) out.append(i);
  return out;
};

::typeWireCases <- function(want) {
local cases = [];
local one = function(name, mod, type, v) {
  cases.append({ name = name, mode = "one", items = [{ m = mod, t = type, v = v }] });
};

// ---- shipcreate_flags ----
{
  local v = Types.ShipCreate();
  v.ShipType = Enums.ShipType.Bomber;
  v.Position.X = 1000;
  v.Position.Y = -2000;
  v.Position.Z = 3000;
  v.HasFlags = true;
  v.Flags = Enums.ShipFlagsBoosting | Enums.ShipFlagsAiming;
  v.Team = Enums.Team.Blue;
  v.Health = 750;
  v.Thrust = 55;
  one("shipcreate_flags", Types, "ShipCreate", v);
}

// ---- rigidbody_moving and rigidbody_at_rest ----
// the at-rest read zeroes both velocities (SPEC §5), though the written
// value had them set
local rigidBody = function(atRest) {
  local v = Types.RigidBody();
  v.Position.X = 1.5;
  v.Position.Y = -2.5;
  v.Position.Z = 3.25;
  v.Orientation.X = 0.1;
  v.Orientation.Y = 0.2;
  v.Orientation.Z = 0.3;
  v.Orientation.W = 0.9;
  v.AtRest = atRest;
  if (!want || !atRest) {
    v.LinearVelocity.X = 10.0;
    v.LinearVelocity.Y = 20.0;
    v.LinearVelocity.Z = -3.0;
    v.AngularVelocity.X = 0.25;
    v.AngularVelocity.Y = 0.5;
    v.AngularVelocity.Z = 0.75;
  }
  return v;
};
one("rigidbody_moving", Types, "RigidBody", rigidBody(false));
one("rigidbody_at_rest", Types, "RigidBody", rigidBody(true));

// ---- chat ----
{
  local v = Wire.Chat();
  setBytes(v.Text, "wire parity");
  v.TextLength = 11;
  one("chat", Wire, "Chat", v);
}

// ---- probe_header ----
{
  local v = Wire.ProbeHeader();
  v.Version = 5;
  v.ProbeId = 0x1122334455667788;
  one("probe_header", Wire, "ProbeHeader", v);
}

// ---- compressed_probe ----
{
  local v = Wire.CompressedProbe();
  v.Boundary = 0.005;
  v.Offset = -4.8585;
  if (want) {
    // main.go: boundary reconstructs integer 1, offset integer 142, in
    // float32 per-op arithmetic (the FMA-boundary vectors, SPEC §7.2 gate 7)
    v.Boundary = f32(f32(f32(1.0 / 1000.0) * 10.0));
    v.Offset = f32(f32(f32(142.0 / 10000.0) * 10.0) - 5.0);
  }
  one("compressed_probe", Wire, "CompressedProbe", v);
}

// ---- probebits ----
{
  local v = Wire.ProbeBits();
  v.Small = 0x1ff;
  v.Boundary = 0x1ffffffff;
  v.Wide = 0xfedcba9876543210;
  v.Sensor = 4294967295;
  v.Nonce = 0xffffffffffffffff;
  one("probebits", Wire, "ProbeBits", v);
}

// ---- probecollider ----
{
  local v = Wire.ProbeCollider();
  v.Armor = 7;
  v.Shape.Type = Wire.ProbeShapeType.Slab;
  v.Shape.Slab.Width = 42;
  v.Shape.Slab.Height = 9;
  v.ExtrasCount = 1;
  v.Extras[0].Type = Wire.ProbeShapeType.Ring;
  v.Extras[0].Ring.Radius = 777;
  one("probecollider", Wire, "ProbeCollider", v);
}

// ---- testdata ----
local testData = function() {
  local v = Wire.TestData();
  v.A = -100;
  v.B = 100;
  v.C = 149;
  v.D = 0x11;
  v.E = 0x22;
  v.F = 0x33;
  v.G = true;
  v.ItemsCount = 3;
  v.Items[0] = 0;
  v.Items[1] = 128;
  v.Items[2] = 255;
  v.FloatValue = 3.1415926;
  v.CompressedFloatValue = 2.5;
  v.DoubleValue = 1.0 / 3.0;
  v.Int8Value = -128;
  v.Int16Value = -32768;
  v.Uint8Value = 255;
  v.Uint16Value = 65535;
  v.Uint32Value = 4294967295;
  v.Uint64Value = 0xffffffffffffffff;
  v.Int64Full = 0x8000000000000000;
  v.Int64Range = -999999999999;
  for (local i = 0; i < v.FixedBytes.len(); i++) v.FixedBytes[i] = (i * 3) & 0xff;
  setBytes(v.Text, "the quick brown fox");
  v.TextLength = 19;
  return v;
};
one("testdata", Wire, "TestData", testData());

// ---- inputpacket ----
local inputPacket = function() {
  local v = Types.InputPacket();
  v.SynchronizeSequence = 7;
  v.CurrentFrame = 123456789;
  v.StartFrame = 123456780;
  v.InputsCount = 2;
  v.Inputs[0].Throttle = 0.5;
  v.Inputs[0].Fire = true;
  v.Inputs[1].StickX = -0.25;
  v.Inputs[1].Boost = true;
  return v;
};
one("inputpacket", Types, "InputPacket", inputPacket());

// ---- probearray ----
{
  local v = Wire.ProbeArray();
  v.Samples[0].Orientation = 90.0;
  v.Samples[0].RawDelta = -5;
  v.Samples[0].BigDelta = -1234567890123;
  v.Samples[0].Weapon = Wire.Weapon.Laser;
  v.Samples[0].HasTarget = true;
  v.Samples[0].TargetId = 777;
  v.Samples[0].SamplesCount = 1;
  v.Samples[0].Samples[0] = 42;
  v.Samples[1].Active = false;
  v.Samples[1].Orientation = -45.5;
  // main.go pins only sample 0's orientation exactly (== 90.0); sample 1's
  // reads back within half its 0.01 step (SPEC §4.3)
  if (want) v.Samples[1].Orientation = { within = -45.5, tol = 0.005 };
  v.Samples[1].RawDelta = 7;
  v.Samples[1].BigDelta = 99;
  v.Samples[1].IdleTicks = 1000;
  v.Samples[1].SamplesCount = 2;
  v.Samples[1].Samples[0] = 7;
  v.Samples[1].Samples[1] = 8;
  v.Config.Retries = 3;
  v.Config.Preferred = Wire.Weapon.Missile;
  one("probearray", Wire, "ProbeArray", v);
}

// ---- degenerate: twelve shapes, one stream, in the C++ test's order ----
{
  local items = [];
  local w = function(type, v) {
    items.append({ m = Degenerate, t = type, v = v });
  };

  local vec2 = Degenerate.Vec2();
  vec2.X = 1.5;
  vec2.Y = -2.25;
  w("Vec2", vec2);

  local spanF64 = Degenerate.SpanF64();
  spanF64.Values[0] = 3.5;
  spanF64.Values[1] = -4.75;
  w("SpanF64", spanF64);

  local spanU64 = Degenerate.SpanU64();
  spanU64.Values[0] = 0xdeadbeefcafebabe;
  spanU64.Values[1] = 1;
  w("SpanU64", spanU64);

  local spanI64 = Degenerate.SpanI64();
  spanI64.Values[0] = -1234567890123;
  spanI64.Values[1] = 42;
  w("SpanI64", spanI64);

  local spanOne = Degenerate.SpanOne();
  spanOne.Values[0] = 0x0123456789abcdef;
  w("SpanOne", spanOne);

  local spanChunk = Degenerate.SpanChunk();
  spanChunk.Values[0] = 0x1111;
  spanChunk.Values[1] = 0x2222;
  spanChunk.Values[2] = 0x3333;
  spanChunk.Values[3] = 0x4444;
  w("SpanChunk", spanChunk);

  local spanTail = Degenerate.SpanTail();
  spanTail.Values[0] = 6.125;
  spanTail.Values[1] = -7.0;
  spanTail.Tail = 0xfeedface;
  w("SpanTail", spanTail);

  local spanTwice = Degenerate.SpanTwice();
  spanTwice.A[0] = 8.5;
  spanTwice.A[1] = 9.5;
  spanTwice.B[0] = -10.5;
  spanTwice.B[1] = -11.5;
  w("SpanTwice", spanTwice);

  local trio = Degenerate.Trio();
  trio.A = 0xabcde;
  trio.B = 0x12345;
  trio.C = 0xfffff;
  w("Trio", trio);

  local trioSole = Degenerate.TrioSole();
  trioSole.Inner.A = 1;
  trioSole.Inner.B = 2;
  trioSole.Inner.C = 3;
  w("TrioSole", trioSole);

  local trioFirst = Degenerate.TrioFirst();
  trioFirst.Inner.A = 0xaaaaa;
  trioFirst.Inner.B = 0x55555;
  trioFirst.Inner.C = 0xf0f0f;
  trioFirst.Trailer = 0xbeef;
  w("TrioFirst", trioFirst);

  local straddle = Degenerate.TrioStraddle();
  straddle.Pad0 = 0x0011223344556677;
  straddle.Pad1 = 0x8899aabbccddeeff;
  straddle.Pad2 = 0xffffffffffffffff;
  straddle.Pad3 = 0;
  straddle.Pad4 = 0x123456789abcdef0;
  straddle.Pad5 = 0xabcdef;
  straddle.Inner.A = 0x11111;
  straddle.Inner.B = 0x22222;
  straddle.Inner.C = 0x33333;
  w("TrioStraddle", straddle);

  cases.append({ name = "degenerate", mode = "seq", items = items });
}

// ---- clauses: mid-byte arrangements, one stream per shape, concatenated ----
{
  local parts = [];
  local part = function(mod, type, v) {
    parts.append({ m = mod, t = type, v = v });
  };

  foreach (c in [0, 1, 3, 4, 5, 7, 12]) {
    local v = Clauses.W13();
    v.ItemsCount = c;
    foreach (i in seq(c)) v.Items[i] = 8191 - i * 733;
    part(Clauses, "W13", v);
  }
  foreach (c in [0, 1, 2, 3, 4, 9]) {
    local v = Clauses.W17();
    v.ItemsCount = c;
    foreach (i in seq(c)) v.Items[i] = 131071 - i * 11117;
    part(Clauses, "W17", v);
  }
  foreach (c in [0, 1, 2, 3, 6]) {
    local v = Clauses.W26();
    v.ItemsCount = c;
    foreach (i in seq(c)) v.Items[i] = 67108863 - i * 5555555;
    part(Clauses, "W26", v);
  }
  foreach (c in [0, 1, 3, 4, 5, 20]) {
    local v = Clauses.W1();
    v.ItemsCount = c;
    foreach (i in seq(c)) v.Items[i] = i % 2;
    part(Clauses, "W1", v);
  }
  foreach (c in [0, 1, 2, 3]) {
    local v = Clauses.W52();
    v.ItemsCount = c;
    foreach (i in seq(c)) v.Items[i] = 4503599627370495 - i * 123456789;
    part(Clauses, "W52", v);
  }
  foreach (c in [0, 1, 2, 3]) {
    local v = Clauses.W50();
    v.ItemsCount = c;
    foreach (i in seq(c)) v.Items[i] = 1125899906842623 - i * 987654321;
    part(Clauses, "W50", v);
  }

  local f13 = Clauses.F13();
  foreach (i in seq(7)) f13.Items[i] = 8191 - i * 911;
  part(Clauses, "F13", f13);

  foreach (c in [0, 1, 3, 4, 5, 10]) {
    local v = Clauses.ArrTri3();
    v.ItemsCount = c;
    foreach (i in seq(c)) {
      v.Items[i].A = i % 2;
      v.Items[i].B = i % 4;
    }
    part(Clauses, "ArrTri3", v);
  }

  local arrEleven = Clauses.ArrEleven();
  for (local i = 0; i < 9; i++) {
    arrEleven.Items[i].A = i % 8;
    arrEleven.Items[i].B = 255 - i * 17;
  }
  part(Clauses, "ArrEleven", arrEleven);

  foreach (tag in [Clauses.EmptyUnionType.None, Clauses.EmptyUnionType.A, Clauses.EmptyUnionType.B]) {
    local v = Clauses.HoldsEmptyUnion();
    v.Lead = 21;
    v.Tail = 99;
    v.U.Type = tag;
    part(Clauses, "HoldsEmptyUnion", v);
  }

  local strsAt = function(s, b) {
    local v = Clauses.Strs();
    v.Lead = 21;
    v.Tail = 5;
    setBytes(v.S, s);
    v.SLength = s.len();
    setBytes(v.B, b);
    v.BLength = b.len();
    return v;
  };
  part(Clauses, "Strs", strsAt("", ""));
  part(Clauses, "Strs", strsAt("abcdefgh", "\xf0\xf1\xf2\xf3\xf4\xf5\xf6\xf7"));
  part(Clauses, "Strs", strsAt("xyz", "\x01\x02\x03"));

  foreach (c in [0, 1, 2, 3, 4]) {
    local v = Clauses.ArrNested();
    v.Lead = 21;
    v.Tail = 5;
    v.ItemsCount = c;
    foreach (i in seq(c)) {
      v.Items[i].A = i % 8;
      v.Items[i].B = 200 - i * 7;
    }
    part(Clauses, "ArrNested", v);
  }

  local sole = Clauses.Sole();
  sole.Only = 5555;
  part(Clauses, "Sole", sole);

  cases.append({ name = "clauses", mode = "parts", items = parts });
}

// ---- joins: branches on both arms, unequal unions, aligns ----
{
  local parts = [];
  local part = function(mod, type, v) {
    parts.append({ m = mod, t = type, v = v });
  };

  foreach (f in [false, true]) {
    local agree = Joins.ArmsAgree();
    agree.Lead = 21;
    agree.Flag = f;
    // the untaken arm reads back zero (SPEC §5), main.go's want
    if (!want || f) agree.A = 1234;
    if (!want || !f) agree.B = 1500;
    agree.Tail = 99;
    part(Joins, "ArmsAgree", agree);

    local disagree = Joins.ArmsDisagree();
    disagree.Lead = 21;
    disagree.Flag = f;
    if (!want || f) disagree.A = 1234;
    if (!want || !f) disagree.B = 5;
    disagree.Tail = 99;
    part(Joins, "ArmsDisagree", disagree);

    local armEmpty = Joins.ArmEmpty();
    armEmpty.Lead = 21;
    armEmpty.Flag = f;
    if (!want || f) armEmpty.A = 456789;
    armEmpty.Tail = 99;
    part(Joins, "ArmEmpty", armEmpty);

    local alignStr = Joins.ArmAlign();
    alignStr.Lead = 21;
    alignStr.Flag = f;
    if (!want || f) {
      setBytes(alignStr.S, "abcd");
      alignStr.SLength = 4;
    }
    if (!want || !f) alignStr.B = 1000;
    alignStr.Tail = 99;
    part(Joins, "ArmAlign", alignStr);

    local alignEmpty = Joins.ArmAlign();
    alignEmpty.Lead = 21;
    alignEmpty.Flag = f;
    if (!want || !f) alignEmpty.B = 1000;
    alignEmpty.Tail = 99;
    part(Joins, "ArmAlign", alignEmpty);
  }

  foreach (o in [false, true]) {
    foreach (i in [false, true]) {
      local v = Joins.ArmsNested();
      v.Lead = 5;
      v.Outer = o;
      if (!want || o) v.Inner = i;
      if (!want || (o && i)) v.X = 500000000;
      if (!want || (o && !i)) v.Y = 17;
      if (!want || !o) v.Z = 4000;
      v.Tail = 33;
      part(Joins, "ArmsNested", v);
    }
  }

  foreach (f in [false, true]) {
    foreach (c in [0, 1, 2, 3]) {
      local v = Joins.ArmArray();
      v.Lead = 21;
      v.Flag = f;
      if (!want || !f) v.B = 300;
      v.Tail = 99;
      if (!want || f) {
        v.ItemsCount = c;
        foreach (i in seq(c)) v.Items[i] = 8191 - i * 777;
      }
      part(Joins, "ArmArray", v);
    }
  }

  foreach (tag in [Joins.UnevenType.None, Joins.UnevenType.Narrow, Joins.UnevenType.Wide]) {
    local v = Joins.HoldsUneven();
    v.Lead = 21;
    v.Tail = 1500;
    v.U.Type = tag;
    if (tag == Joins.UnevenType.Narrow) v.U.Narrow.N = 5;
    if (tag == Joins.UnevenType.Wide) v.U.Wide.W = 123456789012;
    part(Joins, "HoldsUneven", v);
  }

  foreach (c in [0, 1, 2, 3]) {
    local v = Joins.ArrUneven();
    v.Lead = 21;
    v.Tail = 5;
    v.ItemsCount = c;
    foreach (i in seq(c)) {
      if (i % 2 == 0) {
        v.Items[i].Type = Joins.UnevenType.Narrow;
        v.Items[i].Narrow.N = i % 8;
      } else {
        v.Items[i].Type = Joins.UnevenType.Wide;
        v.Items[i].Wide.W = 99887766554 + i;
      }
    }
    part(Joins, "ArrUneven", v);
  }

  foreach (c in [0, 1, 2, 3]) {
    foreach (sl in [0, 4]) {
      local v = Joins.RegainAfterAlign();
      v.Lead = 21;
      v.ItemsCount = c;
      v.SLength = sl;
      v.P = 0xdeadbeef;
      v.Q = (1 << 29) - 7;
      v.R = (1 << 19) - 3;
      v.Tail = 9;
      if (sl > 0) setBytes(v.S, "wxyz");
      foreach (i in seq(c)) v.Items[i] = 8191 - i * 999;
      part(Joins, "RegainAfterAlign", v);
    }
  }

  cases.append({ name = "joins", mode = "parts", items = parts });
}

return cases;
};
`
