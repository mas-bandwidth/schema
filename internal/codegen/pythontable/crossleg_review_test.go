package pythontable

import (
	"testing"
)

// The Python leg's cells of the cross-leg rules the review of 2026-11-21
// found broken in other legs (review-owed/cross-leg/*): a compressed float's
// range is held at float32 and its -0 lands as +0, a counted array below its
// minimum is refused and born at it, and the message form holds a ranged
// value to its announced kind and refuses arm 0 on a union field, named or
// skipped.

// A compressed float whose declared top is 0.1, a decimal float32 cannot
// hold: its float32 end is 0.10000000149011612, and 0.1 written by a caller
// is that float32 on the wire. SPEC.md 4.6 compares at float32, so it lands.
func TestPythonCompressedFloatRangeAtFloat32(t *testing.T) {
	t.Parallel()
	const variable = `package f32range
table Probe { tenth float32 | min = 0, max = 0.1, resolution = 0.01 }
`
	runMessageProbe(t, variable, `import struct
from f32range import Probe, Refused, InvalidValue
from f32range._schema.json_table import from_json, to_json
top = struct.unpack('<f', struct.pack('<f', 0.1))[0]
p = Probe(); p.tenth = 0.1
try: data = p.save()
except InvalidValue as e: raise AssertionError('f32_range_variable_write') from e
assert Probe.load(data).tenth == top, 'f32_range_variable_read'
try: q = from_json(Probe, '{"tenth": 0.1}')
except Refused as e: raise AssertionError('f32_range_json_read') from e
assert q.tenth == top, 'f32_range_json_value'
try: to_json(p)
except InvalidValue as e: raise AssertionError('f32_range_json_write') from e
over = Probe(); over.tenth = 0.11
try: over.save()
except InvalidValue: pass
else: raise AssertionError('f32_range_above_control')
print('ok')
`, map[string][]byte{})

	// The message read: a peer that announces the field uncompressed sends
	// the raw float32 0.1f, which this reader's max = 0.1 holds at float32.
	const writer = "package f32range\nfixed table Msg { tenth float32 }\n"
	const reader = "package f32range\nfixed table Msg { tenth float32 | min = 0, max = 0.1, resolution = 0.01 }\n"
	announcement, wire := messageWire(t, writer, "Msg", `{"tenth":0.1}`)
	runMessageProbe(t, reader, `import struct
from f32range import Msg, MessageVocabulary, announce_read, load_messages, Refused
with open('peer','rb') as f: peer=f.read()
with open('wire','rb') as f: wire=f.read()
v=MessageVocabulary();announce_read(v,peer)
try: value=load_messages(Msg,v,wire)[0]
except Refused as e: raise AssertionError('f32_range_message_read') from e
assert value.tenth==struct.unpack('<f',struct.pack('<f',0.1))[0],'f32_range_message_value'
print('ok')
`, map[string][]byte{"peer": announcement, "wire": wire})
}

// A compressed float holding -0 lands as +0 on the message read, whatever
// the peer announced (SPEC-TABLES: -0 is +0; ruling fixed-compressed-negzero).
func TestPythonMessageCompressedNegativeZero(t *testing.T) {
	t.Parallel()
	const writer = "package negzero\nfixed table Msg { z float32\n w int32 }\n"
	const reader = "package negzero\nfixed table Msg { z float32 | min = -1, max = 1, resolution = 0.01\n w int32 }\n"
	announcement, _ := messageWire(t, writer, "Msg", `{"w":1}`)
	runMessageProbe(t, reader, `import math
from negzero import Msg, MessageVocabulary, announce_read, load_messages
from negzero._schema.message import BitWriter
with open('peer','rb') as f: peer=f.read()
v=MessageVocabulary();announce_read(v,peer)
fid=Msg.descriptor.fields[0].field_id
slot=next(i+1 for i,e in enumerate(v.entries) if e.field_id==fid)
w=BitWriter();w.put(2,8);w.put(0,8);w.put(slot,v.ref_bits);w.put(0x80000000,32);w.put(0,v.ref_bits)
value=load_messages(Msg,v,w.finish())[0]
assert value.z==0 and math.copysign(1.0,value.z)==1.0,('negzero_message_read',value.z)
print('ok')
`, map[string][]byte{"peer": announcement})
}

