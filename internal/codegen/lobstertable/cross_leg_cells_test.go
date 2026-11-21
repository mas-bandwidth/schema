package lobstertable_test

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
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
	g := finiteGenerate(t, crossLegCellsSchema)
	out := finiteRun(t, g, "import .json\n"+crossLegCellsBody, jsonInputs)
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
	for _, row := range []struct{ name, want string }{
		{"variable_write_below_min", "refused"},
		{"json_write_below_min", "refused"},
		{"message_write_below_min", "refused"},
		{"fixed_write_below_min", "refused invalid_value"},
		{"json_read_below_min", "refused count_over_bound"},
		{"json_read_at_min", "read 3F800000"},
		{"json_read_negzero", "read 00000000"},
		{"message_read_below_min", "refused count_over_bound"},
		{"message_read_at_min", "read"},
		{"variable_read_negzero", "read 00000000"},
		{"fixed_read_below_min", "refused wire_damaged"},
		{"fixed_read_at_min", "read"},
		{"fixed_float32_at_end", "read 3DCCCCCD"},
		{"fixed_float32_past_end", "refused invalid_value"},
		{"message_float32_at_end", "read 3DCCCCCD"},
		{"message_float32_past_end", "refused"},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, ran := rows[row.name]
			if !ran {
				t.Fatalf("the row did not run; the probe printed %q then %q", out, reads)
			}
			if got != row.want {
				t.Errorf("%q, want %q", got, row.want)
			}
		})
	}
}
