package moo

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// readBack names each single-instance golden and the type that reads it.
var readBack = []struct{ golden, module, typ string }{
	{"shipcreate_flags", "Types", "ShipCreate"},
	{"rigidbody_moving", "Types", "RigidBody"},
	{"rigidbody_at_rest", "Types", "RigidBody"},
	{"chat", "Wire", "Chat"},
	{"probe_header", "Wire", "ProbeHeader"},
	{"compressed_probe", "Wire", "CompressedProbe"},
	{"probebits", "Wire", "ProbeBits"},
	{"probecollider", "Wire", "ProbeCollider"},
	{"testdata", "Wire", "TestData"},
	{"inputpacket", "Types", "InputPacket"},
	{"probearray", "Wire", "ProbeArray"},
}

func goldens(t testing.TB, names ...string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, n := range names {
		b, err := os.ReadFile(repo("testdata", "wire", n+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		out[n] = b
	}
	return out
}

// TestMooTypeWireReadsTheGoldens reads every single-instance C++ golden into
// a fresh value, requires serializeEnd to accept what is left (no trailing
// byte, zero padding), writes the value back and requires the same bytes: the
// reader recovers every field the writer is held to byte for byte.
func TestMooTypeWireReadsTheGoldens(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated modules under the pinned interpreter)")
	t.Parallel()
	dir, _ := generateInto(t, unitFromDir(t, repo("examples")))
	var names []string
	var probe strings.Builder
	probe.WriteString("--!strict\nlocal serialize = require(\"./serialize\")\nlocal inputs = require(\"./inputs\")\n")
	probe.WriteString(moorun.HexProbe)
	for _, m := range []string{"Types", "Wire"} {
		probe.WriteString("local " + m + " = require(\"./" + m + "\")\n")
	}
	for _, r := range readBack {
		names = append(names, r.golden)
		probe.WriteString("do\n")
		probe.WriteString("\tlocal v = " + r.module + ".New" + r.typ + "()\n")
		probe.WriteString("\tlocal rs = serialize.ReadStream.new(inputs[\"" + r.golden + "\"])\n")
		probe.WriteString("\tassert(" + r.module + ".Read" + r.typ + "(rs, v), \"read " + r.golden + "\")\n")
		probe.WriteString("\tassert(rs:serializeEnd(), \"end " + r.golden + "\")\n")
		probe.WriteString("\tlocal ws = serialize.WriteStream.new(buffer.create(" + r.module + "." + r.typ + "MaxBytes))\n")
		probe.WriteString("\tassert(" + r.module + ".Write" + r.typ + "(ws, v), \"rewrite " + r.golden + "\")\n")
		probe.WriteString("\tws:flush()\n\thex(ws:data())\nend\n")
	}
	writeFile(t, dir, "main.moo", probe.String())
	in := goldens(t, names...)
	lines := moorun.HexLines(t, moorun.Run(t, dir, "main.moo", in))
	if len(lines) != len(readBack) {
		t.Fatalf("probe produced %d lines, want %d", len(lines), len(readBack))
	}
	for i, r := range readBack {
		if !bytes.Equal(lines[i], in[r.golden]) {
			t.Errorf("%s: read then rewrite gave\n  %x\nwant\n  %x", r.golden, lines[i], in[r.golden])
		}
	}
}

// TestMooTypeWireRefusesTruncation reads every single-instance golden cut
// short at every byte: each read refuses (or its serializeEnd does), and
// none raises — hostile bytes never trap a reader.
func TestMooTypeWireRefusesTruncation(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated modules under the pinned interpreter)")
	t.Parallel()
	dir, _ := generateInto(t, unitFromDir(t, repo("examples")))
	var names []string
	var probe strings.Builder
	probe.WriteString("--!strict\nlocal serialize = require(\"./serialize\")\nlocal inputs = require(\"./inputs\")\n")
	probe.WriteString("local Types = require(\"./Types\")\nlocal Wire = require(\"./Wire\")\nlocal mods: { [string]: any } = { Types = Types, Wire = Wire }\n")
	probe.WriteString("local refused, cases = 0, 0\n")
	for _, r := range readBack {
		names = append(names, r.golden)
		probe.WriteString("do\n\tlocal full = inputs[\"" + r.golden + "\"]\n")
		probe.WriteString("\tfor n = 0, buffer.len(full) - 1 do\n")
		probe.WriteString("\t\tlocal cut = buffer.create(n)\n\t\tbuffer.copy(cut, 0, full, 0, n)\n")
		probe.WriteString("\t\tlocal rs = serialize.ReadStream.new(cut)\n")
		probe.WriteString("\t\tlocal ok = mods[\"" + r.module + "\"].Read" + r.typ + "(rs, mods[\"" + r.module + "\"].New" + r.typ + "()) and rs:serializeEnd()\n")
		probe.WriteString("\t\tcases += 1\n\t\tif not ok then refused += 1 else print(\"ACCEPTED " + r.golden + " at \" .. n) end\n")
		probe.WriteString("\tend\nend\n")
	}
	probe.WriteString("print(\"refused \" .. refused .. \" of \" .. cases)\n")
	writeFile(t, dir, "main.moo", probe.String())
	out := string(moorun.Run(t, dir, "main.moo", goldens(t, names...)))
	if strings.Contains(out, "ACCEPTED") || !strings.Contains(out, "refused ") {
		t.Fatalf("a truncated golden was accepted:\n%s", out)
	}
	t.Log(strings.TrimSpace(out))
}

