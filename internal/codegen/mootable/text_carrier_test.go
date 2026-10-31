package mootable

import (
	"encoding/base64"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE BASE64 CARRIER (docs/SPEC-TABLES.md §16.8): textcarrier.moo's encode is
// encoding/base64's StdEncoding byte for byte, and its decode answers each
// text as the strict reader must — the bytes for canonical text,
// base64_not_canonical for a text that drops its CR and LF into padded base64
// that decodes but is not the canonical spelling of its bytes (a pad bit set,
// a CR or LF present), base64_damaged for everything else.

func carrierVerdict(text string) string {
	stripped := strings.NewReplacer("\r", "", "\n", "").Replace(text)
	b, err := base64.StdEncoding.Strict().DecodeString(stripped)
	if err != nil {
		// a set pad bit is refused by Strict; the lenient decode tells it apart
		lb, lerr := base64.StdEncoding.DecodeString(stripped)
		if lerr != nil || strings.ContainsAny(stripped, "\r\n") {
			return "base64_damaged"
		}
		return "base64_not_canonical " + fmt.Sprintf("%x", lb)
	}
	if stripped != text || base64.StdEncoding.EncodeToString(b) != text {
		return "base64_not_canonical " + fmt.Sprintf("%x", b)
	}
	return "ok " + fmt.Sprintf("%x", b)
}

func carrierTexts() []string {
	texts := []string{"", "AQAAAAAAAAAAAB==", "AQAAAAAAAAAAAA==", "AQID", "AQI=", "AQ==", "AR==", "AQJ=", "A===", "AQID\n", "AQ\r\nID",
		"AQ ID", "AQ=D", "====", "AQI", "Zm9v!", "\n", "Zm9vYmFy", "Zm9vYmE=", "Zm9vYg=="}
	r := rand.New(rand.NewPCG(20261028, 16))
	for range 200 {
		b := make([]byte, r.IntN(40))
		for i := range b {
			b[i] = byte(r.Uint32())
		}
		s := base64.StdEncoding.EncodeToString(b)
		if r.IntN(2) == 0 && len(s) > 0 {
			// one byte damaged: a symbol, a pad, a newline or a stray byte
			at := r.IntN(len(s))
			s = s[:at] + string("A=\n#/"[r.IntN(5)]) + s[at+1:]
		}
		texts = append(texts, s)
	}
	return texts
}

func carrierRun(t *testing.T, texts []string, runtime map[string][]byte) []string {
	t.Helper()
	inputs := map[string][]byte{"texts": []byte(strings.Join(texts, "\x00"))}
	main := probePrelude + `local T = require("./textcarrier")
for _, text in string.split(buffer.tostring(inputs.texts), "\0") do
	local bytes, refusal = T.decode(text)
	local hex = ""
	if bytes ~= nil then
		local parts = {}
		for i = 0, buffer.len(bytes) - 1 do
			table.insert(parts, string.format("%02x", buffer.readu8(bytes, i)))
		end
		hex = table.concat(parts)
		if refusal == nil and T.encode(bytes) ~= text then
			refusal = "encode disagrees"
		end
	end
	hexs(if refusal == nil then "ok " .. hex elseif refusal == "base64_damaged" then refusal else refusal .. " " .. hex)
end
`
	return jsonProbe(t, main, inputs, runtime)
}

func TestMooTextCarrier(t *testing.T) {
	slowtest.Gate(t, "moo (it runs textcarrier.moo)")
	t.Parallel()
	texts := carrierTexts()
	got := carrierRun(t, texts, nil)
	if len(got) != len(texts) {
		t.Fatalf("the probe printed %d verdicts for %d texts", len(got), len(texts))
	}
	for i, text := range texts {
		if want := carrierVerdict(text); got[i] != want {
			t.Errorf("%q: moo %s, want %s", text, got[i], want)
		}
	}
}

// TestMooTextCarrierNegativeControl stops checking the spare pad bits, so a
// set one reads as canonical: the carrier gate must name those texts.
func TestMooTextCarrierNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs a sabotaged textcarrier.moo)")
	t.Parallel()
	src := string(Runtime()["textcarrier.moo"])
	const site = "if v2 % 16 ~= 0 then"
	if !strings.Contains(src, site) {
		t.Fatal("the control's site moved: textcarrier.moo no longer checks " + site)
	}
	broken := strings.Replace(src, site, "if false then", 1)
	texts := carrierTexts()
	got := carrierRun(t, texts, map[string][]byte{"textcarrier.moo": []byte(broken)})
	bad := 0
	for i, text := range texts {
		if got[i] != carrierVerdict(text) {
			bad++
		}
	}
	if bad == 0 {
		t.Fatal("ignoring the spare pad bits passed the carrier gate: it cannot see a set pad bit")
	}
	t.Logf("ignoring the pad bits: %d texts named", bad)
}

