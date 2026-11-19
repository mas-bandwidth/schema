package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func verifyJS(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateCanonical(repoRoot, schemaBin, "", "js", genDir); err != nil {
		return err
	}
	serJS := resolveExistingDir(repoRoot, "SERIALIZE_JS", "../serialize.js", "build/serialize.js")
	pkgJSON := fmt.Sprintf("{\n  \"type\": \"module\",\n  \"dependencies\": {\n    \"serialize\": \"file:%s\"\n  }\n}\n", serJS)
	if err := os.WriteFile(filepath.Join(workDir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		return err
	}
	nmDir := filepath.Join(workDir, "node_modules")
	if err := os.MkdirAll(nmDir, 0o755); err != nil {
		return err
	}
	if err := os.Symlink(serJS, filepath.Join(nmDir, "serialize")); err != nil && !os.IsExist(err) {
		return err
	}
	jsSrc := `import fs from "node:fs";
import path from "node:path";
import * as CV from "./gen/CanonicalVariable.js";

const dir = process.argv[2];
const wantWire = new Uint8Array(fs.readFileSync(path.join(dir, "canonical.bin")));
const wantJson = new Uint8Array(fs.readFileSync(path.join(dir, "canonical.json")));

function bytesEq(a, b) {
  if (!a || a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}

function reorder(arr, ord) { return ord.map(i => arr[i]); }

function buildNative(mode) {
  const ord8 = [[0,1,2,3,4,5,6,7], [7,6,5,4,3,2,1,0], [7,4,2,6,0,5,1,3]][mode];
  const ord5 = [[0,1,2,3,4], [4,3,2,1,0], [2,4,0,3,1]][mode];
  const ord5b = [[0,1,2,3,4], [4,3,2,1,0], [3,0,4,2,1]][mode];
  const ord7 = [[0,1,2,3,4,5,6], [6,5,4,3,2,1,0], [5,3,0,6,2,4,1]][mode];
  const ord3 = [[0,1,2], [2,1,0], [1,2,0]][mode];
  const ord4 = [[0,1,2,3], [3,2,1,0], [2,0,3,1]][mode];
  const ord2 = [[0,1], [1,0], [1,0]][mode];

  const b = new CV.Bundle();
  for (const [k, v] of reorder([
    ["", 10], ["Z", -5], ["a", 20], ["ab", 0], ["abc", -7],
    ["long-unbounded-key-alpha", 12345], ["\uff61", 111], ["\u{1f600}", 222]
  ], ord8)) b.by_name.set(k, v);

  for (const [k, v] of reorder([
    ["Alpha", 1], ["a", 5], ["ab", 4], ["beta", 2], ["gamma", 3]
  ], ord5)) b.by_code.set(k, v);

  const mkChild = (lbl, sc) => { const c = new CV.Child(); c.label = lbl; c.score = sc; return c; };
  for (const [k, lbl, sc] of reorder([
    [-9000000000000n, "min-far", -99], [-1n, "minus-one", -1], [0n, "", 0],
    [2n, "two", 2], [9000000000000n, "max-far", 99]
  ], ord5b)) b.by_id.set(k, mkChild(lbl, sc));

  for (const [k, v] of reorder([
    [0n, 0], [1n, 11], [255n, 2550], [256n, 2560],
    [9223372036854775807n, 700], [9223372036854775808n, 800], [18446744073709551614n, 900]
  ], ord7)) b.by_uid.set(k, v);

  b.by_tier = [100, 200, 300, 400];

  const m0 = new Map(reorder([[-100, -10], [0, 0], [7, 70], [42, 420]], ord4));
  const m1 = new Map(reorder([[-5, 55], [12, 120]], ord2));
  for (const [k, m] of reorder([["outer-a", m0], ["outer-m", m1], ["outer-z", new Map()]], ord3)) {
    b.nested.set(k, m);
  }

  b.text_a = ["shared-", "blob"].join("");
  b.text_b = ["shared-", "blob"].join("");
  b.blob_a = new Uint8Array([0xde, 0xad, 0xbe, 0xef]);
  b.blob_b = new Uint8Array([0xde, 0xad, 0xbe, 0xef]);
  return b;
}

function checkBundle(b, tag) {
  const w = CV.BundleSave(b);
  if (!bytesEq(w, wantWire)) throw new Error("FAILED: wire " + tag);
  const j = CV.BundleToJson(b);
  const jb = typeof j === "string" ? new Uint8Array(Buffer.from(j, "utf8")) : j;
  if (!bytesEq(jb, wantJson)) throw new Error("FAILED: json " + tag);
}

for (const jf of ["json_sorted.json", "json_reverse.json", "json_shuffled.json"]) {
  const jraw = fs.readFileSync(path.join(dir, jf), "utf8");
  const rep = new CV.TableVariableReport();
  const b = CV.BundleFromJson(jraw, rep);
  if (!b || rep.damaged || rep.refused) throw new Error("FAILED: FromJson " + jf);
  checkBundle(b, jf);
}
for (let m = 0; m < 3; m++) {
  checkBundle(buildNative(m), "native_" + m);
}
console.log("CANONICAL_OK js 6");
`
	probe := filepath.Join(workDir, "main.mjs")
	if err := os.WriteFile(probe, []byte(jsSrc), 0o644); err != nil {
		return err
	}
	node := resolveTool(repoRoot, "NODE", "dist/node-v25.4.0/bin/node", "node")
	out, err := runChecked(workDir, nil, node, probe, corpusDir)
	return requireToken(out, err, "js")
}

func verifyPython(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateCanonical(repoRoot, schemaBin, "", "python", genDir); err != nil {
		return err
	}
	pySrc := `import pathlib, sys
sys.path.insert(0, "./gen")
import canonical as c

d = pathlib.Path(sys.argv[1])
want_wire = (d / "canonical.bin").read_bytes()
want_json = (d / "canonical.json").read_bytes()

def reorder(items, ord_idx):
    return [items[i] for i in ord_idx]

def build_native(mode: int) -> c.Bundle:
    ord8 = [[0,1,2,3,4,5,6,7], [7,6,5,4,3,2,1,0], [7,4,2,6,0,5,1,3]][mode]
    ord5 = [[0,1,2,3,4], [4,3,2,1,0], [2,4,0,3,1]][mode]
    ord5b = [[0,1,2,3,4], [4,3,2,1,0], [3,0,4,2,1]][mode]
    ord7 = [[0,1,2,3,4,5,6], [6,5,4,3,2,1,0], [5,3,0,6,2,4,1]][mode]
    ord3 = [[0,1,2], [2,1,0], [1,2,0]][mode]
    ord4 = [[0,1,2,3], [3,2,1,0], [2,0,3,1]][mode]
    ord2 = [[0,1], [1,0], [1,0]][mode]

    b = c.Bundle()
    for k, v in reorder([
        ("", 10), ("Z", -5), ("a", 20), ("ab", 0), ("abc", -7),
        ("long-unbounded-key-alpha", 12345), ("\uff61", 111), ("\U0001f600", 222)
    ], ord8):
        b.by_name[k] = v

    for k, v in reorder([
        ("Alpha", 1), ("a", 5), ("ab", 4), ("beta", 2), ("gamma", 3)
    ], ord5):
        b.by_code[k] = v

    def mk_child(lbl, sc):
        ch = c.Child()
        ch.label = lbl
        ch.score = sc
        return ch

    for k, lbl, sc in reorder([
        (-9000000000000, "min-far", -99), (-1, "minus-one", -1), (0, "", 0),
        (2, "two", 2), (9000000000000, "max-far", 99)
    ], ord5b):
        b.by_id[k] = mk_child(lbl, sc)

    for k, v in reorder([
        (0, 0), (1, 11), (255, 2550), (256, 2560),
        (9223372036854775807, 700), (9223372036854775808, 800), (18446744073709551614, 900)
    ], ord7):
        b.by_uid[k] = v

    b.by_tier[c.Tier.Bronze] = 100
    b.by_tier[c.Tier.Silver] = 200
    b.by_tier[c.Tier.Gold] = 300
    b.by_tier[c.Tier.Platinum] = 400

    m0 = dict(reorder([(-100, -10), (0, 0), (7, 70), (42, 420)], ord4))
    m1 = dict(reorder([(-5, 55), (12, 120)], ord2))
    for k, inner in reorder([("outer-a", m0), ("outer-m", m1), ("outer-z", {})], ord3):
        b.nested[k] = inner

    s1 = "".join(["shared-", "blob"])
    s2 = "".join(["shared-", "blob"])
    assert s1 is not s2, "FAILED: s1 is s2"
    b.text_a = s1
    b.text_b = s2

    b1 = bytes(bytearray([0xde, 0xad, 0xbe, 0xef]))
    b2 = bytes(bytearray([0xde, 0xad, 0xbe, 0xef]))
    assert b1 is not b2, "FAILED: b1 is b2"
    b.blob_a = b1
    b.blob_b = b2
    return b

def check_bundle(b: c.Bundle, tag: str) -> None:
    w = b.save()
    if w != want_wire:
        raise AssertionError(f"FAILED: wire {tag}")
    j = b.to_json().encode("utf-8")
    if j != want_json:
        raise AssertionError(f"FAILED: json {tag}")

for jf in ("json_sorted.json", "json_reverse.json", "json_shuffled.json"):
    jraw = (d / jf).read_text(encoding="utf-8")
    b = c.Bundle.from_json(jraw)
    check_bundle(b, jf)

for m in range(3):
    check_bundle(build_native(m), f"native_{m}")

print("CANONICAL_OK python 6")
`
	probe := filepath.Join(workDir, "main.py")
	if err := os.WriteFile(probe, []byte(pySrc), 0o644); err != nil {
		return err
	}
	py := resolveGlobTool(repoRoot, "PYTHON3", "dist/python-*/bin/python3", envOr("PYTHON", "python3"))
	out, err := runChecked(workDir, nil, py, probe, corpusDir)
	return requireToken(out, err, "python")
}

func verifyRuby(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateCanonical(repoRoot, schemaBin, "", "ruby", genDir); err != nil {
		return err
	}
	rbSrc := `$LOAD_PATH.unshift(File.expand_path("./gen", __dir__))
require "canonical"

dir = ARGV[0]
want_wire = File.binread(File.join(dir, "canonical.bin"))
want_json = File.binread(File.join(dir, "canonical.json"))

def reorder(items, ord)
  ord.map { |i| items[i] }
end

def build_native(mode)
  ord8 = [[0,1,2,3,4,5,6,7], [7,6,5,4,3,2,1,0], [7,4,2,6,0,5,1,3]][mode]
  ord5 = [[0,1,2,3,4], [4,3,2,1,0], [2,4,0,3,1]][mode]
  ord5b = [[0,1,2,3,4], [4,3,2,1,0], [3,0,4,2,1]][mode]
  ord7 = [[0,1,2,3,4,5,6], [6,5,4,3,2,1,0], [5,3,0,6,2,4,1]][mode]
  ord3 = [[0,1,2], [2,1,0], [1,2,0]][mode]
  ord4 = [[0,1,2,3], [3,2,1,0], [2,0,3,1]][mode]
  ord2 = [[0,1], [1,0], [1,0]][mode]

  b = Canonical::Bundle.new
  reorder([
    ["", 10], ["Z", -5], ["a", 20], ["ab", 0], ["abc", -7],
    ["long-unbounded-key-alpha", 12345], ["\uff61", 111], ["\u{1f600}", 222]
  ], ord8).each { |k, v| b.by_name[k] = v }

  reorder([
    ["Alpha", 1], ["a", 5], ["ab", 4], ["beta", 2], ["gamma", 3]
  ], ord5).each { |k, v| b.by_code[k] = v }

  reorder([
    [-9000000000000, "min-far", -99], [-1, "minus-one", -1], [0, "", 0],
    [2, "two", 2], [9000000000000, "max-far", 99]
  ], ord5b).each do |k, lbl, sc|
    ch = Canonical::Child.new
    ch.label = lbl
    ch.score = sc
    b.by_id[k] = ch
  end

  reorder([
    [0, 0], [1, 11], [255, 2550], [256, 2560],
    [9223372036854775807, 700], [9223372036854775808, 800], [18446744073709551614, 900]
  ], ord7).each { |k, v| b.by_uid[k] = v }

  b.by_tier[Canonical::Tier::Bronze] = 100
  b.by_tier[Canonical::Tier::Silver] = 200
  b.by_tier[Canonical::Tier::Gold] = 300
  b.by_tier[Canonical::Tier::Platinum] = 400

  m0 = {}
  reorder([[-100, -10], [0, 0], [7, 70], [42, 420]], ord4).each { |k, v| m0[k] = v }
  m1 = {}
  reorder([[-5, 55], [12, 120]], ord2).each { |k, v| m1[k] = v }
  reorder([["outer-a", m0], ["outer-m", m1], ["outer-z", {}]], ord3).each { |k, inner| b.nested[k] = inner }

  s1 = ["shared-", "blob"].join
  s2 = ["shared-", "blob"].join
  raise "FAILED: s1.equal?(s2)" if s1.equal?(s2)
  b.text_a = s1
  b.text_b = s2

  b1 = [0xde, 0xad, 0xbe, 0xef].pack("C*")
  b2 = [0xde, 0xad, 0xbe, 0xef].pack("C*")
  raise "FAILED: b1.equal?(b2)" if b1.equal?(b2)
  b.blob_a = b1
  b.blob_b = b2
  b
end

def check_bundle(b, want_wire, want_json, tag)
  w = b.save
  raise "FAILED: wire #{tag}" unless w == want_wire
  j = b.to_json.b
  raise "FAILED: json #{tag}" unless j == want_json
end

["json_sorted.json", "json_reverse.json", "json_shuffled.json"].each do |jf|
  jraw = File.read(File.join(dir, jf), encoding: "UTF-8")
  b = Canonical::Bundle.from_json(jraw)
  check_bundle(b, want_wire, want_json, jf)
end

(0..2).each do |m|
  check_bundle(build_native(m), want_wire, want_json, "native_#{m}")
end

puts "CANONICAL_OK ruby 6"
`
	probe := filepath.Join(workDir, "main.rb")
	if err := os.WriteFile(probe, []byte(rbSrc), 0o644); err != nil {
		return err
	}
	rb := resolveGlobTool(repoRoot, "RUBY", "dist/ruby-*/bin/ruby", "ruby")
	out, err := runChecked(workDir, nil, rb, probe, corpusDir)
	return requireToken(out, err, "ruby")
}

func verifyLua(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateCanonical(repoRoot, schemaBin, "", "lua", genDir); err != nil {
		return err
	}
	luaSrc := `package.path = "./gen/?.lua;" .. package.path
local T = require("CanonicalTables")
local J = require("json")
local S = require("save")

local dir = arg[1]
local function read_all(p)
  local f = assert(io.open(p, "rb"))
  local d = f:read("*a")
  f:close()
  return d
end

local want_wire = read_all(dir .. "/canonical.bin")
local want_json = read_all(dir .. "/canonical.json")

local function reorder(items, ord)
  local out = {}
  for i = 1, #ord do out[i] = items[ord[i] + 1] end
  return out
end

local function build_native(mode)
  local ord8 = ({ {0,1,2,3,4,5,6,7}, {7,6,5,4,3,2,1,0}, {7,4,2,6,0,5,1,3} })[mode + 1]
  local ord5 = ({ {0,1,2,3,4}, {4,3,2,1,0}, {2,4,0,3,1} })[mode + 1]
  local ord5b = ({ {0,1,2,3,4}, {4,3,2,1,0}, {3,0,4,2,1} })[mode + 1]
  local ord7 = ({ {0,1,2,3,4,5,6}, {6,5,4,3,2,1,0}, {5,3,0,6,2,4,1} })[mode + 1]
  local ord3 = ({ {0,1,2}, {2,1,0}, {1,2,0} })[mode + 1]
  local ord4 = ({ {0,1,2,3}, {3,2,1,0}, {2,0,3,1} })[mode + 1]
  local ord2 = ({ {0,1}, {1,0}, {1,0} })[mode + 1]

  local b = T.NewBundle()
  for _, e in ipairs(reorder({
    {"", 10}, {"Z", -5}, {"a", 20}, {"ab", 0}, {"abc", -7},
    {"long-unbounded-key-alpha", 12345}, {"\xEF\xBD\xA1", 111}, {"\xF0\x9F\x98\x80", 222}
  }, ord8)) do b.by_name[e[1]] = e[2] end

  for _, e in ipairs(reorder({
    {"Alpha", 1}, {"a", 5}, {"ab", 4}, {"beta", 2}, {"gamma", 3}
  }, ord5)) do b.by_code[e[1]] = e[2] end

  for _, e in ipairs(reorder({
    {-9000000000000, "min-far", -99}, {-1, "minus-one", -1}, {0, "", 0},
    {2, "two", 2}, {9000000000000, "max-far", 99}
  }, ord5b)) do
    b.by_id[e[1]] = { label = e[2], score = e[3] }
  end

  for _, e in ipairs(reorder({
    {0, 0}, {1, 11}, {255, 2550}, {256, 2560},
    {math.maxinteger, 700}, {math.mininteger, 800}, {-2, 900}
  }, ord7)) do b.by_uid[e[1]] = e[2] end

  b.by_tier = { 100, 200, 300, 400 }

  local m0 = {}
  for _, e in ipairs(reorder({{-100, -10}, {0, 0}, {7, 70}, {42, 420}}, ord4)) do m0[e[1]] = e[2] end
  local m1 = {}
  for _, e in ipairs(reorder({{-5, 55}, {12, 120}}, ord2)) do m1[e[1]] = e[2] end
  for _, e in ipairs(reorder({{"outer-a", m0}, {"outer-m", m1}, {"outer-z", {}}}, ord3)) do
    b.nested[e[1]] = e[2]
  end

  b.text_a = { value = table.concat({"shared-", "blob"}) }
  b.text_b = { value = table.concat({"shared-", "blob"}) }
  assert(b.text_a ~= b.text_b)
  b.blob_a = { value = "\xDE\xAD\xBE\xEF" }
  b.blob_b = { value = "\xDE\xAD\xBE\xEF" }
  assert(b.blob_a ~= b.blob_b)
  return b
end

local function check_bundle(b, tag)
  local w = S.Save(T, "Bundle", b)
  assert(w == want_wire, "FAILED: wire " .. tag)
  local j = J.ToJson(T, "Bundle", b)
  assert(j == want_json, "FAILED: json " .. tag)
end

for _, jf in ipairs({"json_sorted.json", "json_reverse.json", "json_shuffled.json"}) do
  local jraw = read_all(dir .. "/" .. jf)
  local rep = {}
  local b = J.FromJson(T, "Bundle", jraw, rep)
  assert(b ~= nil and not rep.damaged and (rep.refused == 0 or not rep.refused), "FAILED: FromJson " .. jf)
  check_bundle(b, jf)
end

for m = 0, 2 do
  check_bundle(build_native(m), "native_" .. tostring(m))
end

print("CANONICAL_OK lua 6")
`
	probe := filepath.Join(workDir, "main.lua")
	if err := os.WriteFile(probe, []byte(luaSrc), 0o644); err != nil {
		return err
	}
	luaBin := resolveGlobTool(repoRoot, "LUA", "dist/lua-*/bin/lua", "lua")
	out, err := runChecked(workDir, nil, luaBin, probe, corpusDir)
	return requireToken(out, err, "lua")
}

func renderLuaCanonicalFixtures(wantWire, wantJSON []byte, perms [3][]byte, isMoo bool) string {
	var b strings.Builder
	b.WriteString("local M = {}\n")
	if isMoo {
		b.WriteString("local function bytes(t: {number}): buffer\n\tlocal buf = buffer.create(#t)\n\tfor i = 1, #t do buffer.writeu8(buf, i - 1, t[i]) end\n\treturn buf\nend\n")
	} else {
		b.WriteString("local function bytes(t: {number}): buffer\n\tlocal buf = buffer.create(#t)\n\tfor i, v in ipairs(t) do buffer.writeu8(buf, i - 1, v) end\n\treturn buf\nend\n")
	}
	emitBuf := func(name string, raw []byte) {
		fmt.Fprintf(&b, "M.%s = bytes({", name)
		for i, by := range raw {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "%d", by)
		}
		b.WriteString("})\n")
	}
	emitBuf("want_wire", wantWire)
	emitBuf("want_json", wantJSON)
	emitBuf("json0", perms[0])
	emitBuf("json1", perms[1])
	emitBuf("json2", perms[2])
	b.WriteString("return M\n")
	return b.String()
}

