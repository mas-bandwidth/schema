package mootable

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tablecook"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
)

func mooCookSplitMix64(s *uint64) uint64 {
	*s += 0x9e3779b97f4a7c15
	z := *s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// TestMooCookForgeryFuzz mutates cooked images deterministically and opens and
// walks each through table_cookread.moo (C.open + C.read), verifying every
// admitted image stays within buffer bounds and agrees with Header + region
// bounds.
func TestMooCookForgeryFuzz(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo cook forgery walk fuzz")
	u := mooBase64UnitFromSource(t, "package moocook\ntable Node { x int32\n next *Node }\ntable Root { head *Node\n label string(16) }\n")
	m := tabletext.NewModel(u)
	var tr tabletext.Report
	inst := m.New(m.Lookup("Root"))
	if !m.Read(inst, []byte(`{"label":"hello","head":{"x":7,"next":{"x":9}}}`), &tr) {
		t.Fatalf("m.Read: %+v", tr)
	}
	cookImg, err := tablecook.Cook(m, inst, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := generateUnitDir(t, u, nil)

	seed := uint64(0x4d4f4f434f4f4b33)
	const mutants = 250
	inputs := map[string][]byte{}
	for i := range mutants {
		cmut := append([]byte(nil), cookImg...)
		switch mooCookSplitMix64(&seed) % 4 {
		case 0:
			cmut = cmut[:int(mooCookSplitMix64(&seed)%uint64(len(cmut)))]
		case 1:
			cmut[int(mooCookSplitMix64(&seed)%uint64(len(cmut)))] ^= byte(1 << (mooCookSplitMix64(&seed) % 8))
		case 2:
			if len(cmut) >= 48 {
				binary.LittleEndian.PutUint64(cmut[32:40], mooCookSplitMix64(&seed)%1024)
			}
		default:
			cmut = append(cmut, 0xff)
		}
		inputs[fmt.Sprintf("c%d", i)] = cmut
	}
	script := fmt.Sprintf(`--!strict
local _Probe = require("./ProbeTable")
local C = require("./table_cookread")
local inputs = require("./inputs")
local opened = 0
local walked = 0
local refused = 0
for i = 0, %d do
	local b = inputs["c" .. tostring(i)]
	local h, why = C.open("Root", b, 0, buffer.len(b))
	if h == nil then
		assert(why ~= nil and why ~= "")
		refused += 1
	else
		opened += 1
		local v, rwhy = C.read(h)
		if v ~= nil then
			walked += 1
		else
			assert(rwhy ~= nil and rwhy.name ~= "")
		end
	end
end
print("ok " .. tostring(opened) .. " " .. tostring(walked) .. " " .. tostring(refused))
`, mutants-1)
	if err := os.WriteFile(filepath.Join(dir, "main.moo"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(moorun.Run(t, dir, "main.moo", inputs))
	if !strings.HasPrefix(strings.TrimSpace(out), "ok ") {
		t.Fatalf("moo cook forgery fuzz failed: %q", out)
	}
}

// TestMooCookForgeryFuzzNegativeControl removes table_cookread.moo's region
// length bound check and verifies a truncated cook opens past the buffer.
func TestMooCookForgeryFuzzNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo cook forgery fuzz negative control")
	u := mooBase64UnitFromSource(t, "package moocook\ntable Node { x int32 }\ntable Root { head *Node\n label string(16) }\n")
	m := tabletext.NewModel(u)
	var tr tabletext.Report
	inst := m.New(m.Lookup("Root"))
	if !m.Read(inst, []byte(`{"label":"hello","head":{"x":7}}`), &tr) {
		t.Fatalf("m.Read: %+v", tr)
	}
	cookImg, err := tablecook.Cook(m, inst, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rt := Runtime()
	patched := maps.Clone(rt)
	src := string(patched["table_cookread.moo"])
	anchor := `return nil, "truncated"`
	if !strings.Contains(src, anchor) {
		t.Fatalf("missing anchor %q in table_cookread.moo", anchor)
	}
	// Replace the post-header region truncation check (keep the 64-byte header check first).
	first := strings.Index(src, anchor)
	rest := src[first+len(anchor):]
	if !strings.Contains(rest, anchor) {
		t.Fatal("expected second truncated check for region extent in table_cookread.moo")
	}
	patched["table_cookread.moo"] = []byte(src[:first+len(anchor)] + strings.ReplaceAll(rest, anchor, "if false then return nil, \"truncated\" end"))
	dir := generateUnitDir(t, u, patched)
	short := cookImg[:64+8]
	_ = os.WriteFile(filepath.Join(dir, "unused"), nil, 0o644)
	script := `--!strict
local _Probe = require("./ProbeTable")
local C = require("./table_cookread")
local inputs = require("./inputs")
local h, _ = C.open("Root", inputs["short"], 0, buffer.len(inputs["short"]))
if h ~= nil then
	print("admitted-truncated")
else
	print("refused")
end
`
	if err := os.WriteFile(filepath.Join(dir, "main.moo"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(moorun.Run(t, dir, "main.moo", map[string][]byte{"short": short}))
	if !strings.Contains(out, "admitted-truncated") {
		t.Fatalf("negative control did not admit truncated cook: %q", out)
	}
}
