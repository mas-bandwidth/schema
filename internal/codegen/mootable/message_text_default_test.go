package mootable_test

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestMessageStringDefaultElidedOnlyWhenEqual holds the message form's text
// elision to the reference (docs/SPEC-TABLES.md §3.3): a string or bytes
// field is left out only when its length AND its bytes are the declared
// default's. A value of the default's length ("xyz" under "abc") and the
// empty value both ride and round-trip unchanged; the default itself is
// still elided, so its bytes never reach the wire.
func TestMessageStringDefaultElidedOnlyWhenEqual(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	u := unitFromSource(t, "package probe\nfixed table P\n{\n    s string(8) = \"abc\"\n    b bytes(4) = \"ab\"\n}\n")
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	bodies := [][2]string{{"xyz", "zz"}, {"", ""}, {"abc", "ab"}}

	var main strings.Builder
	main.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\nlocal MS = require(\"./table_message\")\n" + g.requireAll() + "local T = require(\"./ProbeTable\")\n")
	main.WriteString(`local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local function body(s: string, b: string): any
	local v: any = T.NewP()
	v.S = s
	v.B = buffer.create(4)
	buffer.writestring(v.B, 0, b)
	v.BLength = #b
	return v
end
local values = {}
`)
	for _, b := range bodies {
		fmt.Fprintf(&main, "table.insert(values, body(%q, %q))\n", b[0], b[1])
	}
	main.WriteString(`local report: any = {}
local out = MS.SaveMessages("P", values, report)
if out == nil then print("SAVE " .. tostring(report.reason)) return end
print(hex(out))
local vocabulary = MS.NewVocabulary()
if not MS.AnnounceRead(vocabulary, inputs.conn, report) then print("ANNOUNCE " .. tostring(report.reason)) return end
local n, back = MS.LoadMessages(vocabulary, "P", out, 3, report)
if n ~= 3 then print("LOAD " .. tostring(n) .. " " .. tostring(report.reason)) return end
for i = 1, n do
	local v: any = back[i]
	print("[" .. tostring(v.S) .. "|" .. buffer.readstring(v.B, 0, v.BLength) .. "]")
end
`)
	got := strings.Split(g.run(t, main.String(), map[string][]byte{"conn": tablewire.Announce(u)}), "\n")
	if len(got) != 1+len(bodies) {
		t.Fatalf("moo answered %q", got)
	}
	if !strings.Contains(got[0], hex.EncodeToString([]byte("xyz"))) {
		t.Errorf("the batch does not carry \"xyz\": %s", got[0])
	}
	if strings.Contains(got[0], hex.EncodeToString([]byte("abc"))) {
		t.Errorf("the default \"abc\" was written, not elided: %s", got[0])
	}
	for i, b := range bodies {
		if w := "[" + b[0] + "|" + b[1] + "]"; got[1+i] != w {
			t.Errorf("body %d did not round-trip: got %s want %s", i, got[1+i], w)
		}
	}
}