func verifyLuau(repoRoot, schemaBin, workDir string, wantWire, wantJSON []byte, perms [3][]byte) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateCanonical(repoRoot, schemaBin, "", "luau", genDir); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(genDir, "fixtures.luau"), []byte(renderLuaCanonicalFixtures(wantWire, wantJSON, perms, false)), 0o644); err != nil {
		return err
	}
	src := `local T = require("./CanonicalTable")
local J = require("./schema_json")
local S = require("./schema_save")
local F = require("./fixtures")

local function bufEq(a: buffer?, b: buffer): boolean
	if a == nil or buffer.len(a) ~= buffer.len(b) then return false end
	for i = 0, buffer.len(a) - 1 do
		if buffer.readu8(a, i) ~= buffer.readu8(b, i) then return false end
	end
	return true
end

local function reorder<T>(items: {T}, ord: {number}): {T}
	local out = {}
	for i, idx in ipairs(ord) do out[i] = items[idx + 1] end
	return out
end

local function mkName(k: string, v: number)
	local e = T.NewBundleByNameEntry()
	e.Key = buffer.fromstring(k)
	e.Value = v
	return e
end

local function mkCode(k: string, v: number)
	local e = T.NewBundleByCodeEntry()
	if #k > 0 then buffer.writestring(e.Key, 0, k) end
	e.KeyLength = #k
	e.Value = v
	return e
end

local function mkId(k: integer, lbl: string, sc: number)
	local e = T.NewBundleByIdEntry()
	e.Key = k
	if #lbl > 0 then buffer.writestring(e.Value.Label, 0, lbl) end
	e.Value.LabelLength = #lbl
	e.Value.Score = sc
	return e
end

local function mkUid(k: integer, v: number)
	local e = T.NewBundleByUidEntry()
	e.Key = k
	e.Value = v
	return e
end

local function mkInner(k: number, v: number)
	local e = T.NewBundleNestedEntryValueEntry()
	e.Key = k
	e.Value = v
	return e
end

local function mkNested(k: string, vals: {any})
	local e = T.NewBundleNestedEntry()
	if #k > 0 then buffer.writestring(e.Key, 0, k) end
	e.KeyLength = #k
	e.Value = vals
	return e
end

local function buildNative(mode: number)
	local ord8 = ({ {0,1,2,3,4,5,6,7}, {7,6,5,4,3,2,1,0}, {7,4,2,6,0,5,1,3} })[mode + 1]
	local ord5 = ({ {0,1,2,3,4}, {4,3,2,1,0}, {2,4,0,3,1} })[mode + 1]
	local ord5b = ({ {0,1,2,3,4}, {4,3,2,1,0}, {3,0,4,2,1} })[mode + 1]
	local ord7 = ({ {0,1,2,3,4,5,6}, {6,5,4,3,2,1,0}, {5,3,0,6,2,4,1} })[mode + 1]
	local ord3 = ({ {0,1,2}, {2,1,0}, {1,2,0} })[mode + 1]
	local ord4 = ({ {0,1,2,3}, {3,2,1,0}, {2,0,3,1} })[mode + 1]
	local ord2 = ({ {0,1}, {1,0}, {1,0} })[mode + 1]

	local b = T.NewBundle()
	b.ByName = reorder({
		mkName("", 10), mkName("Z", -5), mkName("a", 20), mkName("ab", 0), mkName("abc", -7),
		mkName("long-unbounded-key-alpha", 12345), mkName("\xEF\xBD\xA1", 111), mkName("\xF0\x9F\x98\x80", 222),
	}, ord8)
	b.ByCode = reorder({
		mkCode("Alpha", 1), mkCode("a", 5), mkCode("ab", 4), mkCode("beta", 2), mkCode("gamma", 3),
	}, ord5)
	b.ById = reorder({
		mkId(-9000000000000i, "min-far", -99), mkId(-1i, "minus-one", -1), mkId(0i, "", 0),
		mkId(2i, "two", 2), mkId(9000000000000i, "max-far", 99),
	}, ord5b)
	b.ByUid = reorder({
		mkUid(0i, 0), mkUid(1i, 11), mkUid(255i, 2550), mkUid(256i, 2560),
		mkUid(9223372036854775807i, 700), mkUid(0x8000000000000000i, 800), mkUid(0xfffffffffffffffei, 900),
	}, ord7)
	b.ByTier = { 100, 200, 300, 400 }
	b.Nested = reorder({
		mkNested("outer-a", reorder({mkInner(-100, -10), mkInner(0, 0), mkInner(7, 70), mkInner(42, 420)}, ord4)),
		mkNested("outer-m", reorder({mkInner(-5, 55), mkInner(12, 120)}, ord2)),
		mkNested("outer-z", {}),
	}, ord3)

	b.TextA = buffer.fromstring("shared-" .. "blob")
	b.TextB = buffer.fromstring("shared-" .. "blob")
	if b.TextA == b.TextB then error("FAILED: TextA == TextB") end
	b.BlobA = buffer.fromstring("\xDE\xAD\xBE\xEF")
	b.BlobB = buffer.fromstring("\xDE\xAD\xBE\xEF")
	if b.BlobA == b.BlobB then error("FAILED: BlobA == BlobB") end
	return b
end

local function checkBundle(b: any, tag: string)
	local w = S.save(T.BundleDescriptor, b)
	if not bufEq(w, F.want_wire) then error("FAILED: wire " .. tag) end
	local j = J.ToJson("Bundle", b, {})
	if not bufEq(j, F.want_json) then error("FAILED: json " .. tag) end
end

for idx, jbuf in ipairs({F.json0, F.json1, F.json2}) do
	local rep: {[string]: any} = {}
	local b = J.FromJson("Bundle", jbuf, rep)
	if b == nil or rep.damaged or rep.refused then error("FAILED: FromJson " .. tostring(idx)) end
	checkBundle(b, "json_" .. tostring(idx))
end

for m = 0, 2 do
	checkBundle(buildNative(m), "native_" .. tostring(m))
end

print("CANONICAL_OK luau 6")
`
	if err := os.WriteFile(filepath.Join(genDir, "probe.luau"), []byte(src), 0o644); err != nil {
		return err
	}
	luauBin := resolveGlobTool(repoRoot, "LUAU", "dist/luau-*/luau", "luau")
	out, err := runChecked(genDir, nil, luauBin, "probe.luau")
	return requireToken(out, err, "luau")
}

