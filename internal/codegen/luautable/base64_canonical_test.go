package luautable_test

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
)

// goBase64Verdict is schema-go's own base64 verdict, the step generated
// <Name>LoadText performs before it reads the wire (internal/codegen/gotable/
// json.go, docs/SPEC-TABLES.md §16.8): a strict standard decode, then a plain
// decode to tell a non-canonical body from a damaged one, then the canonical
// encoded length. It answers "" when base64 accepts the text; the wire read
// decides what follows. The four damaged/non-canonical texts below never reach
// that wire read, so this is the generated Go's verdict for them.
func goBase64Verdict(text string) string {
	wire, err := base64.StdEncoding.Strict().DecodeString(text)
	if err != nil {
		if _, plainErr := base64.StdEncoding.DecodeString(text); plainErr == nil {
			return "base64_not_canonical"
		}
		return "base64_damaged"
	}
	if len(text) != base64.StdEncoding.EncodedLen(len(wire)) {
		return "base64_not_canonical"
	}
	return ""
}

// b64CanonProbe is the Luau main that pins the base64 carrier
// (docs/SPEC-TABLES.md §16.8): it reads the canonical text, saves it back and prints the
// SaveText and wire it writes, then feeds every non-canonical or damaged text
// and prints the refusal it names.
func b64CanonProbe(g *generatedUnit, rootName string) string {
	var b strings.Builder
	b.WriteString("local inputs = require(\"./inputs\")\n")
	b.WriteString("local text = require(\"./schema_text\")\n")
	b.WriteString("local save = require(\"./schema_save\")\n")
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
	fmt.Fprintf(&b, "local ROOT = %q\n", rootName)
	b.WriteString(`local T = nil
for _, m in mods do
	if m[ROOT .. "Descriptor"] ~= nil then T = m; break end
end
if T == nil then print("canonical\tNOMODULE"); return end
local d = St.lookup(ROOT)
local function fresh() return {} end
local function hexstr(buf)
	local parts = table.create(buffer.len(buf))
	for i = 0, buffer.len(buf) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(buf, i))
	end
	return table.concat(parts)
end
local rep = fresh()
local v = text.LoadText(T, ROOT, buffer.tostring(inputs.canonical), rep)
if v == nil then
	print("canonical\tREFUSED\t" .. tostring(rep.reason))
else
	local encoded = text.SaveText(d, v)
	local out = save.save(d, v)
	if encoded == nil or out == nil then
		print("canonical\tSAVENIL")
	else
		print("canonical\tSAVED\t" .. encoded .. "\t" .. hexstr(out))
	end
end
for _, name in { "padbits", "innereq", "linebreak", "trailspace", "badchar", "cut" } do
	local r = fresh()
	local vv = text.LoadText(T, ROOT, buffer.tostring(inputs[name]), r)
	if vv == nil then
		print(name .. "\t" .. tostring(r.reason))
	else
		print(name .. "\tread")
	end
end
`)
	return b.String()
}

