package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// These functional tests hold general arms to the same descriptor contract as
// ordinary fields, including columns the compact registry listing omits.
const generalArmSchema = `package armmeta
fixed table Node { value int32 }
enum Mode { A, B }
union Choice
{
 scalar int32 | min = -9, max = 17
 text string(12)
 values [..3]int32
 pointer *Node
 small int16
 mode Mode
}
table Root { choice Choice }
`

const generalArmProbe = `local V = require("ArmmetaView")
local arms = {}
for _, u in ipairs(V.UnitView().unions) do
  if u.name == "Choice" then
    for _, arm in ipairs(u.variants) do if arm.field then arms[arm.name] = arm.field end end
  end
end
%s
local expected = {
 scalar = { shape = "int", width = 4, signed = true, lo = -9, hi = 17, elem_size = 4, counted = false, optional = false, present = "", at = 0, arm_offset = 8 },
 text = { shape = "string", bound = 12, array_bound = 12, elem_size = 1, counted = true, companion = 16, at = 0, arm_offset = 8 },
 values = { shape = "array", bound = 3, array_bound = 3, elem_size = 4, counted = true, companion = 12, elem_step = 4, at = 0, arm_offset = 8 },
 pointer = { shape = "pointer", target = "Node", counted = false, at = 0, arm_offset = 8 },
 small = { shape = "int", width = 2, signed = true, optional = false, present = "", at = 0, arm_offset = 8 },
 mode = { shape = "enum", enum = "Mode", counted = false, at = 0, arm_offset = 8 },
}
local red = {}
for name, columns in pairs(expected) do
  local f = arms[name] or {}
  for key, want in pairs(columns) do
    if f[key] ~= want then red[#red + 1] = name .. "." .. key .. ": got " .. tostring(f[key]) .. ", want " .. tostring(want) end
  end
end
local e = arms.values and arms.values.elem or {}
if e.shape ~= "int" or e.width ~= 4 or e.signed ~= true then red[#red + 1] = "values.elem: missing scalar descriptor" end
table.sort(red)
for _, line in ipairs(red) do print(line) end
`

func generalArmGate(t *testing.T, plant string) []string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Armmeta.schema")
	if err := os.WriteFile(path, []byte(generalArmSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	src, ok := ViewFile(u)
	if !ok {
		t.Fatal("no view")
	}
	files[ViewName(u)+".lua"] = []byte(src)
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(fmt.Sprintf(generalArmProbe, plant)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", nil)))
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func TestLuaUnitViewGeneralArmDescriptorsFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 descriptor probe")
	for _, line := range generalArmGate(t, "") {
		t.Error(line)
	}
}

func TestLuaUnitViewGeneralArmDescriptorsNegativeControlFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 descriptor probe")
	for _, tc := range []struct{ name, plant, red string }{
		{"width", "arms.scalar.width = 8", "scalar.width:"},
		{"range", "arms.scalar.lo = -10", "scalar.lo:"},
		{"capacity", "arms.text.array_bound = 11", "text.array_bound:"},
		{"companion", "arms.values.companion = 8", "values.companion:"},
		{"presence", "arms.small.present = nil", "small.present:"},
		{"pointer", "arms.pointer.target = nil", "pointer.target:"},
		{"element", "arms.values.elem.width = 8", "values.elem:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			red := generalArmGate(t, tc.plant)
			if len(red) != 1 || !strings.HasPrefix(red[0], tc.red) {
				t.Fatalf("plant %s: red %q, want only %s", tc.name, red, tc.red)
			}
			t.Logf("broken twin: %s", red[0])
		})
	}
}
