package luatable

import (
	"fmt"
	"strings"
	"testing"
)

// THE DESCRIPTORS ARE CONSTANTS (docs/PORTING.md M13). The generated module
// builds every derived descriptor fact once, at require, and freezes its data
// against new keys (descriptor.lua D.seal); Lua has no way to refuse the
// overwrite of an existing key without a proxy on every read, so the rest of
// the rule is this gate: every instance of every manifest unit goes through
// every runtime surface that reads a descriptor (load, strict load, measure,
// save, JSON text, cook, the fixed form), and the module's whole data tree,
// fingerprinted before and after, must be the same string. A lazy cache
// written into a descriptor raises where it is written; an overwrite changes
// the fingerprint.

// constantsProbe is the Lua main of one unit: it reaches every surface the
// unit's runtime carries, and prints "constant" when the descriptor tree is
// byte for byte what it was at require.
func constantsProbe(module string, roots []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "local T = require(%q)\n", module)
	b.WriteString(`local D = require("descriptor")
local inputs = require("inputs")
local function opt(name)
  local ok, m = pcall(require, name)
  if ok then return m end
  return nil
end
local load, save = require("load"), require("save")
local json, cookwrite, text = opt("json"), opt("cookwrite"), opt("text")
local before = D.fingerprint(T)
-- a surface may refuse a wire; it may not write into a descriptor
local function try(f, ...)
  local ok, err = pcall(f, ...)
  if not ok and tostring(err):find("a descriptor is a constant", 1, true) then error(err, 0) end
  return ok, err
end
local function report()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0,
    widened = 0, duplicate = 0, blobShared = 0, missingRequired = 0, wireDamaged = false }
end
`)
	b.WriteString("local roots = {")
	for _, r := range roots {
		fmt.Fprintf(&b, "%q,", r)
	}
	b.WriteString(`}
for i, root in ipairs(roots) do
  local wire = inputs["w" .. i]
  local v = load.LoadTolerant(T, root, wire, report())
  try(load.Load, T, root, wire, report())
  try(load.LoadMeasure, T, root, wire)
  if T.ReadFixed then try(T.ReadFixed, root, wire) end
  if v ~= nil then
    try(save.Save, T, root, v)
    try(save.Measure, T, root, v)
    if json then try(json.ToJson, T, root, v) end
    if text then try(text.SaveText, T, root, v) end
    if cookwrite then
      try(cookwrite.CookMeasure, T, root, v)
      try(cookwrite.Cook, T, root, v)
    end
    if T.WriteFixed then try(T.WriteFixed, root, v) end
  end
end
if D.fingerprint(T) ~= before then print("changed") else print("constant") end
`)
	return b.String()
}

func TestLuaDescriptorsAreConstants(t *testing.T) {
	t.Parallel()
	for _, key := range instanceUnits(t) {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			b := buildManifestUnit(t, key)
			inputs, roots, _ := instanceWires(t, key)
			probe := constantsProbe(b.module, roots)
			out, stderr, err := try(t, b.files, probe, inputs)
			if err != nil || strings.TrimSpace(out) != "constant" {
				t.Fatalf("unit %s: a runtime surface changed a descriptor: %q (%v)\n%s", key, out, err, stderr)
			}
		})
	}
}

// TestLuaDescriptorsAreSealed holds the seal itself: a new key written into
// any descriptor table raises, the metatable is not removable, and what the
// module derived at require (a table's pointer reachability, the type-id
// index) is already there, built before anything is read.
func TestLuaDescriptorsAreSealed(t *testing.T) {
	t.Parallel()
	b := buildManifestUnit(t, "tabledemo")
	out, stderr, err := try(t, b.files, `local T = require("`+b.module+`")
local function raises(f)
  local ok, err = pcall(f)
  return not ok and tostring(err):find("a descriptor is a constant", 1, true) ~= nil
end
local desc = T.Tables["KeyedConfig"]
print("new key in a table descriptor", raises(function() desc.cache = {} end))
print("new key in a field descriptor", raises(function() desc.fields[1].cache = 1 end))
print("new key in the union descriptors", raises(function() T.Unions["Effect"].arms[1].x = 1 end))
print("new key in the registry", raises(function() T.Registry.x = 1 end))
print("new key in the wire ids", raises(function() T.WireIds[1] = true end))
print("the seal cannot be lifted", getmetatable(desc) == false and not pcall(setmetatable, desc, nil))
print("reachability is built", desc.reach ~= nil and desc.reach.nodes ~= nil)
print("the type-id index is built", T.TypeIds[desc.type_id] == desc)
`, nil)
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, stderr)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if !strings.HasSuffix(line, "\ttrue") {
			t.Errorf("sealed gate: %s", line)
		}
	}
}