// TestLuauLoadTextRefusesNonCanonicalBase64 pins docs/SPEC-TABLES.md §16.8 in
// this target: the base64 carrier is CANONICAL — standard alphabet, padded,
// pad bits zero, no CR or LF — and LoadText refuses a body that decodes but is
// not that spelling with base64_not_canonical, while a body that does not
// decode stays base64_damaged. The verdicts are schema-go's own (the generated
// Go's base64 step, goBase64Verdict), not typed here: the fuzz repro
// AQAAAAAAAAAAAB== (nonzero pad bits) and a line break are non-canonical; an
// inner '=', a space and a byte outside the alphabet are damaged; and a cut
// length is damaged. For every instance row the canonical SaveText reads back
// clean and SaveText re-encodes to the same canonical text, except a
// blob_shared row (go-refusal=blob_shared), whose canonical wire the strict read refuses by name
// (docs/SPEC-TABLES.md §2.5, §6.5).
func TestLuauLoadTextRefusesNonCanonicalBase64(t *testing.T) {
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
	total, compared, refusedRows := 0, 0, 0
	for _, key := range keys {
		total += len(rows[key])
		g := generateUnit(t, units, key)
		if g.err != nil {
			if _, ok := refused[key]; !ok {
				t.Errorf("unit %s is refused and the refused list does not name it: %v", key, g.err)
			}
			refusedRows += len(rows[key])
			continue
		}
		for _, row := range rows[key] {
			wire := rowWire(t, row)
			canonical := base64.StdEncoding.EncodeToString(wire)
			mid := len(canonical) / 2
			if mid == 0 {
				mid = len(canonical)
			}
			cases := []struct {
				name string
				text string
			}{
				{"padbits", "AQAAAAAAAAAAAB=="},
				{"innereq", canonical[:mid] + "=" + canonical[mid:]},
				{"linebreak", canonical[:mid] + "\n" + canonical[mid:]},
				{"trailspace", canonical + " "},
				{"badchar", "!" + canonical[1:]},
				{"cut", canonical[:len(canonical)-1]},
			}
			inputs := map[string][]byte{"canonical": []byte(canonical)}
			want := map[string]string{}
			for _, c := range cases {
				v := goBase64Verdict(c.text)
				if v == "" {
					t.Fatalf("instance %s (%s/%s): %s text %q is not a damaged or non-canonical case", row.caseName, key, row.root, c.name, c.text)
				}
				want[c.name] = v
				inputs[c.name] = []byte(c.text)
			}
			if want["padbits"] != "base64_not_canonical" || want["linebreak"] != "base64_not_canonical" {
				t.Fatalf("instance %s (%s/%s): the oracle does not call the fuzz repro and the line break non-canonical: %+v", row.caseName, key, row.root, want)
			}
			if want["badchar"] != "base64_damaged" || want["cut"] != "base64_damaged" {
				t.Fatalf("instance %s (%s/%s): the oracle does not call the outside byte and the cut length damaged: %+v", row.caseName, key, row.root, want)
			}
			if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b64CanonProbe(g, row.root)), 0o644); err != nil {
				t.Fatal(err)
			}
			out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", inputs)))
			lines := strings.Split(out, "\n")
			if len(lines) != 1+len(cases) {
				t.Fatalf("instance %s (%s/%s): unexpected output %q", row.caseName, key, row.root, out)
			}
			fields := strings.Split(lines[0], "\t")
			if fields[0] != "canonical" {
				t.Fatalf("instance %s (%s/%s): first line %q", row.caseName, key, row.root, lines[0])
			}
			if row.blobShared {
				// LoadText IS the strict read (§6.5), and one blob named from
				// two slots is refused by name (docs/SPEC-TABLES.md §2.5): the
				// canonical text is a readable wire whose strict read refuses
				// blob_shared, so there is no value to SaveText. The damaged
				// and non-canonical texts still name their carrier refusals.
				if len(fields) != 3 || fields[1] != "REFUSED" || fields[2] != "blob_shared" {
					t.Fatalf("instance %s (%s/%s): the canonical blob_shared text did not refuse blob_shared: %q", row.caseName, key, row.root, lines[0])
				}
			} else {
				if len(fields) < 2 || fields[1] != "SAVED" || len(fields) != 4 {
					t.Fatalf("instance %s (%s/%s): the canonical text did not read clean: %q", row.caseName, key, row.root, lines[0])
				}
				if fields[2] != canonical {
					t.Errorf("instance %s (%s/%s): SaveText is %q, want the canonical %q", row.caseName, key, row.root, fields[2], canonical)
				}
				if fields[3] != hexBytes(wire) {
					t.Errorf("instance %s (%s/%s): the saved wire is not the instance's wire", row.caseName, key, row.root)
				}
			}
			for i, c := range cases {
				got := strings.Split(lines[i+1], "\t")
				if len(got) != 2 || got[0] != c.name {
					t.Fatalf("instance %s (%s/%s): case line %q", row.caseName, key, row.root, lines[i+1])
				}
				if got[1] != want[c.name] {
					t.Errorf("instance %s (%s/%s): %s text %q answered %q, schema-go answers %q", row.caseName, key, row.root, c.name, c.text, got[1], want[c.name])
				}
			}
			compared++
		}
	}
	if compared == 0 {
		t.Fatal("no instance compared")
	}
	if compared+refusedRows != total {
		t.Errorf("%d instances compared and %d of refused units, of %d in the manifest", compared, refusedRows, total)
	}
	t.Logf("compared %d instances through canonical base64 (%d in the manifest, %d of refused units)", compared, total, refusedRows)
}
