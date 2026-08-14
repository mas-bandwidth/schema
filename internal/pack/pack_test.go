package pack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/internal/check"
	"github.com/mas-bandwidth/schema/internal/ir"
	"github.com/mas-bandwidth/schema/internal/parser"
)

// The load-bearing gate: the pack encoder must reproduce the PINNED WIRE
// GOLDENS — the same bytes the five generated runtimes byte-match in the
// language test suites. The instances below are the test suite's own
// (test/main.cpp), spelled as the JSON the data compiler consumes. A bit-order
// or framing divergence in the interpreter cannot survive this test.

func loadCorpus(t *testing.T) *ir.Unit {
	t.Helper()
	return loadUnit(t, "../../examples")
}

func loadUnit(t *testing.T, dir string) *ir.Unit {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []check.SourceFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		f, perrs := parser.Parse(p, data)
		if len(perrs) > 0 {
			t.Fatalf("corpus does not parse: %v", perrs[0])
		}
		files = append(files, check.SourceFile{
			Path:  p,
			Name:  e.Name(),
			Base:  strings.TrimSuffix(e.Name(), ".schema"),
			Bytes: data,
			AST:   f,
		})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("corpus does not check: %v", cerrs[0])
	}
	return u
}

func packJSON(t *testing.T, u *ir.Unit, typeName, jsonText string) []byte {
	t.Helper()
	var obj map[string]any
	dec := json.NewDecoder(strings.NewReader(jsonText))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		t.Fatalf("test JSON: %v", err)
	}
	enc := &Encoder{Unit: u}
	wire, err := enc.EncodeInstance(typeName, obj)
	if err != nil {
		t.Fatalf("%s: %v", typeName, err)
	}
	return wire
}

func golden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../../testdata/wire", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func checkWire(t *testing.T, name string, got, want []byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d bytes, golden is %d", name, len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: byte %d is 0x%02x, golden has 0x%02x", name, i, got[i], want[i])
		}
	}
}

func TestPackAgainstWireGoldens(t *testing.T) {
	u := loadCorpus(t)

	// test/main.cpp's rigidbody instance — floats, nested structs, the branch taken
	checkWire(t, "rigidbody_moving", packJSON(t, u, "RigidBody", `{
		"position":         { "x": 1.5,  "y": -2.5, "z": 3.25 },
		"orientation":      { "x": 0.1,  "y": 0.2,  "z": 0.3, "w": 0.9 },
		"at_rest":          false,
		"linear_velocity":  { "x": 10.0, "y": 20.0, "z": -3.0 },
		"angular_velocity": { "x": 0.25, "y": 0.5,  "z": 0.75 }
	}`), golden(t, "rigidbody_moving.bin"))

	// the branch untaken: velocity keys present in JSON, dropped from the wire
	checkWire(t, "rigidbody_at_rest", packJSON(t, u, "RigidBody", `{
		"position":         { "x": 1.5,  "y": -2.5, "z": 3.25 },
		"orientation":      { "x": 0.1,  "y": 0.2,  "z": 0.3, "w": 0.9 },
		"at_rest":          true,
		"linear_velocity":  { "x": 10.0, "y": 20.0, "z": -3.0 },
		"angular_velocity": { "x": 0.25, "y": 0.5,  "z": 0.75 }
	}`), golden(t, "rigidbody_at_rest.bin"))

	// string framing: length prefix, align, bytes (SPEC §4.7)
	checkWire(t, "chat", packJSON(t, u, "Chat", `{ "text": "wire parity" }`),
		golden(t, "chat.bin"))

	// counted array of nested composites, absent fields as zero values
	checkWire(t, "inputpacket", packJSON(t, u, "InputPacket", `{
		"synchronize_sequence": 7,
		"current_frame": 123456789,
		"start_frame":   123456780,
		"inputs": [
			{ "throttle": 0.5,   "fire": true },
			{ "stick_x": -0.25,  "boost": true }
		]
	}`), golden(t, "inputpacket.bin"))

	// odd bit widths and full-range 64-bit values — the json.Number path
	checkWire(t, "probebits", packJSON(t, u, "ProbeBits", `{
		"small":    511,
		"boundary": 8589934591,
		"wide":     18364758544493064720,
		"sensor":   4294967295,
		"nonce":    18446744073709551615
	}`), golden(t, "probebits.bin"))

	// the everything instance (test/main.cpp's TestData): the COMPRESSED FLOAT
	// among strings, counted arrays, raw bits, an align and a fixed byte array.
	// The quantized integer is float32 arithmetic in every runtime, so a
	// float64 shortcut here would land on a different step and this golden
	// would break.
	checkWire(t, "testdata", packJSON(t, u, "TestData", `{
		"a": -100, "b": 100, "c": 149,
		"d": 17, "e": 34, "f": 51,
		"g": true,
		"items": [0, 128, 255],
		"float_value":            3.1415926,
		"compressed_float_value": 2.5,
		"double_value":           0.3333333333333333,
		"int8_value":   -128,
		"int16_value":  -32768,
		"uint8_value":  255,
		"uint16_value": 65535,
		"uint32_value": 4294967295,
		"uint64_value": 18446744073709551615,
		"int64_full":   -9223372036854775808,
		"int64_range":  -999999999999,
		"fixed_bytes": [0, 3, 6, 9, 12, 15, 18, 21, 24, 27, 30, 33, 36, 39, 42, 45, 48],
		"text": "the quick brown fox"
	}`), golden(t, "testdata.bin"))
}

