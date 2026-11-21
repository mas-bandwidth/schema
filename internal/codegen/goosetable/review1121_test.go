package goosetable

// THE GOOSE LEG HOLDS EVERY VALUE TO ITS DECLARATION (review 2026-11-21,
// GOOSE-1 to GOOSE-5 and the leg's cells of the cross-leg rules). Goose
// refuses in every build, so each property below is a refusal by name:
//
//   - the message form's named read reconstructs a ranged integer as base
//     plus offset exactly and holds it to the ANNOUNCED kind, refusing
//     out_of_range rather than truncating a narrow value (GOOSE-1) or
//     wrapping a 64-bit or 128-bit one (GOOSE-2);
//   - the fixed form's read refuses a NaN or an infinity as non_finite_float
//     (GOOSE-3);
//   - the variable form's save refuses a ranged integer or fixed-point value
//     off its range, a bits(N) value past its width and a compressed float
//     off its range (GOOSE-4);
//   - both JSON writers refuse a compressed float off its range (GOOSE-5);
//   - a float32 field's range is compared AT FLOAT32 (SPEC.md §4.6), so the
//     float32 nearest a bound it cannot spell (max = 0.1) is lawful on every
//     read and write;
//   - a counted array [Min..Max]T below its Min is refused on every read and
//     write;
//   - a compressed float's -0 is stored as +0.
//
// The oracle is the Go reference (internal/tabletext, internal/tablewire),
// which shares no code with the Goose runtime. Unit tests here use no real
// time, no socket and no network: each run is `goose run` of a generated
// program in t.TempDir().

import (
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// reviewUnit loads one schema source as a unit.
func reviewUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := loadSchemaPaths([]string{path})
	if err != nil {
		t.Fatalf("the probe schema does not load: %v", err)
	}
	return u
}

// reviewLines splits a probe's stdout into lines of words, keyed by the first.
func reviewLines(out string) map[string][]string {
	lines := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			lines[f[0]] = f[1:]
		}
	}
	return lines
}

// f32Text spells a float32 as JSON text that reads back to exactly it.
func f32Text(v float32) string { return strconv.FormatFloat(float64(v), 'g', -1, 32) }

// overTenth is the float32 one step above float32(0.1), off a [.., 0.1] range.
var overTenth = math.Nextafter32(0.1, 1)

// ---- the message form ------------------------------------------------------

// reviewBits is a message body's bit writer: least significant bit first.
type reviewBits struct {
	out []byte
	n   int
}

func (w *reviewBits) put(v uint64, bits int) {
	for i := range bits {
		if w.n%8 == 0 {
			w.out = append(w.out, 0)
		}
		if i < 64 && v>>uint(i)&1 != 0 {
			w.out[len(w.out)-1] |= 1 << uint(w.n%8)
		}
		w.n++
	}
}

// reviewMsgCase is one batch the reader loads, and what it must answer:
// "refused <reason>", or the landed fields' printed values.
type reviewMsgCase struct {
	name, want string
	batch      []byte
}

// reviewMsgProbe loads each batch under the writer's announcement into the
// reader's root Msg and prints, per case, the count, the refusal and the
// fields named in show (Goose expressions on `back<k>[0]`).
func reviewMsgProbe(t *testing.T, reader, writer *ir.Unit, cases []reviewMsgCase, show []string) map[string][]string {
	t.Helper()
	ns := goose.Namespace(reader)
	files := map[string][]byte{"announce.bin": tablewire.Announce(writer)}
	var b strings.Builder
	b.WriteString("fn main() {\n")
	b.WriteString("    var peer: u8[>..] = [];\n    if !read_file(\"announce.bin\", peer) { print(\"read-failed\"); return; }\n")
	b.WriteString("    var voc = schema_rt::MsgVocabulary { .. };\n    var vrep = schema_rt::MsgReport { .. };\n")
	b.WriteString("    if !schema_rt::announce_read(voc, peer, vrep) { print(\"announce-refused \", schema_rt::msg_reason_name(vrep.reason)); return; }\n")
	for k, c := range cases {
		files[fmt.Sprintf("b%d.bin", k)] = c.batch
		fmt.Fprintf(&b, "    var data%d: u8[>..] = [];\n", k)
		fmt.Fprintf(&b, "    if !read_file(\"b%d.bin\", data%d) { print(\"read-failed\"); return; }\n", k, k)
		fmt.Fprintf(&b, "    var back%d: %s::Msg[1] = [%s::Msg { .. }; 1];\n", k, ns, ns)
		fmt.Fprintf(&b, "    var rep%d = schema_rt::MsgReport { .. };\n", k)
		fmt.Fprintf(&b, "    let n%d = %s::MsgLoadMessages(back%d[0..1], voc, data%d, rep%d);\n", k, ns, k, k, k)
		fmt.Fprintf(&b, "    print(%q, \" \", n%d, \" \", schema_rt::msg_reason_name(rep%d.reason)", c.name, k, k)
		for _, s := range show {
			fmt.Fprintf(&b, ", \" \", %s", strings.ReplaceAll(s, "@", fmt.Sprintf("back%d[0]", k)))
		}
		b.WriteString(");\n")
	}
	b.WriteString("}\n")
	return reviewLines(runCustomFixedGoose(t, reader, nil, b.String(), files))
}

