package luautable_test

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// cookWriteStrictProbe exercises the strict read -> cook write -> cook open
// contract on PaddedFrame (tables/block), whose rows carry the 0-based buffer
// mapping for [Team]uint8 alongside [4]uint16, string(15), ?int32 and the
// frame's bytes(12):
//  1. L.load (the strict read) reads the form-3 fixed wire into a whole value
//     with Teams as a 0-based buffer, and W.Cook writes both byte orders byte
//     for byte with schema-go's internal/tablecook.
//  2. R.open reads the cooked file back, preserves Teams as a 0-based buffer,
//     and re-cooks to the same bytes.
//  3. L.ReadStrictVariable reads the form-1 variable wire of the same frame
//     (including read_keyed over [Team]uint8) into a 0-based buffer, S.save on
//     a variable-class view writes the form-1 wire (write_keyed over
//     [Team]uint8), S.save on the fixed descriptor writes the form-3 wire, and
//     W.Cook writes the same cook bytes; L.load on the fixed root refuses the
//     form-1 wire as previous_form.
//  4. A truncated wire is refused by L.load before cooking, and a non-finite
//     float in a row is refused by W.Cook with PaddedRow.value.
const cookWriteStrictProbe = `--!strict
local T = require("./PaddedTable")
local St = require("./schema_tables")
local L = require("./schema_load")
local S = require("./schema_save")
local W = require("./schema_cook")
local R = require("./schema_cookread")
local inputs = require("./inputs")

local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end

W.BuildVersion = T.TableMessageBuildVersion
local d = St.lookup("PaddedFrame")

-- 1. Strict fixed-form load -> cook write (little and big) -> cook open -> re-cook
local rep3: any = {}
local v3 = L.load("PaddedFrame", inputs.form3, rep3)
if v3 == nil or rep3.reason ~= nil or rep3.malformed then
	print("form3_load_refused:" .. tostring(rep3.reason))
	return
end
if typeof(v3.Rows[1].Teams) ~= "buffer" then
	print("form3_teams_type:" .. typeof(v3.Rows[1].Teams))
	return
end
local m3 = W.CookMeasure(d, v3)
local c3le, why3le = W.Cook(d, v3, "little")
local c3be, why3be = W.Cook(d, v3, "big")
if c3le == nil or c3be == nil then
	print("form3_cook_refused:" .. tostring(why3le) .. ":" .. tostring(why3be))
	return
end
print(string.format("form3 %d %d %s %s", m3, buffer.len(c3le), hex(c3le), hex(c3be)))

local opened, whyOpen = R.open("PaddedFrame", c3le, T.TableMessageBuildVersion)
if opened == nil then
	print("open_refused:" .. tostring(whyOpen))
	return
end
if typeof(opened.Rows[1].Teams) ~= "buffer" then
	print("open_teams_type:" .. typeof(opened.Rows[1].Teams))
	return
end
local recook, whyRecook = W.Cook(d, opened, "little")
if recook == nil then
	print("recook_refused:" .. tostring(whyRecook))
	return
end
print("open " .. hex(recook))

-- 2. Strict variable-form read & save over [Team]uint8 -> cook write
local repPrev: any = {}
local vPrev = L.load("PaddedFrame", inputs.form1, repPrev)
if vPrev ~= nil or repPrev.reason ~= "previous_form" then
	print("prev_not_refused:" .. tostring(vPrev ~= nil) .. ":" .. tostring(repPrev.reason))
	return
end
local rep1: any = {}
local v1 = L.ReadStrictVariable("PaddedFrame", inputs.form1, rep1)
if v1 == nil or rep1.reason ~= nil or rep1.malformed then
	print("form1_load_refused:" .. tostring(rep1.reason))
	return
end
if typeof(v1.Rows[1].Teams) ~= "buffer" then
	print("form1_teams_type:" .. typeof(v1.Rows[1].Teams))
	return
end
local dVar = table.clone(d)
dVar.fixed = false
local saved1, whySave1 = S.save(dVar, v1)
if saved1 == nil then
	print("form1_save_refused:" .. tostring(whySave1 and whySave1.name))
	return
end
local saved3, whySave3 = S.save(d, v1)
if saved3 == nil then
	print("form3_save_refused:" .. tostring(whySave3 and whySave3.name))
	return
end
local c1le, why1le = W.Cook(d, v1, "little")
if c1le == nil then
	print("form1_cook_refused:" .. tostring(why1le))
	return
end
print("form1 " .. hex(saved1) .. " " .. hex(saved3) .. " " .. hex(c1le))

-- 3. Strict refusals: truncated wire on L.load, non-finite float on W.Cook
local short = buffer.create(buffer.len(inputs.form3) - 1)
buffer.copy(short, 0, inputs.form3, 0, buffer.len(short))
local repShort: any = {}
local vShort = L.load("PaddedFrame", short, repShort)
v3.Rows[1].Value = 0 / 0
local cNan, whyNan = W.Cook(d, v3, "little")
print(string.format("refuse %s:%s %s:%s", tostring(vShort == nil), tostring(repShort.reason), tostring(cNan == nil), tostring(whyNan)))
`

