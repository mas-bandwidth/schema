package pack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/internal/check"
	"github.com/mas-bandwidth/schema/internal/ir"
	"github.com/mas-bandwidth/schema/internal/parser"
)

// The load-bearing gate: the pack encoder must reproduce the PINNED WIRE
// GOLDENS — the same bytes the four generated runtimes byte-match in the
// language test suites. The instances below are the test suite's own
// (test/main.cpp), spelled as the JSON the data compiler consumes. A bit-order
// or framing divergence in the interpreter cannot survive this test.

func loadCorpus(t *testing.T) *ir.Unit {
	t.Helper()
	dir := "../../examples"
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