// reviewBatch is one body of a batch of one, written field by field under
// the writer's vocabulary: each fill puts a field's reference and value.
func reviewBatch(t *testing.T, writer *ir.Unit, fill func(w *reviewBits, slot func(string) (uint64, int), refBits int)) []byte {
	t.Helper()
	vocab := tablewire.MessageVocabulary(writer)
	refBits := ir.TableMessageRefBits(len(vocab))
	field := map[string]*ir.Field{}
	for _, f := range writer.Tables["Msg"].Fields {
		field[f.Name] = f
	}
	slot := func(name string) (uint64, int) {
		f, ok := field[name]
		if !ok {
			t.Fatalf("no writer field %s", name)
		}
		e := ir.TableFieldEntry(f)
		bits := ir.TableMessageValueBits(e.Kind, e.Shape)
		for i, v := range vocab {
			if v.Key() == e.Key() {
				return uint64(i + 1), int(bits)
			}
		}
		t.Fatalf("field %s outside the writer's vocabulary", name)
		return 0, 0
	}
	w := &reviewBits{}
	fill(w, slot, refBits)
	w.put(0, refBits) // the body's terminator
	return append([]byte{2, 0}, w.out...)
}

// The reader: bare integers of every width a ranged sum can pass.
const reviewMsgReader = `package probe

fixed table Msg
{
    a uint8
    b uint16
    c uint64
    d int64
    e int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
}
`

// The writer under another build: each field ranged over the top six values
// of its kind in three bits, so offsets 6 and 7 name values past the kind,
// which no conforming writer emits; b is announced a uint8 and widens into
// the reader's uint16.
const reviewMsgWriter = `package probe

fixed table Msg
{
    a uint8 = 250 | min = 250, max = 255
    b uint8 = 250 | min = 250, max = 255
    c uint64 = 18446744073709551610 | min = 18446744073709551610, max = 18446744073709551615
    d int64 = 9223372036854775802 | min = 9223372036854775802, max = 9223372036854775807
    e int128 = 170141183460469231731687303715884105722 | min = 170141183460469231731687303715884105722, max = 170141183460469231731687303715884105727
}
`

