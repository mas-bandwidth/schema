package lobstertable_test

import (
	"encoding/hex"
	"maps"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE ACROSS-LEGS VALUE RULES ON THE PATHS NO OTHER LOBSTER TEST WALKS
// (review-owed/cross-leg/counted-array-min and negative-zero):
//
//   - a counted array [Min..Max]T below its minimum is refused by every
//     writer (variable, JSON, message, fixed) and by every reader (JSON,
//     message, fixed; the variable read is TestLobsterCountedArrayMinimum);
//   - a compressed float's -0 lands as +0 on the variable and JSON reads (the
//     fixed read is TestFixedCompressedNegZeroRead, the binary writers
//     TestLobsterCompressedNegZeroStoredAsPlusZero; the text form spells -0
//     and its read lands +0, docs/SPEC-TABLES.md §3.3).
//
// Lobster refuses a table write in every build, so each writer row is a
// refusal, never an assert.
const crossLegCellsSchema = `package crosscells

fixed table Root
{
    few [2..4]int32
    r   float32 = 1.0 | min = -10.0, max = 10.0, resolution = 0.5
    hi  float32 = 0.05 | min = 0.0, max = 0.1, resolution = 0.05
}

table Probe
{
    few [2..4]int32
    r   float32 = 1.0 | min = -10.0, max = 10.0, resolution = 0.5
}
`

const crossLegCellsBody = `
def cbits(x:float) -> string:
    return number_to_string(schema_rt.f32bits(x), 16, 8)

def short(name:string) -> schema_rt.Record:
    let rec = schema_rt.new_record(schema_rt.table_of(name))
    let sl = fslot(rec, "few")
    sl.elems[0].i = 0x11111111
    sl.count = 1
    return rec

def full(name:string) -> schema_rt.Record:
    let rec = schema_rt.new_record(schema_rt.table_of(name))
    let sl = fslot(rec, "few")
    sl.elems[0].i = 0x11111111
    sl.elems[1].i = 0x22222222
    fslot(rec, "r").cell.f = 2.5
    return rec

// the writers, a count below the minimum
def writers():
    let vw, vwhy = schema_rt.save_reasoned(short("Probe"))
    print("variable_write_below_min " + (if vwhy != "" and vw == "": "refused" else: "written"))
    let jw, jwhy = schema_rt.json_write(short("Probe"))
    print("json_write_below_min " + (if jwhy != "" and jw == "": "refused" else: "written"))
    let mw, size = schema_rt.save_messages([short("Root")], schema_rt.LoadReport {})
    print("message_write_below_min " + (if size < 0 and mw == "": "refused" else: "written"))
    let row = crosscells.NewRoot()
    row.FewCount = 1
    let n, fw, why = crosscells.RootFixedSaveChecked([row], schema_rt.zeros(crosscells.RootFixedMeasure(1)))
    print("fixed_write_below_min " + (if n < 0: "refused " + why else: "written " + string(length(fw))))
writers()

// the JSON read
def jread(name:string):
    let text = read_file(name + ".json")
    assert text
    let rep = schema_rt.LoadReport {}
    let back = schema_rt.json_read("Probe", text, rep)
    if back:
        print(name + " read " + cbits(fslot(back, "r").cell.f))
    else:
        print(name + " refused " + rep.reason)
jread("json_read_below_min")
jread("json_read_at_min")
jread("json_read_negzero")

// the message read: the sender announces [0..4] and sends one element, then
// the two this reader's minimum asks for
def mread(count:int):
    let v = schema_rt.new_vocabulary(4096)
    assert schema_rt.announce_read(v, schema_rt.announce(), schema_rt.LoadReport {})
    let slots = schema_rt.message_slots_of(schema_rt.message_facts.fields, "Root")
    let e = schema_rt.message_entry(v, slots[0])
    assert e
    e.shape.min = 0
    let w = schema_rt.MessageWriter { ref_bits: v.ref_bits }
    schema_rt.mw_put(w, slots[0], v.ref_bits)
    schema_rt.mw_put(w, count, schema_rt.message_bits(e.shape.max - e.shape.min))
    for(count):
        schema_rt.mw_put(w, 7, e.element.bits)
    schema_rt.mw_put(w, 0, w.ref_bits)
    schema_rt.mw_align(w)
    var header = schema_rt.zeros(2)
    header = write_int8_le(header, 0, 2)
    header = write_int8_le(header, 1, 0)
    let rep = schema_rt.LoadReport {}
    let vals, used, ok = schema_rt.load_messages("Root", v, header + w.b, rep, 2)
    print((if count < 2: "message_read_below_min" else: "message_read_at_min") + (if ok and used > 0 and length(vals) == 1: " read" else: " refused " + schema_rt.strict_reason(rep)))
mread(1)
mread(2)

// a float32 range is held at float32 in the fixed and message forms too: the
// float32 nearest the end 0.1 is written and read back, and one float32 step
// past it is never written
def ends(name:string, bits:int):
    let x = read_float32_le(write_int32_le(schema_rt.zeros(4), 0, bits), 0)
    let row = crosscells.NewRoot()
    row.Hi = x
    let n, fw, why = crosscells.RootFixedSaveChecked([row], schema_rt.zeros(crosscells.RootFixedMeasure(1)))
    if n < 0:
        print("fixed_float32_" + name + " refused " + why)
    else:
        let plan:[schema_rt.TableFixedEntry] = []
        for(64): push(plan, schema_rt.TableFixedEntry {})
        let back = [crosscells.NewRoot()]
        let report = schema_rt.TableReport {}
        let m = crosscells.RootFixedLoad(back, fw, plan, report)
        print("fixed_float32_" + name + (if m == 1: " read " + cbits(back[0].Hi) else: " written, then refused " + report.reason))
    let rec = schema_rt.new_record(schema_rt.table_of("Root"))
    fslot(rec, "hi").cell.f = x
    let mw, size = schema_rt.save_messages([rec], schema_rt.LoadReport {})
    if size < 0:
        print("message_float32_" + name + " refused")
    else:
        let v = schema_rt.new_vocabulary(4096)
        assert schema_rt.announce_read(v, schema_rt.announce(), schema_rt.LoadReport {})
        let rep = schema_rt.LoadReport {}
        let vals, used, ok = schema_rt.load_messages("Root", v, mw, rep, 2)
        print("message_float32_" + name + (if ok and used > 0 and length(vals) == 1: " read " + cbits(fslot(vals[0], "hi").cell.f) else: " written, then refused " + schema_rt.strict_reason(rep)))
ends("at_end", 0x3DCCCCCD)
ends("past_end", 0x3DCCCCCE)

// every writer at one count: refused outside [2, 4], and inside it the bytes
// the Go side holds against the reference's own (elided at the birth value)
def counted(name:string, count:int, set:bool) -> schema_rt.Record:
    let rec = schema_rt.new_record(schema_rt.table_of(name))
    let sl = fslot(rec, "few")
    while length(sl.elems) < max(4, count):
        push(sl.elems, schema_rt.Value {})
    for(max(0, count)) i:
        sl.elems[i].i = if set: i + 1 else: 0
    sl.count = count
    return rec
def at_count(count:int, set:bool):
    let tag = "count_" + (if count < 0: "m1" else: string(count)) + (if set: "_set" else: "_zero")
    let vw, vwhy = schema_rt.save_reasoned(counted("Probe", count, set))
    print(tag + "_variable " + (if vwhy != "": "refused" else: fhex(vw)))
    let mw, size = schema_rt.save_messages([counted("Root", count, set)], schema_rt.LoadReport {})
    print(tag + "_message " + (if size < 0: "refused" else: fhex(substring(mw, 0, size))))
    let measured = schema_rt.measure_messages([counted("Root", count, set)], schema_rt.LoadReport {})
    print(tag + "_measure " + (if measured < 0: "refused" else: (if measured == size: "agrees" else: "differs")))
    let row = crosscells.NewRoot()
    if count <= 4:
        for(max(0, count)) i:
            row.Few[i] = if set: i + 1 else: 0
    row.FewCount = count
    let n, fw, why = crosscells.RootFixedSaveChecked([row], schema_rt.zeros(crosscells.RootFixedMeasure(1)))
    print(tag + "_fixed " + (if n < 0: "refused" else: fhex(substring(fw, 0, n))))
at_count(-1, false)
at_count(0, false)
at_count(1, true)
at_count(5, true)
at_count(2, false)
at_count(2, true)
at_count(3, false)
at_count(3, true)
at_count(4, true)

// the message read refuses the birth value spelled out by a same-build writer
def spelled():
    let v = schema_rt.new_vocabulary(4096)
    assert schema_rt.announce_read(v, schema_rt.announce(), schema_rt.LoadReport {})
    let slots = schema_rt.message_slots_of(schema_rt.message_facts.fields, "Root")
    let e = schema_rt.message_entry(v, slots[0])
    assert e
    let w = schema_rt.MessageWriter { ref_bits: v.ref_bits }
    schema_rt.mw_put(w, slots[0], v.ref_bits)
    schema_rt.mw_put(w, 0, schema_rt.message_bits(e.shape.max - e.shape.min))
    for(2):
        schema_rt.mw_put(w, 0, e.element.bits)
    schema_rt.mw_put(w, 0, w.ref_bits)
    schema_rt.mw_align(w)
    var header = schema_rt.zeros(2)
    header = write_int8_le(header, 0, 2)
    header = write_int8_le(header, 1, 0)
    let rep = schema_rt.LoadReport {}
    let vals, used, ok = schema_rt.load_messages("Root", v, header + w.b, rep, 2)
    print("message_read_birth_spelled_out" + (if ok and used > 0 and length(vals) == 1: " read" else: " refused " + schema_rt.strict_reason(rep)))
spelled()

// the hand-editable text form holds the minimum too
def tread(name:string):
    let text = read_file(name + ".txt")
    assert text
    print(name + (if schema_rt.text_load("Probe", text): " read" else: " refused"))
tread("text_read_below_min")
tread("text_read_at_min")

// the clean images the Go side patches for the binary reads
def images():
    let w, why = schema_rt.save_reasoned(full("Probe"))
    print("variable_image " + why + "|" + fhex(w))
    let row = crosscells.NewRoot()
    row.Few[0] = 0x11111111
    row.Few[1] = 0x22222222
    print("fixed_image " + fhex(crosscells.RootFixedSave([row])))
images()
`

const crossLegCellsReads = `
def cbits(x:float) -> string:
    return number_to_string(schema_rt.f32bits(x), 16, 8)
def vread():
    let data = read_file("negzero.bin")
    assert data
    let back, reason = schema_rt.load_strict("Probe", data, schema_rt.LoadReport {})
    if back:
        print("variable_read_negzero read " + cbits(fslot(back, "r").cell.f))
    else:
        print("variable_read_negzero refused " + reason)
vread()
def fread(name:string):
    let data = read_file(name + ".bin")
    assert data
    let plan:[schema_rt.TableFixedEntry] = []
    for(64): push(plan, schema_rt.TableFixedEntry {})
    let back = [crosscells.NewRoot()]
    let report = schema_rt.TableReport {}
    let n = crosscells.RootFixedLoad(back, data, plan, report)
    print(name + (if n == 1: " read" else: " refused " + report.reason))
fread("fixed_read_below_min")
fread("fixed_read_at_min")
`

func TestLobsterCrossLegCells(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain")
	jsonInputs := map[string][]byte{
		"json_read_below_min.json": []byte(`{"few":[1]}`),
		"json_read_at_min.json":    []byte(`{"few":[1,2]}`),
		"json_read_negzero.json":   []byte(`{"r":-0.0}`),
	}
	textInputs := map[string][]byte{
		"text_read_below_min.txt": []byte(`(:few (1))`),
		"text_read_at_min.txt":    []byte(`(:few (1 2))`),
	}
	inputs := maps.Clone(jsonInputs)
	maps.Copy(inputs, textInputs)
	g := finiteGenerate(t, crossLegCellsSchema)
	out := finiteRun(t, g, "import .json\n"+crossLegCellsBody, inputs)
	rows := map[string]string{}
	for _, line := range out {
		name, rest, _ := strings.Cut(line, " ")
		rows[name] = rest
	}
	variable, err := hex.DecodeString(strings.TrimPrefix(rows["variable_image"], "|"))
	if err != nil || len(variable) == 0 {
		t.Fatalf("the variable image: %q", rows["variable_image"])
	}
	fixed, err := hex.DecodeString(rows["fixed_image"])
	if err != nil || len(fixed) == 0 {
		t.Fatalf("the fixed image: %q", rows["fixed_image"])
	}
	// the fixed record stores the count as a u32 before the elements
	at := strings.Index(string(fixed), "\x02\x00\x00\x00\x11\x11\x11\x11\x22\x22\x22\x22")
	if at < 0 || strings.Count(string(fixed), "\x02\x00\x00\x00\x11\x11\x11\x11") != 1 {
		t.Fatalf("the fixed image holds no one counted array: %x", fixed)
	}
	below := append([]byte(nil), fixed...)
	below[at] = 1
	reads := finiteRun(t, g, crossLegCellsReads, map[string][]byte{
		"negzero.bin":              patch32(t, variable, 0x40200000, 0x80000000),
		"fixed_read_below_min.bin": below,
		"fixed_read_at_min.bin":    fixed,
	})
	for _, line := range reads {
		name, rest, _ := strings.Cut(line, " ")
		rows[name] = rest
	}
	// the reference agrees on the JSON read rows
	want := tabletext.NewModel(g.u)
	for name, text := range jsonInputs {
		var rep tabletext.Report
		got := want.Read(want.New(want.Lookup("Probe")), text, &rep) && !rep.Refused
		if got != (name != "json_read_below_min.json") {
			t.Errorf("the reference reads %s: %t (%s)", name, got, rep.Reason)
		}
	}
	// EVERY WRITER AT EVERY COUNT, AGAINST THE REFERENCE: outside [2, 4] both
	// refuse; inside it the bytes are the reference's own, so a count at the
	// minimum with every element at its zero is elided as the birth value
	// and never read by Go as an explicitly stored default.
	for _, c := range []struct {
		tag   string
		count int
		set   bool
	}{
		{"count_m1_zero", -1, false}, {"count_0_zero", 0, false}, {"count_1_set", 1, true}, {"count_5_set", 5, true},
		{"count_2_zero", 2, false}, {"count_2_set", 2, true}, {"count_3_zero", 3, false}, {"count_3_set", 3, true}, {"count_4_set", 4, true},
	} {
		// a count inside the range is read from text; one outside it no
		// text spells, so it is set on a fresh instance
		value := func(root string) *tabletext.Instance {
			inst := want.New(want.Lookup(root))
			if c.count < 2 || c.count > 4 {
				inst.Fields[0].Count = c.count
				return inst
			}
			elems := make([]string, c.count)
			for i := range elems {
				elems[i] = "0"
				if c.set {
					elems[i] = strconv.Itoa(i + 1)
				}
			}
			var rep tabletext.Report
			if !want.Read(inst, []byte(`{"few":[`+strings.Join(elems, ",")+`]}`), &rep) {
				t.Fatalf("%s: the reference refuses the value: %+v", c.tag, rep)
			}
			return inst
		}
		variable, verr := tablewire.Encode(want, value("Probe"))
		message, merr := tablewire.EncodeMessages(want, []*tabletext.Instance{value("Root")})
		fixedBytes, ferr := tablewire.EncodeFixed(want, value("Root"))
		inside := c.count >= 2 && c.count <= 4
		for form, ref := range map[string]struct {
			data []byte
			err  error
		}{"variable": {variable, verr}, "message": {message, merr}, "fixed": {fixedBytes, ferr}} {
			if (ref.err == nil) != inside {
				t.Errorf("%s_%s: the reference's verdict %v, want written: %t", c.tag, form, ref.err, inside)
				continue
			}
			wantRow := "refused"
			if inside {
				wantRow = hex.EncodeToString(ref.data)
			}
			if got := rows[c.tag+"_"+form]; got != wantRow {
				t.Errorf("%s_%s: %q, want the reference's %q", c.tag, form, got, wantRow)
			}
		}
		wantMeasure := "refused"
		if inside {
			wantMeasure = "agrees"
		}
		if got := rows[c.tag+"_measure"]; got != wantMeasure {
			t.Errorf("%s_measure: %q, want %q", c.tag, got, wantMeasure)
		}
	}
	for _, row := range []struct{ name, want string }{
		{"variable_write_below_min", "refused"},
		{"json_write_below_min", "refused"},
		{"message_write_below_min", "refused"},
		{"fixed_write_below_min", "refused"},
		{"json_read_below_min", "refused"},
		{"json_read_at_min", "read 3F800000"},
		{"json_read_negzero", "read 00000000"},
		{"message_read_below_min", "refused"},
		{"message_read_at_min", "read"},
		{"variable_read_negzero", "read 00000000"},
		{"fixed_read_below_min", "refused"},
		{"fixed_read_at_min", "read"},
		{"fixed_float32_at_end", "read 3DCCCCCD"},
		{"fixed_float32_past_end", "refused"},
		{"message_float32_at_end", "read 3DCCCCCD"},
		{"message_float32_past_end", "refused"},
		{"message_read_birth_spelled_out", "refused"},
		{"text_read_below_min", "refused"},
		{"text_read_at_min", "read"},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, ran := rows[row.name]
			if !ran {
				t.Fatalf("the row did not run; the probe printed %q then %q", out, reads)
			}
			if probeVerdict(got) != row.want {
				t.Errorf("%q, want %q", got, row.want)
			}
		})
	}
}

// probeVerdict is a probe row as the across-legs contract compares it: a read, a
// write, or a refusal. Why a read or write refused is the leg's own
// diagnostic (docs/rulings.json refusal-verdict-only), so the reason the
// probe prints after "refused" is shown on failure but never compared.
func probeVerdict(row string) string {
	if i := strings.Index(row, "refused"); i >= 0 {
		return row[:i+len("refused")]
	}
	return row
}

// probeVerdicts is probeVerdict on every line of a probe's output.
func probeVerdicts(out string) string {
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		lines[i] = probeVerdict(line)
	}
	return strings.Join(lines, "\n")
}
