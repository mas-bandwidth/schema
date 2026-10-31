package luautable_test

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
)

// textProbe is the Luau main for one instance's base64 text carrier
// (docs/SPEC-TABLES.md §16.8): it loads the wire strictly, SaveText's the
// value and byte-compares the wire Save writes; it then LoadText's Go's own
// base64 of the wire and byte-compares the wire again; and it feeds the three
// damaged texts, each of which must refuse as base64_damaged.
func textProbe(g *generatedUnit, rootName string) string {
	var b strings.Builder
	b.WriteString("local inputs = require(\"./inputs\")\n")
	b.WriteString("local text = require(\"./schema_text\")\n")
	b.WriteString("local save = require(\"./schema_save\")\n")
	b.WriteString("local load = require(\"./schema_load\")\n")
	b.WriteString("local St = require(\"./schema_tables\")\n")
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	b.WriteString("local mods = {}\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "mods[#mods + 1] = require(\"./%s\")\n", m)
	}
	q := fmt.Sprintf("%q", rootName)
	b.WriteString("local ROOT = " + q + "\n")
	b.WriteString(`local T = nil
for _, m in mods do
	if m[ROOT .. "Descriptor"] ~= nil then T = m; break end
end
if T == nil then print("NOMODULE"); return end
local d = St.lookup(ROOT)
local function fresh()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local function hexstr(b)
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local report = fresh()
local v = load.load(ROOT, inputs.wire, report)
if v == nil or report.refused > 0 then print("WIREREFUSED " .. tostring(report.reason)); return end
local encoded = text.SaveText(d, v)
if encoded == nil then print("SAVETEXTNIL"); return end
local out = save.save(d, v)
if out == nil then print("SAVENIL"); return end
print(hexstr(out))
print(encoded)
local rep2 = fresh()
local v2 = text.LoadText(T, ROOT, buffer.tostring(inputs.text), rep2)
if v2 == nil then print("TEXTREFUSED " .. tostring(rep2.reason)); return end
local out2 = save.save(d, v2)
if out2 == nil then print("SAVE2NIL"); return end
print(hexstr(out2))
local reasons = {}
for _, name in { "badchar", "badpad", "cut" } do
	local rep = fresh()
	local vv = text.LoadText(T, ROOT, buffer.tostring(inputs[name]), rep)
	if vv == nil then
		reasons[#reasons + 1] = tostring(rep.reason)
	else
		reasons[#reasons + 1] = "read"
	end
end
print(table.concat(reasons, ","))
`)
	return b.String()
}

// textOutput is one textProbe run: the wire Save writes from the binary load,
// the base64 SaveText writes, the wire LoadText then Save writes, and the
// three damaged inputs' refusals.
type textOutput struct {
	fromWire []byte
	text     string
	fromText []byte
	damaged  string
}

// textRun runs textProbe over one instance's wire and its Go-computed base64.
func textRun(t *testing.T, g *generatedUnit, wire []byte, rootName string) (textOutput, error) {
	t.Helper()
	text := base64.StdEncoding.EncodeToString(wire)
	if len(text) < 2 {
		return textOutput{}, fmt.Errorf("the wire is too short to damage")
	}
	inputs := map[string][]byte{
		"wire":    wire,
		"text":    []byte(text),
		"badchar": []byte("!" + text[1:]),
		"badpad":  []byte(text + "="),
		"cut":     []byte(text[:len(text)-1]),
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(textProbe(g, rootName)), 0o644); err != nil {
		return textOutput{}, err
	}
	out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", inputs)))
	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		return textOutput{}, fmt.Errorf("unexpected output %q", out)
	}
	fromWire, err := hex.DecodeString(lines[0])
	if err != nil {
		return textOutput{}, fmt.Errorf("bad saved-wire line %q: %w", lines[0], err)
	}
	fromText, err := hex.DecodeString(lines[2])
	if err != nil {
		return textOutput{}, fmt.Errorf("bad reloaded-wire line %q: %w", lines[2], err)
	}
	return textOutput{fromWire: fromWire, text: lines[1], fromText: fromText, damaged: lines[3]}, nil
}