// TestGooseMessageRangedHeldToAnnouncedKind pins GOOSE-1 and GOOSE-2: a
// ranged integer read by name is base plus offset exactly, held to the
// announced kind's domain, and refused out_of_range when it lands outside
// it — never truncated into a narrow reader (257 as 1), never let past the
// announced kind because the reader is wider, never wrapped at 64 or 128
// bits. The lawful top of each range lands whole.
func TestGooseMessageRangedHeldToAnnouncedKind(t *testing.T) {
	slowtest.Gate(t, "goose (it runs the generated message read)")
	t.Parallel()
	reader := reviewUnit(t, reviewMsgReader)
	writer := reviewUnit(t, reviewMsgWriter)
	one := func(name string, offset uint64) []byte {
		return reviewBatch(t, writer, func(w *reviewBits, slot func(string) (uint64, int), refBits int) {
			ref, bits := slot(name)
			w.put(ref, refBits)
			w.put(offset, bits)
		})
	}
	cases := []reviewMsgCase{
		{"a-255", "1 ok 255", one("a", 5)},
		{"a-256", "refused out_of_range", one("a", 6)},
		{"a-257", "refused out_of_range", one("a", 7)},
		{"b-255-widened", "1 ok 255", one("b", 5)},
		{"b-257-past-announced", "refused out_of_range", one("b", 7)},
		{"c-top", "1 ok 18446744073709551615", one("c", 5)},
		{"c-wraps-to-0", "refused out_of_range", one("c", 6)},
		{"c-wraps-to-1", "refused out_of_range", one("c", 7)},
		{"d-top", "1 ok 9223372036854775807", one("d", 5)},
		{"d-wraps-negative", "refused out_of_range", one("d", 6)},
		{"e-top", "1 ok 18446744073709551615 9223372036854775807", one("e", 5)},
		{"e-wraps-negative", "refused out_of_range", one("e", 6)},
	}
	show := map[byte][]string{'a': {"@.A"}, 'b': {"@.B"}, 'c': {"@.C"}, 'd': {"@.D"}, 'e': {"@.E.lo", "@.E.hi"}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := reviewMsgProbe(t, reader, writer, []reviewMsgCase{c}, show[c.name[0]])[c.name]
			if after, ok := strings.CutPrefix(c.want, "refused "); ok {
				if len(got) < 2 || got[0] == "1" || got[1] != after {
					t.Errorf("the load answered %q, want %s", got, c.want)
				}
				return
			}
			if strings.Join(got, " ") != c.want {
				t.Errorf("the load answered %q, want %q", got, c.want)
			}
		})
	}
}

