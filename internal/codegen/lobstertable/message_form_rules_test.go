package lobstertable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE MESSAGE FORM'S ACROSS-LEGS RULES (review-owed/cross-leg/message-form;
// the Go reference's tablewire message decode, PR #4441):
//
//  1. A ranged integer's base plus offset is held to the ANNOUNCED kind's
//     domain, exactly and never wrapped, on the named read and on the skip
//     path, before the reader's own range.
//  2. A ranged array element is walked on the skip path, so each one is held
//     by rule 1.
//  3. A union FIELD written with arm reference 0 is damage on the skip path,
//     as it is on the named read; an array element's None stays positional.
//  5. bytes(N) counts from the announced minimum and is held to the announced
//     maximum.
//
// (Rule 4, duplicates by id in linear work, is LOBSTER-3.) Each row writes a
// batch by hand under an announcement whose entry the row reshapes, the way
// a sender with another declaration would announce it; a row under the
// foreign vocabulary carries another build version, so a field Root does not
// declare is unknown and skipped by its announced entry alone. Each row is
// compared by its verdict alone (docs/rulings.json refusal-verdict-only).
func TestLobsterMessageFormAcrossLegRules(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain")
	g, err := generateLoadUnit(t, []string{writeSchema(t, "Probe", messageRulesSchema)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "rules.lobster"), []byte(messageRulesProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "rules.lobster", nil)))
	if probeVerdicts(got) != messageRulesWant {
		t.Fatalf("message form rules:\n%s\nwant:\n%s", got, messageRulesWant)
	}
}

const messageRulesSchema = `package probe

union Choice
{
    item uint8
}

fixed table Root
{
    n      int16
    big    uint64
    blob   bytes(6)
    choice Choice
}

fixed table Other
{
    small int32
    nums  [..2]int32
    pick  Choice
    picks [..2]Choice
}
`

const messageRulesWant = `named-ranged-top:read 127
named-ranged-past-kind:refused
named-u64-wraps:refused
named-u64-top:read -1
skip-ranged-top:read
skip-ranged-past-kind:refused
skip-element-top:read
skip-element-past-kind:refused
skip-union-field-arm-zero:refused
skip-union-element-none:read
named-union-field-arm-zero:refused
bytes-from-min:read abc
bytes-past-max:refused`

const messageRulesProbe = `import .ProbeTable

let slots = schema_rt.message_slots_of(schema_rt.message_facts.fields, "Root")
let other = schema_rt.message_slots_of(schema_rt.message_facts.fields, "Other")

def vocabulary(foreign:bool) -> schema_rt.MessageVocabulary:
    let v = schema_rt.new_vocabulary(4096)
    assert schema_rt.announce_read(v, schema_rt.announce(), schema_rt.LoadReport {})
    if foreign:
        v.build_version = v.build_version + 1
    return v

def ranged(s:schema_rt.MessageShape, kind:int, bits:int, base:int):
    s.kind = kind
    s.packing = 1
    s.bits = bits
    s.base_lo = base
    s.base_hi = 0

def entry_of(v:schema_rt.MessageVocabulary, ref:int) -> schema_rt.MessageEntry:
    let e = schema_rt.message_entry(v, ref)
    assert e
    return e

def finish(w:schema_rt.MessageWriter) -> string:
    schema_rt.mw_put(w, 0, w.ref_bits)
    schema_rt.mw_align(w)
    var header = schema_rt.zeros(2)
    header = write_int8_le(header, 0, 2)
    header = write_int8_le(header, 1, 0)
    return header + w.b

def result(name:string, v:schema_rt.MessageVocabulary, w:schema_rt.MessageWriter, field:int):
    let rep = schema_rt.LoadReport {}
    let values, _, ok = schema_rt.load_messages("Root", v, finish(w), rep, 2)
    if not ok:
        print(name + ":refused " + schema_rt.strict_reason(rep))
    elif field < 0:
        print(name + ":read")
    elif field == 2:
        print(name + ":read " + values[0].fields[2].cell.s)
    else:
        print(name + ":read " + string(values[0].fields[field].cell.i))

def writer(v:schema_rt.MessageVocabulary, ref:int) -> schema_rt.MessageWriter:
    let w = schema_rt.MessageWriter { ref_bits: v.ref_bits }
    schema_rt.mw_put(w, ref, v.ref_bits)
    return w

// rule 1, named: an int8 announced as base 100 plus 5 bits, read into an int16
for([27, 31]) offset:
    let v = vocabulary(false)
    ranged(entry_of(v, slots[0]).shape, 2, 5, 100)
    let w = writer(v, slots[0])
    schema_rt.mw_put(w, offset, 5)
    result(if offset == 27: "named-ranged-top" else: "named-ranged-past-kind", v, w, 0)

// rule 1, named: a uint64 base 1 plus 64 bits, whose sum must not wrap
for([-1, -2]) offset:
    let v = vocabulary(false)
    ranged(entry_of(v, slots[1]).shape, 9, 64, 1)
    let w = writer(v, slots[1])
    schema_rt.mw_put(w, offset, 64)
    result(if offset == -1: "named-u64-wraps" else: "named-u64-top", v, w, 1)

// rule 1, skip: Other.small is unknown to Root and skipped by its entry
for([27, 31]) offset:
    let v = vocabulary(true)
    ranged(entry_of(v, other[0]).shape, 2, 5, 100)
    let w = writer(v, other[0])
    schema_rt.mw_put(w, offset, 5)
    result(if offset == 27: "skip-ranged-top" else: "skip-ranged-past-kind", v, w, -1)

// rule 2: a skipped array's ranged element is walked and held
for([27, 31]) offset:
    let v = vocabulary(true)
    let e = entry_of(v, other[1])
    ranged(e.element, 2, 5, 100)
    let w = writer(v, other[1])
    schema_rt.mw_put(w, 1, schema_rt.message_bits(e.shape.max - e.shape.min))
    schema_rt.mw_put(w, offset, 5)
    result(if offset == 27: "skip-element-top" else: "skip-element-past-kind", v, w, -1)

// rule 3: a skipped union FIELD at arm 0 is damage; a skipped element's None is not
def rule3_1():
    let v = vocabulary(true)
    let w = writer(v, other[2])
    schema_rt.mw_put(w, 0, v.ref_bits)
    result("skip-union-field-arm-zero", v, w, -1)
rule3_1()
def rule3_2():
    let v = vocabulary(true)
    let e = entry_of(v, other[3])
    let w = writer(v, other[3])
    schema_rt.mw_put(w, 1, schema_rt.message_bits(e.shape.max - e.shape.min))
    schema_rt.mw_put(w, 0, v.ref_bits)
    result("skip-union-element-none", v, w, -1)
rule3_2()
def rule3_3():
    let v = vocabulary(false)
    let w = writer(v, slots[3])
    schema_rt.mw_put(w, 0, v.ref_bits)
    result("named-union-field-arm-zero", v, w, -1)
rule3_3()

// rule 5: bytes(6) announced with minimum 2; the count rides above it
for([1, 5]) raw:
    let v = vocabulary(false)
    let e = entry_of(v, slots[2])
    e.shape.min = 2
    e.shape.max = 6
    let w = writer(v, slots[2])
    schema_rt.mw_put(w, raw, schema_rt.message_bits(e.shape.max - e.shape.min))
    if raw == 1:
        schema_rt.mw_align(w)
        schema_rt.mw_raw(w, "abc")
    result(if raw == 1: "bytes-from-min" else: "bytes-past-max", v, w, 2)
`
