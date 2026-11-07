package luautable_test

// TestLuauTimestampBinaryForms pins the Luau table surface for the `timestamp`
// scalar kind across the binary forms (docs/SPEC-TABLES.md §2.11):
//   - On the variable form, a timestamp field writes kind 36 (L = 1, 8 bytes
//     two's-complement little-endian nanoseconds), elides at 0i, and refuses a
//     wire whose kind byte is int64 (kind 5) as kind_mismatch.
//   - On the fixed form, a timestamp slot writes 8 little-endian bytes, matches
//     Go's fixed-form wire byte for byte, and reads back into a Luau integer
//     through both fixed.load and ReadStrict.
// Every reference wire is computed by schema-go's own generated Go surface over
// the same unit, never typed by hand.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const luauTimestampSchema = `package probe

fixed table Session
{
    started   timestamp
    ended     timestamp
    min_mark  timestamp
    max_mark  timestamp
}

table Event
{
    id        uint32
    opened_at timestamp
    closed_at timestamp
    min_at    timestamp
    max_at    timestamp
}
`

const goTimestampOracleTest = `package probe

import (
	"encoding/hex"
	"fmt"
	"testing"
)

func TestOracle(t *testing.T) {
	// 1. Variable form: default elision + full positive/negative/extremes
	var b0 EventBuilder
	if !b0.Init() { t.Fatal("b0 init") }
	defer b0.Shutdown()
	b0.GetRoot().ID = 7
	if !b0.Lock() { t.Fatal("b0 lock") }
	v0 := b0.AsConst()
	w0 := make([]byte, EventMeasure(v0))
	if EventSave(v0, w0) != int64(len(w0)) { t.Fatal("b0 save") }
	fmt.Printf("var_elided %s\n", hex.EncodeToString(w0))

	var b1 EventBuilder
	if !b1.Init() { t.Fatal("b1 init") }
	defer b1.Shutdown()
	r1 := b1.GetRoot()
	r1.ID = 42
	r1.OpenedAt = 1729296000123456789
	r1.ClosedAt = -86400000000000
	r1.MinAt = -9223372036854775808
	r1.MaxAt = 9223372036854775807
	if !b1.Lock() { t.Fatal("b1 lock") }
	v1 := b1.AsConst()
	w1 := make([]byte, EventMeasure(v1))
	if EventSave(v1, w1) != int64(len(w1)) { t.Fatal("b1 save") }
	fmt.Printf("var_full %s\n", hex.EncodeToString(w1))

	// 2. Fixed form: two Session records
	sessions := []Session{
		{
			Started: 1729296000123456789,
			Ended:   -86400000000000,
			MinMark: -9223372036854775808,
			MaxMark: 9223372036854775807,
		},
		{
			Started: 0,
			Ended:   123456789,
			MinMark: -1,
			MaxMark: 0,
		},
	}
	fw := make([]byte, SessionFixedMeasure(int64(len(sessions))))
	if SessionFixedSave(sessions, fw) != int64(len(fw)) { t.Fatal("fixed save") }
	fmt.Printf("fixed_full %s\n", hex.EncodeToString(fw))
}
`

const luauTimestampProbe = `--!strict
local T = require("./ProbeTable")
local S = require("./schema_save")
local L = require("./schema_load")
local R = require("./schema_refusal")
local F = require("./schema_fixed")

local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end

-- 1. Variable form: elided default timestamp (0i)
local e0 = T.EventDescriptor.new()
e0.Id = 7
local w0 = S.save(T.EventDescriptor, e0)
local m0 = S.measure(T.EventDescriptor, e0)
assert(w0 ~= nil and m0 == buffer.len(w0), "var_elided measure")
print("var_elided " .. hex(w0))

-- Variable form: populated positive, negative, and int64 extremes
local e1 = T.EventDescriptor.new()
e1.Id = 42
e1.OpenedAt = 1729296000123456789i
e1.ClosedAt = -86400000000000i
e1.MinAt = 0x8000000000000000i
e1.MaxAt = 9223372036854775807i
local w1 = S.save(T.EventDescriptor, e1)
local m1 = S.measure(T.EventDescriptor, e1)
assert(w1 ~= nil and m1 == buffer.len(w1), "var_full measure")
print("var_full " .. hex(w1))

local rep = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local got1 = L.load("Event", w1, rep)
assert(rep.refused == 0 and not rep.malformed and rep.kindMismatch == 0 and rep.clamped == 0, "var_full load clean")
assert(got1.Id == 42, "var_full Id")
assert(got1.OpenedAt == 1729296000123456789i, "var_full OpenedAt")
assert(got1.ClosedAt == -86400000000000i, "var_full ClosedAt")
assert(got1.MinAt == 0x8000000000000000i and got1.MaxAt == 9223372036854775807i, "var_full extremes")

local srep1: any = {}
local strict1 = R.ReadStrict(T, "Event", w1, srep1)
assert(strict1 ~= nil and srep1.reason == nil, "var_full ReadStrict")