// TestLuauBase64TextRoundTripsEveryInstance is the base64 text carrier's gate
// (docs/SPEC-TABLES.md §16.8): for every instance row the target generates,
// the wire's strict load SaveText's to Go's encoding/base64 StdEncoding of the
// wire file, and LoadText of that same text saves back to the wire file byte
// for byte. Three damaged texts — a byte outside the alphabet, a wrong pad and
// a cut length — each refuse as base64_damaged.
func TestLuauBase64TextRoundTripsEveryInstance(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	rows := instanceRows(t)
	var keys []string
	for k := range rows {
		if _, ok := units[k]; !ok {
			t.Errorf("instance rows name unit %s, which the manifest does not declare", k)
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	total, compared, refusedRows, refusedTexts := 0, 0, 0, 0
	for _, key := range keys {
		total += len(rows[key])
		g := generateUnit(t, units, key)
		if g.err != nil {
			if _, ok := refused[key]; !ok {
				t.Errorf("unit %s is refused and the refused list does not name it: %v", key, g.err)
			}
			refusedRows += len(rows[key])
			t.Logf("refused unit: %s (%d instances): %v", key, len(rows[key]), g.err)
			continue
		}
		for _, row := range rows[key] {
			wire := rowWire(t, row)
			got, err := textRun(t, g, wire, row.root)
			if err != nil {
				// a blob_shared wire (go-refusal=blob_shared) names one blob
				// record from two slots: every writer refuses it (docs/SPEC-TABLES.md
				// §2.5), so its binary load cannot SaveText.
				if row.blobShared {
					refusedTexts++
					continue
				}
				t.Errorf("instance %s (%s/%s): %v", row.caseName, key, row.root, err)
				continue
			}
			if string(got.fromWire) != string(wire) {
				t.Errorf("instance %s (%s/%s): the save of the binary load is not the wire: %s", row.caseName, key, row.root, firstDiff(got.fromWire, wire))
				continue
			}
			wantText := base64.StdEncoding.EncodeToString(wire)
			if got.text != wantText {
				t.Errorf("instance %s (%s/%s): SaveText is not Go's StdEncoding of the wire: got %q want %q", row.caseName, key, row.root, got.text, wantText)
				continue
			}
			if string(got.fromText) != string(wire) {
				t.Errorf("instance %s (%s/%s): LoadText then Save is not the wire: %s", row.caseName, key, row.root, firstDiff(got.fromText, wire))
				continue
			}
			if got.damaged != "base64_damaged,base64_damaged,base64_damaged" {
				t.Errorf("instance %s (%s/%s): damaged texts answered %q, want base64_damaged three times", row.caseName, key, row.root, got.damaged)
				continue
			}
			compared++
		}
	}
	if compared == 0 {
		t.Fatal("no instance compared")
	}
	if compared+refusedRows+refusedTexts != total {
		t.Errorf("%d instances compared, %d text refusals and %d of refused units, of %d in the manifest", compared, refusedTexts, refusedRows, total)
	}
	t.Logf("compared %d instances through base64 text (%d text refusals, %d in the manifest, %d of refused units)", compared, refusedTexts, total, refusedRows)
}

// TestLuauBase64TextNegativeControl patches the emitted schema_text.luau in memory —
// one anchor, matched exactly once — so the pad is dropped on write, and
// requires the text comparison to go red on an instance that round-trips clean
// unpatched (docs/SPEC-TABLES.md §16.8: padded on write).
func TestLuauBase64TextNegativeControl(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	rows := instanceRows(t)
	var keys []string
	for k := range rows {
		if _, ok := units[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	const anchor = "\t\tif pad > 0 then parts[#parts + 1] = string.rep(\"=\", pad) end\n"
	for _, key := range keys {
		g := generateUnit(t, units, key)
		if g.err != nil {
			continue
		}
		for _, row := range rows[key] {
			wire := rowWire(t, row)
			clean, err := textRun(t, g, wire, row.root)
			if err != nil || string(clean.fromText) != string(wire) || clean.text != base64.StdEncoding.EncodeToString(wire) {
				continue // the round-trip gate names it; the control needs a clean baseline
			}
			if len(wire)%3 == 0 {
				continue // no pad on this wire, so dropping the pad changes nothing
			}
			path := filepath.Join(g.dir, "schema_text.luau")
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(src), anchor) != 1 {
				t.Fatalf("the base64 pad anchor is not in the emitted runtime exactly once")
			}
			patched := strings.Replace(string(src), anchor, "\t\tif pad > 0 then parts[#parts + 1] = \"\" end\n", 1)
			if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := textRun(t, g, wire, row.root)
			if werr := os.WriteFile(path, src, 0o644); werr != nil {
				t.Fatal(werr)
			}
			if err != nil {
				t.Fatalf("instance %s/%s: the patched runtime failed: %v", key, row.root, err)
			}
			if got.text != base64.StdEncoding.EncodeToString(wire) {
				t.Logf("instance %s/%s: the text comparison went red after dropping the pad (%q, base64 %q)", key, row.root, got.text, base64.StdEncoding.EncodeToString(wire))
				return
			}
		}
	}
	t.Fatal("no clean instance went red under the patched pad: the control proves nothing")
}
