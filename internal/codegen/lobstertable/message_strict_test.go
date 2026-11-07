package lobstertable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

func TestLobsterMessageOneRead(t *testing.T) {
	t.Parallel()
	g, err := generateLoadUnit(t, []string{writeSchema(t, "Probe", strictMessageSchema)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(strictMessageProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func() string { return strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", nil))) }
	if got := run(); got != strictMessageWant {
		t.Fatalf("one read:\n%s\nwant:\n%s", got, strictMessageWant)
	}
	path := filepath.Join(g.dir, "message.lobster")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "if id == e.id:\n                return message_refuse(rep, \"duplicate\")"
	if strings.Count(string(src), anchor) != 1 {
		t.Fatal("body duplicate anchor must occur once")
	}
	changed := strings.Replace(string(src), anchor, "if id == e.id:\n                pass()", 1)
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := run(); !strings.Contains(got, "repeat-known:accepted") || !strings.Contains(got, "second-record:accepted") {
		t.Fatalf("duplicate control did not admit repeated fields: %s", got)
	}
}

const strictMessageSchema = `package probe
fixed table Root { n int32
 text string(3)
 nums [..2]int32
 fixed_nums [3]int32
}
fixed table Other { alien int32
 bad float32
 words string(7)
}
`

const strictMessageWant = `roundtrip
repeat-known:refused
second-record:refused
repeat-unknown:refused
unknown-valid:accepted
unknown-nan:refused
unknown-utf8:refused
text-bound:refused
array-bound:refused
fixed-growth:accepted
report-reuse:accepted`

const strictMessageProbe = `import .ProbeTable

let rec = schema_rt.new_record(schema_rt.table_of("Root"))
rec.fields[0].cell.i = 7
let wire, size = schema_rt.save_messages([rec], schema_rt.LoadReport {})
assert size > 0
let vocab = schema_rt.new_vocabulary(4096)
assert schema_rt.announce_read(vocab, schema_rt.announce(), schema_rt.LoadReport {})
let rows, count, good = schema_rt.load_messages("Root", vocab, wire, schema_rt.LoadReport {}, 2)
assert good and count == 1 and rows[0].fields[0].cell.i == 7
let saved, saved_size = schema_rt.save_messages(rows, schema_rt.LoadReport {})
assert saved == wire and saved_size == size
print("roundtrip")
let slots = schema_rt.message_slots_of(schema_rt.message_facts.fields, "Root")
let other = schema_rt.message_slots_of(schema_rt.message_facts.fields, "Other")

def finish(w:schema_rt.MessageWriter, records:int = 1) -> string:
    schema_rt.mw_align(w)
    var header = schema_rt.zeros(2)
    header = write_int8_le(header, 0, 2)
    header = write_int8_le(header, 1, records - 1)
    return header + w.b

def result(name:string, data:string, v:schema_rt.MessageVocabulary):
    let values, _, ok = schema_rt.load_messages("Root", v, data, schema_rt.LoadReport {}, 2)
    if not ok: assert length(values) == 0
    print(name + (if ok: ":accepted" else: ":refused"))

def repeat_body(w:schema_rt.MessageWriter, slot:int, value:schema_rt.Slot, entry:schema_rt.MessageEntry):
    for(2):
        schema_rt.mw_put(w, slot, w.ref_bits)
        assert schema_rt.mw_value(w, value, entry)
    schema_rt.mw_put(w, 0, w.ref_bits)

let repeated = schema_rt.MessageWriter { ref_bits: vocab.ref_bits }
repeat_body(repeated, slots[0], rec.fields[0], schema_rt.message_own_entry(slots, 0))
result("repeat-known", finish(repeated), vocab)
let second = schema_rt.MessageWriter { ref_bits: vocab.ref_bits }
assert schema_rt.mw_body(second, rec)
repeat_body(second, slots[0], rec.fields[0], schema_rt.message_own_entry(slots, 0))
result("second-record", finish(second, 2), vocab)
let orec = schema_rt.new_record(schema_rt.table_of("Other"))
orec.fields[0].cell.i = 19
let unknown_repeat = schema_rt.MessageWriter { ref_bits: vocab.ref_bits }
repeat_body(unknown_repeat, other[0], orec.fields[0], schema_rt.message_own_entry(other, 0))
result("repeat-unknown", finish(unknown_repeat), vocab)
let unknown_good = schema_rt.MessageWriter { ref_bits: vocab.ref_bits }
schema_rt.mw_put(unknown_good, other[0], vocab.ref_bits)
assert schema_rt.mw_value(unknown_good, orec.fields[0], schema_rt.message_own_entry(other, 0))
schema_rt.mw_put(unknown_good, 0, vocab.ref_bits)
result("unknown-valid", finish(unknown_good), vocab)
let bad_float = schema_rt.MessageWriter { ref_bits: vocab.ref_bits }
schema_rt.mw_put(bad_float, other[1], vocab.ref_bits)
schema_rt.mw_put(bad_float, 0x7fc00000, 32)
schema_rt.mw_put(bad_float, 0, vocab.ref_bits)
result("unknown-nan", finish(bad_float), vocab)
let invalid_text = schema_rt.MessageWriter { ref_bits: vocab.ref_bits }
schema_rt.mw_put(invalid_text, other[2], vocab.ref_bits)
schema_rt.mw_put(invalid_text, 1, 3)
schema_rt.mw_raw(invalid_text, "\xff")
schema_rt.mw_put(invalid_text, 0, vocab.ref_bits)
result("unknown-utf8", finish(invalid_text), vocab)
let text_entry = schema_rt.message_entry(vocab, slots[1])
assert text_entry
let old_max = text_entry.shape.max
text_entry.shape.max = 7
let long_text = schema_rt.MessageWriter { ref_bits: vocab.ref_bits }
schema_rt.mw_put(long_text, slots[1], vocab.ref_bits)
schema_rt.mw_put(long_text, 4, 3)
schema_rt.mw_raw(long_text, "abcd")
schema_rt.mw_put(long_text, 0, vocab.ref_bits)
result("text-bound", finish(long_text), vocab)
text_entry.shape.max = old_max
let array_entry = schema_rt.message_entry(vocab, slots[2])
assert array_entry
let old_count = array_entry.shape.max
array_entry.shape.max = 3
let long_array = schema_rt.MessageWriter { ref_bits: vocab.ref_bits }
schema_rt.mw_put(long_array, slots[2], vocab.ref_bits)
schema_rt.mw_put(long_array, 3, 2)
for(3): schema_rt.mw_put(long_array, 0, 32)
schema_rt.mw_put(long_array, 0, vocab.ref_bits)
result("array-bound", finish(long_array), vocab)
array_entry.shape.max = old_count
let fixed_entry = schema_rt.message_entry(vocab, slots[3])
assert fixed_entry
fixed_entry.shape.min = 2
fixed_entry.shape.max = 2
let smaller = schema_rt.MessageWriter { ref_bits: vocab.ref_bits }
schema_rt.mw_put(smaller, slots[3], vocab.ref_bits)
schema_rt.mw_put(smaller, 23, 32)
schema_rt.mw_put(smaller, 47, 32)
schema_rt.mw_put(smaller, 0, vocab.ref_bits)
let grown, grown_count, grown_ok = schema_rt.load_messages("Root", vocab, finish(smaller), schema_rt.LoadReport {}, 2)
assert grown_ok and grown_count == 1
assert grown[0].fields[3].elems[0].i == 23 and grown[0].fields[3].elems[1].i == 47
assert grown[0].fields[3].elems[2].i == 0
print("fixed-growth:accepted")
let reused = schema_rt.LoadReport {}
let batch:[schema_rt.Record] = []
let _, first_ok = schema_rt.load_messages_into("Root", vocab, "", reused, 2, batch)
assert not first_ok and length(batch) == 0
let again_count, again_ok = schema_rt.load_messages_into("Root", vocab, wire, reused, 2, batch)
assert again_ok and again_count == 1 and batch[0].fields[0].cell.i == 7
assert not reused.refused and not reused.malformed and reused.reason == ""
print("report-reuse:accepted")
`