func verifyMoo(repoRoot, schemaMooBin, workDir string, wantWire, wantJSON []byte, perms [3][]byte) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateCanonical(repoRoot, "", schemaMooBin, "moo", genDir); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(genDir, "fixtures.moo"), []byte(renderLuaCanonicalFixtures(wantWire, wantJSON, perms, true)), 0o644); err != nil {
		return err
	}
	src := `local serialize = require("./serialize")
local T = require("./CanonicalTable")
local S = require("./table_save")
local F = require("./fixtures")

local function bufEq(a: buffer?, b: buffer): boolean
	if a == nil or buffer.len(a :: buffer) ~= buffer.len(b) then return false end
	local ab = a :: buffer
	for i = 0, buffer.len(ab) - 1 do
		if buffer.readu8(ab, i) ~= buffer.readu8(b, i) then return false end
	end
	return true
end

local function mkName(k: string, v: number): any
	local e = T.NewBundleByNameEntry()
	e.Key = k
	e.Value = v
	return e
end

local function mkCode(k: string, v: number): any
	local e = T.NewBundleByCodeEntry()
	e.Key = k
	e.Value = v
	return e
end

local function mkId(lo: number, hi: number, lbl: string, sc: number): any
	local e = T.NewBundleByIdEntry()
	e.Key = serialize.Int64.new(lo, hi)
	e.Value.Label = lbl
	e.Value.Score = sc
	return e
end

local function mkUid(lo: number, hi: number, v: number): any
	local e = T.NewBundleByUidEntry()
	e.Key = serialize.Uint64.new(lo, hi)
	e.Value = v
	return e
end

local function mkInner(k: number, v: number): any
	local e = T.NewBundleNestedEntryValueEntry()
	e.Key = k
	e.Value = v
	return e
end

local function mkNested(k: string, vals: {any}): any
	local e = T.NewBundleNestedEntry()
	e.Key = k
	e.Value = vals
	return e
end

local function reorder(items: {any}, ord: {number}): {any}
	local out: {any} = {}
	for i = 1, #ord do out[i] = items[ord[i] + 1] end
	return out
end

local function buildNative(mode: number): any
	local ord8s: {{number}} = { {0,1,2,3,4,5,6,7}, {7,6,5,4,3,2,1,0}, {7,4,2,6,0,5,1,3} }
	local ord5s: {{number}} = { {0,1,2,3,4}, {4,3,2,1,0}, {2,4,0,3,1} }
	local ord5bs: {{number}} = { {0,1,2,3,4}, {4,3,2,1,0}, {3,0,4,2,1} }
	local ord7s: {{number}} = { {0,1,2,3,4,5,6}, {6,5,4,3,2,1,0}, {5,3,0,6,2,4,1} }
	local ord3s: {{number}} = { {0,1,2}, {2,1,0}, {1,2,0} }
	local ord4s: {{number}} = { {0,1,2,3}, {3,2,1,0}, {2,0,3,1} }
	local ord2s: {{number}} = { {0,1}, {1,0}, {1,0} }

	local b = T.NewBundle()
	-- -9000000000000 = 0xFFFFF7D086327000 -> lo=0x86327000, hi=0xFFFFF7D0
	-- +9000000000000 = 0x0000082F79CD9000 -> lo=0x79CD9000, hi=0x0000082F
	b.ByName = reorder({
		mkName("", 10), mkName("Z", -5), mkName("a", 20), mkName("ab", 0), mkName("abc", -7),
		mkName("long-unbounded-key-alpha", 12345), mkName("\xEF\xBD\xA1", 111), mkName("\xF0\x9F\x98\x80", 222),
	}, ord8s[mode + 1])
	b.ByCode = reorder({
		mkCode("Alpha", 1), mkCode("a", 5), mkCode("ab", 4), mkCode("beta", 2), mkCode("gamma", 3),
	}, ord5s[mode + 1])
	b.ById = reorder({
		mkId(0x86327000, 0xFFFFF7D0, "min-far", -99),
		mkId(0xFFFFFFFF, 0xFFFFFFFF, "minus-one", -1),
		mkId(0, 0, "", 0),
		mkId(2, 0, "two", 2),
		mkId(0x79CD9000, 0x0000082F, "max-far", 99),
	}, ord5bs[mode + 1])
	b.ByUid = reorder({
		mkUid(0, 0, 0),
		mkUid(1, 0, 11),
		mkUid(255, 0, 2550),
		mkUid(256, 0, 2560),
		mkUid(0xFFFFFFFF, 0x7FFFFFFF, 700),
		mkUid(0, 0x80000000, 800),
		mkUid(0xFFFFFFFE, 0xFFFFFFFF, 900),
	}, ord7s[mode + 1])
	b.ByTier = { 100, 200, 300, 400 }
	b.Nested = reorder({
		mkNested("outer-a", reorder({mkInner(-100, -10), mkInner(0, 0), mkInner(7, 70), mkInner(42, 420)}, ord4s[mode + 1])),
		mkNested("outer-m", reorder({mkInner(-5, 55), mkInner(12, 120)}, ord2s[mode + 1])),
		mkNested("outer-z", {}),
	}, ord3s[mode + 1])

	b.TextA = buffer.fromstring("shared-" .. "blob")
	b.TextB = buffer.fromstring("shared-" .. "blob")
	b.BlobA = buffer.fromstring("\xDE\xAD\xBE\xEF")
	b.BlobB = buffer.fromstring("\xDE\xAD\xBE\xEF")
	return b
end

local function checkBundle(b: any, tag: string)
	local w = S.save(T.BundleDescriptor, b)
	if not bufEq(w, F.want_wire) then error("FAILED: wire " .. tag) end
	local jlen = T.BundleToJsonMeasure(b)
	if jlen < 0 then error("FAILED: jmeasure " .. tag) end
	local jbuf = buffer.create(jlen)
	if T.BundleToJson(b, jbuf) ~= jlen or not bufEq(jbuf, F.want_json) then
		error("FAILED: json " .. tag)
	end
end

local jsons: {buffer} = {F.json0, F.json1, F.json2}
for i = 1, 3 do
	local b = T.NewBundle()
	local rep: {[string]: any} = {}
	if not T.BundleFromJson(b, jsons[i], rep) or rep.damaged or rep.refused then
		error("FAILED: FromJson " .. tostring(i))
	end
	checkBundle(b, "json_" .. tostring(i))
end

for m = 0, 2 do
	checkBundle(buildNative(m), "native_" .. tostring(m))
end

print("CANONICAL_OK moo 6")
`
	if err := os.WriteFile(filepath.Join(genDir, "probe.moo"), []byte(src), 0o644); err != nil {
		return err
	}
	mooBin := resolveMooTool(repoRoot)
	out, err := runChecked(genDir, nil, mooBin, "probe.moo")
	return requireToken(out, err, "moo")
}

