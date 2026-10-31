package squirreltable_test

// A TABLE FLOAT IS FINITE in the MESSAGE and FIXED forms too
// (docs/SPEC-TABLES.md §3.3, §3.4; Glenn, 2026-10-27, "same rule as
// packets"); nonfinite_test.go holds the variable form.
//
//   - MESSAGE (form 2): the writer refuses a NaN or an infinity before the
//     quantizer runs (SaveMessages and MeasureMessages answer -1, reason
//     `non_finite_float: <Owner>.<field>`), exactly as the Go oracle's
//     EncodeMessage refuses; the finite batch is the oracle's bytes; and a
//     batch whose float payload is patched to a non-finite pattern is refused
//     whole by LoadMessages, naming the field the oracle's DecodeMessages
//     names on the same bytes.
//   - FIXED (form 3): FixedSave refuses one (-1, FixedReason
//     non_finite_float, FixedField the field), a float32 that narrows to an
//     infinity included; FixedLoad refuses a record whose float bytes are a
//     NaN or an infinity, on the raw bits; -0 and the subnormals round trip
//     bit exact. Form 3 has no Go engine (its oracle is the C++ corpus), so
//     these rows pin the rule itself.
//
// TestSquirrelTableFiniteFloatFormsNegativeControl removes each form's read
// and write check in turn and requires exactly that check's rows to go red.

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const nfFormsSchema = `package mf

type Inner
{
    c float32
    d float64
}

fixed table Floats
{
    a float32
    b float64
    n Inner
    xs [3]float32
    q float32 | min = -10, max = 10, resolution = 0.01
}
`

// nfFormsLoc is one float slot of Floats: its Squirrel path, the oracle's
// setter, and the <Owner>.<field> a refusal names.
type nfFormsLoc struct {
	name, path, owner string
	f32, quantized    bool
	set               func(*tabletext.Instance, float64)
}

func nfFormsLocations(t *testing.T) []nfFormsLoc {
	return []nfFormsLoc{
		{"a", "v.A", "Floats.a", true, false, func(i *tabletext.Instance, x float64) { nfField(t, i, "a").Cell.F = x }},
		{"b", "v.B", "Floats.b", false, false, func(i *tabletext.Instance, x float64) { nfField(t, i, "b").Cell.F = x }},
		{"c", "v.N.C", "Inner.c", true, false, func(i *tabletext.Instance, x float64) {
			nfField(t, nfField(t, i, "n").Cell.Tab, "c").Cell.F = x
		}},
		{"d", "v.N.D", "Inner.d", false, false, func(i *tabletext.Instance, x float64) {
			nfField(t, nfField(t, i, "n").Cell.Tab, "d").Cell.F = x
		}},
		{"xs", "v.Xs[1]", "Floats.xs", true, false, func(i *tabletext.Instance, x float64) { nfField(t, i, "xs").Elems[1].F = x }},
		{"q", "v.Q", "Floats.q", true, true, func(i *tabletext.Instance, x float64) { nfField(t, i, "q").Cell.F = x }},
	}
}

// the finite values each slot holds in the base record: distinctive bits the
// read rows find in the oracle's batch and replace.
var nfFormsBase = map[string]uint64{
	"a": uint64(math.Float32bits(1.25)), "b": math.Float64bits(-7.0e-200),
	"c": uint64(math.Float32bits(13.5)), "d": math.Float64bits(3.0e100),
	"xs": uint64(math.Float32bits(-0.375)),
}

// nfBitAt reads width bits LSB-first at bit offset at.
func nfBitAt(b []byte, at, width int) uint64 {
	var v uint64
	for i := range width {
		p := at + i
		v |= uint64(b[p>>3]>>(p&7)&1) << i
	}
	return v
}

// nfReplaceBits replaces the one bit-aligned occurrence of from (width bits,
// LSB-first) in b with to.
func nfReplaceBits(t *testing.T, b []byte, from, to uint64, width int) []byte {
	t.Helper()
	at, n := -1, 0
	for p := 0; p+width <= len(b)*8; p++ {
		if nfBitAt(b, p, width) == from {
			at, n = p, n+1
		}
	}
	if n != 1 {
		t.Fatalf("the base value %x appears %d times in the batch, want once", from, n)
	}
	out := append([]byte(nil), b...)
	for i := range width {
		p := at + i
		out[p>>3] &^= 1 << (p & 7)
		out[p>>3] |= byte(to>>i&1) << (p & 7)
	}
	return out
}

