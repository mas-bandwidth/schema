package luatable

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// textProbe is the Lua main for one instance row: it requires the table
// module and the load, save and base64 text runtimes, loads the wire, saves it
// as base64 text, reads that text back and saves it again, then offers three
// damaged bodies and prints the text and the verdicts.
func textProbe(module, root string) string {
	return `local T = require("` + module + `")
local load = require("load")
local save = require("save")
local text = require("text")
local inputs = require("inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.LoadTolerant(T, "` + root + `", inputs.wire, report)
print(text.SaveText(T, "` + root + `", v))
local back = text.LoadText(T, "` + root + `", inputs.expect)
local saved = save.Save(T, "` + root + `", back)
local function refuse(s)
  local ok, err = pcall(function() return text.LoadText(T, "` + root + `", s) end)
  if ok then return "taken" end
  if tostring(err):find("base64_damaged", 1, true) then return "damaged" end
  return "other:" .. tostring(err)
end
print((saved == inputs.wire and "same" or "diff") .. ":" ..
  refuse(inputs.outside) .. ":" .. refuse(inputs.pad) .. ":" .. refuse(inputs.cut))
`
}

// textSaveProbe is the negative control's Lua main: it only writes the text,
// so a damaged encoding is compared rather than read back.
func textSaveProbe(module, root string) string {
	return `local T = require("` + module + `")
local load = require("load")
local text = require("text")
local inputs = require("inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.LoadTolerant(T, "` + root + `", inputs.wire, report)
print(text.SaveText(T, "` + root + `", v))
`
}

// damagedTexts returns the three bodies the carrier must refuse: a byte
// outside the standard alphabet, a wrong pad, and a cut length.
func damagedTexts(good string) (outside, pad, cut []byte) {
	outside = []byte(good)
	if len(outside) > 0 {
		outside[0] = '!'
	}
	pad = []byte(good + "=")
	cut = []byte(good)
	if len(cut) > 0 {
		cut = cut[:len(cut)-1]
	}
	return outside, pad, cut
}

// textInstance generates one instance row's unit (with the emitted text
// runtime optionally patched), runs main and returns its trimmed output.
func textInstance(t *testing.T, repo string, paths []string, row manifestRow, good, main string, patch func([]byte) []byte) string {
	t.Helper()
	u := luarun.LoadUnit(t, paths...)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("unit %s: %v", row.unit, err)
	}
	if patch != nil {
		src, ok := files["text.lua"]
		if !ok {
			t.Fatal("the emitted unit has no text.lua")
		}
		files["text.lua"] = patch(src)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	wireData, err := os.ReadFile(filepath.Join(repo, row.wire))
	if err != nil {
		t.Fatalf("case %s: read wire %s: %v", row.caseName, row.wire, err)
	}
	outside, pad, cut := damagedTexts(good)
	return strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{
		"wire": wireData, "expect": []byte(good), "outside": outside, "pad": pad, "cut": cut,
	})), "\n")
}