func verifySquirrel(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateCanonical(repoRoot, schemaBin, "", "squirrel", genDir); err != nil {
		return err
	}
	sqSrc := fmt.Sprintf(`local loader = dofile("./gen/schema_module.nut", true);
local T = loader.require("CanonicalTables");
local dir = %q;

local function check(ok, msg) {
  if (!ok) { print("FAILED: " + msg + "\n"); throw "FAILED: " + msg; }
}
local function read_blob(p) {
  local f = file(p, "rb");
  local n = f.len();
  local b = blob(n);
  if (n > 0) b.writeblob(f.readblob(n));
  f.close();
  b.seek(0);
  return b;
}
local function read_str(p) {
  local b = read_blob(p);
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%%c", b[i] & 0xff);
  return s;
}
local function blob_eq(a, b) {
  if (a == null || a.len() != b.len()) return false;
  for (local i = 0; i < a.len(); i++) if ((a[i] & 0xff) != (b[i] & 0xff)) return false;
  return true;
}
local function reorder(items, ord) {
  local out = [];
  foreach (idx in ord) out.append(items[idx]);
  return out;
}

local want_wire = read_blob(dir + "/canonical.bin");
local want_json = read_str(dir + "/canonical.json");

local function mkName(k, v) { local e = T.BundleByNameEntry(); e.Key = k; e.Value = v; return e; }
local function mkCode(k, v) { local e = T.BundleByCodeEntry(); e.Key = k; e.Value = v; return e; }
local function mkId(k, lbl, sc) { local e = T.BundleByIdEntry(); e.Key = k; e.Value.Label = lbl; e.Value.Score = sc; return e; }
local function mkUid(k, v) { local e = T.BundleByUidEntry(); e.Key = k; e.Value = v; return e; }
local function mkInner(k, v) { local e = T.BundleNestedEntryValueEntry(); e.Key = k; e.Value = v; return e; }
local function mkNested(k, vals) { local e = T.BundleNestedEntry(); e.Key = k; e.Value = vals; return e; }

local function build_native(mode) {
  local ord8 = [[0,1,2,3,4,5,6,7], [7,6,5,4,3,2,1,0], [7,4,2,6,0,5,1,3]][mode];
  local ord5 = [[0,1,2,3,4], [4,3,2,1,0], [2,4,0,3,1]][mode];
  local ord5b = [[0,1,2,3,4], [4,3,2,1,0], [3,0,4,2,1]][mode];
  local ord7 = [[0,1,2,3,4,5,6], [6,5,4,3,2,1,0], [5,3,0,6,2,4,1]][mode];
  local ord3 = [[0,1,2], [2,1,0], [1,2,0]][mode];
  local ord4 = [[0,1,2,3], [3,2,1,0], [2,0,3,1]][mode];
  local ord2 = [[0,1], [1,0], [1,0]][mode];

  local b = T.Bundle();
  b.ByName = reorder([
    mkName("", 10), mkName("Z", -5), mkName("a", 20), mkName("ab", 0), mkName("abc", -7),
    mkName("long-unbounded-key-alpha", 12345), mkName("\xEF\xBD\xA1", 111), mkName("\xF0\x9F\x98\x80", 222)
  ], ord8);
  b.ByCode = reorder([
    mkCode("Alpha", 1), mkCode("a", 5), mkCode("ab", 4), mkCode("beta", 2), mkCode("gamma", 3)
  ], ord5);
  b.ById = reorder([
    mkId(-9000000000000, "min-far", -99), mkId(-1, "minus-one", -1), mkId(0, "", 0),
    mkId(2, "two", 2), mkId(9000000000000, "max-far", 99)
  ], ord5b);
  local hiBit = (-9223372036854775807) - 1;
  b.ByUid = reorder([
    mkUid(0, 0), mkUid(1, 11), mkUid(255, 2550), mkUid(256, 2560),
    mkUid(9223372036854775807, 700), mkUid(hiBit, 800), mkUid(-2, 900)
  ], ord7);
  b.ByTier = [100, 200, 300, 400];
  b.Nested = reorder([
    mkNested("outer-a", reorder([mkInner(-100, -10), mkInner(0, 0), mkInner(7, 70), mkInner(42, 420)], ord4)),
    mkNested("outer-m", reorder([mkInner(-5, 55), mkInner(12, 120)], ord2)),
    mkNested("outer-z", [])
  ], ord3);

  b.TextA = format("%%s%%s", "shared-", "blob");
  b.TextB = format("%%s%%s", "shared-", "blob");
  local ba = blob(4); ba.writen(0xde, 'b'); ba.writen(0xad, 'b'); ba.writen(0xbe, 'b'); ba.writen(0xef, 'b'); ba.seek(0);
  local bb = blob(4); bb.writen(0xde, 'b'); bb.writen(0xad, 'b'); bb.writen(0xbe, 'b'); bb.writen(0xef, 'b'); bb.seek(0);
  b.BlobA = ba;
  b.BlobB = bb;
  return b;
}

local function check_bundle(b, tag) {
  local w = T.BundleSave(b);
  check(blob_eq(w, want_wire), "wire " + tag);
  local j = T.BundleToJson(b);
  check(j == want_json, "json " + tag);
}

foreach (jf in ["json_sorted.json", "json_reverse.json", "json_shuffled.json"]) {
  local jraw = read_str(dir + "/" + jf);
  local rep = {};
  local b = T.BundleFromJson(jraw, rep);
  check(b != null && !("damaged" in rep && rep.damaged) && !("refused" in rep && rep.refused), "FromJson " + jf);
  check_bundle(b, jf);
}
for (local m = 0; m < 3; m++) {
  check_bundle(build_native(m), "native_" + m);
}
print("CANONICAL_OK squirrel 6\n");
`, corpusDir)
	probe := filepath.Join(workDir, "probe.nut")
	if err := os.WriteFile(probe, []byte(sqSrc), 0o644); err != nil {
		return err
	}
	sqBin := resolveSquirrelTool(repoRoot)
	out, err := runChecked(workDir, nil, sqBin, probe)
	return requireToken(out, err, "squirrel")
}