// TestLuaDescriptorsConstantNegativeControl is the reversed witness: two
// plants into the emitted runtime, each of which the gate above must catch.
// The first is the banned plain-cache idiom (a derived table written back
// into the descriptor on first use, the form the Go soak found); the second
// overwrites a descriptor's value, which the seal cannot refuse and only the
// fingerprint sees.
func TestLuaDescriptorsConstantNegativeControl(t *testing.T) {
	t.Parallel()
	b := buildManifestUnit(t, "tabledemo")
	inputs, roots, _ := instanceWires(t, "tabledemo")
	probe := constantsProbe(b.module, roots)
	anchor := "  local desc = T.Tables[name]\n  report.nonFiniteFloat = nil\n"
	cases := []struct {
		name  string
		plant string
		want  func(out, stderr string, err error) bool
	}{
		{
			name:  "lazy cache written into a descriptor",
			plant: anchor + "  if desc ~= nil and desc.lazy == nil then desc.lazy = {} end\n",
			want: func(_, stderr string, err error) bool {
				return err != nil && strings.Contains(stderr, "a descriptor is a constant")
			},
		},
		{
			name:  "descriptor value overwritten",
			plant: anchor + "  if desc ~= nil then desc.size = desc.size + 1 end\n",
			want: func(out string, _ string, err error) bool {
				return err == nil && strings.TrimSpace(out) == "changed"
			},
		},
	}
	for _, c := range cases {
		files := b.patched(t, "load.lua", anchor, c.plant)
		out, stderr, err := try(t, files, probe, inputs)
		if !c.want(out, stderr, err) {
			t.Errorf("negative control %q: the gate did not go red: out=%q err=%v\n%s", c.name, out, err, stderr)
		}
	}
	// and the gate REACHES every surface it claims: an overwrite planted at
	// each entry point turns the fingerprint, so a surface the probe never
	// calls would leave its plant green and be named here
	bump := "T.Tables[name].size = T.Tables[name].size + 1\n"
	entries := []struct{ file, anchor string }{
		{"load.lua", "function L.LoadMeasure(T, name, bytes)\n"},
		{"load.lua", "function L.Load(T, name, bytes, report)\n"},
		{"save.lua", "function S.Save(T, name, v)\n"},
		{"json.lua", "function J.ToJson(T, name, v)\n"},
		{"text.lua", "function Text.SaveText(T, name, v)\n"},
		{"cookwrite.lua", "function C.Cook(T, name, v, capacity, order)\n"},
		{"fixed.lua", "function F.write(T, name, value)\n"},
	}
	for _, e := range entries {
		files := b.patched(t, e.file, e.anchor, e.anchor+bump)
		out, stderr, err := try(t, files, probe, inputs)
		if err != nil || strings.TrimSpace(out) != "changed" {
			t.Errorf("the descriptor gate never reaches %s %q: out=%q err=%v\n%s", e.file, strings.TrimSpace(e.anchor), out, err, stderr)
		}
	}
}

// TestLuaRuntimeBuildsNoDescriptorOnFirstUse holds the two sites the audit
// found building a constant lazily, the plain-cache idiom M13 bans by name:
// the CRC-32C table (built on first use inside crc32c) and the fixed form's
// layout tree (built on the first read of a known layout and stored back into
// the descriptor). Both are built when the file or the module is required.
func TestLuaRuntimeBuildsNoDescriptorOnFirstUse(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := runtime.ReadFile("runtime/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if load := read("load.lua"); strings.Contains(load, "local crc_table = nil") || strings.Contains(load, "if crc_table == nil") {
		t.Error("load.lua builds the CRC-32C table on first use")
	}
	fixed := read("fixed.lua")
	if strings.Contains(fixed, "if entry.tree == nil then") {
		t.Error("fixed.lua builds a known layout's tree on first read and stores it back into the descriptor")
	}
	if strings.Count(fixed, "entry.tree = ") != 1 || !strings.Contains(fixed, "function F.prepare(T)") {
		t.Error("the layout tree must be assigned once, in F.prepare")
	}
}