// TestGooseMessageFloat32RangeAndCountMin pins the message read's cells of
// review-owed/cross-leg/float32-range and counted-array-min: a compressed
// float32's range is held at float32, so float32(0.1) under max = 0.1 lands
// and the next float32 above it refuses out_of_range; a counted array whose
// count is below the reader's own Min refuses count_over_bound. The batches
// are the Go reference's own encoding under a writer with a plain float32
// and a [0..4] array.
func TestGooseMessageFloat32RangeAndCountMin(t *testing.T) {
	slowtest.Gate(t, "goose (it runs the generated message read)")
	t.Parallel()
	reader := reviewUnit(t, `package probe

fixed table Msg
{
    x float32 = 0.05 | min = 0, max = 0.1, resolution = 0.001
    n [2..4]uint8
}
`)
	writer := reviewUnit(t, `package probe

fixed table Msg
{
    x float32
    n [0..4]uint8
}
`)
	m := tabletext.NewModel(writer)
	batch := func(text string) []byte {
		inst := m.New(writer.Tables["Msg"])
		var rep tabletext.Report
		if !m.Read(inst, []byte(text), &rep) || rep.Refused {
			t.Fatalf("writer text %s: %+v", text, rep)
		}
		b, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	tenth := float32(0.1)
	cases := []reviewMsgCase{
		{"tenth", fmt.Sprintf("1 ok %d 2", math.Float32bits(tenth)), batch(fmt.Sprintf(`{"x":%s,"n":[1,2]}`, f32Text(tenth)))},
		{"over-tenth", "refused out_of_range", batch(fmt.Sprintf(`{"x":%s,"n":[1,2]}`, f32Text(overTenth)))},
		{"count-below-min", "refused count_over_bound", batch(`{"x":0.05,"n":[1]}`)},
	}
	lines := reviewMsgProbe(t, reader, writer, cases, []string{"schema_rt::fixed_f32_bits(@.X)", "@.NCount"})
	for _, c := range cases {
		got := lines[c.name]
		if after, ok := strings.CutPrefix(c.want, "refused "); ok {
			if len(got) < 2 || got[0] == "1" || got[1] != after {
				t.Errorf("%s: the load answered %q, want %s", c.name, got, c.want)
			}
			continue
		}
		if strings.Join(got, " ") != c.want {
			t.Errorf("%s: the load answered %q, want %q", c.name, got, c.want)
		}
	}
}

// ---- the fixed form --------------------------------------------------------

const reviewFixedSchema = `package probe

fixed table F
{
    a float32
    b float64
    n [2..4]uint8
}
`

// TestGooseFixedReadRefusesNonFinite pins GOOSE-3 and the fixed form's cells
// of review-owed/cross-leg/counted-array-min: a record holding a NaN or an
// infinity in a float32 or float64 field is refused non_finite_float, as the
// Go reference's finite pass refuses it; a stored count below the declared
// Min is refused count_over_bound; the save refuses a count below Min. The
// lawful file the probe patches loads.
func TestGooseFixedReadRefusesNonFinite(t *testing.T) {
	slowtest.Gate(t, "goose (it runs the generated fixed read)")
	t.Parallel()
	u := reviewUnit(t, reviewFixedSchema)
	ns := goose.Namespace(u)
	saved := runCustomFixedGoose(t, u, nil, fmt.Sprintf(`fn main() {
    var v: %[1]s::F[1] = [%[1]s::F { .. }; 1];
    v[0].A = 1.5;
    v[0].B = 2.5;
    v[0].N[0] = 7;
    v[0].N[1] = 8;
    v[0].NCount = 2;
    var out: u8[>..] = [];
    let w = %[1]s::FFixedSave(v[0..1], out);
    hex(out);
    v[0].NCount = 1;
    var short: u8[>..] = [];
    print("short ", %[1]s::FFixedSave(v[0..1], short), " ", short.len);
}
`, ns), nil)
	parts := strings.Split(saved, "\n")
	if len(parts) != 2 {
		t.Fatalf("the save probe printed %q", saved)
	}
	if parts[1] != "short -1 0" {
		t.Errorf("the save of a count below Min answered %q, want -1 with nothing appended", parts[1])
	}
	file, err := hex.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("the save printed %q: %v", parts[0], err)
	}
	rec := 20 + int(uint32(file[16])|uint32(file[17])<<8|uint32(file[18])<<16|uint32(file[19])<<24) + 8
	patch := func(at int, b ...byte) []byte {
		out := append([]byte(nil), file...)
		copy(out[rec+at:], b)
		return out
	}
	cases := []struct {
		name, want string
		data       []byte
	}{
		{"lawful", "1 ok", file},
		{"a-nan", "-1 non_finite_float", patch(0, 0x00, 0x00, 0xc0, 0x7f)},
		{"a-inf", "-1 non_finite_float", patch(0, 0x00, 0x00, 0x80, 0x7f)},
		{"b-neg-inf", "-1 non_finite_float", patch(4, 0, 0, 0, 0, 0, 0, 0xf0, 0xff)},
		{"b-nan", "-1 non_finite_float", patch(4, 1, 0, 0, 0, 0, 0, 0xf8, 0x7f)},
		{"count-below-min", "-1 count_over_bound", patch(12, 1, 0, 0, 0, 7, 0)},
	}
	var b strings.Builder
	b.WriteString("fn main() {\n")
	inputs := map[string][]byte{}
	for k, c := range cases {
		inputs[fmt.Sprintf("f%d.bin", k)] = c.data
		fmt.Fprintf(&b, "    var data%d: u8[>..] = [];\n    if !read_file(\"f%d.bin\", data%d) { print(\"read-failed\"); return; }\n", k, k, k)
		fmt.Fprintf(&b, "    var v%d: %s::F[1] = [%s::F { .. }; 1];\n    var rep%d = schema_rt::FixedReport { .. };\n", k, ns, ns, k)
		fmt.Fprintf(&b, "    let n%d = %s::FFixedLoad(v%d[0..1], data%d, rep%d);\n", k, ns, k, k, k)
		fmt.Fprintf(&b, "    print(%q, \" \", n%d, \" \", schema_rt::fixed_refusal_name(rep%d.reason));\n", c.name, k, k)
	}
	b.WriteString("}\n")
	lines := reviewLines(runCustomFixedGoose(t, u, nil, b.String(), inputs))
	for _, c := range cases {
		got := strings.Join(lines[c.name], " ")
		if got != c.want {
			t.Errorf("%s: FFixedLoad answered %q, want %q", c.name, got, c.want)
		}
	}
}

// ---- the variable form -----------------------------------------------------

const reviewVarSchema = `package probe

table Root
{
    r uint8 = 15 | min = 10, max = 20
    s int16 = 0 | min = -5, max = 5
    q fixed(8, 8) = 1 | min = -2, max = 2
    w bits(5)
    x float32 = 0.05 | min = 0, max = 0.1, resolution = 0.001
    n [2..4]uint8
}
`

