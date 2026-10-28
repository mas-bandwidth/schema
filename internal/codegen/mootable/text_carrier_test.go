package mootable

import (
	"encoding/base64"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

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