func verifyGoose(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateCanonical(repoRoot, schemaBin, "", "goose", genDir); err != nil {
		return err
	}
	gooseSrc := fmt.Sprintf(`import os;
import .canonical_unit;
import .canonical_json;
import .CanonicalView;
import .json_value;
import .schema_rt;
import .schema_json;
import .wire_save;

fn check(ok: bool, msg: const u8[:]) {
    if !ok { write_stderr(str("FAILED: ", msg, "\n")); exit(1); }
}
fn same(a: const u8[:], b: const u8[:]) -> bool {
    if a.len != b.len { return false; }
    for i in a.len { if a[i] != b[i] { return false; } }
    return true;
}
fn permute_span(vals: schema_rt::LoadVal[>..]&, first: i64, n: i64, mode: i64) {
    if n <= 1 || mode == 0 { return; }
    var snap: schema_rt::LoadVal[>..] = [];
    for i in n { snap.push(vals[first + i]); }
    if mode == 1 {
        for i in n { vals[first + i] = snap[n - 1 - i]; }
    } else {
        // Deterministic odd-step shuffle
        var used: bool[>..] = [];
        for _ in n { used.push(false); }
        var cur: i64 = (n / 2);
        for i in n {
            while used[cur] { cur = (cur + 1) %% n; }
            used[cur] = true;
            vals[first + i] = snap[cur];
            cur = (cur + 3) %% n;
        }
    }
}

fn main() {
    let dir = %q;
    var want_wire: u8[>..] = [];
    var want_json: u8[>..] = [];
    check(read_file(str(dir, "/canonical.bin"), want_wire), "read canonical.bin");
    check(read_file(str(dir, "/canonical.json"), want_json), "read canonical.json");

    let jfiles: (const u8[:])[3] = ["json_sorted.json", "json_reverse.json", "json_shuffled.json"];
    for jf in jfiles {
        var jraw: u8[>..] = [];
        check(read_file(str(dir, "/", jf), jraw), jf);
        var data: u8[>..] = [];
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var jr = schema_rt::json_report();
        let root = schema_rt::json_load_table(canonical::TableUnit, canonical::TextUnit, "Bundle", jraw, data, vals, nodes, jr);
        check(root >= 0 && !jr.damaged, str("FromJson ", jf));
        var wout: u8[>..<] = [];
        check(schema_rt::save_table(canonical::TableUnit, "Bundle", data, vals, nodes, root, wout) == 0, str("save ", jf));
        check(same(wout, want_wire), str("wire ", jf));
        var jout: u8[>..] = [];
        check(schema_rt::json_save_table(canonical::TableUnit, canonical::TextUnit, "Bundle", data, vals, nodes, root, jout) >= 0, str("toJson ", jf));
        check(same(jout, want_json), str("json ", jf));
    }

    // Native in-memory map entry & distinct dynamic equal-blob permutations (modes 0, 1, 2)
    var base_json: u8[>..] = [];
    check(read_file(str(dir, "/json_sorted.json"), base_json), "base_json");
    for mode in 3 {
        var data: u8[>..] = [];
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var jr = schema_rt::json_report();
        let root = schema_rt::json_load_table(canonical::TableUnit, canonical::TextUnit, "Bundle", base_json, data, vals, nodes, jr);
        check(root >= 0 && !jr.damaged, "base load");

        // Permute all root maps (ordinals 0:by_name, 1:by_code, 2:by_id, 3:by_uid, 5:nested)
        let ffirst = vals[root].first;
        for fidx in [0, 1, 2, 3, 5] {
            let ms = ffirst + fidx;
            if fidx == 5 {
                // Also permute inner maps inside nested entries
                for k in vals[ms].len {
                    let entry_rec = vals[ms].first + k;
                    let inner_map_slot = vals[entry_rec].first + 1;
                    permute_span(vals, vals[inner_map_slot].first, vals[inner_map_slot].len, mode);
                }
            }
            permute_span(vals, vals[ms].first, vals[ms].len, mode);
        }

        // Dynamically append two distinct equal "shared-" + "blob" slices and two [0xde,0xad,0xbe,0xef] slices
        let s_dyn1 = str("shared-", "blob");
        let s_dyn2 = str("shared-", "blob");
        let off_s1 = data.len; data.append(s_dyn1);
        let off_s2 = data.len; data.append(s_dyn2);
        check(off_s1 != off_s2, "distinct text offsets");
        let n_ta = (vals[ffirst + 6].bits.lo as! i64) - 2;
        let n_tb = (vals[ffirst + 7].bits.lo as! i64) - 2;
        nodes[n_ta].off = off_s1;
        nodes[n_ta].len = 11;
        nodes[n_tb].off = off_s2;
        nodes[n_tb].len = 11;

        let raw4: u8[4] = [0xde, 0xad, 0xbe, 0xef];
        let off_b1 = data.len; data.append(raw4);
        let off_b2 = data.len; data.append(raw4);
        check(off_b1 != off_b2, "distinct blob offsets");
        let n_ba = (vals[ffirst + 8].bits.lo as! i64) - 2;
        let n_bb = (vals[ffirst + 9].bits.lo as! i64) - 2;
        nodes[n_ba].off = off_b1;
        nodes[n_ba].len = 4;
        nodes[n_bb].off = off_b2;
        nodes[n_bb].len = 4;

        var wout: u8[>..<] = [];
        check(schema_rt::save_table(canonical::TableUnit, "Bundle", data, vals, nodes, root, wout) == 0, "save native");
        check(same(wout, want_wire), "wire native");
        var jout: u8[>..] = [];
        check(schema_rt::json_save_table(canonical::TableUnit, canonical::TextUnit, "Bundle", data, vals, nodes, root, jout) >= 0, "toJson native");
        check(same(jout, want_json), "json native");
    }
    print("CANONICAL_OK goose 6\n");
}
`, corpusDir)
	srcFile := filepath.Join(genDir, "probe.goose")
	exeFile := filepath.Join(genDir, "probe_bin")
	if err := os.WriteFile(srcFile, []byte(gooseSrc), 0o644); err != nil {
		return err
	}
	gooseBin := resolveGooseTool(repoRoot)
	if out, err := runChecked(genDir, nil, gooseBin, "build", "-O2", "-o", exeFile, srcFile); err != nil {
		return fmt.Errorf("goose build: %w\n%s", err, out)
	}
	out, err := runChecked(genDir, nil, exeFile)
	return requireToken(out, err, "goose")
}

