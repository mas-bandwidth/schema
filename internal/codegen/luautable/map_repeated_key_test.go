package luautable_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const repeatedKeySchema = "package probe\n\ntable Root\n{\n    entries map[int32]int32\n}\n"

// repeatedKeyProbe reads both wires through every strict variable read and
// prints one verdict per (input, read).
const repeatedKeyProbe = `local inputs = require("./inputs")
local T = require("./ProbeTable")
local L = require("./schema_load")
local R = require("./schema_refusal")
local Ret = require("./schema_retain")
local reads = {
	{ "load", function(b) return L.load("Root", b, {}) end },
	{ "strict", function(b) return R.ReadStrict(T, "Root", b, {}) end },
	{ "retain", function(b) return (Ret.LoadRetain("Root", b, {})) end },
}
for _, name in { "clean", "repeated" } do
	for _, r in reads do
		local v = r[2](inputs[name])
		print(name .. "." .. r[1] .. "=" .. (if v == nil then "refused" else "read"))
	end
end
`

// repeatedKeyWires is the engine's own wire for two entries with distinct
// keys, and the same wire with the second key forged equal to the first: a
// repeated map key, which no writer produces.
func repeatedKeyWires(t *testing.T) (clean, repeated []byte) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(repeatedKeySchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var rep tabletext.Report
	if !m.Read(inst, []byte(`{"entries": {"19088743": 1, "19088744": 2}}`), &rep) {
		t.Fatalf("the engine does not read the two-entry text: %+v", rep)
	}
	clean, err = tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	second := []byte{0x68, 0x45, 0x23, 0x01}
	at := bytes.Index(clean, second)
	if at < 0 || bytes.Contains(clean[at+1:], second) {
		t.Fatalf("the second key is not in the wire exactly once: %x", clean)
	}
	repeated = append([]byte(nil), clean...)
	repeated[at] = 0x67
	// THE ORACLE: the engine reads the clean wire and refuses the repeat
	// (docs/plans/strict-only.md decision 5).
	for _, c := range []struct {
		name string
		wire []byte
		ok   bool
	}{{"clean", clean, true}, {"repeated", repeated, false}} {
		var r tabletext.Report
		got, err := tablewire.Decode(m, m.New(m.Lookup("Root")), c.wire, &r)
		if (got && err == nil) != c.ok {
			t.Fatalf("the engine's verdict on %s: read=%t err=%v, want read=%t", c.name, got, err, c.ok)
		}
	}
	return clean, repeated
}

func repeatedKeyRun(t *testing.T, from, to string) string {
	t.Helper()
	clean, repeated := repeatedKeyWires(t)
	g := generateFiles(t, map[string]string{"Probe.schema": repeatedKeySchema})
	if g.err != nil {
		t.Fatalf("the Luau unit does not generate: %v", g.err)
	}
	if from != "" {
		p := filepath.Join(g.dir, "schema_load.luau")
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(src), from) != 1 {
			t.Fatalf("the sabotage anchor %q does not occur exactly once: the runtime moved", from)
		}
		if err := os.WriteFile(p, []byte(strings.Replace(string(src), from, to, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(repeatedKeyProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"clean": clean, "repeated": repeated})))
}

const repeatedKeyWant = "clean.load=read\nclean.strict=read\nclean.retain=read\nrepeated.load=refused\nrepeated.strict=refused\nrepeated.retain=refused"

// TestLuauMapRepeatedKeyRefused: a repeated map key is damage, and every strict
// variable read refuses the whole value, as the engine does
// (docs/plans/strict-only.md decision 5; docs/SPEC-TABLES.md §2.8, §4). The
// clean wire, the engine's own, reads on every path.
func TestLuauMapRepeatedKeyRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	if out := repeatedKeyRun(t, "", ""); out != repeatedKeyWant {
		t.Fatalf("repeated map key:\n%s\nwant\n%s", out, repeatedKeyWant)
	}
}

// TestLuauMapRepeatedKeyRefusedNegativeControl drops the repeat from the strict
// read's refusing set and requires the row to go red.
func TestLuauMapRepeatedKeyRefusedNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	out := repeatedKeyRun(t, `"blobShared", "clamped", "kindMismatch", "duplicate" }`, `"blobShared", "clamped", "kindMismatch" }`)
	if out == repeatedKeyWant {
		t.Fatalf("the planted defect left the row green:\n%s", out)
	}
}