const nfFormsProbe = `local I = dofile("inputs.nut", true);
local T = dofile("mf/schema_module.nut", true).require("__MOD__");
local hexInt = function(h) {
  local n = 0;
  foreach (c in h) n = (n << 4) | (c <= '9' ? c - '0' : c - 'a' + 10);
  return n;
};
local value = function(h) {
  if (h.len() == 16) return casti2f(hexInt(h));
  local b = blob(4);
  b.writen(hexInt(h), 'i');
  b.seek(0);
  return b.readn('f');
};
local f32bits = function(x) {
  local b = blob(4);
  b.writen(x, 'f');
  b.seek(0);
  return format("%08x", b.readn('i') & 0xffffffff);
};
local f64bits = function(x) { local b = castf2i(x); return format("%08x%08x", (b >>> 32) & 0xffffffff, b & 0xffffffff); };
local hex = function(b, n) {
  local s = "";
  for (local i = 0; i < n; i++) s += format("%02x", b[i] & 0xff);
  return s;
};
local nn = function(x) { return x == null ? "-" : x; };
local baseValue = function() {
  local v = T.Floats();
__BASE__
  return v;
};
local voc = {};
local ar = {};
if (!T.AnnounceRead(voc, T.TableMessageAnnouncement, ar)) throw "announce: " + ar.reason;
__ROWS__
`

// nfFormsRow is one probe statement and its expected line.
type nfFormsRow struct {
	key, code, want string
	kind            string // the check that must red it: msg-read, msg-write, fixed-read, fixed-write, or ""
}