// A counted array [Min..Max]T: a fresh value is born at Min, and a count
// below Min is refused on the variable, JSON and message reads and on the
// writes (SPEC.md 5.1 row 3, 4.2).
func TestPythonCountedArrayMinimum(t *testing.T) {
	t.Parallel()
	const variable = "package countmin\ntable Probe { a [2..4]int32 }\n"
	runMessageProbe(t, variable, `from countmin import Probe, Refused, InvalidValue
from countmin._schema.json_table import from_json
assert len(Probe().a)==2,('count_min_birth',Probe().a)
Probe.load(Probe().save())
p=Probe();p.a=[1]
try: p.save()
except InvalidValue: pass
else: raise AssertionError('count_min_variable_write')
try: from_json(Probe,'{"a":[1]}')
except Refused as e: assert e.reason=='count_over_bound',e.reason
else: raise AssertionError('count_min_json_read')
print('ok')
`, map[string][]byte{})

	const fixedWriter = "package countmin\nfixed table Msg { a [..4]int32 }\n"
	const fixedReader = "package countmin\nfixed table Msg { a [2..4]int32 }\n"
	announcement, wire := messageWire(t, fixedWriter, "Msg", `{"a":[7]}`)
	runMessageProbe(t, fixedReader, `from countmin import Msg, MessageVocabulary, announce_read, load_messages, Refused, InvalidValue
with open('peer','rb') as f: peer=f.read()
with open('wire','rb') as f: wire=f.read()
assert len(Msg().a)==2,('count_min_fixed_birth',Msg().a)
v=MessageVocabulary();announce_read(v,peer)
try: load_messages(Msg,v,wire)
except Refused as e: assert e.reason=='count_over_bound',e.reason
else: raise AssertionError('count_min_message_read')
m=Msg();m.a=[1]
try: Msg.fixed_save([m])
except InvalidValue: pass
else: raise AssertionError('count_min_fixed_write')
print('ok')
`, map[string][]byte{"peer": announcement, "wire": wire})
}

// The message form: a ranged integer is base plus offset held to the
// announced kind's domain, never wrapped, and a union field written with arm
// reference 0 is refused, whether this reader names the field or skips it.
func TestPythonMessageRangedAndUnionArmZero(t *testing.T) {
	t.Parallel()
	const writer = `package msgrules
fixed table Inner { v int32 }
union Choice { nested Inner }
fixed table Root { x int8 = 100 | min = 100, max = 127
 c Choice }
`
	announcement, _ := messageWire(t, writer, "Root", `{"x":100}`)
	probe := `from msgrules import Root, MessageVocabulary, announce_read, load_messages, Refused
from msgrules._schema.message import BitWriter
with open('peer','rb') as f: peer=f.read()
v=MessageVocabulary();announce_read(v,peer)
r=v.ref_bits
x=next(i+1 for i,e in enumerate(v.entries) if e.kind==2)
shape=v.entries[x-1].shape
assert shape.packing==1 and shape.base==100 and shape.bits==5,(shape.packing,shape.base,shape.bits)
def wire(*fields):
 w=BitWriter();w.put(2,8);w.put(0,8)
 for put in fields: put(w)
 w.put(0,r);return w.finish()
over=wire(lambda w:(w.put(x,r),w.put(31,5)))
try: load_messages(Root,v,over)
except Refused as e: assert e.reason=='out_of_range',('ranged_held_'+NAME,e.reason)
else: raise AssertionError('ranged_held_'+NAME)
assert load_messages(Root,v,wire(lambda w:(w.put(x,r),w.put(27,5))))[0] is not None
c=next(i+1 for i,e in enumerate(v.entries) if e.kind==15)
zero=wire(lambda w:(w.put(c,r),w.put(0,r)))
try: load_messages(Root,v,zero)
except Refused as e: assert e.reason=='wire_damaged',('union_arm_zero_'+NAME,e.reason)
else: raise AssertionError('union_arm_zero_'+NAME)
print('ok')
`
	inputs := func() map[string][]byte { return map[string][]byte{"peer": announcement} }
	named := `package msgrules
fixed table Inner { v int32 }
union Choice { nested Inner }
fixed table Root { x int16
 c Choice }
`
	runMessageProbe(t, named, "NAME='named'\n"+probe, inputs())
	skipped := "package msgrules\nfixed table Root { other int32 }\n"
	runMessageProbe(t, skipped, "NAME='skipped'\n"+probe, inputs())
}