// TestMooTextCarrierSurface: every textual instance's wire, through the
// generated <Name>LoadText and <Name>SaveText, is the same wire (the carrier
// is byte for byte, §16.8), and the value LoadText lands writes the golden
// text; a text with a CR inside, or one byte outside the alphabet, refuses by
// its name before any read.
func TestMooTextCarrierSurface(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated carrier)")
	t.Parallel()
	m := readTextManifest(t)
	byUnit := map[string][]textInstance{}
	for _, in := range m.instances {
		byUnit[in.unit] = append(byUnit[in.unit], in)
	}
	total := 0
	for _, key := range sortedKeys(byUnit) {
		u := loadUnit(t, m.units[key])
		if _, owed := wireOwed[key]; owed || !unitCarriesJson(u) {
			continue
		}
		dir := generateUnitDir(t, u, nil)
		inputs := map[string][]byte{}
		var b strings.Builder
		b.WriteString(probePrelude)
		b.WriteString(`local function run(mod: any, root: string, text: string)
	local value, why = mod[root .. "LoadText"](text, nil)
	if value == nil then
		hexs("refused " .. tostring(why))
		return
	end
	local again = mod[root .. "SaveText"](value)
	hexs(if again == text then "same" else "differs")
	local out = buffer.create(mod[root .. "ToJsonMeasure"](value))
	mod[root .. "ToJson"](value, out)
	hexs(buffer.tostring(out))
	local _, cr = mod[root .. "LoadText"](string.sub(text, 1, 4) .. "\r" .. string.sub(text, 5), nil)
	local _, bad = mod[root .. "LoadText"]("#" .. string.sub(text, 2), nil)
	hexs(tostring(cr) .. " " .. tostring(bad))
end
`)
		for i, in := range byUnit[key] {
			wire, err := os.ReadFile(filepath.Join(repoRoot, in.wire))
			if err != nil {
				t.Fatal(err)
			}
			inputs[fmt.Sprintf("b%d", i)] = []byte(base64.StdEncoding.EncodeToString(wire))
			fmt.Fprintf(&b, "run(require(\"./%s\"), %q, buffer.tostring(inputs.b%d))\n", tableModule(u, in.root), in.root, i)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.moo"), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		lines := decodeHexLines(t, moorun.Run(t, dir, "main.moo", inputs))
		at := 0
		for _, in := range byUnit[key] {
			total++
			if at >= len(lines) {
				t.Fatalf("unit %s: the probe stopped early", key)
			}
			if strings.HasPrefix(lines[at], "refused") {
				t.Errorf("%s (%s): LoadText %s", in.name, key, lines[at])
				at++
				continue
			}
			if lines[at] != "same" {
				t.Errorf("%s (%s): SaveText(LoadText(text)) is not the text", in.name, key)
			}
			if lines[at+1] != string(readGolden(t, in.name)) {
				t.Errorf("%s (%s): the value LoadText landed does not write the golden", in.name, key)
			}
			if lines[at+2] != "base64_not_canonical base64_damaged" {
				t.Errorf("%s (%s): the damaged texts answered %s", in.name, key, lines[at+2])
			}
			at += 3
		}
	}
	t.Logf("%d instances", total)
}
