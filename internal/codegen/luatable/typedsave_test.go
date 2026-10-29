package luatable

import (
	"fmt"
	"strings"
	"testing"
)

// THE TYPED SAVE (docs/PORTING.md M22). Every unit's module carries typed save
// bodies (typedsave.go) beside the descriptor walk save.Save is, and
// save.SaveTyped(T, name, value) writes through them. They must write the same
// bytes as the walk, every refusal included. The probe saves, both ways: every
// instance row of the unit after its load, every declared table at its
// constructor's defaults, and every declared table with each scalar leaf moved
// off its default in turn (both sides of the default elision), and a float
// leaf made a NaN (the refusal).

func typedProbe(module string, roots []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "local T = require(%q)\n", module)
	b.WriteString("local roots = {")
	for _, r := range roots {
		fmt.Fprintf(&b, "%q,", r)
	}
	b.WriteString(`}
local load, save = require("load"), require("save")
local inputs = require("inputs")
local agree, differ = 0, 0
local function both(what, root, v)
  local ok1, a, ar = pcall(save.Save, T, root, v)
  local ok2, b, br = pcall(save.SaveTyped, T, root, v)
  if ok1 == ok2 and a == b and ar == br then
    agree = agree + 1
  else
    differ = differ + 1
    if differ <= 3 then print("differ", what, root) end
  end
end
local function report()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
for i, root in ipairs(roots) do
  local v = load.LoadTolerant(T, root, inputs["w" .. i], report())
  if v ~= nil then both("instance " .. i, root, v) end
end
local names = {}
for name, d in pairs(T.Tables) do
  if d.table and not d.map_entry and T["New" .. name] then names[#names + 1] = name end
end
table.sort(names)
for _, name in ipairs(names) do
  local d = T.Tables[name]
  both("defaults", name, T["New" .. name]())
  for _, fd in ipairs(d.fields) do
    local v = T["New" .. name]()
    local moved = true
    if fd.shape == "bool" then
      v[fd.name] = not v[fd.name]
    elseif fd.shape == "int" and math.type(fd.default) == "integer" then
      v[fd.name] = fd.default + 1
    elseif fd.shape == "float" then
      v[fd.name] = fd.default + 1.5
    elseif fd.shape == "string" then
      v[fd.name] = (fd.default or "") .. "x"
    else
      moved = false
    end
    if moved then both("moved " .. fd.name, name, v) end
    if fd.shape == "float" then
      local n = T["New" .. name]()
      n[fd.name] = 0.0 / 0.0
      both("nan " .. fd.name, name, n)
    end
  end
end
print(agree, differ)
`)
	return b.String()
}

// typedRun runs the probe over files and answers (agree, differ, detail).
func typedRun(t *testing.T, b builtUnit, files map[string][]byte) (agree, differ int, detail string) {
	t.Helper()
	inputs, roots, _ := instanceWires(t, b.key)
	out, stderr, err := try(t, files, typedProbe(b.module, roots), inputs)
	if err != nil {
		t.Fatalf("unit %s: %v\n%s", b.key, err, stderr)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if _, err := fmt.Sscanf(lines[len(lines)-1], "%d %d", &agree, &differ); err != nil {
		t.Fatalf("unit %s: the probe printed %q", b.key, out)
	}
	return agree, differ, strings.Join(lines[:len(lines)-1], "; ")
}

func TestLuaTypedSaveEqualsDescriptorSave(t *testing.T) {
	t.Parallel()
	for _, key := range instanceUnits(t) {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			b := buildManifestUnit(t, key)
			agree, differ, detail := typedRun(t, b, b.files)
			if differ != 0 || agree == 0 {
				t.Fatalf("unit %s: typed and descriptor saves agree on %d and differ on %d: %s", key, agree, differ, detail)
			}
		})
	}
}

// TestLuaTypedSaveNegativeControl is the reversed witness: three plants into
// the typed bodies of tabledemo's module (the descriptor walk is untouched),
// a string leaf written under the wrong kind, an integer leaf written at the
// wrong width, and the default elision compared against the wrong value, each
// of which must turn the equivalence red.
func TestLuaTypedSaveNegativeControl(t *testing.T) {
	t.Parallel()
	b := buildManifestUnit(t, "tabledemo")
	module := b.module + ".lua"
	if agree, differ, detail := typedRun(t, b, b.files); differ != 0 || agree == 0 {
		t.Fatalf("the clean module does not agree: %d, %d: %s", agree, differ, detail)
	}
	for _, c := range []struct{ name, from, to string }{
		{"string leaf under the wrong kind", ", 12)\n        leb(w, #x)", ", 13)\n        leb(w, #x)"},
		{"integer leaf at the wrong width", "raw(w, pack(\"<i4\", x or 0))", "raw(w, pack(\"<i2\", x or 0))"},
		{"default elision against the wrong value", "      if x ~= 0 then\n", "      if x ~= 7 then\n"},
	} {
		files := b.patched(t, module, c.from, c.to)
		if _, differ, _ := typedRun(t, b, files); differ == 0 {
			t.Errorf("%s: the equivalence stayed green", c.name)
		}
	}
}

// TestLuaTypedSaveRunsFewerInstructions holds what the typed body is for
// without a clock: the Lua VM's own instruction count (debug.sethook, a count
// hook) over the same saves, typed against the descriptor walk, on a table of
// scalar leaves. The typed save must execute at most 70% of the walk's
// instructions.
func TestLuaTypedSaveRunsFewerInstructions(t *testing.T) {
	t.Parallel()
	b := buildManifestUnit(t, "tabledemo")
	out, stderr, err := try(t, b.files, `local T = require("`+b.module+`")
local save = require("save")
local v = T.NewRangedSigned()
for i, f in ipairs(T.Tables["RangedSigned"].fields) do
  if f.shape == "int" and math.type(f.default) == "integer" then v[f.name] = f.default + 1 end
end
local function steps(f)
  local n = 0
  debug.sethook(function() n = n + 1 end, "", 1)
  for _ = 1, 50 do f(T, "RangedSigned", v) end
  debug.sethook()
  return n
end
assert(save.Save(T, "RangedSigned", v) == save.SaveTyped(T, "RangedSigned", v))
print(steps(save.Save), steps(save.SaveTyped))
`, nil)
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, stderr)
	}
	var walk, typed int
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "%d %d", &walk, &typed); err != nil || walk == 0 {
		t.Fatalf("probe printed %q", out)
	}
	t.Logf("descriptor walk %d VM instructions, typed %d (%.0f%%)", walk, typed, 100*float64(typed)/float64(walk))
	if float64(typed) > 0.7*float64(walk) {
		t.Errorf("the typed save runs %d instructions against the walk's %d: not the fast path", typed, walk)
	}
}