// TestGooseVariableSaveHoldsTheDeclaration pins GOOSE-4 and the variable
// save's cells of the cross-leg rules: a value a caller holds off its
// declaration — a ranged integer or fixed-point value off its range, a
// bits(N) value past its width, a compressed float off its range (held at
// float32), a counted array below its Min — is refused invalid_value and
// nothing is written; each lawful twin at the edge saves; a compressed
// float's -0 is stored as +0.
func TestGooseVariableSaveHoldsTheDeclaration(t *testing.T) {
	slowtest.Gate(t, "goose (it runs the generated variable save)")
	t.Parallel()
	u := reviewUnit(t, reviewVarSchema)
	m := tabletext.NewModel(u)
	v := m.New(u.Tables["Root"])
	var rep tabletext.Report
	if !m.Read(v, []byte(`{"r":12,"s":3,"q":1.5,"w":7,"x":0.02,"n":[1,2,3]}`), &rep) || rep.Refused {
		t.Fatalf("the probe text: %+v", rep)
	}
	wire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}
	all := "0xFFFFFFFFFFFFFFFF"
	cases := []struct {
		name, set, want string
	}{
		{"r-over", "vals[s0].bits = schema_rt::Int128 { lo: 21, hi: 0 };", "invalid_value"},
		{"r-top", "vals[s0].bits = schema_rt::Int128 { lo: 20, hi: 0 };", ""},
		{"r-under", "vals[s0].bits = schema_rt::Int128 { lo: 9, hi: 0 };", "invalid_value"},
		{"s-under", "vals[s1].bits = schema_rt::Int128 { lo: 0xFFFFFFFFFFFFFFFA, hi: " + all + " };", "invalid_value"},
		{"s-bottom", "vals[s1].bits = schema_rt::Int128 { lo: 0xFFFFFFFFFFFFFFFB, hi: " + all + " };", ""},
		{"q-over", "vals[s2].bits = schema_rt::Int128 { lo: 513, hi: 0 };", "invalid_value"},
		{"q-top", "vals[s2].bits = schema_rt::Int128 { lo: 512, hi: 0 };", ""},
		{"w-over", "vals[s3].bits = schema_rt::Int128 { lo: 32, hi: 0 };", "invalid_value"},
		{"w-top", "vals[s3].bits = schema_rt::Int128 { lo: 31, hi: 0 };", ""},
		{"x-over", fmt.Sprintf("vals[s4].bits = schema_rt::Int128 { lo: %d, hi: 0 };", math.Float32bits(overTenth)), "invalid_value"},
		{"x-tenth", fmt.Sprintf("vals[s4].bits = schema_rt::Int128 { lo: %d, hi: 0 };", math.Float32bits(0.1)), ""},
		{"x-negzero", "vals[s4].bits = schema_rt::Int128 { lo: 0x80000000, hi: 0 };", ""},
		{"x-zero", "vals[s4].bits = schema_rt::Int128 { lo: 0, hi: 0 };", ""},
		{"n-below-min", "vals[s5].len = 1;", "invalid_value"},
		{"n-min", "vals[s5].len = 2;", ""},
	}
	var b strings.Builder
	b.WriteString(`fn hexw(b: const u8[:]) -> u8[>..] {
    let digits = "0123456789abcdef";
    var line: u8[>..] = [];
    for x in b {
        line.push(digits[(x >> 4) as! i64]);
        line.push(digits[(x & 15) as! i64]);
    }
    return line;
}

fn main() {
    var data: u8[>..] = [];
    if !read_file("in.bin", data) { print("read-failed"); return; }
`)
	unit := goose.Namespace(u) + "::" + UnitName
	for k, c := range cases {
		fmt.Fprintf(&b, "    var vals%d: schema_rt::LoadVal[>..] = [];\n    var nodes%d: schema_rt::LoadNode[>..] = [];\n    var report%d = schema_rt::LoadReport { .. };\n", k, k, k)
		fmt.Fprintf(&b, "    let root%d = schema_rt::load_table(%s, \"Root\", data, vals%d, nodes%d, report%d);\n", k, unit, k, k, k)
		fmt.Fprintf(&b, "    if root%d < 0 || schema_rt::load_error(report%d, root%d) != 0 { print(\"load-refused\"); return; }\n", k, k, k)
		set := c.set
		for i := 5; i >= 0; i-- {
			set = strings.ReplaceAll(set, fmt.Sprintf("vals[s%d]", i), fmt.Sprintf("vals%d[vals%d[root%d].first + %d]", k, k, k, i))
		}
		fmt.Fprintf(&b, "    %s\n", set)
		fmt.Fprintf(&b, "    var out%d: u8[>..<] = [];\n    out%d.push(0xEE);\n", k, k)
		fmt.Fprintf(&b, "    let r%d = schema_rt::save_table(%s, \"Root\", data, vals%d, nodes%d, root%d, out%d);\n", k, unit, k, k, k, k)
		fmt.Fprintf(&b, "    print(%q, \" _\", schema_rt::save_refusal_name(r%d), \" \", hexw(out%d));\n", c.name, k, k)
	}
	b.WriteString("}\n")
	lines := reviewLines(runCustomFixedGoose(t, u, nil, b.String(), map[string][]byte{"in.bin": wire}))
	for _, c := range cases {
		got := lines[c.name]
		if len(got) != 2 {
			t.Errorf("%s: the probe printed %q", c.name, got)
			continue
		}
		if got[0] != "_"+c.want {
			t.Errorf("%s: the save answered %q, want %q", c.name, strings.TrimPrefix(got[0], "_"), c.want)
			continue
		}
		if c.want != "" && got[1] != "ee" {
			t.Errorf("%s: a refused save wrote %s past the caller's byte", c.name, got[1])
		}
	}
	if neg, zero := lines["x-negzero"], lines["x-zero"]; len(neg) == 2 && len(zero) == 2 && neg[1] != zero[1] {
		t.Errorf("a compressed float's -0 was not stored as +0:\n  -0 %s\n  +0 %s", neg[1], zero[1])
	}
}

