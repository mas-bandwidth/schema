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
	if err := generateBitPhase(repoRoot, schemaBin, "", "js", genDir); err != nil {
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
import { ReadStream, WriteStream } from "serialize";
import * as BP from "./gen/BitPhase.js";
import * as BPF from "./gen/BitPhaseFlat.js";

const dir = process.argv[2];
const reuse = {
  EmptyZero: new BP.EmptyZero(),
  Phase0: new BP.Phase0(),
  Phase1: new BP.Phase1(),
  Phase7: new BP.Phase7(),
  Phase31: new BP.Phase31(),
  Phase32: new BP.Phase32(),
  Phase33: new BP.Phase33(),
  Phase56: new BP.Phase56(),
  Phase63: new BP.Phase63(),
};
const reuseFlat = {
  EmptyZero: new BP.EmptyZero(),
  Phase0: new BP.Phase0(),
  Phase1: new BP.Phase1(),
  Phase7: new BP.Phase7(),
  Phase31: new BP.Phase31(),
  Phase32: new BP.Phase32(),
  Phase33: new BP.Phase33(),
  Phase56: new BP.Phase56(),
  Phase63: new BP.Phase63(),
};
const lines = fs.readFileSync(path.join(dir, "manifest.tsv"), "utf8").trim().split("\n");
let count = 0;
for (const line of lines) {
  if (!line.trim()) continue;
  const [kind, tname, fname] = line.trim().split("\t");
  const raw = new Uint8Array(fs.readFileSync(path.join(dir, fname)));

  // 1. Stream tier (BitPhase.js)
  {
    const rd = BP["Read" + tname];
    const wr = BP["Write" + tname];
    const rs = new ReadStream(raw);
    if (kind === "valid") {
      const val = reuse[tname];
      if (!rd(rs, val)) throw new Error("FAILED: stream read " + fname);
      const ws = new WriteStream(new Uint8Array(512));
      if (!wr(ws, val)) throw new Error("FAILED: stream write " + fname);
      ws.flush();
      const got = ws.data();
      if (got.length !== raw.length || got.some((b, i) => b !== raw[i])) {
        throw new Error("FAILED: stream bytes " + fname);
      }
    } else {
      const tmp = new BP[tname]();
      if (rd(rs, tmp)) throw new Error("FAILED: stream accepted " + fname);
    }
  }

  // 2. Flat static-cursor DataView tier (BitPhaseFlat.js)
  {
    const rdFlat = BPF["Read" + tname + "Flat"];
    const wrFlat = BPF["Write" + tname + "Flat"];
    const rpad = new Uint8Array(raw.length + BPF.FlatReadSlack);
    rpad.set(raw);
    const rview = new DataView(rpad.buffer);
    if (kind === "valid") {
      const val = reuseFlat[tname];
      if (!rdFlat(rview, raw.length * 8, val)) throw new Error("FAILED: flat read " + fname);
      const wpad = new Uint8Array(512 + BPF.FlatReadSlack);
      const wn = wrFlat(new DataView(wpad.buffer), val);
      if (wn < 0) throw new Error("FAILED: flat write " + fname);
      const got = wpad.subarray(0, wn);
      if (got.length !== raw.length || got.some((b, i) => b !== raw[i])) {
        throw new Error("FAILED: flat bytes " + fname);
      }
    } else {
      const tmp = new BP[tname]();
      if (rdFlat(rview, raw.length * 8, tmp)) throw new Error("FAILED: flat accepted " + fname);
    }
  }
  count++;
}
console.log("BITPHASE_OK js " + count);
`
	probe := filepath.Join(workDir, "main.mjs")
	if err := os.WriteFile(probe, []byte(jsSrc), 0o644); err != nil {
		return err
	}
	node := resolveTool(repoRoot, "NODE", "dist/node-v25.4.0/bin/node", "node")
	if out, err := runChecked(workDir, nil, node, probe, corpusDir); err != nil {
		return fmt.Errorf("js checked: %w\n%s", err, out)
	}
	out, err := runChecked(workDir, []string{"NODE_ENV=production"}, node, probe, corpusDir)
	return requireToken(out, err, "js")
}

func verifyPython(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateBitPhase(repoRoot, schemaBin, "", "python", genDir); err != nil {
		return err
	}
	pySrc := `import pathlib, sys
sys.path.insert(0, "./gen")
import bitphase as bp

d = pathlib.Path(sys.argv[1])
count = 0
for line in (d / "manifest.tsv").read_text().splitlines():
    line = line.strip()
    if not line:
        continue
    kind, tname, fname = line.split("\t")
    raw = (d / fname).read_bytes()
    cls = getattr(bp, tname)
    if kind == "valid":
        val = cls.read(raw)
        got = val.write()
        if got != raw:
            raise AssertionError(f"FAILED: bytes {fname}: got {len(got)} want {len(raw)}")
    else:
        accepted = False
        try:
            cls.read(raw)
            accepted = True
        except bp.Refused:
            pass
        if accepted:
            raise AssertionError(f"FAILED: accepted {fname}")
    count += 1
print(f"BITPHASE_OK python {count}")
`
	probe := filepath.Join(workDir, "main.py")
	if err := os.WriteFile(probe, []byte(pySrc), 0o644); err != nil {
		return err
	}
	py := resolvePythonTool(repoRoot)
	out, err := runChecked(workDir, nil, py, probe, corpusDir)
	return requireToken(out, err, "python")
}

func verifyRuby(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateBitPhase(repoRoot, schemaBin, "", "ruby", genDir); err != nil {
		return err
	}
	rbSrc := `$LOAD_PATH.unshift(File.expand_path("./gen", __dir__))
require "bitphase"

dir = ARGV[0]
count = 0
File.readlines(File.join(dir, "manifest.tsv"), chomp: true).each do |line|
  line = line.strip
  next if line.empty?
  kind, tname, fname = line.split("\t")
  raw = File.binread(File.join(dir, fname))
  cls = Bitphase.const_get(tname)
  if kind == "valid"
    val = cls.read(raw)
    got = val.write
    raise "FAILED: bytes #{fname}" unless got == raw
  else
    accepted = false
    begin
      cls.read(raw)
      accepted = true
    rescue Bitphase::Refused
      accepted = false
    end
    raise "FAILED: accepted #{fname}" if accepted
  end
  count += 1
end
puts "BITPHASE_OK ruby #{count}"
`
	probe := filepath.Join(workDir, "main.rb")
	if err := os.WriteFile(probe, []byte(rbSrc), 0o644); err != nil {
		return err
	}
	rb := resolveRubyTool(repoRoot)
	out, err := runChecked(workDir, nil, rb, probe, corpusDir)
	return requireToken(out, err, "ruby")
}

func verifyLua(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateBitPhase(repoRoot, schemaBin, "", "lua", genDir); err != nil {
		return err
	}
	luaSrc := `package.path = "./gen/?.lua;" .. package.path
local serialize = require("serialize")
local BP = require("BitPhase")

local dir = arg[1]
local reuse = {
  EmptyZero = BP.NewEmptyZero(),
  Phase0 = BP.NewPhase0(),
  Phase1 = BP.NewPhase1(),
  Phase7 = BP.NewPhase7(),
  Phase31 = BP.NewPhase31(),
  Phase32 = BP.NewPhase32(),
  Phase33 = BP.NewPhase33(),
  Phase56 = BP.NewPhase56(),
  Phase63 = BP.NewPhase63(),
}
local function read_all(p)
  local f = assert(io.open(p, "rb"))
  local d = f:read("*a")
  f:close()
  return d
end
local count = 0
for line in io.lines(dir .. "/manifest.tsv") do
  local kind, tname, fname = line:match("^(%S+)\t(%S+)\t(%S+)$")
  if kind then
    local raw = read_all(dir .. "/" .. fname)
    local rs = serialize.ReadStream.new(raw)
    if kind == "valid" then
      local v = reuse[tname]
      assert(BP["Read" .. tname](rs, v), "FAILED: read " .. fname)
      local ws = serialize.WriteStream.new(512)
      assert(BP["Write" .. tname](ws, v), "FAILED: write " .. fname)
      ws:flush()
      assert(ws:data() == raw, "FAILED: bytes " .. fname)
    else
      local tmp = BP["New" .. tname]()
      assert(not BP["Read" .. tname](rs, tmp), "FAILED: accepted " .. fname)
    end
    count = count + 1
  end
end
print("BITPHASE_OK lua " .. tostring(count))
`
	probe := filepath.Join(workDir, "main.lua")
	if err := os.WriteFile(probe, []byte(luaSrc), 0o644); err != nil {
		return err
	}
	luaBin := resolveGlobTool(repoRoot, "LUA", "dist/lua-*/bin/lua", "lua")
	out, err := runChecked(workDir, nil, luaBin, probe, corpusDir)
	return requireToken(out, err, "lua")
}

func renderEmbeddedLuaVectors(cases []manifestCase, blobs map[string][]byte, isMoo bool) string {
	var b strings.Builder
	b.WriteString("local M = {}\n")
	if isMoo {
		b.WriteString("local function bytes(t: {number}): buffer\n\tlocal buf = buffer.create(#t)\n\tfor i = 1, #t do buffer.writeu8(buf, i - 1, t[i]) end\n\treturn buf\nend\n")
	} else {
		b.WriteString("local function bytes(t: {number}): buffer\n\tlocal buf = buffer.create(#t)\n\tfor i, v in ipairs(t) do buffer.writeu8(buf, i - 1, v) end\n\treturn buf\nend\n")
	}
	b.WriteString("M.blobs = {\n")
	for _, c := range cases {
		raw := blobs[c.File]
		fmt.Fprintf(&b, "\t[%q] = bytes({", c.File)
		for i, by := range raw {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "%d", by)
		}
		b.WriteString("}),\n")
	}
	b.WriteString("}\nM.cases = {\n")
	for _, c := range cases {
		fmt.Fprintf(&b, "\t{%q, %q, %q},\n", c.Kind, c.Type, c.File)
	}
	b.WriteString("}\nreturn M\n")
	return b.String()
}

func verifyLuau(repoRoot, schemaBin, workDir string, cases []manifestCase, blobs map[string][]byte) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateBitPhase(repoRoot, schemaBin, "", "luau", genDir); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(genDir, "vectors.luau"), []byte(renderEmbeddedLuaVectors(cases, blobs, false)), 0o644); err != nil {
		return err
	}
	src := `local serialize = require("./serialize")
local BP = require("./BitPhase")
local V = require("./vectors")

local function bufEq(a: buffer, b: buffer): boolean
	if buffer.len(a) ~= buffer.len(b) then return false end
	for i = 0, buffer.len(a) - 1 do
		if buffer.readu8(a, i) ~= buffer.readu8(b, i) then return false end
	end
	return true
end

local reuse: {[string]: any} = {
	EmptyZero = BP.NewEmptyZero(),
	Phase0 = BP.NewPhase0(),
	Phase1 = BP.NewPhase1(),
	Phase7 = BP.NewPhase7(),
	Phase31 = BP.NewPhase31(),
	Phase32 = BP.NewPhase32(),
	Phase33 = BP.NewPhase33(),
	Phase56 = BP.NewPhase56(),
	Phase63 = BP.NewPhase63(),
}
local rdMap: {[string]: any} = {
	EmptyZero = BP.ReadEmptyZero, Phase0 = BP.ReadPhase0, Phase1 = BP.ReadPhase1,
	Phase7 = BP.ReadPhase7, Phase31 = BP.ReadPhase31, Phase32 = BP.ReadPhase32,
	Phase33 = BP.ReadPhase33, Phase56 = BP.ReadPhase56, Phase63 = BP.ReadPhase63,
}
local wrMap: {[string]: any} = {
	EmptyZero = BP.WriteEmptyZero, Phase0 = BP.WritePhase0, Phase1 = BP.WritePhase1,
	Phase7 = BP.WritePhase7, Phase31 = BP.WritePhase31, Phase32 = BP.WritePhase32,
	Phase33 = BP.WritePhase33, Phase56 = BP.WritePhase56, Phase63 = BP.WritePhase63,
}
local newMap: {[string]: any} = {
	EmptyZero = BP.NewEmptyZero, Phase0 = BP.NewPhase0, Phase1 = BP.NewPhase1,
	Phase7 = BP.NewPhase7, Phase31 = BP.NewPhase31, Phase32 = BP.NewPhase32,
	Phase33 = BP.NewPhase33, Phase56 = BP.NewPhase56, Phase63 = BP.NewPhase63,
}

local count = 0
for _, c in ipairs(V.cases) do
	local kind, tname, fname = c[1], c[2], c[3]
	local raw = V.blobs[fname]
	local rs = serialize.ReadStream.new(raw)
	if kind == "valid" then
		local v = reuse[tname]
		if not rdMap[tname](rs, v) then error("FAILED: read " .. fname) end
		local ws = serialize.WriteStream.new(buffer.create(512))
		if not wrMap[tname](ws, v) then error("FAILED: write " .. fname) end
		ws:flush()
		if not bufEq(ws:data(), raw) then error("FAILED: bytes " .. fname) end
	else
		local tmp = newMap[tname]()
		if rdMap[tname](rs, tmp) then error("FAILED: accepted " .. fname) end
	end
	count += 1
end
print("BITPHASE_OK luau " .. tostring(count))
`
	if err := os.WriteFile(filepath.Join(genDir, "probe.luau"), []byte(src), 0o644); err != nil {
		return err
	}
	luauBin := resolveGlobTool(repoRoot, "LUAU", "dist/luau-*/luau", "luau")
	out, err := runChecked(genDir, nil, luauBin, "probe.luau")
	return requireToken(out, err, "luau")
}

func verifyMoo(repoRoot, schemaMooBin, workDir string, cases []manifestCase, blobs map[string][]byte) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateBitPhase(repoRoot, "", schemaMooBin, "moo", genDir); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(genDir, "vectors.moo"), []byte(renderEmbeddedLuaVectors(cases, blobs, true)), 0o644); err != nil {
		return err
	}
	src := `local serialize = require("./serialize")
local BP = require("./BitPhase")
local V = require("./vectors")

local function bufEq(a: buffer, b: buffer): boolean
	if buffer.len(a) ~= buffer.len(b) then return false end
	for i = 0, buffer.len(a) - 1 do
		if buffer.readu8(a, i) ~= buffer.readu8(b, i) then return false end
	end
	return true
end

local r_ez = BP.NewEmptyZero()
local r0 = BP.NewPhase0()
local r1 = BP.NewPhase1()
local r7 = BP.NewPhase7()
local r31 = BP.NewPhase31()
local r32 = BP.NewPhase32()
local r33 = BP.NewPhase33()
local r56 = BP.NewPhase56()
local r63 = BP.NewPhase63()

local count = 0
for i = 1, #V.cases do
	local c = V.cases[i]
	local kind, tname, fname = c[1], c[2], c[3]
	local raw = V.blobs[fname]
	local rs = serialize.ReadStream.new(raw)
	if kind == "valid" then
		local ws = serialize.WriteStream.new(buffer.create(512))
		local ok = false
		if tname == "EmptyZero" then ok = BP.ReadEmptyZero(rs, r_ez) and BP.WriteEmptyZero(ws, r_ez)
		elseif tname == "Phase0" then ok = BP.ReadPhase0(rs, r0) and BP.WritePhase0(ws, r0)
		elseif tname == "Phase1" then ok = BP.ReadPhase1(rs, r1) and BP.WritePhase1(ws, r1)
		elseif tname == "Phase7" then ok = BP.ReadPhase7(rs, r7) and BP.WritePhase7(ws, r7)
		elseif tname == "Phase31" then ok = BP.ReadPhase31(rs, r31) and BP.WritePhase31(ws, r31)
		elseif tname == "Phase32" then ok = BP.ReadPhase32(rs, r32) and BP.WritePhase32(ws, r32)
		elseif tname == "Phase33" then ok = BP.ReadPhase33(rs, r33) and BP.WritePhase33(ws, r33)
		elseif tname == "Phase56" then ok = BP.ReadPhase56(rs, r56) and BP.WritePhase56(ws, r56)
		elseif tname == "Phase63" then ok = BP.ReadPhase63(rs, r63) and BP.WritePhase63(ws, r63)
		end
		if not ok then error("FAILED: rw " .. fname) end
		ws:flush()
		if not bufEq(ws:data(), raw) then error("FAILED: bytes " .. fname) end
	else
		local accepted = false
		if tname == "EmptyZero" then accepted = BP.ReadEmptyZero(rs, BP.NewEmptyZero())
		elseif tname == "Phase0" then accepted = BP.ReadPhase0(rs, BP.NewPhase0())
		elseif tname == "Phase1" then accepted = BP.ReadPhase1(rs, BP.NewPhase1())
		elseif tname == "Phase7" then accepted = BP.ReadPhase7(rs, BP.NewPhase7())
		elseif tname == "Phase31" then accepted = BP.ReadPhase31(rs, BP.NewPhase31())
		elseif tname == "Phase32" then accepted = BP.ReadPhase32(rs, BP.NewPhase32())
		elseif tname == "Phase33" then accepted = BP.ReadPhase33(rs, BP.NewPhase33())
		elseif tname == "Phase56" then accepted = BP.ReadPhase56(rs, BP.NewPhase56())
		elseif tname == "Phase63" then accepted = BP.ReadPhase63(rs, BP.NewPhase63())
		end
		if accepted then error("FAILED: accepted " .. fname) end
	end
	count = count + 1
end
print("BITPHASE_OK moo " .. tostring(count))
`
	if err := os.WriteFile(filepath.Join(genDir, "probe.moo"), []byte(src), 0o644); err != nil {
		return err
	}
	mooBin := resolveGlobTool(repoRoot, "MOO", "dist/moo-*/moo", "moo")
	out, err := runChecked(genDir, nil, mooBin, "probe.moo")
	return requireToken(out, err, "moo")
}

func verifySquirrel(repoRoot, schemaBin, corpusDir, workDir string, cases []manifestCase) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateBitPhase(repoRoot, schemaBin, "", "squirrel", genDir); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, `local serialize = dofile(%q, true);
local loader = dofile("./gen/schema_module.nut", true);
local BP = loader.require("BitPhase");
local dir = %q;

local function check(ok, msg) {
  if (!ok) { print("FAILED: " + msg + "\n"); throw "FAILED: " + msg; }
}
local function load_blob(fname) {
  local f = file(dir + "/" + fname, "rb");
  local n = f.len();
  local out = blob(n);
  if (n > 0) out.writeblob(f.readblob(n));
  f.close();
  out.seek(0);
  return out;
}
local function blob_eq(a, b, len) {
  if (b.len() != len) return false;
  for (local i = 0; i < len; i++) if ((a[i] & 0xff) != (b[i] & 0xff)) return false;
  return true;
}
local reuse = {
  EmptyZero = BP.EmptyZero(),
  Phase0 = BP.Phase0(), Phase1 = BP.Phase1(), Phase7 = BP.Phase7(), Phase31 = BP.Phase31(),
  Phase32 = BP.Phase32(), Phase33 = BP.Phase33(), Phase56 = BP.Phase56(), Phase63 = BP.Phase63(),
};
local cases = [
`, filepath.Join(repoRoot, "internal/codegen/squirrel/serialize/serialize.nut"), corpusDir)
	for _, c := range cases {
		fmt.Fprintf(&b, "  [%q, %q, %q],\n", c.Kind, c.Type, c.File)
	}
	b.WriteString(`];
local count = 0;
foreach (c in cases) {
  local kind = c[0], tname = c[1], fname = c[2];
  local raw = load_blob(fname);
  local rs = serialize.ReadStream(raw);
  if (kind == "valid") {
    local v = reuse[tname];
    check(BP["Read" + tname](rs, v), "read " + fname);
    local wbuf = blob(512);
    local ws = serialize.WriteStream(wbuf);
    check(BP["Write" + tname](ws, v), "write " + fname);
    ws.flush();
    check(blob_eq(wbuf, raw, ws.bytesProcessed()), "bytes " + fname);
  } else {
    local tmp = BP[tname]();
    check(!BP["Read" + tname](rs, tmp), "accepted " + fname);
  }
  count++;
}
print("BITPHASE_OK squirrel " + count + "\n");
`)
	probe := filepath.Join(workDir, "probe.nut")
	if err := os.WriteFile(probe, []byte(b.String()), 0o644); err != nil {
		return err
	}
	sqBin := resolveGlobTool(repoRoot, "SQUIRREL", "dist/squirrel-*/bin/sq", "sq")
	if v := os.Getenv("SQ"); toolUsable(v) {
		sqBin = v
	}
	out, err := runChecked(workDir, nil, sqBin, probe)
	return requireToken(out, err, "squirrel")
}

func verifyGoose(repoRoot, schemaBin, corpusDir, workDir string, cases []manifestCase) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateBitPhase(repoRoot, schemaBin, "", "goose", genDir); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, `import os;
import .BitPhase;
import .schema_rt;
import .serialize;

fn check(ok: bool, msg: const u8[:]) {
    if !ok { write_stderr(str("FAILED: ", msg, "\n")); exit(1); }
}
fn same(a: const u8[:], b: const u8[:]) -> bool {
    if a.len != b.len { return false; }
    for i in a.len { if a[i] != b[i] { return false; } }
    return true;
}
fn load_bin(fname: const u8[:], out: u8[>..]&) {
    out.clear();
    check(read_file(str(%q, "/", fname), out), fname);
}
fn main() {
    var raw: u8[>..] = [];
    var pbuf: u8[512] = [0; 512];
    var wbuf: u8[512] = [0; 512];
    var r_EmptyZero = bitphase::NewEmptyZero();
    var r_Phase0 = bitphase::NewPhase0();
    var r_Phase1 = bitphase::NewPhase1();
    var r_Phase7 = bitphase::NewPhase7();
    var r_Phase31 = bitphase::NewPhase31();
    var r_Phase32 = bitphase::NewPhase32();
    var r_Phase33 = bitphase::NewPhase33();
    var r_Phase56 = bitphase::NewPhase56();
    var r_Phase63 = bitphase::NewPhase63();
`, corpusDir)
	for _, c := range cases {
		if c.Kind == "valid" {
			fmt.Fprintf(&b, `    {
        load_bin(%q, raw);
        var rs = serialize_read_stream_init_padded(pbuf, raw);
        check(bitphase::Read%s(rs, r_%s), "read %s");
        for k in 512 { wbuf[k] = 0; }
        var ws = serialize_write_stream_init(wbuf);
        check(bitphase::Write%s(ws, r_%s) == SERIALIZE_ERROR_NONE, "write %s");
        serialize_flush(ws);
        check(same(wbuf[0..serialize_bytes_processed(ws)], raw), "bytes %s");
    }
`, c.File, c.Type, c.Type, c.File, c.Type, c.Type, c.File, c.File)
		} else {
			fmt.Fprintf(&b, `    {
        load_bin(%q, raw);
        var tmp = bitphase::New%s();
        var rs = serialize_read_stream_init_padded(pbuf, raw);
        check(!bitphase::Read%s(rs, tmp), "accepted %s");
    }
`, c.File, c.Type, c.Type, c.File)
		}
	}
	fmt.Fprintf(&b, "    print(str(\"BITPHASE_OK goose \", %d, \"\\n\"));\n}\n", len(cases))
	srcFile := filepath.Join(genDir, "probe.goose")
	exeFile := filepath.Join(genDir, "probe_bin")
	if err := os.WriteFile(srcFile, []byte(b.String()), 0o644); err != nil {
		return err
	}
	gooseBin := resolveGlobTool(repoRoot, "GOOSE", "dist/goose-*/goose", resolveGlobTool(repoRoot, "GOOSE", "dist/goose-*/bin/goose", "goose"))
	if out, err := runChecked(genDir, nil, gooseBin, "build", "-O2", "-o", exeFile, srcFile); err != nil {
		return fmt.Errorf("goose build: %w\n%s", err, out)
	}
	out, err := runChecked(genDir, nil, exeFile)
	return requireToken(out, err, "goose")
}

func verifyLobster(repoRoot, schemaBin, corpusDir, workDir string, cases []manifestCase) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateBitPhase(repoRoot, schemaBin, "", "lobster", genDir); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, `import .BitPhase
import .schema_runtime
import .serialize

def check(ok:bool, msg:string):
    if not ok:
        print("FAILED: " + msg)
        assert false

def load_bin(fname:string) -> string:
    let d = read_file(%q + "/" + fname)
    check(d != nil, "open " + fname)
    return if d: d else: ""

let r_EmptyZero = bitphase.NewEmptyZero()
let r_Phase0 = bitphase.NewPhase0()
let r_Phase1 = bitphase.NewPhase1()
let r_Phase7 = bitphase.NewPhase7()
let r_Phase31 = bitphase.NewPhase31()
let r_Phase32 = bitphase.NewPhase32()
let r_Phase33 = bitphase.NewPhase33()
let r_Phase56 = bitphase.NewPhase56()
let r_Phase63 = bitphase.NewPhase63()
`, corpusDir)
	for _, c := range cases {
		if c.Kind == "valid" {
			fmt.Fprintf(&b, `if true:
    let raw = load_bin(%q)
    let rs = serialize_read_stream_padded(raw)
    check(bitphase.Read%s(rs, r_%s), "read %s")
    let ws = serialize_write_stream(schema_rt.zeros(512))
    check(bitphase.Write%s(ws, r_%s), "write %s")
    ws.flush()
    check(substring(ws.buffer, 0, ws.bytes_processed()) == raw, "bytes %s")
`, c.File, c.Type, c.Type, c.File, c.Type, c.Type, c.File, c.File)
		} else {
			fmt.Fprintf(&b, `if true:
    let raw = load_bin(%q)
    let tmp = bitphase.New%s()
    let rs = serialize_read_stream_padded(raw)
    check(not bitphase.Read%s(rs, tmp), "accepted %s")
`, c.File, c.Type, c.Type, c.File)
		}
	}
	fmt.Fprintf(&b, "print(\"BITPHASE_OK lobster %d\")\n", len(cases))
	probe := filepath.Join(genDir, "probe.lobster")
	if err := os.WriteFile(probe, []byte(b.String()), 0o644); err != nil {
		return err
	}
	lobsterBin := resolveGlobTool(repoRoot, "LOBSTER", "dist/lobster-*/bin/lobster", "lobster")
	out, err := runChecked(genDir, nil, lobsterBin, probe)
	return requireToken(out, err, "lobster")
}

func resolveGlobTool(repoRoot, envKey, pattern, fallback string) string {
	if v := os.Getenv(envKey); toolUsable(v) {
		return v
	}
	matches, _ := filepath.Glob(filepath.Join(repoRoot, pattern))
	for _, matche := range slices.Backward(matches) {
		if fileExists(matche) {
			return matche
		}
	}
	return fallback
}

func resolvePythonTool(repoRoot string) string {
	for _, k := range []string{"PYTHON3", "PYTHON"} {
		if v := os.Getenv(k); toolUsable(v) {
			return v
		}
	}
	matches, _ := filepath.Glob(filepath.Join(repoRoot, "dist/python-*/bin/python3"))
	if len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return "python3"
}

func resolveRubyTool(repoRoot string) string {
	if v := os.Getenv("RUBY"); toolUsable(v) {
		return v
	}
	matches, _ := filepath.Glob(filepath.Join(repoRoot, "dist/ruby-*/bin/ruby"))
	if len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return "ruby"
}