func nfFormsVectors(t *testing.T) (*ir.Unit, []nfFormsRow, map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Mf.schema")
	if err := os.WriteFile(path, []byte(nfFormsSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, model, st := nfLoadPath(t, path, "Floats")
	locs := nfFormsLocations(t)
	baseInst := func() *tabletext.Instance {
		inst := model.New(st)
		for _, l := range locs {
			if bits, ok := nfFormsBase[l.name]; ok {
				if l.f32 {
					l.set(inst, float64(math.Float32frombits(uint32(bits))))
				} else {
					l.set(inst, math.Float64frombits(bits))
				}
			}
		}
		nfField(t, inst, "q").Cell.F = float64(float32(2.5))
		return inst
	}
	var vocabulary tablewire.Vocabulary
	var announced tabletext.Report
	if err := vocabulary.AnnounceRead(tablewire.Announce(u), &announced); err != nil {
		t.Fatal(err)
	}
	batch, err := tablewire.EncodeMessage(model, baseInst())
	if err != nil {
		t.Fatalf("the oracle refused the base record: %v", err)
	}
	inputs := map[string][]byte{"batch": batch}
	var rows []nfFormsRow
	rows = append(rows, nfFormsRow{
		key:  "msg:base",
		code: `{ local v = baseValue(); local mr = {}; local z = T.FloatsMeasureMessages([v], mr); local b = blob(z < 0 ? 1 : z); local sr = {}; local n = T.FloatsSaveMessages([v], b, sr); print("msg:base " + (n < 0 ? "refused" : hex(b, n)) + "\n"); local out = [T.Floats()]; local rr = {}; print("msg:baseload " + T.FloatsLoadMessages(out, voc, I["batch"], rr) + " " + nn(rr.reason) + "\n"); }`,
		want: "msg:base " + fmt.Sprintf("%x", batch),
	}, nfFormsRow{key: "msg:baseload", want: "msg:baseload 1 -"})

	nonFinite := []nfCase{nfCases[5], nfCases[6], nfCases[7], nfCases[8], nfCases[9], nfCases[10]}
	for _, c := range nonFinite {
		for _, l := range locs {
			// MESSAGE WRITE: the oracle and the leg refuse the same field
			inst := baseInst()
			l.set(inst, math.Float64frombits(c.b64))
			_, err := tablewire.EncodeMessage(model, inst)
			var refusal *tablewire.NonFiniteRefusal
			if !errors.As(err, &refusal) || refusal.Field != l.owner {
				t.Fatalf("message write %s at %s: the oracle answered %v, want non_finite_float: %s", c.name, l.name, err, l.owner)
			}
			key := "msg-write:" + c.name + "/" + l.name
			rows = append(rows, nfFormsRow{
				key:  key,
				code: fmt.Sprintf(`{ local v = baseValue(); %s = value(%q); local mr = {}; local z = T.FloatsMeasureMessages([v], mr); local sr = {}; local n = T.FloatsSaveMessages([v], blob(512), sr); print(%q + " " + z + " " + n + " " + nn(sr.reason) + "\n"); }`, l.path, fmt.Sprintf("%016x", c.b64), key),
				want: key + " -1 -1 non_finite_float: " + l.owner,
				kind: "msg-write",
			})
			// FIXED WRITE
			fkey := "fixed-write:" + c.name + "/" + l.name
			rows = append(rows, nfFormsRow{
				key:  fkey,
				code: fmt.Sprintf(`{ local v = baseValue(); %s = value(%q); print(%q + " " + T.FloatsFixedSave(blob(512), [v]) + " " + nn(T.FixedReason) + " " + nn(T.FixedField) + "\n"); }`, l.path, fmt.Sprintf("%016x", c.b64), fkey),
				want: fkey + " -1 non_finite_float " + l.owner,
				kind: "fixed-write",
			})
			if l.quantized {
				continue // its index always names a finite value
			}
			// MESSAGE READ: the base batch with this slot's payload patched
			width, to := 64, c.b64
			if l.f32 {
				width, to = 32, uint64(c.b32)
			}
			bad := nfReplaceBits(t, batch, nfFormsBase[l.name], to, width)
			var rep tabletext.Report
			n, _, derr := tablewire.DecodeMessages(model, []*tabletext.Instance{model.New(st)}, bad, &vocabulary, &rep)
			if !errors.As(derr, &refusal) || refusal.Field != l.owner || n != 0 {
				t.Fatalf("message read %s at %s: the oracle answered %d, %v, want non_finite_float: %s", c.name, l.name, n, derr, l.owner)
			}
			rkey := "msg-read:" + c.name + "/" + l.name
			inputs[rkey] = bad
			rows = append(rows, nfFormsRow{
				key:  rkey,
				code: fmt.Sprintf(`{ local out = [T.Floats()]; local rr = {}; local n = T.FloatsLoadMessages(out, voc, I[%q], rr); print(%q + " " + n + " " + nn(rr.reason) + "\n"); }`, rkey, rkey),
				want: rkey + " -1 non_finite_float: " + l.owner,
				kind: "msg-read",
			})
			// FIXED READ: the leg's own finite file with this slot's bytes patched
			fkey = "fixed-read:" + c.name + "/" + l.name
			rows = append(rows, nfFormsRow{
				key: fkey,
				code: fmt.Sprintf(`{ local b = blob(512); local n = T.FloatsFixedSave(b, [baseValue()]); local f = blob(n); for (local i = 0; i < n; i++) f[i] = b[i]; local want = %q; local to = %q; local at = -1, hits = 0; for (local i = 0; i + want.len() / 2 <= n; i++) { local s = hex(f, i + want.len() / 2).slice(2 * i); if (s == want) { at = i; hits++; } } if (hits != 1) throw "the base value appears " + hits + " times in the fixed file"; for (local k = 0; k < to.len() / 2; k++) f[at + k] = hexInt(to.slice(2 * k, 2 * k + 2)); local out = [T.Floats()]; print(%q + " " + T.FloatsFixedLoad(f, out, 1) + " " + nn(T.FixedReason) + " " + nn(T.FixedField) + "\n"); }`,
					nfLEHex(nfFormsBase[l.name], width), nfLEHex(to, width), fkey),
				want: fkey + " -1 non_finite_float " + l.owner,
				kind: "fixed-read",
			})
		}
	}
	// a float32 that narrows to an infinity is refused by both writers
	for _, l := range locs {
		if !l.f32 {
			continue
		}
		key := "overflow:" + l.name
		rows = append(rows, nfFormsRow{
			key:  key,
			code: fmt.Sprintf(`{ local v = baseValue(); %s = 1e300; local sr = {}; local m = T.FloatsSaveMessages([v], blob(512), sr); print(%q + " " + m + " " + nn(sr.reason) + " " + T.FloatsFixedSave(blob(512), [v]) + " " + nn(T.FixedField) + "\n"); }`, l.path, key),
			want: key + " -1 non_finite_float: " + l.owner + " -1 " + l.owner,
			kind: "write-both",
		})
	}
	// -0 AND THE SUBNORMALS ride both forms bit exact
	for _, c := range []nfCase{nfCases[1], nfCases[2], nfCases[3], nfCases[4]} {
		key := "exact:" + c.name
		code := fmt.Sprintf(`{ local v = baseValue(); v.A = value(%q); v.B = value(%q); v.N.C = value(%q); v.Xs[1] = value(%q); local mr = {}; local z = T.FloatsMeasureMessages([v], mr); local b = blob(z); local sr = {}; T.FloatsSaveMessages([v], b, sr); local out = [T.Floats()]; local rr = {}; local n = T.FloatsLoadMessages(out, voc, b, rr); local fb = blob(512); local fn = T.FloatsFixedSave(fb, [v]); local ff = blob(fn); for (local i = 0; i < fn; i++) ff[i] = fb[i]; local fo = [T.Floats()]; local fl = T.FloatsFixedLoad(ff, fo, 1); local w = out[0], x = fo[0]; print(%q + " " + n + " " + f32bits(w.A) + "," + f64bits(w.B) + "," + f32bits(w.N.C) + "," + f32bits(w.Xs[1]) + " " + fl + " " + f32bits(x.A) + "," + f64bits(x.B) + "," + f32bits(x.N.C) + "," + f32bits(x.Xs[1]) + "\n"); }`,
			fmt.Sprintf("%08x", c.b32), fmt.Sprintf("%016x", c.b64), fmt.Sprintf("%08x", c.b32), fmt.Sprintf("%08x", c.b32), key)
		bits := fmt.Sprintf("%08x,%016x,%08x,%08x", c.b32, c.b64, c.b32, c.b32)
		rows = append(rows, nfFormsRow{key: key, code: code, want: key + " 1 " + bits + " 1 " + bits})
	}
	return u, rows, inputs
}

// nfFormsBaseCode is the probe's base(): the same finite values baseInst
// gives the oracle, by their bits.
func nfFormsBaseCode() string {
	var b strings.Builder
	for _, l := range nfFormsLocations(nil) {
		if bits, ok := nfFormsBase[l.name]; ok {
			if l.f32 {
				fmt.Fprintf(&b, "  %s = value(%q);\n", l.path, fmt.Sprintf("%08x", bits))
			} else {
				fmt.Fprintf(&b, "  %s = value(%q);\n", l.path, fmt.Sprintf("%016x", bits))
			}
		}
	}
	b.WriteString("  v.Q = 2.5;\n")
	return b.String()
}

// nfLEHex is the little-endian hex of v's low width bits.
func nfLEHex(v uint64, width int) string {
	var s strings.Builder
	for i := 0; i < width/8; i++ {
		fmt.Fprintf(&s, "%02x", byte(v>>(8*i)))
	}
	return s.String()
}

func nfLoadPath(t *testing.T, path, root string) (*ir.Unit, *tabletext.Model, *ir.Struct) {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	u, model, _ := nfLoadNamed(t, string(src))
	st := model.Lookup(root)
	if st == nil {
		t.Fatalf("the unit declares no %s", root)
	}
	return u, model, st
}

func nfFormsRun(t *testing.T, u *ir.Unit, rows []nfFormsRow, inputs map[string][]byte, patch func(map[string][]byte)) map[string]string {
	t.Helper()
	dir := t.TempDir()
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	if patch != nil {
		patch(tables)
	}
	out := filepath.Join(dir, "mf")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}
	var code strings.Builder
	for _, r := range rows {
		if r.code != "" {
			code.WriteString(r.code + "\n")
		}
	}
	probe := strings.ReplaceAll(nfFormsProbe, "__MOD__", squirreltable.ModuleName(u))
	probe = strings.ReplaceAll(probe, "__BASE__", nfFormsBaseCode())
	probe = strings.ReplaceAll(probe, "__ROWS__", code.String())
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n") {
		key, _, _ := strings.Cut(line, " ")
		got[key] = line
	}
	return got
}