// The fixed-point + 128-bit unit (examples128/) — the wire families the data
// compiler could not reach until 2026-08-14. The goldens are the ones
// test/ludicrous_main.cpp derives BY HAND from STANDARD.md and the five
// generated runtimes byte-match, so this is the same standing gate: fixed(I,
// F) at four Q formats including the F = 0 corner and an array, uint128 raw,
// int128 ranged both inside and past 64 bits, and 128-bit specified defaults
// (wide.bias and wide.seed are absent from the JSON on purpose).
//
// Fixed values are WHOLE UNITS here, the domain the bounds and defaults are
// written in: 45.5 is the Q16.16 raw 2981888, and reach's whole-unit spelling
// is exact — json.Number keeps the literal text, so the scaling never rides a
// float64 approximation.
func TestPackLudicrousAgainstWireGoldens(t *testing.T) {
	u := loadUnit(t, "../../examples128")

	const state = `{
		"mode": "Ludicrous",
		"probe": {
			"angle":    45.5,
			"position": -12346.1875,
			"reach":    999999.9999847412109375,
			"ticks":    777777,
			"samples":  [-8, 8]
		},
		"wide": {
			"entity_id": 1512366075204170947332355369683137040,
			"energy":    4999999999,
			"flux":      633825300114114700748351602695
		},
		"keys": [1, 170141183460469231731687303715884105728],
		"has_target": %s,
		"target_id": 42
	}`

	checkWire(t, "ludicrous_state", packJSON(t, u, "LudicrousState", fmt.Sprintf(state, "true")),
		golden(t, "ludicrous_state.bin"))
	// the untaken branch: target_id is present in the JSON and absent from the wire
	checkWire(t, "ludicrous_state_untargeted", packJSON(t, u, "LudicrousState", fmt.Sprintf(state, "false")),
		golden(t, "ludicrous_state_untargeted.bin"))
}

