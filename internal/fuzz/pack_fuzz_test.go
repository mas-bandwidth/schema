// Fuzzing over internal/pack — the SECOND input surface. The language
// pipeline hardening (fuzz_test.go) covers schema source; pack consumes two
// other things from outside the trust boundary: arbitrary JSON instance data
// (the data compiler's input) and raw table bytes (DecodeTable reads what in
// production comes off disk or the wire). Neither had a fuzzer; the doctrine
// applied to the language — "any crash on malformed input is our bug, however
// nonsensical the input" — applies verbatim here.
//
// Run briefly with -fuzz FuzzDecodeTable or -fuzz FuzzPackJSON, same shape as
// the pipeline targets (fuzz_test.go).
package fuzz_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/internal/pack"
	"github.com/mas-bandwidth/schema/ir"
)

// packSchema exercises every scalar family the table wire carries: bounded
// ints, floats, enums, flags, strings, bytes, arrays, nested structs, bools
// and defaults — one struct per trap the encoder has to route.
const packSchema = `package fz

enum Mode { Alpha, Beta, Gamma }

flags Caps { A, B, C }

type Inner {
    factor float32 = 2.5
    tag    string(8)
}

table Cfg {
    a     int32   = 5 | min = -1000, max = 1000
    b     float32                           = 1.5
    big   uint64  | min = 0, max = 18446744073709551615
    mode  Mode                              = Beta
    caps  Caps
    name  string(32)
    blob  bytes(64)
    inner Inner
    items [..8]int32 | min = 0, max = 255
    on    bool = true
}
`

func packUnit(tb testing.TB) *ir.Unit {
	tb.Helper()
	u := unitOf(map[string]string{"Fz.schema": packSchema})
	if u == nil {
		tb.Fatal("pack fuzz schema does not parse+check — fix packSchema")
	}
	return u
}

// FuzzDecodeTable: table bytes from OUTSIDE — any input either decodes (with
// a report) or is refused with an error. Panics and hangs are compiler bugs,
// exactly as for schema source. Seeded with real encodings so mutation
// starts from valid wire and corrupts outward — lengths, ids, kinds.
func FuzzDecodeTable(f *testing.F) {
	u := packUnit(f)
	enc := &pack.Encoder{Unit: u}
	for _, seed := range []string{
		`{}`,
		`{"a": -7, "mode": "Gamma", "name": "hello", "items": [1, 2, 250]}`,
		`{"caps": ["A", "C"], "blob": "3q2+7w==", "inner": {"factor": 0.5, "tag": "x"}, "on": false}`,
		`{"big": 18446744073709551615, "b": -1.25}`,
	} {
		var obj map[string]any
		dec := json.NewDecoder(strings.NewReader(seed))
		dec.UseNumber()
		if err := dec.Decode(&obj); err != nil {
			f.Fatal(err)
		}
		wire, err := enc.EncodeTable("Cfg", obj)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(wire)
		if len(wire) > 3 {
			f.Add(wire[:len(wire)/2]) // a truncation, pre-seeded
		}
	}
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		out, rep, err := pack.DecodeTable(u, "Cfg", data)
		if err != nil {
			return // refusing bytes is a normal outcome
		}
		if out == nil || rep == nil {
			t.Fatalf("DecodeTable returned no error and nil result (out=%v rep=%v)", out, rep)
		}
	})
}

// FuzzPackJSON: the data compiler's input surface — arbitrary JSON text
// through the exact dialect production uses (trailing-comma stripping +
// UseNumber), then both encoders. Two properties ride on top of survival:
// clamp mode must never turn an encodable value into a panic, and any bytes
// EncodeTable emits must round-trip through DecodeTable cleanly — the
// encoder and decoder are two halves of one wire contract, so an encoding
// the decoder refuses or misreads is a bug in one of them no matter which.
func FuzzPackJSON(f *testing.F) {
	u := packUnit(f)
	for _, seed := range []string{
		`{}`,
		`{"a": 5}`,
		`{"a": -7, "mode": "Gamma", "name": "hello", "items": [1, 2, 250],}`,
		`{"caps": ["A", "C"], "blob": "3q2+7w==", "inner": {"factor": 0.5, "tag": "x"}, "on": false}`,
		`{"big": 18446744073709551615, "b": -1.25}`,
		`{"a": 1e309}`,
		`{"a": "not a number"}`,
		`{"inner": []}`,
		`{"items": [[]]}`,
		`{"name": "\ud800"}`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, jsonText string) {
		var obj map[string]any
		dec := json.NewDecoder(strings.NewReader(pack.StripTrailingCommas(jsonText)))
		dec.UseNumber()
		if err := dec.Decode(&obj); err != nil {
			return // not JSON: nothing to compile
		}
		enc := &pack.Encoder{Unit: u}
		clamp := &pack.Encoder{Unit: u, ClampBounds: true}
		if _, err := enc.EncodeInstance("Cfg", obj); err == nil {
			// encodable strictly => encodable clamped (clamp only widens)
			if _, cerr := clamp.EncodeInstance("Cfg", obj); cerr != nil {
				t.Fatalf("strict encoder accepts this instance but clamp mode refuses it: %v\n%s", cerr, jsonText)
			}
		}
		wire, err := enc.EncodeTable("Cfg", obj)
		if err != nil {
			return
		}
		out, rep, err := pack.DecodeTable(u, "Cfg", wire)
		if err != nil || out == nil {
			t.Fatalf("EncodeTable produced bytes DecodeTable refuses (%v)\njson: %s\nwire: %x", err, jsonText, wire)
		}
		if rep.Malformed || rep.Unknown != 0 || rep.KindMismatch != 0 {
			t.Fatalf("EncodeTable produced bytes DecodeTable flags (malformed=%v unknown=%d kindMismatch=%d)\njson: %s\nwire: %x",
				rep.Malformed, rep.Unknown, rep.KindMismatch, jsonText, wire)
		}
	})
}
