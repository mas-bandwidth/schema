package luatable

import (
	"fmt"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

func TestLuaUnopenedDamageDiscardsTheRoot(t *testing.T) {
	t.Parallel()
	b, _ := armUnit(t)
	for _, wire := range [][]byte{nil, {1}, {1, 0, 0}} {
		out, stderr, err := try(t, b.files, agProbe(b.module, "Holder"), map[string][]byte{"wire": wire})
		if err != nil {
			t.Fatalf("probe: %v: %s", err, stderr)
		}
		got, err := agParseLine(out)
		if err != nil || !got.counts.malformed || got.loaded {
			t.Fatalf("damage returned a root: %q (%v)", out, err)
		}
	}
}

// Remove the runtime discard guards, retaining the malformed verdict and the
// same fixture and assertions. The guards must be what prevents partial values.
func TestLuaDamageDiscardNegativeControl(t *testing.T) {
	t.Parallel()
	b, u := armUnit(t)
	for name, why := range armVerdicts(t, b, u, b.files) {
		if why != "" {
			t.Fatalf("control baseline %s: %s", name, why)
		}
	}
	files := maps.Clone(b.files)
	const final = "  release(ctx)\n  if report.malformed then return nil end\n  return root"
	files["load.lua"] = luaDiscardPatch(t, files["load.lua"], final, "  release(ctx)\n  return root")
	red := 0
	for _, why := range armVerdicts(t, b, u, files) {
		if why != "" {
			red++
		}
	}
	if red != 5 {
		t.Fatalf("removing the final discard exposed %d damaged-arm rows, want 5", red)
	}

	files = maps.Clone(b.files)
	const unopened = "  if o == nil then\n    if report.malformed then return nil end\n"
	files["load.lua"] = luaDiscardPatch(t, files["load.lua"], unopened, "  if o == nil then\n")
	out, stderr, err := try(t, files, agProbe(b.module, "Holder"), map[string][]byte{"wire": nil})
	if err != nil {
		t.Fatalf("unopened control: %v: %s", err, stderr)
	}
	got, err := agParseLine(out)
	if err != nil || !got.counts.malformed || !got.loaded {
		t.Fatalf("removing the open discard did not expose a root: %q (%v)", out, err)
	}
}

func luaDiscardPatch(t *testing.T, src []byte, from, to string) []byte {
	t.Helper()
	if strings.Count(string(src), from) != 1 {
		t.Fatal("runtime sabotage anchor must occur exactly once")
	}
	return []byte(strings.Replace(string(src), from, to, 1))
}

func TestLuaMessageDamageDiscardNegativeControl(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	units, conns, _ := manifestMessageData(t, filepath.Join(repo, "testdata/conformance/tables/MANIFEST.txt"))
	conn := conns["backend_conn"]
	var paths []string
	for _, p := range units[conn.unit] {
		paths = append(paths, filepath.Join(repo, p))
	}
	unit := luarun.LoadUnit(t, paths...)
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	probe := fmt.Sprintf(`local T = require(%q)
local M = require("message")
local inputs = require("inputs")
local report = { malformed = false, refused = 0 }
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
assert(T.AnnounceRead(vocab, inputs.announce, report))
local values = M.loadMessages(T, "LoginRequest", vocab, inputs.wire, report, false)
print(tostring(report.malformed) .. "|" .. tostring(values and #values or 0))
`, ModuleName(unit))
	ann := readFile(t, filepath.Join(repo, conn.ann))
	for _, tc := range []struct {
		name, wire, from, to string
		trailing             bool
	}{
		{"body", "backend_batch_damaged_second_message.bin", "      report.malformed = true\n      return {}", "      report.malformed = true\n      return values", false},
		{"tail", "login_full_message.bin", "  if report.malformed then return {} end\n", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := readFile(t, filepath.Join(repo, "testdata/wire/tables", tc.wire))
			if tc.trailing {
				wire = append(wire, 0)
			}
			inputs := map[string][]byte{"announce": ann, "wire": wire}
			out, stderr, err := try(t, files, probe, inputs)
			if err != nil || strings.TrimSpace(out) != "true|0" {
				t.Fatalf("baseline %q: %v: %s", out, err, stderr)
			}
			broken := maps.Clone(files)
			broken["message.lua"] = luaDiscardPatch(t, broken["message.lua"], tc.from, tc.to)
			out, stderr, err = try(t, broken, probe, inputs)
			if err != nil || strings.TrimSpace(out) != "true|1" {
				t.Fatalf("discard sabotage did not expose a body: %q: %v: %s", out, err, stderr)
			}
		})
	}
}
