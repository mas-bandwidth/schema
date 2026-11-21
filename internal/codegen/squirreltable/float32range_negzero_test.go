package squirreltable_test

// A COMPRESSED FLOAT32'S RANGE IS HELD AT FLOAT32, and its -0 IS +0, in every
// read and write of the Squirrel leg (docs/SPEC.md §4.6: the triple is
// evaluated at float32; docs/SPEC-TABLES.md §3.3 and the ruling
// fixed-compressed-negzero). An end float32 cannot spell exactly (max = 0.1)
// is the float32 nearest it: the value at that end writes and reads back, in
// the variable, JSON, message and fixed forms, and one float32 step past it
// is refused. A -0 is stored as +0 by every writer and lands as +0 on every
// read; the read is fed the -0 bits by patching a stored 0.25 in place.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

const float32RangeSchema = `package probe

table V
{
    r float32 | min = 0, max = 0.1, resolution = 0.1
    d float32 = 0.1 | min = 0, max = 0.1, resolution = 0.1
    z float32 = 0.5 | min = -1, max = 1, resolution = 0.25
}

fixed table F
{
    r float32 | min = 0, max = 0.1, resolution = 0.1
    d float32 = 0.1 | min = 0, max = 0.1, resolution = 0.1
    z float32 = 0.5 | min = -1, max = 1, resolution = 0.25
}
`