func cookWriteStrictGate(t *testing.T, mutate func(t *testing.T, files map[string][]byte)) string {
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
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(cookWriteStrictProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	tbl := m.Lookup("PaddedFrame")
	inst := m.New(tbl)
	var report tabletext.Report
	if !m.Read(inst, []byte(paddedCookBuilt), &report) {
		t.Fatalf("schema-go text read: %+v", report)
	}
	form3, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("form3 wire: %v", err)
	}
	tbl.FixedDeclared = false
	form1, err := tablewire.Encode(m, inst)
	tbl.FixedDeclared = true
	if err != nil {
		t.Fatalf("form1 wire: %v", err)
	}
	wantLE, err := tablecook.Cook(m, inst, tablecook.Options{})
	if err != nil {
		t.Fatalf("cook little: %v", err)
	}
	wantBE, err := tablecook.Cook(m, inst, tablecook.Options{Big: true})
	if err != nil {
		t.Fatalf("cook big: %v", err)
	}
	out, runErr := runLuauErr(t, dir, "main.luau", map[string][]byte{"form3": form3, "form1": form1})
	if runErr != nil {
		return fmt.Sprintf("probe failed: %v\n%s", runErr, out)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 4 {
		return fmt.Sprintf("probe printed %d lines: %q", len(lines), string(out))
	}
	wantLine0 := fmt.Sprintf("form3 %d %d %s %s", len(wantLE), len(wantLE), hex.EncodeToString(wantLE), hex.EncodeToString(wantBE))
	if lines[0] != wantLine0 {
		return fmt.Sprintf("form3 mismatch: got %q", lines[0])
	}
	wantLine1 := "open " + hex.EncodeToString(wantLE)
	if lines[1] != wantLine1 {
		return fmt.Sprintf("open mismatch: got %q", lines[1])
	}
	wantLine2 := "form1 " + hex.EncodeToString(form1) + " " + hex.EncodeToString(form3) + " " + hex.EncodeToString(wantLE)
	if lines[2] != wantLine2 {
		return fmt.Sprintf("form1 mismatch: got %q", lines[2])
	}
	const wantLine3 = "refuse true:truncated true:non_finite_float: PaddedRow.value"
	if lines[3] != wantLine3 {
		return fmt.Sprintf("refuse mismatch: got %q, want %q", lines[3], wantLine3)
	}
	return ""
}

// TestLuauCookWriteStrict holds the strict read (L.load on form 3,
// L.ReadStrictVariable on form 1) and the cook writer and reader to the
// 0-based buffer mapping for [N]uint8, [..N]uint8 and [E]uint8 and to
// schema-go's internal/tablecook bytes across both byte orders.
func TestLuauCookWriteStrict(t *testing.T) {
	slowtest.Gate(t, "luau")
	t.Parallel()
	if why := cookWriteStrictGate(t, nil); why != "" {
		t.Fatal(why)
	}
}

// TestLuauCookWriteStrictNegativeControl plants the 1-based list fault into
// each runtime module that touches a byte array along the strict load -> save
// -> cook -> open path and requires the gate to go red on each.
func TestLuauCookWriteStrictNegativeControl(t *testing.T) {
	slowtest.Gate(t, "luau")
	t.Parallel()
	for _, ctl := range []struct {
		name string
		file string
		from string
		to   string
	}{
		{
			name: "fixed-byte-array-as-list",
			file: "schema_fixed.luau",
			from: "\tif typeof(def) == \"buffer\" then\n",
			to:   "\tif false then\n",
		},
		{
			name: "load-keyed-byte-array-as-list",
			file: "schema_load.luau",
			from: "\t\t\tif at > 0 and dst ~= nil and x ~= nil then put(dst, at, x) end\n",
			to:   "\t\t\tif at > 0 and dst ~= nil and x ~= nil then dst[at] = x end\n",
		},
		{
			name: "save-keyed-byte-array-as-list",
			file: "schema_save.luau",
			from: "\tfor slot = 1, fd.bound do\n\t\tlocal keyvs = fd.keyvs\n\t\tlocal x = elem_at(val, slot)\n",
			to:   "\tfor slot = 1, fd.bound do\n\t\tlocal keyvs = fd.keyvs\n\t\tlocal x = val[slot]\n",
		},
		{
			name: "cook-byte-array-as-list",
			file: "schema_cook.luau",
			from: "\tlocal bytes = typeof(x) == \"buffer\"\n",
			to:   "\tlocal bytes = false\n",
		},
		{
			name: "cookread-byte-array-as-list",
			file: "schema_cookread.luau",
			from: "\t\t\tif typeof(dst) == \"buffer\" then\n",
			to:   "\t\t\tif false then\n",
		},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			why := cookWriteStrictGate(t, func(t *testing.T, files map[string][]byte) {
				src := string(files[ctl.file])
				if strings.Count(src, ctl.from) != 1 {
					t.Fatalf("%s carries %q %d times, want 1", ctl.file, ctl.from, strings.Count(src, ctl.from))
				}
				files[ctl.file] = []byte(strings.Replace(src, ctl.from, ctl.to, 1))
			})
			if why == "" {
				t.Fatalf("NEGATIVE CONTROL FAILED (%s): cookWriteStrictGate stayed green with fault planted in %s", ctl.name, ctl.file)
			}
			t.Logf("red as required (%s): %s", ctl.name, firstLine(why))
		})
	}
}