func TestSquirrelTableFiniteFloatForms(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	u, rows, inputs := nfFormsVectors(t)
	got := nfFormsRun(t, u, rows, inputs, nil)
	for _, r := range rows {
		if got[r.key] != r.want {
			t.Errorf("%s: %q, want %q", r.key, got[r.key], r.want)
		}
	}
	t.Logf("compared %d message- and fixed-form finite-float rows", len(rows))
}

// nfFormsSabotages removes one check of one form each; the rows of that
// kind, and only those, must go red.
var nfFormsSabotages = []struct {
	name, file string
	edits      [][2]string
	kinds      []string
}{
	{"message-read", "schema_table_message.nut", [][2]string{
		{"if((raw&0x7ff0000000000000)==0x7ff0000000000000){if(r.nonFinite==null)r.nonFinite=fd.nf;return 0.0;}", ""},
		{"if((raw&0x7f800000)==0x7f800000){if(r.nonFinite==null)r.nonFinite=fd.nf;return 0.0;}", ""},
	}, []string{"msg-read"}},
	{"message-write", "schema_table_message.nut", [][2]string{
		{"    if (!finite) { w.reason = \"non_finite_float: \" + fd.nf; w.bad = true; return; }\n", ""},
	}, []string{"msg-write", "write-both"}},
	{"fixed-read", "schema_table_fixed.nut", [][2]string{
		{"  if ((bits & 0x7f800000) == 0x7f800000) return null;\n  scratch.seek(0); scratch.writen(bits, 'i');", "  scratch.seek(0); scratch.writen(bits, 'i');"},
		{"  if ((bits & 0x7ff0000000000000) == 0x7ff0000000000000) return null;\n  return casti2f(bits);", "  return casti2f(bits);"},
	}, []string{"fixed-read"}},
	{"fixed-write", "schema_table_fixed.nut", [][2]string{
		{"  if ((bits & 0x7f800000) == 0x7f800000) return false;\n  _M.put(b, at, 4, bits);", "  _M.put(b, at, 4, bits);"},
		{"  if ((bits & 0x7ff0000000000000) == 0x7ff0000000000000) return false;\n  _M.put(b, at, 8, bits);", "  _M.put(b, at, 8, bits);"},
	}, []string{"fixed-write", "write-both"}},
}