// TestLuaBase64TextRoundTripsEveryInstance loads every instance row the target
// generates, writes the base64 text of its wire, and requires it to equal Go's
// encoding/base64.StdEncoding of the same wire file (docs/SPEC-TABLES.md
// §16.8). The text read back must save to the identical wire bytes, and a body
// with a byte outside the alphabet, a wrong pad or a cut length must refuse
// base64_damaged.
func TestLuaBase64TextRoundTripsEveryInstance(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifestPath)
	unitFiles := map[string][]string{}
	for k, paths := range units {
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join(repo, p))
		}
		unitFiles[k] = full
	}
	refused := map[string]string{}
	refusedRows := map[string][]string{}
	compared := 0
	instances := 0
	var mismatches []string
	// A row the manifest marks no-text has no text spelling (docs/SPEC-TABLES.md
	// §16.7, §16.8): the shared-blob rows are refused by the strict read for the
	// binary reason blob_shared, so LoadText cannot round-trip them.
	noText := map[string]bool{}
	if data, err := os.ReadFile(manifestPath); err == nil {
		for line := range strings.SplitSeq(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) >= 5 && f[0] == "instance" {
				for _, x := range f[5:] {
					if x == "no-text" {
						noText[f[1]] = true
					}
				}
			}
		}
	}
	for _, row := range manifestRows(t, manifestPath, "instance") {
		if noText[row.caseName] {
			continue
		}
		paths, ok := unitFiles[row.unit]
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf("instance %s: no manifest unit", row.caseName))
			continue
		}
		if _, refusedUnit := refused[row.unit]; refusedUnit {
			refusedRows[row.unit] = append(refusedRows[row.unit], row.caseName)
			continue
		}
		u := luarun.LoadUnit(t, paths...)
		if _, err := Generate(u); err != nil {
			refused[row.unit] = err.Error()
			refusedRows[row.unit] = append(refusedRows[row.unit], row.caseName)
			continue
		}
		wireData, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatalf("case %s: read wire %s: %v", row.caseName, row.wire, err)
		}
		good := base64.StdEncoding.EncodeToString(wireData)
		t.Logf("row %s unit %s", row.caseName, row.unit)
		out := textInstance(t, repo, paths, row, good, textProbe(ModuleName(u), row.root), nil)
		instances++
		lines := strings.Split(out, "\n")
		if len(lines) != 2 {
			mismatches = append(mismatches, fmt.Sprintf("instance %s: bad output %q", row.caseName, out))
			continue
		}
		if lines[0] != good {
			mismatches = append(mismatches, fmt.Sprintf("instance %s: SaveText %q != base64(wire) %q", row.caseName, lines[0], good))
			continue
		}
		part := strings.SplitN(lines[1], ":", 4)
		if len(part) != 4 {
			mismatches = append(mismatches, fmt.Sprintf("instance %s: bad verdict %q", row.caseName, lines[1]))
			continue
		}
		if part[0] != "same" {
			mismatches = append(mismatches, fmt.Sprintf("instance %s: LoadText then Save != wire", row.caseName))
			continue
		}
		for i, what := range []string{"outside alphabet", "wrong pad", "cut length"} {
			if part[i+1] != "damaged" {
				mismatches = append(mismatches, fmt.Sprintf("instance %s: %s answered %s, want base64_damaged", row.caseName, what, part[i+1]))
			}
		}
		compared++
	}
	names := make([]string, 0, len(refused))
	for unit := range refused {
		names = append(names, unit)
	}
	sort.Strings(names)
	for _, unit := range names {
		t.Logf("refused unit %s (instances %s): %s", unit, strings.Join(refusedRows[unit], ", "), refused[unit])
	}
	for _, m := range mismatches {
		t.Error(m)
	}
	if compared == 0 {
		t.Fatalf("no instance round-tripped (%d instances, %d refused)", instances, len(refused))
	}
	skipped := 0
	for _, rows := range refusedRows {
		skipped += len(rows)
	}
	t.Logf("%d of %d instances carry base64 text byte-for-byte; %d refused unit(s) holding %d instance(s)", compared, instances, len(refused), skipped)
}

// TestLuaBase64TextNegativeControl patches the emitted runtime in memory — one
// anchor, matched exactly once — so the pad is dropped on write, and requires
// the text comparison to go red.
func TestLuaBase64TextNegativeControl(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifestPath)
	unitFiles := map[string][]string{}
	for k, paths := range units {
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join(repo, p))
		}
		unitFiles[k] = full
	}
	// the encoder's one padding write (docs/SPEC-TABLES.md §16.8); sabotaged,
	// the text is unpadded and no longer the wire's padded standard base64
	const anchor = "  if n > 0 then out[#out + 1] = string.rep(\"=\", n) end\n"
	const sabotaged = "  if false then out[#out + 1] = string.rep(\"=\", n) end\n"
	var patched int
	var diverged int
	compared := 0
	// A ROW THE MANIFEST MARKS A NAMED WIRE REFUSAL has no text to compare:
	// SaveText raises the writer's name (docs/SPEC-TABLES.md §2.5, §16.7).
	wireRefusal := manifestWireRefusalRows(t, manifestPath)
	for _, row := range manifestRows(t, manifestPath, "instance") {
		if wireRefusal[row.caseName] {
			continue
		}
		paths, ok := unitFiles[row.unit]
		if !ok {
			continue
		}
		u := luarun.LoadUnit(t, paths...)
		if _, err := Generate(u); err != nil {
			continue
		}
		wireData, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatalf("case %s: read wire %s: %v", row.caseName, row.wire, err)
		}
		good := base64.StdEncoding.EncodeToString(wireData)
		patch := func(src []byte) []byte {
			if n := strings.Count(string(src), anchor); n != 1 {
				t.Fatalf("the negative-control anchor is matched %d times, want exactly once", n)
			}
			patched++
			return []byte(strings.Replace(string(src), anchor, sabotaged, 1))
		}
		out := textInstance(t, repo, paths, row, good, textSaveProbe(ModuleName(u), row.root), patch)
		if out == good {
			compared++
		} else {
			diverged++
		}
	}
	if patched == 0 {
		t.Fatal("the negative control patched no emitted runtime")
	}
	if diverged == 0 {
		t.Fatalf("the sabotaged text still matched the wire (%d instances compared)", compared)
	}
	t.Logf("%d of %d instances diverge with the pad dropped", diverged, diverged+compared)
}