// TestMooTypeWireRefusesHostileValues plants one hostile value per rule into
// a golden image and requires the read to refuse it, each for its reason:
// a non-finite float pattern (non_finite_float), a const byte that differs
// (false, unlatched), a ranged int past its range (value_out_of_range), an
// enum above max, a string with a zero byte (embedded_nul).
func TestMooTypeWireRefusesHostileValues(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated modules under the pinned interpreter)")
	t.Parallel()
	dir, _ := generateInto(t, unitFromDir(t, repo("examples")))
	probe := `--!strict
local serialize = require("./serialize")
local Types = require("./Types")
local Wire = require("./Wire")

local function image(write: (any) -> boolean): buffer
	local ws = serialize.WriteStream.new(buffer.create(512))
	assert(write(ws))
	ws:flush()
	return ws:data()
end
local function report(name: string, ok: boolean, rs: any)
	print(name .. " " .. (if ok then "accepted" else rs:refusal()))
end

do -- a float32 pattern with an all-ones exponent is refused on its raw bits
	local v = Types.NewInput()
	local b = image(function(s) return Types.WriteInput(s, v) end)
	buffer.writeu32(b, 0, 0x7F800000) -- stick_x = +inf
	local rs = serialize.ReadStream.new(b)
	report("float-inf", Types.ReadInput(rs, Types.NewInput()), rs)
	buffer.writeu32(b, 0, 0x7FC00001) -- a NaN with a payload
	rs = serialize.ReadStream.new(b)
	report("float-nan", Types.ReadInput(rs, Types.NewInput()), rs)
end

do -- the const(0xAB, 8) magic of ProbeHeader
	local b = image(function(s) return Wire.WriteProbeHeader(s, Wire.NewProbeHeader()) end)
	buffer.writeu8(b, 0, 0xAC)
	local rs = serialize.ReadStream.new(b)
	report("const", Wire.ReadProbeHeader(rs, Wire.NewProbeHeader()), rs)
end

do -- reserved(5) bits set
	local b = image(function(s) return Wire.WriteProbeHeader(s, Wire.NewProbeHeader()) end)
	buffer.writeu8(b, 1, 0x80)
	local rs = serialize.ReadStream.new(b)
	report("reserved", Wire.ReadProbeHeader(rs, Wire.NewProbeHeader()), rs)
end

do -- Handle.object_id in [0, MaxObjects - 1]: every offset bit set
	local b = image(function(s) return Types.WriteHandle(s, Types.NewHandle()) end)
	buffer.writeu8(b, 0, 0xFF)
	buffer.writeu8(b, 1, 0xFF)
	local rs = serialize.ReadStream.new(b)
	report("range", Types.ReadHandle(rs, Types.NewHandle()), rs)
end

do -- a string with a zero byte inside its length
	local v = Wire.NewChat()
	v.Text = "abc"
	local b = image(function(s) return Wire.WriteChat(s, v) end)
	buffer.writeu8(b, 3, 0)
	local rs = serialize.ReadStream.new(b)
	report("nul", Wire.ReadChat(rs, Wire.NewChat()), rs)
end
`
	writeFile(t, dir, "main.moo", probe)
	out := string(moorun.Run(t, dir, "main.moo", nil))
	want := []string{
		"float-inf non_finite_float",
		"float-nan non_finite_float",
		"const none",
		"reserved none",
		"range value_out_of_range",
		"nul embedded_nul",
	}
	got := strings.Split(strings.TrimSpace(out), "\n")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("hostile reads:\n%s\nwant:\n%s", out, strings.Join(want, "\n"))
	}
}

// TestMooTypeWireWriteRefusesOutOfContract: a value outside its field's
// contract makes the write return false before anything rides, unlatched,
// and never traps (docs/PORT-MOO.md, THE WIRE).
func TestMooTypeWireWriteRefusesOutOfContract(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated modules under the pinned interpreter)")
	t.Parallel()
	dir, _ := generateInto(t, unitFromDir(t, repo("examples")))
	probe := expandMacros(t, `--!strict
local serialize = require("./serialize")
local Types = require("./Types")
local Wire = require("./Wire")
local function try(name: string, write: (any) -> boolean)
	local ws = serialize.WriteStream.new(buffer.create(512))
	local ok, result = pcall(write, ws)
	print(name .. " " .. (if not ok then "raised" elseif result then "written" else "refused"))
end
local h = Types.NewHandle(); h.ObjectId = -1
try("range", function(s) return Types.WriteHandle(s, h) end)
local h2 = Types.NewHandle(); h2.ObjectId = 1.5
try("fraction", function(s) return Types.WriteHandle(s, h2) end)
local i = Types.NewInput(); i.StickX = F32(1.0); i.Yaw = 0x7F800000
try("inf", function(s) return Types.WriteInput(s, i) end)
local c = Wire.NewChat(); c.Text = "a\0b"
try("nul", function(s) return Wire.WriteChat(s, c) end)
local c2 = Wire.NewChat(); c2.Text = "\xC0\x80"
try("utf8", function(s) return Wire.WriteChat(s, c2) end)
local p = Wire.NewProbeHeader(); p.ProbeId = -1
try("unsigned", function(s) return Wire.WriteProbeHeader(s, p) end)
local r = Types.NewRigidBody(); r.Position.X = 1.5
try("double-as-number", function(s) return Types.WriteRigidBody(s, r) end)
local ok = Types.NewHandle(); ok.ObjectId = 3
try("valid", function(s) return Types.WriteHandle(s, ok) end)
`)
	writeFile(t, dir, "main.moo", probe)
	out := strings.TrimSpace(string(moorun.Run(t, dir, "main.moo", nil)))
	want := "range refused\nfraction refused\ninf refused\nnul refused\nutf8 refused\nunsigned refused\ndouble-as-number refused\nvalid written"
	if out != want {
		t.Fatalf("out-of-contract writes:\n%s\nwant:\n%s", out, want)
	}
}
