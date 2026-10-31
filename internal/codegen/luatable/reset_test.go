package luatable

import (
	"fmt"
	"strings"
	"testing"
)

// THE DEFAULTS IN PLACE (docs/PORTING.md M1). A caller-owned load puts the
// caller's value back to the declared defaults in place and reads into it, so
// D.reset must leave a value exactly what the module's New<Name>() builds,
// whatever the value held: this probe takes every declared table of a unit,
// dirties a constructed value and a loaded instance everywhere (every scalar
// moved, every list and counted array grown, every union holding an arm, every
// map holding an entry), resets it, and requires it to be deep-equal to the
// constructor's, and the nested tables and unions of a value that already had
// the shape to be the SAME tables (nothing allocated for them).

func resetProbe(module string, roots []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "local T = require(%q)\n", module)
	b.WriteString("local roots = {")
	for _, r := range roots {
		fmt.Fprintf(&b, "%q,", r)
	}
	b.WriteString(`}
local D, load = require("descriptor"), require("load")
local inputs = require("inputs")
local function deep(a, b, path)
  if type(a) ~= type(b) then return path .. ": type " .. type(a) .. " vs " .. type(b) end
  if type(a) ~= "table" then
    if a ~= b then return path .. ": " .. tostring(a) .. " vs " .. tostring(b) end
    return nil
  end
  for k, v in pairs(a) do
    local err = deep(v, b[k], path .. "." .. tostring(k))
    if err then return err end
  end
  for k in pairs(b) do
    if a[k] == nil then return path .. "." .. tostring(k) .. ": absent vs present" end
  end
  return nil
end
-- every scalar moved, every sequence grown, every map and union holding
-- something, every nested table dirtied the same way
local function dirty(name, v, depth)
  if depth > 8 then return end
  for _, fd in ipairs(T.Tables[name].fields) do
    local s = fd.shape
    local x = v[fd.name]
    if s == "bool" then v[fd.name] = not (x == true)
    elseif s == "int" or s == "flags" then v[fd.name] = (math.type(x) == "integer" and x or 0) + 1
    elseif s == "float" then v[fd.name] = (type(x) == "number" and x or 0.0) + 1.5
    elseif s == "string" or s == "bytes" then v[fd.name] = (type(x) == "string" and x or "") .. "x"
    elseif s == "wide" then v[fd.name] = { lo = 5, hi = 6 }
    elseif s == "wstring" then v[fd.name] = { 65, 66 }
    elseif s == "enum" then v[fd.name] = "NoSuchVariant"
    elseif s == "map" then v[fd.name] = { [1] = 1, junk = 2 }
    elseif s == "array" then
      local e, seq = fd.elem, {}
      for i = 1, 4 do
        if e.shape == "table" then
          seq[i] = T["New" .. e.target]()
          dirty(e.target, seq[i], depth + 1)
        elseif e.shape == "union" then
          seq[i] = { tag = 1, arm = i }
        elseif e.shape == "string" or e.shape == "bytes" then
          seq[i] = "junk"
        elseif e.shape == "wide" then
          seq[i] = { lo = i, hi = i }
        elseif e.shape == "wstring" then
          seq[i] = { 65 }
        else
          seq[i] = i
        end
      end
      v[fd.name] = seq
    elseif s == "union" then v[fd.name] = { tag = 1, arm = { 1, 2 }, other = 3 }
    elseif s == "table" then
      if type(x) ~= "table" then x = T["New" .. fd.target]() v[fd.name] = x end
      dirty(fd.target, x, depth + 1)
    elseif s == "pointer" or s == "string_blob" or s == "bytes_blob" then v[fd.name] = { value = 1 } end
    if fd.optional then v[fd.pkey] = true end
  end
end
local checked, same = 0, 0
local function check(name, v)
  local before = {}
  for k, x in pairs(v) do if type(x) == "table" then before[k] = x end end
  D.reset(T, T.Tables[name], v)
  checked = checked + 1
  local err = deep(v, T["New" .. name](), name)
  if err then print("RESET DIFFERS", err) return end
  -- a nested table, union or fixed array that was there is still the same table
  for k, x in pairs(before) do
    if v[k] == x then same = same + 1 end
  end
end
local names = {}
for name, d in pairs(T.Tables) do
  if T["New" .. name] then names[#names + 1] = name end
end
table.sort(names)
for _, name in ipairs(names) do
  local v = T["New" .. name]()
  dirty(name, v, 0)
  check(name, v)
  check(name, T["New" .. name]())
end
for i, root in ipairs(roots) do
  local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
  local v = load.LoadTolerant(T, root, inputs["w" .. i], report)
  if v ~= nil then
    dirty(root, v, 0)
    check(root, v)
  end
end
print("checked", checked, same)
`)
	return b.String()
}

func TestLuaResetEqualsTheConstructor(t *testing.T) {
	t.Parallel()
	for _, key := range instanceUnits(t) {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			b := buildManifestUnit(t, key)
			inputs, roots, _ := instanceWires(t, key)
			out, stderr, err := try(t, b.files, resetProbe(b.module, roots), inputs)
			if err != nil {
				t.Fatalf("unit %s: %v\n%s", key, err, stderr)
			}
			var checked, same int
			last := out[strings.LastIndex(out, "checked"):]
			if _, err := fmt.Sscanf(last, "checked %d %d", &checked, &same); err != nil || strings.Contains(out, "RESET DIFFERS") {
				t.Fatalf("unit %s: %s", key, out)
			}
			// a value that already had the shape keeps its nested tables, unions
			// and fixed arrays: nothing is allocated for them
			if key == "tabledemo" && same == 0 {
				t.Errorf("tabledemo: reset replaced every nested table and array it was handed (%d checks)", checked)
			}
		})
	}
}