// TestGooseVariableReadFloat32RangeAndCountMin pins the variable read's cells
// of float32-range and counted-array-min: a writer's plain float32(0.1) under
// a compressed reader's max = 0.1 loads, the next float32 above refuses, and
// a writer's count below the reader's Min refuses — each the Go reference's
// own verdict over the same wire.
func TestGooseVariableReadFloat32RangeAndCountMin(t *testing.T) {
	slowtest.Gate(t, "goose (it runs the generated variable read)")
	t.Parallel()
	reader := reviewUnit(t, `package probe

table Root
{
    x float32 = 0.05 | min = 0, max = 0.1, resolution = 0.001
    n [2..4]uint8
}
`)
	writer := reviewUnit(t, `package probe

table Root
{
    x float32
    n [0..4]uint8
}
`)
	wm := tabletext.NewModel(writer)
	rm := tabletext.NewModel(reader)
	cases := []struct {
		name, text string
		ok         bool
	}{
		{"tenth", fmt.Sprintf(`{"x":%s,"n":[1,2]}`, f32Text(0.1)), true},
		{"over-tenth", fmt.Sprintf(`{"x":%s,"n":[1,2]}`, f32Text(overTenth)), false},
		{"count-below-min", `{"x":0.05,"n":[1]}`, false},
	}
	inputs := map[string][]byte{}
	var b strings.Builder
	b.WriteString("fn main() {\n")
	unit := goose.Namespace(reader) + "::" + UnitName
	for k, c := range cases {
		inst := wm.New(writer.Tables["Root"])
		var rep tabletext.Report
		if !wm.Read(inst, []byte(c.text), &rep) || rep.Refused {
			t.Fatalf("writer text %s: %+v", c.text, rep)
		}
		wire, err := tablewire.Encode(wm, inst)
		if err != nil {
			t.Fatal(err)
		}
		back := rm.New(reader.Tables["Root"])
		var grep tabletext.Report
		gok, gerr := tablewire.Decode(rm, back, wire, &grep)
		// the Go reference holds the count's Min; its float32-range cell
		// lands on main with the stream (PRs #4440, #4441), so only the count
		// row asks it here
		if goOK := gok && gerr == nil && !grep.Refused; c.name == "count-below-min" && goOK != c.ok {
			t.Fatalf("%s: the Go reference's read answered ok=%v (%v %+v), want %v", c.name, goOK, gerr, grep, c.ok)
		}
		inputs[fmt.Sprintf("w%d.bin", k)] = wire
		fmt.Fprintf(&b, "    var data%d: u8[>..] = [];\n    if !read_file(\"w%d.bin\", data%d) { print(\"read-failed\"); return; }\n", k, k, k)
		fmt.Fprintf(&b, "    var vals%d: schema_rt::LoadVal[>..] = [];\n    var nodes%d: schema_rt::LoadNode[>..] = [];\n    var report%d = schema_rt::LoadReport { .. };\n", k, k, k)
		fmt.Fprintf(&b, "    let root%d = schema_rt::load_table(%s, \"Root\", data%d, vals%d, nodes%d, report%d);\n", k, unit, k, k, k, k)
		fmt.Fprintf(&b, "    print(%q, \" \", schema_rt::load_error(report%d, root%d));\n", c.name, k, k)
	}
	b.WriteString("}\n")
	lines := reviewLines(runCustomFixedGoose(t, reader, nil, b.String(), inputs))
	for _, c := range cases {
		got := lines[c.name]
		if len(got) != 1 || (got[0] == "0") != c.ok {
			t.Errorf("%s: load_error answered %q, want ok=%v", c.name, got, c.ok)
		}
	}
}

