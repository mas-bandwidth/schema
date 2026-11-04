package luautable_test

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

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// paddedCookTexts are tables/block PaddedFrame instances the load reads and
// the cook writes: the empty frame, and rows carrying a fixed [4]uint16, a
// string(15), an optional and the frame's bytes(12). The enum-keyed
// [Team]uint8 rides paddedCookBuilt instead: the load does not yet read a
// populated byte-keyed array (schema_load.luau read_keyed indexes the buffer
// as a list), so that value is built in Luau rather than loaded.
var paddedCookTexts = []string{
	`{}`,
	`{"marker":7,"stamp":1234567890123,"blob":"AAECAwQFBgcICQoL","rows":[{"tag":1,"value":2.5,"flag":true,"id":99,"label":"first","slots":[1,2,3,4],"counter":-3},{"tag":2,"slots":[65535,0,0,9]}]}`,
}

// paddedCookBuilt is the frame paddedCookProbe builds field by field, its
// byte-keyed arrays populated in the [N]uint8 storage the type mapping names.
const paddedCookBuilt = `{"marker":7,"stamp":1234567890123,"blob":"AAECAwQFBgcICQoL","rows":[{"tag":1,"value":2.5,"flag":true,"id":99,"label":"first","slots":[1,2,3,4],"teams":{"Red":5,"Blue":6,"Green":7,"Gold":8},"counter":-3},{"tag":2,"teams":{"Blue":255},"slots":[65535,0,0,9]}]}`

// paddedCookProbe cooks the frame a constructor builds, the frame
// paddedCookBuilt spells, and the frame the load reads from the wire, and
// prints each file as hex.
const paddedCookProbe = `--!strict
local T = require("./PaddedTable")
local St = require("./schema_tables")
local L = require("./schema_load")
local W = require("./schema_cook")
local inputs = require("./inputs")
local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local function say(c: buffer?, why: string?)
	print(if c == nil then "REFUSED " .. tostring(why) else hex(c))
end
W.BuildVersion = T.TableMessageBuildVersion
local d = St.lookup("PaddedFrame")
say(W.Cook(d, T.NewPaddedFrame(), "little"))
local f = T.NewPaddedFrame()
f.Marker = 7
f.Stamp = 1234567890123i
for i = 0, 11 do buffer.writeu8(f.Blob, i, i) end
f.BlobLength = 12
f.RowsCount = 2
local r1 = f.Rows[1]
r1.Tag, r1.Value, r1.Flag, r1.Id = 1, 2.5, true, 99
buffer.writestring(r1.Label, 0, "first")
r1.LabelLength = 5
r1.Slots = { 1, 2, 3, 4 }
for i, n in { 5, 6, 7, 8 } do buffer.writeu8(r1.Teams, i - 1, n) end
r1.Counter, r1.CounterPresent = -3, true
local r2 = f.Rows[2]
r2.Tag = 2
r2.Slots = { 65535, 0, 0, 9 }
buffer.writeu8(r2.Teams, 1, 255)
say(W.Cook(d, f, "little"))
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = L.load("PaddedFrame", inputs.wire, report)
if v == nil then print("LOAD_REFUSED") else
	local n = W.CookMeasure(d, v)
	local c, whyC = W.Cook(d, v, "little")
	if c == nil then print("REFUSED " .. tostring(whyC)) else
		print(string.format("%d %d", n, buffer.len(c)))
		print(hex(c))
	end
end
`

