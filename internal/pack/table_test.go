package pack

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/internal/check"
	"github.com/mas-bandwidth/schema/internal/ir"
	"github.com/mas-bandwidth/schema/internal/parser"
)

// unitFromSource builds a unit from inline schema text — the evolution
// harness's lever: encode under one schema, decode under an edited one.
func unitFromSource(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Test.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Test.schema", Name: "Test.schema", Base: "Test", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

func obj(t *testing.T, jsonText string) map[string]any {
	t.Helper()
	var o map[string]any
	dec := json.NewDecoder(strings.NewReader(jsonText))
	dec.UseNumber()
	if err := dec.Decode(&o); err != nil {
		t.Fatal(err)
	}
	return o
}

const schemaV1 = `package evo

enum Mode { Alpha, Beta }

type Inner {
    factor float32 = 2.5
}

type Cfg {
    a     int32   [min = 0, max = 1000] = 5
    b     float32                       = 1.5
    mode  Mode                          = Beta
    name  string(32)
    inner Inner
    items [<= 8]int32 [min = 0, max = 255]
}
`

// v2 ADDS c (new field), REMOVES b, and CHANGES a's type (int32 -> float32).
const schemaV2 = `package evo

enum Mode { Alpha, Beta }

type Inner {
    factor float32 = 2.5
    gain   float32 = 1.0
}

type Cfg {
    a     float32 = 5.0
    c     bool    = true
    mode  Mode    = Beta
    name  string(32)
    inner Inner
    items [<= 8]int32 [min = 0, max = 255]
}
`

// TestTableRoundTrip: same-schema encode/decode is faithful, defaults elide
// and reappear, and the report is silent.
func TestTableRoundTrip(t *testing.T) {
	u := unitFromSource(t, schemaV1)
	enc := &Encoder{Unit: u}
	wire, err := enc.EncodeTable("Cfg", obj(t, `{
		"a": 7, "b": 3.5, "mode": "Alpha", "name": "hello",
		"inner": { "factor": 9.5 }, "items": [1, 2, 3]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	out, rep, err := DecodeTable(u, "Cfg", wire)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Unknown != 0 || rep.KindMismatch != 0 || rep.Clamped != 0 || rep.Malformed {
		t.Fatalf("same-schema decode not silent: %+v", rep)
	}
	if out["a"].(*big.Int).Int64() != 7 || out["b"].(float64) != 3.5 || out["name"].(string) != "hello" {
		t.Fatalf("round trip lost values: %v", out)
	}
	if out["mode"].(*big.Int).Int64() != 1 { // Alpha
		t.Fatalf("enum: %v", out["mode"])
	}
	if out["inner"].(map[string]any)["factor"].(float64) != 9.5 {
		t.Fatalf("nested: %v", out["inner"])
	}
	if len(out["items"].([]any)) != 3 {
		t.Fatalf("array: %v", out["items"])
	}

	// all-default instance: everything elides, decode restores every default
	wire2, err := enc.EncodeTable("Cfg", obj(t, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(wire2) != 2 { // bare terminator
		t.Fatalf("all-default instance should be 2 bytes, got %d", len(wire2))
	}
	out2, _, _ := DecodeTable(u, "Cfg", wire2)
	if out2["a"].(*big.Int).Int64() != 5 || out2["b"].(float64) != 1.5 {
		t.Fatalf("defaults not restored: %v", out2)
	}
	if out2["mode"].(*big.Int).Int64() != 2 { // Beta
		t.Fatalf("enum default: %v", out2["mode"])
	}
	if out2["inner"].(map[string]any)["factor"].(float64) != 2.5 {
		t.Fatalf("nested default: %v", out2["inner"])
	}
}

// TestTableEvolution is the versioning requirement, proven both directions:
// old reader + new data, new reader + old data — nothing crashes, unknown
// fields skip, absent fields default, a changed type skips instead of
// misdecoding, and every event is counted.
func TestTableEvolution(t *testing.T) {
	v1 := unitFromSource(t, schemaV1)
	v2 := unitFromSource(t, schemaV2)

	// NEW data (v2: a is float now, c added, b gone; inner gained gain)
	encV2 := &Encoder{Unit: v2}
	newWire, err := encV2.EncodeTable("Cfg", obj(t, `{
		"a": 7.5, "c": false, "mode": "Alpha", "name": "fresh",
		"inner": { "factor": 9.5, "gain": 4.0 }, "items": [10]
	}`))
	if err != nil {
		t.Fatal(err)
	}

	// OLD reader (v1) on NEW data: the live-config-push case
	out, rep, err := DecodeTable(v1, "Cfg", newWire)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Malformed {
		t.Fatal("old reader marked new data malformed")
	}
	if rep.Unknown != 2 { // c (top level) + gain (nested)
		t.Fatalf("unknown count: want 2 (c, inner.gain), got %d", rep.Unknown)
	}
	if rep.KindMismatch != 1 { // a: f32 wire vs v1's i32 expectation
		t.Fatalf("kind mismatch count: want 1 (a), got %d", rep.KindMismatch)
	}
	if out["a"].(*big.Int).Int64() != 5 { // a skipped -> v1 default, never misdecoded
		t.Fatalf("a should fall back to the v1 default 5, got %v", out["a"])
	}
	if out["b"].(float64) != 1.5 { // removed in v2 -> absent -> v1 default
		t.Fatalf("b should default to 1.5, got %v", out["b"])
	}
	if out["name"].(string) != "fresh" || out["mode"].(*big.Int).Int64() != 1 {
		t.Fatalf("surviving fields lost: %v", out)
	}
	if out["inner"].(map[string]any)["factor"].(float64) != 9.5 {
		t.Fatalf("nested surviving field lost: %v", out["inner"])
	}

	// OLD data (v1), NEW reader (v2): the new-server-old-file case
	encV1 := &Encoder{Unit: v1}
	oldWire, err := encV1.EncodeTable("Cfg", obj(t, `{
		"a": 9, "b": 8.5, "name": "aged", "inner": { "factor": 1.25 }
	}`))
	if err != nil {
		t.Fatal(err)
	}
	out2, rep2, err := DecodeTable(v2, "Cfg", oldWire)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.Malformed {
		t.Fatal("new reader marked old data malformed")
	}
	if rep2.Unknown != 1 { // b, removed in v2
		t.Fatalf("unknown count: want 1 (b), got %d", rep2.Unknown)
	}
	if rep2.KindMismatch != 1 { // a: i32 wire vs v2's f32 expectation
		t.Fatalf("kind mismatch: want 1 (a), got %d", rep2.KindMismatch)
	}
	if out2["a"].(float64) != 5.0 { // v2 default
		t.Fatalf("a should fall back to the v2 default 5.0, got %v", out2["a"])
	}
	if out2["c"].(bool) != true { // added in v2, absent in old data -> default
		t.Fatalf("c should default true, got %v", out2["c"])
	}
	if out2["name"].(string) != "aged" || out2["inner"].(map[string]any)["factor"].(float64) != 1.25 {
		t.Fatalf("surviving fields lost: %v", out2)
	}
	if out2["inner"].(map[string]any)["gain"].(float64) != 1.0 { // nested added field defaults
		t.Fatalf("inner.gain should default 1.0, got %v", out2["inner"])
	}
}

// TestTableClamping: hostile or stale numeric data clamps and counts instead
// of crashing or rejecting — the as-safe-and-permissive-as-possible rule.
func TestTableClamping(t *testing.T) {
	// widened schema writes 2000; the narrow reader's a is [0, 1000]
	wide := unitFromSource(t, strings.Replace(schemaV1, "max = 1000", "max = 4000", 1))
	narrow := unitFromSource(t, schemaV1)
	enc := &Encoder{Unit: wide}
	wire, err := enc.EncodeTable("Cfg", obj(t, `{ "a": 2000 }`))
	if err != nil {
		t.Fatal(err)
	}
	out, rep, err := DecodeTable(narrow, "Cfg", wire)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Clamped != 1 || out["a"].(*big.Int).Int64() != 1000 {
		t.Fatalf("want clamp to 1000 counted once, got %v (%+v)", out["a"], rep)
	}
}

// TestTableBytes: bytes(N) travels as an array of u8 — the writer says so and
// the reader must expect exactly that (a reader expecting kArray elements
// would skip every bytes field as a kind mismatch).
func TestTableBytes(t *testing.T) {
	u := unitFromSource(t, `package evo

type Blob {
    data bytes(8)
    tag  int32
}
`)
	enc := &Encoder{Unit: u}
	wire, err := enc.EncodeTable("Blob", obj(t, `{ "data": [1, 2, 250], "tag": 7 }`))
	if err != nil {
		t.Fatal(err)
	}
	out, rep, err := DecodeTable(u, "Blob", wire)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Unknown != 0 || rep.KindMismatch != 0 || rep.Clamped != 0 || rep.Malformed {
		t.Fatalf("bytes round trip not silent: %+v", rep)
	}
	data := out["data"].([]any)
	if len(data) != 3 || data[0].(*big.Int).Int64() != 1 || data[2].(*big.Int).Int64() != 250 {
		t.Fatalf("bytes lost: %v", out["data"])
	}
	if out["tag"].(*big.Int).Int64() != 7 {
		t.Fatalf("tag lost: %v", out["tag"])
	}
}

// TestTableBranchGuards: fields on an untaken branch stay off the wire —
// TLV's native optionality carries the branch, and the reader's prefilled
// defaults stand in for the untaken side (notes/table-wire.md).
func TestTableBranchGuards(t *testing.T) {
	u := unitFromSource(t, `package evo

type Body {
    at_rest bool
    if !at_rest {
        vx float32
        vy float32
    }
}
`)
	enc := &Encoder{Unit: u}

	// guard says at rest: velocities must not encode even though the JSON
	// carries one (dense-encoder precedent: untaken side silently ignored)
	atRest, err := enc.EncodeTable("Body", obj(t, `{ "at_rest": true, "vx": 5.0 }`))
	if err != nil {
		t.Fatal(err)
	}
	if len(atRest) != 6 { // at_rest bool field (4) + terminator (2)
		t.Fatalf("at-rest wire should be 6 bytes (guard only), got %d", len(atRest))
	}
	out, rep, err := DecodeTable(u, "Body", atRest)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Unknown != 0 || rep.Malformed {
		t.Fatalf("decode not silent: %+v", rep)
	}
	if out["at_rest"].(bool) != true || out["vx"].(float64) != 0 {
		t.Fatalf("untaken side should decode to defaults: %v", out)
	}

	// guard says moving: the velocity rides
	moving, err := enc.EncodeTable("Body", obj(t, `{ "at_rest": false, "vx": 5.0 }`))
	if err != nil {
		t.Fatal(err)
	}
	out2, _, err := DecodeTable(u, "Body", moving)
	if err != nil {
		t.Fatal(err)
	}
	if out2["at_rest"].(bool) != false || out2["vx"].(float64) != 5.0 {
		t.Fatalf("taken side lost: %v", out2)
	}
}

func TestFieldIdCollisionCheck(t *testing.T) {
	u := unitFromSource(t, schemaV1)
	if err := CheckTableIds(u); err != nil {
		t.Fatalf("corpus should have no collisions: %v", err)
	}
}