// ---- the JSON text form ----------------------------------------------------

// TestGooseJsonWritersHoldTheRange pins GOOSE-5 and the JSON cells of the
// cross-leg rules on BOTH Goose JSON writers — the typed writer of a fixed
// root (<T>ToJson) and the flat value table's (json_save_table): a compressed
// float off its range is refused (-1, nothing appended), float32(0.1) under
// max = 0.1 is written, and a compressed -0 is written as the +0 text the Go
// reference writes. The typed and flat reads take float32(0.1) and refuse a
// counted array below its Min.
func TestGooseJsonWritersHoldTheRange(t *testing.T) {
	slowtest.Gate(t, "goose (it runs the generated JSON writers)")
	t.Parallel()
	src := `package probe

fixed table J
{
    x float32 = 0.05 | min = 0, max = 0.1, resolution = 0.001
    n [2..4]uint8
}

table V
{
    x float32 = 0.05 | min = 0, max = 0.1, resolution = 0.001
    n [2..4]uint8
}
`
	u := reviewUnit(t, src)
	ns := goose.Namespace(u)
	m := tabletext.NewModel(u)
	zeroText := func(root string) string {
		inst := m.New(u.Tables[root])
		var rep tabletext.Report
		if !m.Read(inst, []byte(`{"x":0,"n":[1,2]}`), &rep) || rep.Refused {
			t.Fatalf("%s text: %+v", root, rep)
		}
		out, err := m.Write(inst)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	vInst := m.New(u.Tables["V"])
	var vrep tabletext.Report
	if !m.Read(vInst, []byte(`{"x":0.02,"n":[1,2]}`), &vrep) || vrep.Refused {
		t.Fatalf("V text: %+v", vrep)
	}
	wire, err := tablewire.Encode(m, vInst)
	if err != nil {
		t.Fatal(err)
	}
	type wcase struct {
		name string
		bits uint32
		ok   bool
	}
	wcases := []wcase{
		{"over", math.Float32bits(overTenth), false},
		{"tenth", math.Float32bits(0.1), true},
		{"negzero", 0x80000000, true},
	}
	var b strings.Builder
	b.WriteString(`fn textw(b: const u8[:]) -> u8[>..] {
    let digits = "0123456789abcdef";
    var line: u8[>..] = [];
    for x in b {
        line.push(digits[(x >> 4) as! i64]);
        line.push(digits[(x & 15) as! i64]);
    }
    if line.len == 0 { line.push('-'); }
    return line;
}

fn main() {
    var data: u8[>..] = [];
    if !read_file("in.bin", data) { print("read-failed"); return; }
`)
	unit := ns + "::" + UnitName
	text := ns + "::" + TextUnitName
	for k, c := range wcases {
		fmt.Fprintf(&b, "    var j%d = %s::J { .. };\n    j%d.X = schema_rt::fixed_bits_f32(0x%08x);\n    j%d.N[0] = 1;\n    j%d.N[1] = 2;\n    j%d.NCount = 2;\n", k, ns, k, c.bits, k, k, k)
		fmt.Fprintf(&b, "    var jo%d: u8[>..] = [];\n    let jn%d = %s::JToJson(j%d, jo%d);\n", k, k, ns, k, k)
		fmt.Fprintf(&b, "    print(\"typed-%s \", jn%d, \" \", textw(jo%d));\n", c.name, k, k)
		fmt.Fprintf(&b, "    var vals%d: schema_rt::LoadVal[>..] = [];\n    var nodes%d: schema_rt::LoadNode[>..] = [];\n    var report%d = schema_rt::LoadReport { .. };\n", k, k, k)
		fmt.Fprintf(&b, "    let root%d = schema_rt::load_table(%s, \"V\", data, vals%d, nodes%d, report%d);\n", k, unit, k, k, k)
		fmt.Fprintf(&b, "    if root%d < 0 || schema_rt::load_error(report%d, root%d) != 0 { print(\"load-refused\"); return; }\n", k, k, k)
		fmt.Fprintf(&b, "    vals%d[vals%d[root%d].first].bits = schema_rt::Int128 { lo: 0x%08x, hi: 0 };\n", k, k, k, c.bits)
		fmt.Fprintf(&b, "    var vo%d: u8[>..] = [];\n    let vn%d = schema_rt::json_save_table(%s, %s, \"V\", data, vals%d, nodes%d, root%d, vo%d);\n", k, k, unit, text, k, k, k, k)
		fmt.Fprintf(&b, "    print(\"flat-%s \", vn%d, \" \", textw(vo%d));\n", c.name, k, k)
	}
	// the reads: float32(0.1) lands, a count below Min refuses
	reads := []struct {
		name, text string
		ok         bool
	}{
		{"tenth", fmt.Sprintf(`{"x":%s,"n":[1,2]}`, f32Text(0.1)), true},
		{"short", `{"x":0.05,"n":[1]}`, false},
	}
	inputs := map[string][]byte{"in.bin": wire}
	for k, r := range reads {
		inputs[fmt.Sprintf("t%d.json", k)] = []byte(r.text)
		fmt.Fprintf(&b, "    var tx%d: u8[>..] = [];\n    if !read_file(\"t%d.json\", tx%d) { print(\"read-failed\"); return; }\n", k, k, k)
		fmt.Fprintf(&b, "    var jr%d = %s::J { .. };\n    var jrep%d = schema_rt::JsonReport { .. };\n", k, ns, k)
		fmt.Fprintf(&b, "    print(\"read-typed-%s \", (if %s::JFromJson(jr%d, tx%d, jrep%d) { 1 } else { 0 }));\n", r.name, ns, k, k, k)
		fmt.Fprintf(&b, "    var tdata%d: u8[>..] = [];\n    var tvals%d: schema_rt::LoadVal[>..] = [];\n    var tnodes%d: schema_rt::LoadNode[>..] = [];\n    var trep%d = schema_rt::JsonReport { .. };\n", k, k, k, k)
		fmt.Fprintf(&b, "    print(\"read-flat-%s \", (if schema_rt::json_load_table(%s, %s, \"V\", tx%d, tdata%d, tvals%d, tnodes%d, trep%d) >= 0 { 1 } else { 0 }));\n", r.name, unit, text, k, k, k, k, k)
	}
	b.WriteString("}\n")
	lines := reviewLines(runCustomFixedGoose(t, u, nil, b.String(), inputs))
	for _, form := range []string{"typed", "flat"} {
		root := map[string]string{"typed": "J", "flat": "V"}[form]
		for _, c := range wcases {
			got := lines[form+"-"+c.name]
			if len(got) != 2 {
				t.Errorf("%s %s: the probe printed %q", form, c.name, got)
				continue
			}
			if c.ok != (got[0] != "-1") {
				t.Errorf("%s %s: the writer answered %s (%s), want ok=%v", form, c.name, got[0], got[1], c.ok)
			}
			if !c.ok && got[1] != "-" {
				t.Errorf("%s %s: a refused write appended %s", form, c.name, got[1])
			}
			if c.name == "negzero" && c.ok {
				if strings.TrimSpace(mustHex(t, got[1])) != strings.TrimSpace(zeroText(root)) {
					t.Errorf("%s: a compressed -0 was written as %q, want the +0 text %q", form, mustHex(t, got[1]), zeroText(root))
				}
			}
		}
	}
	for _, r := range reads {
		for _, form := range []string{"typed", "flat"} {
			got := lines["read-"+form+"-"+r.name]
			if len(got) != 1 || (got[0] == "1") != r.ok {
				t.Errorf("%s read %s: answered %q, want ok=%v", form, r.name, got, r.ok)
			}
		}
	}
}

func mustHex(t *testing.T, s string) string {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		return s
	}
	return string(b)
}