func verifyLobster(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateCanonical(repoRoot, schemaBin, "", "lobster", genDir); err != nil {
		return err
	}
	lobSrc := fmt.Sprintf(`import .CanonicalTable
import .canonicalView
import .collections
import .json
import .load
import .schema_save

def check(ok:bool, msg:string):
    if not ok:
        print("FAILED: " + msg)
        assert false

def must_read(p:string) -> string:
    let d = read_file(p)
    check(d != nil, "read " + p)
    return if d: d else: ""

def permute_vals(xs:[schema_rt.Value], mode:int) -> [schema_rt.Value]:
    let n = length(xs)
    if n <= 1 or mode == 0:
        return xs
    let out = []
    if mode == 1:
        for(n) i:
            push(out, xs[n - 1 - i])
    else:
        let used = []
        for(n) _:
            push(used, false)
        var cur = n / 2
        for(n) _:
            while used[cur]:
                cur = (cur + 1) %% n
            used[cur] = true
            push(out, xs[cur])
            cur = (cur + 3) %% n
    return out

let dir = %q
let want_wire = must_read(dir + "/canonical.bin")
let want_json = must_read(dir + "/canonical.json")

for(["json_sorted.json", "json_reverse.json", "json_shuffled.json"]) jf:
    let jraw = must_read(dir + "/" + jf)
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.json_read("Bundle", jraw, rep)
    check(rec != nil and not rep.damaged and not rep.refused, "FromJson " + jf)
    if rec:
        let w, wwhy = schema_rt.save_reasoned(rec)
        check(wwhy == "" and w == want_wire, "wire " + jf)
        let j, jwhy = schema_rt.json_write(rec)
        check(jwhy == "" and j == want_json, "json " + jf)

let base_json = must_read(dir + "/json_sorted.json")
for(3) mode:
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.json_read("Bundle", base_json, rep)
    check(rec != nil, "base")
    if rec:
        for([0, 1, 2, 3, 5]) fidx:
            let slot = rec.fields[fidx]
            if fidx == 5:
                for(slot.entries) ev:
                    let er = ev.tab
                    if er:
                        er.fields[1].entries = permute_vals(er.fields[1].entries, mode)
            slot.entries = permute_vals(slot.entries, mode)

        // Fresh dynamic equal string and byte blobs with nil blob_node
        let s1 = concat_string(["shared-", "blob"], "")
        let s2 = concat_string(["shared-", "blob"], "")
        rec.fields[6].cell.blob = s1
        rec.fields[6].cell.blob_node = nil
        rec.fields[7].cell.blob = s2
        rec.fields[7].cell.blob_node = nil

        var b1 = schema_rt.zeros(4)
        b1 = write_int8_le(b1, 0, 0xde)
        b1 = write_int8_le(b1, 1, 0xad)
        b1 = write_int8_le(b1, 2, 0xbe)
        b1 = write_int8_le(b1, 3, 0xef)
        var b2 = schema_rt.zeros(4)
        b2 = write_int8_le(b2, 0, 0xde)
        b2 = write_int8_le(b2, 1, 0xad)
        b2 = write_int8_le(b2, 2, 0xbe)
        b2 = write_int8_le(b2, 3, 0xef)
        rec.fields[8].cell.blob = b1
        rec.fields[8].cell.blob_node = nil
        rec.fields[9].cell.blob = b2
        rec.fields[9].cell.blob_node = nil

        let w, wwhy = schema_rt.save_reasoned(rec)
        check(wwhy == "" and w == want_wire, "wire native_" + number_to_string(mode, 10, 0))
        let j, jwhy = schema_rt.json_write(rec)
        check(jwhy == "" and j == want_json, "json native_" + number_to_string(mode, 10, 0))

print("CANONICAL_OK lobster 6")
`, corpusDir)
	probe := filepath.Join(genDir, "probe.lobster")
	if err := os.WriteFile(probe, []byte(lobSrc), 0o644); err != nil {
		return err
	}
	lobsterBin := resolveGlobTool(repoRoot, "LOBSTER", "dist/lobster-*/bin/lobster", "lobster")
	out, err := runChecked(genDir, nil, lobsterBin, probe)
	return requireToken(out, err, "lobster")
}