-- Kind 36 vs kind 5 (int64): mutating OpenedAt's kind byte (0x24 -> 0x05) is kind_mismatch
local badKind = buffer.create(buffer.len(w1))
buffer.copy(badKind, 0, w1, 0, buffer.len(w1))
local patched = false
for i = 0, buffer.len(badKind) - 1 do
	if buffer.readu8(badKind, i) == 36 then
		buffer.writeu8(badKind, i, 5)
		patched = true
		break
	end
end
assert(patched, "found kind 36 byte")
local badRep: any = {}
local strictBad = R.ReadStrict(T, "Event", badKind, badRep)
assert(strictBad == nil and badRep.reason == "kind_mismatch" and badRep.field == "opened_at", "int64 kind byte refused as kind_mismatch on opened_at")
print("var_kind_mismatch " .. tostring(badRep.reason) .. " " .. tostring(badRep.field))

-- 2. Fixed form: two Session records
local s1 = T.SessionDescriptor.new()
s1.Started = 1729296000123456789i
s1.Ended = -86400000000000i
s1.MinMark = 0x8000000000000000i
s1.MaxMark = 9223372036854775807i

local s2 = T.SessionDescriptor.new()
s2.Started = 0i
s2.Ended = 123456789i
s2.MinMark = -1i
s2.MaxMark = 0i

local sessionFixed = { hash = T.SessionFixedHash, recordBytes = T.SessionFixedRecordBytes, layout = T.SessionFixedLayout }
local fw = F.save(T.SessionDescriptor, sessionFixed, { s1, s2 })
print("fixed_full " .. hex(fw))

local fback = F.load(T.SessionDescriptor, sessionFixed, fw)
assert(#fback == 2, "fixed count")
assert(fback[1].Started == 1729296000123456789i, "fixed[1].Started")
assert(fback[1].Ended == -86400000000000i, "fixed[1].Ended")
assert(fback[1].MinMark == 0x8000000000000000i and fback[1].MaxMark == 9223372036854775807i, "fixed[1].Marks")
assert(fback[2].Started == 0i and fback[2].Ended == 123456789i and fback[2].MinMark == -1i and fback[2].MaxMark == 0i, "fixed[2]")

local fw1 = F.save(T.SessionDescriptor, sessionFixed, { s1 })
local frep: any = {}
local fstrict = R.ReadStrict(T, "Session", fw1, frep)
assert(fstrict ~= nil and frep.reason == nil, "fixed ReadStrict")
assert(fstrict.Started == 1729296000123456789i and fstrict.Ended == -86400000000000i, "fixed ReadStrict values")
print("fixed_strict_ok 2")
`

func goTimestampOracle(t *testing.T) map[string][]byte {
	t.Helper()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(schemaPath, []byte(luauTimestampSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	mod := "module probe\n\ngo 1.26\n"
	for _, data := range files {
		if strings.Contains(string(data), `"github.com/mas-bandwidth/serialize.go"`) {
			runtime := slowtest.SerializeGo(t, root)
			mod += fmt.Sprintf("\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime)
			break
		}
	}
	files["go.mod"] = []byte(mod)
	files["oracle_test.go"] = []byte(goTimestampOracleTest)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-v", "-count=1", "-timeout", "600s", "-run", "^TestOracle$", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	wants := map[string][]byte{}
	for line := range strings.SplitSeq(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && (f[0] == "var_elided" || f[0] == "var_full" || f[0] == "fixed_full") {
			raw, err := hex.DecodeString(f[1])
			if err != nil {
				t.Fatalf("oracle %s hex: %v", f[0], err)
			}
			wants[f[0]] = raw
		}
	}
	if len(wants) != 3 {
		t.Fatalf("expected 3 oracle wires, got %d:\n%s", len(wants), out)
	}
	return wants
}

func TestLuauTimestampBinaryForms(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	t.Parallel()
	wants := goTimestampOracle(t)

	g := generateFiles(t, map[string]string{"Probe.schema": luauTimestampSchema})
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(luauTimestampProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", nil))

	gots := map[string][]byte{}
	var sawKindMismatch, sawFixedStrictOk bool
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "var_elided", "var_full", "fixed_full":
			raw, err := hex.DecodeString(f[1])
			if err != nil {
				t.Fatalf("Luau %s hex: %v", f[0], err)
			}
			gots[f[0]] = raw
		case "var_kind_mismatch":
			if len(f) == 3 && f[1] == "kind_mismatch" && f[2] == "opened_at" {
				sawKindMismatch = true
			}
		case "fixed_strict_ok":
			if f[1] == "2" {
				sawFixedStrictOk = true
			}
		}
	}
	for k, want := range wants {
		got, ok := gots[k]
		if !ok {
			t.Fatalf("missing Luau output for %s:\n%s", k, out)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s wire mismatch:\n got (%d): %x\nwant (%d): %x", k, len(got), got, len(want), want)
		}
	}
	if !sawKindMismatch {
		t.Errorf("did not observe var_kind_mismatch kind_mismatch opened_at in output:\n%s", out)
	}
	if !sawFixedStrictOk {
		t.Errorf("did not observe fixed_strict_ok 2 in output:\n%s", out)
	}
}