// paddedCookGate cooks the constructor's frame, paddedCookBuilt and each
// paddedCookTexts row through the Luau writer and answers the first
// disagreement with schema-go's internal/tablecook, or "". mutate, when set,
// edits the emitted files first.
func paddedCookGate(t *testing.T, mutate func(t *testing.T, files map[string][]byte)) string {
	t.Helper()
	u, files := buildVersionFiles(t)
	files = maps.Clone(files)
	if mutate != nil {
		mutate(t, files)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(paddedCookProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	cook := func(what, text string) ([]byte, []byte) {
		inst := m.New(m.Lookup("PaddedFrame"))
		var report tabletext.Report
		if !m.Read(inst, []byte(text), &report) {
			t.Fatalf("%s: schema-go's text read: %+v", what, report)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: wire: %v", what, err)
		}
		want, err := tablecook.Cook(m, inst, tablecook.Options{})
		if err != nil {
			t.Fatalf("%s: schema-go's cook: %v", what, err)
		}
		return wire, want
	}
	_, fresh := cook("the empty frame", `{}`)
	_, built := cook("the built frame", paddedCookBuilt)
	for i, text := range paddedCookTexts {
		wire, want := cook(fmt.Sprintf("row %d", i), text)
		out, runErr := runLuauErr(t, dir, "main.luau", map[string][]byte{"wire": wire})
		if runErr != nil {
			return fmt.Sprintf("row %d: the probe failed: %v\n%s", i, runErr, out)
		}
		lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
		if len(lines) != 4 {
			return fmt.Sprintf("row %d: the probe printed %q", i, out)
		}
		if lines[0] != hex.EncodeToString(fresh) {
			return "the constructor's frame cooks to " + lines[0] + ", the tool's is " + hex.EncodeToString(fresh)
		}
		if lines[1] != hex.EncodeToString(built) {
			return "the built frame cooks to " + lines[1] + ", the tool's is " + hex.EncodeToString(built)
		}
		if lines[2] != fmt.Sprintf("%d %d", len(want), len(want)) {
			return fmt.Sprintf("row %d: measure and length %s, the tool's file is %d bytes", i, lines[2], len(want))
		}
		if lines[3] != hex.EncodeToString(want) {
			return fmt.Sprintf("row %d: the cook differs from the tool's:\n  luau %s\n  go   %s", i, lines[3], hex.EncodeToString(want))
		}
	}
	return ""
}

// runLuauErr is luaurun.Run that answers a failed script as an error, so a
// control can require the failure rather than stop on it.
func runLuauErr(t *testing.T, dir, main string, inputs map[string][]byte) ([]byte, error) {
	t.Helper()
	luau, why := luaurun.Interpreter()
	if luau == "" {
		t.Fatalf("luaurun: the Luau interpreter does not resolve: %s", why)
	}
	if err := os.WriteFile(filepath.Join(dir, "inputs.luau"), luaurun.InputsModule(inputs), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(luau, main)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("%v: %s", err, stderr.Bytes())
	}
	return stdout.Bytes(), nil
}

// TestLuauCookWritesInlineByteArrays holds the cook writer to the tool's
// bytes (docs/SPEC-TABLES.md §7.6) over the block corpus's inline storage,
// where a [N]uint8 and an [E]uint8 are a buffer, 0-based (docs/PORT-LUAU.md,
// THE TYPE MAPPING): the record a constructor builds, a record built with its
// byte-keyed arrays populated, and the records the load reads all cook to
// schema-go's internal/tablecook file, byte for byte.
func TestLuauCookWritesInlineByteArrays(t *testing.T) {
	t.Parallel()
	if why := paddedCookGate(t, nil); why != "" {
		t.Fatal(why)
	}
}

// TestLuauCookWritesInlineByteArraysNegativeControl reads a byte array's
// slots as a list again, the walk every other array takes, and requires the
// gate red.
func TestLuauCookWritesInlineByteArraysNegativeControl(t *testing.T) {
	t.Parallel()
	why := paddedCookGate(t, func(t *testing.T, files map[string][]byte) {
		const from = "\tlocal bytes = typeof(x) == \"buffer\"\n"
		src := string(files["schema_cook.luau"])
		if strings.Count(src, from) != 1 {
			t.Fatalf("schema_cook.luau carries %q %d times, want 1", from, strings.Count(src, from))
		}
		files["schema_cook.luau"] = []byte(strings.Replace(src, from, "\tlocal bytes = false\n", 1))
	})
	if why == "" {
		t.Fatal("NEGATIVE CONTROL FAILED: the cook still matched the tool with byte arrays read as lists")
	}
	t.Logf("red as required: %s", firstLine(why))
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