// TestSquirrelTableFiniteFloatFormsNegativeControl removes each form's read
// or write check in memory (every anchor matched exactly once) and requires
// exactly that check's rows to go red while every other row stays green.
func TestSquirrelTableFiniteFloatFormsNegativeControl(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	u, rows, inputs := nfFormsVectors(t)
	for _, sab := range nfFormsSabotages {
		t.Run(sab.name, func(t *testing.T) {
			got := nfFormsRun(t, u, rows, inputs, func(tables map[string][]byte) {
				src := string(tables[sab.file])
				for _, e := range sab.edits {
					if n := strings.Count(src, e[0]); n != 1 {
						t.Fatalf("the control's anchor %q appears %d times in %s, want exactly once", e[0], n, sab.file)
					}
					src = strings.Replace(src, e[0], e[1], 1)
				}
				tables[sab.file] = []byte(src)
			})
			red, wantRed := 0, 0
			for _, r := range rows {
				mustRed := false
				for _, k := range sab.kinds {
					mustRed = mustRed || r.kind == k
				}
				if mustRed {
					wantRed++
				}
				switch same := got[r.key] == r.want; {
				case !same && mustRed:
					red++
				case !same:
					t.Errorf("%s went red under the %s sabotage: %q, want %q", r.key, sab.name, got[r.key], r.want)
				case mustRed:
					t.Errorf("%s stayed green with the %s check removed", r.key, sab.name)
				}
			}
			if red != wantRed || red == 0 {
				t.Fatalf("the %s sabotage red %d of its %d rows", sab.name, red, wantRed)
			}
			t.Logf("the %s sabotage reds exactly its %d rows", sab.name, red)
		})
	}
}