func resolveSquirrelTool(repoRoot string) string {
	for _, k := range []string{"SQ", "SQUIRREL"} {
		if v := os.Getenv(k); toolUsable(v) {
			return v
		}
	}
	for _, pat := range []string{
		filepath.Join(repoRoot, "dist/squirrel-*/bin/sq"),
		"/home/nova/sdk/squirrel-*-double/bin/sq",
		"/home/nova/sdk/squirrel-*/bin/sq",
	} {
		matches, _ := filepath.Glob(pat)
		for _, m := range matches {
			if fileExists(m) {
				return m
			}
		}
	}
	return "sq"
}

func resolveMooTool(repoRoot string) string {
	if v := os.Getenv("MOO"); toolUsable(v) {
		return v
	}
	for _, pat := range []string{
		filepath.Join(repoRoot, "dist/moo-*/bin/moo"),
		filepath.Join(repoRoot, "dist/moo-*/moo"),
		"/home/nova/sdk/moo-*-checked/bin/moo",
		"/home/nova/sdk/moo-*/bin/moo",
	} {
		matches, _ := filepath.Glob(pat)
		for _, matche := range slices.Backward(matches) {
			if !strings.Contains(matche, "broken") && fileExists(matche) {
				return matche
			}
		}
	}
	return "moo"
}

func resolveGooseTool(repoRoot string) string {
	if v := os.Getenv("GOOSE"); toolUsable(v) {
		return v
	}
	for _, pat := range []string{
		filepath.Join(repoRoot, "dist/goose-*/goose"),
		filepath.Join(repoRoot, "dist/goose-*/bin/goose"),
		"/home/nova/sdk/goose-*/goose",
		"/home/nova/sdk/goose-*/bin/goose",
	} {
		matches, _ := filepath.Glob(pat)
		for _, matche := range slices.Backward(matches) {
			if fileExists(matche) {
				return matche
			}
		}
	}
	return "goose"
}