const float32RangeMain = `local T = dofile("probe/schema_module.nut", true).require(%q);
local f32 = function(x) { local b = blob(4); b.writen(x, 'f'); b.seek(0); return b.readn('f'); };
local sign = function(x) { return x == 0 ? (1.0 / x > 0 ? "+0" : "-0") : "" + x; };
local same32 = function(x, want) { return x != null && f32(x) == f32(want); };
// patch rewrites the one stored 0.25 (00 00 80 3e) in b to -0 (00 00 00 80)
local patch = function(b) {
  local hits = 0;
  for (local i = 0; i + 4 <= b.len(); i++) {
    if ((b[i] & 0xff) == 0 && (b[i+1] & 0xff) == 0 && (b[i+2] & 0xff) == 0x80 && (b[i+3] & 0xff) == 0x3e) {
      b[i+2] = 0; b[i+3] = 0x80; hits++;
    }
  }
  return hits == 1;
};
// negbits counts the stored -0 patterns (00 00 00 80) in b
local negbits = function(b) {
  local hits = 0;
  for (local i = 0; i + 4 <= b.len(); i++) if ((b[i] & 0xff) == 0 && (b[i+1] & 0xff) == 0 && (b[i+2] & 0xff) == 0 && (b[i+3] & 0xff) == 0x80) hits++;
  return hits;
};
local say = function(k, v) { print(k + " " + v + "\n"); };

// THE VARIABLE FORM
local varSave = function(setup) { local v = T.V(); setup(v); return T.VSave(v); };
local b = varSave(function(v) { v.R = 0.1; });
local back = b == null ? null : T.VLoad(b);
say("var_max", b == null ? "unwritten" : (back == null ? "refused" : (same32(back.R, 0.1) ? "read" : "moved")));
say("var_f32", varSave(function(v) { v.R = f32(0.1); }) == null ? "refused" : "wrote");
say("var_past", varSave(function(v) { v.R = 0.1000001; }) == null ? "refused" : "wrote");
b = varSave(function(v) { v.Z = -0.0; });
back = b == null ? null : T.VLoad(b);
say("var_negzero_store", back == null ? "none" : sign(back.Z) + " " + negbits(b));
b = varSave(function(v) { v.Z = 0.25; });
back = (b != null && patch(b)) ? T.VLoad(b) : null;
say("var_negzero_read", back == null ? "none" : sign(back.Z));

// THE JSON FORM
local j = T.VFromJson("{\"r\":0.1}");
say("json_max", j == null ? "refused" : (same32(j.R, 0.1) ? "read" : "moved"));
j = T.VFromJson("{\"r\":0.1000001}");
say("json_past", j == null ? "refused" : "read");
j = T.VFromJson("{\"z\":-0.0}");
say("json_negzero_read", j == null ? "refused" : sign(j.Z));
local v = T.V(); v.R = 0.1;
say("json_write_max", T.VToJson(v) == null ? "refused" : "wrote");
v = T.V(); v.Z = -0.0;
local text = T.VToJson(v);
say("json_write_negzero", text == null ? "refused" : (text.find("-0") == null ? "+0" : "-0"));

// THE MESSAGE FORM
local voc = {};
if (!T.AnnounceRead(voc, T.TableMessageAnnouncement, {})) throw "announce";
local msg = function(setup) {
  local f = T.F(); setup(f);
  local out = blob(64);
  local n = T.FSaveMessages([f], out, {});
  if (n < 0) return null;
  local c = blob(n);
  for (local i = 0; i < n; i++) c[i] = out[i];
  local got = [T.F()];
  local rr = {};
  local k = T.FLoadMessages(got, voc, c, rr);
  return rr.refused ? "refused" : got[0];
};
local m = msg(function(f) { f.R = 0.1; });
say("msg_max", m == null ? "unwritten" : (typeof m == "string" ? m : (same32(m.R, 0.1) ? "read" : "moved")));
say("msg_f32", msg(function(f) { f.R = f32(0.1); }) == null ? "refused" : "wrote");
say("msg_past", msg(function(f) { f.R = 0.1000001; }) == null ? "refused" : "wrote");

// THE FIXED FORM
local fixedSave = function(setup) {
  local f = T.F(); setup(f);
  local out = blob(T.FFixedMeasure(1));
  return T.FFixedSave(out, [f]) < 0 ? null : out;
};
local fixedLoad = function(b) {
  local got = [T.F()];
  return T.FFixedLoad(b, got, null) == 1 ? got[0] : null;
};
b = fixedSave(function(f) { f.R = 0.1; });
back = b == null ? null : fixedLoad(b);
say("fixed_max", b == null ? "unwritten" : (back == null ? "refused" : (same32(back.R, 0.1) ? "read" : "moved")));
say("fixed_f32", fixedSave(function(f) { f.R = f32(0.1); }) == null ? "refused" : "wrote");
say("fixed_past", fixedSave(function(f) { f.R = 0.1000001; }) == null ? "refused" : "wrote");
b = fixedSave(function(f) { f.Z = -0.0; });
back = b == null ? null : fixedLoad(b);
say("fixed_negzero_store", back == null ? "none" : sign(back.Z) + " " + negbits(b));
b = fixedSave(function(f) { f.Z = 0.25; });
back = (b != null && patch(b)) ? fixedLoad(b) : null;
say("fixed_negzero_read", back == null ? "none" : sign(back.Z));
`

func TestSquirrelFloat32RangeAtFloat32AndNegativeZero(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	dir := t.TempDir()
	mod := emitUnit(t, dir, "probe", unitFromSchema(t, float32RangeSchema))
	writeFile(t, filepath.Join(dir, "main.nut"), fmt.Appendf(nil, float32RangeMain, mod))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", nil)), "\n"), "\n") {
		if k, v, ok := strings.Cut(line, " "); ok {
			got[k] = v
		}
	}
	for _, row := range [][2]string{
		{"var_max", "read"}, {"var_f32", "wrote"}, {"var_past", "refused"},
		{"var_negzero_store", "+0 0"}, {"var_negzero_read", "+0"},
		{"json_max", "read"}, {"json_past", "refused"}, {"json_negzero_read", "+0"},
		{"json_write_max", "wrote"}, {"json_write_negzero", "+0"},
		{"msg_max", "read"}, {"msg_f32", "wrote"}, {"msg_past", "refused"},
		{"fixed_max", "read"}, {"fixed_f32", "wrote"}, {"fixed_past", "refused"},
		{"fixed_negzero_store", "+0 0"}, {"fixed_negzero_read", "+0"},
	} {
		if got[row[0]] != row[1] {
			t.Errorf("%s: %q, want %q", row[0], got[row[0]], row[1])
		}
	}
}