// Fixed point data rides the field's declared Q grid: whole units in, the raw
// scaled integer out, rounded to nearest with halves AWAY FROM ZERO (the
// family's convention — the generated shallow-narrowing Quantize rounds the
// same way). Pinned by self-comparison rather than a golden: a value one raw
// step up must pack as that step, and the exact half must land on it too.
func TestPackFixedRounding(t *testing.T) {
	u := loadUnit(t, "../../examples128")

	// Q16.16: one raw step is 1/65536 of a whole unit
	const step = 1.0 / 65536.0
	probe := func(angle string) []byte {
		return packJSON(t, u, "FixedProbe", fmt.Sprintf(`{
			"angle": %s, "position": 0, "reach": 0, "ticks": 0, "samples": [0, 0]
		}`, angle))
	}
	up := probe(fmt.Sprintf("%.20f", 45.5+step))
	checkWire(t, "fixed half rounds away from zero (+)", probe(fmt.Sprintf("%.20f", 45.5+step/2)), up)
	checkWire(t, "fixed rounds to nearest (+)", probe(fmt.Sprintf("%.20f", 45.5+step*0.9)), up)
	if bytesEqual(probe("45.5"), up) {
		t.Fatal("a whole raw step must move the wire — the rounding test is measuring nothing")
	}
	down := probe(fmt.Sprintf("%.20f", -45.5-step))
	checkWire(t, "fixed half rounds away from zero (-)", probe(fmt.Sprintf("%.20f", -45.5-step/2)), down)

	// F = 0 is a ranged integer, and a fractional value still rounds onto it
	ticks := func(v string) []byte {
		return packJSON(t, u, "FixedProbe", fmt.Sprintf(`{
			"angle": 0, "position": 0, "reach": 0, "ticks": %s, "samples": [0, 0]
		}`, v))
	}
	checkWire(t, "fixed(32, 0) rounds to nearest", ticks("777776.5"), ticks("777777"))
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The new families refuse as loudly as the old ones: a fixed value outside its
// whole-unit bounds, a uint128 that does not fit 128 bits, and an int128
// outside its declared range.
func TestPackWideRefusals(t *testing.T) {
	u := loadUnit(t, "../../examples128")
	enc := &Encoder{Unit: u}

	cases := []struct {
		name, typeName, jsonText, wantErr string
	}{
		{"fixed above max", "FixedProbe", `{ "angle": 180.5 }`, "outside wire range [-180, 180] (whole units)"},
		{"fixed below min", "FixedProbe", `{ "angle": -180.5 }`, "outside wire range"},
		{"uint128 overflows", "WideProbe", `{ "entity_id": 340282366920938463463374607431768211456 }`, "does not fit uint128"},
		{"uint128 negative", "WideProbe", `{ "entity_id": -1 }`, "does not fit uint128"},
		{"int128 out of range", "WideProbe", `{ "flux": 1267650600228229401496703205377 }`, "outside wire range"},
		{"fixed is a number", "FixedProbe", `{ "angle": "45.5" }`, "expected JSON number"},
	}
	for _, c := range cases {
		var obj map[string]any
		dec := json.NewDecoder(strings.NewReader(c.jsonText))
		dec.UseNumber()
		if err := dec.Decode(&obj); err != nil {
			t.Fatal(err)
		}
		_, err := enc.EncodeInstance(c.typeName, obj)
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Fatalf("%s: want error containing %q, got %v", c.name, c.wantErr, err)
		}
	}
}

// TestPackRefusals: the compile step must say NO loudly — a typo'd key, an
// out-of-range value, an unknown enum variant.
func TestPackRefusals(t *testing.T) {
	u := loadCorpus(t)
	enc := &Encoder{Unit: u}

	cases := []struct {
		name, typeName, jsonText, wantErr string
	}{
		{"unknown field", "Chat", `{ "txet": "typo" }`, "unknown field"},
		{"string too long", "Test", `{ "test_b": 1001 }`, "outside wire range"},
		{"bad variant", "ShipCreate", `{ "ship_type": "Dreadnought" }`, "not a variant"},
	}
	for _, c := range cases {
		var obj map[string]any
		dec := json.NewDecoder(strings.NewReader(c.jsonText))
		dec.UseNumber()
		if err := dec.Decode(&obj); err != nil {
			t.Fatal(err)
		}
		_, err := enc.EncodeInstance(c.typeName, obj)
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Fatalf("%s: want error containing %q, got %v", c.name, c.wantErr, err)
		}
	}
}
