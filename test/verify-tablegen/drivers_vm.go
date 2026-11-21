package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

func resolvePythonTool(repoRoot string) string {
	for _, k := range []string{"PYTHON3", "PYTHON"} {
		if v := os.Getenv(k); toolUsable(v) {
			return v
		}
	}
	return resolveGlobTool(repoRoot, "PYTHON3", "dist/python-*/bin/python3", "python3")
}

func resolveRubyTool(repoRoot string) string {
	if v := os.Getenv("RUBY"); toolUsable(v) {
		return v
	}
	return resolveGlobTool(repoRoot, "RUBY", "dist/ruby-*/bin/ruby", "ruby")
}

// ============================================================================
// 10. JavaScript (ESM strict + NODE_ENV=production + Block/Cook/Flat imports)
// ============================================================================

func verifyJS(repoRoot, schemaBin, packFile, workDir string, units []UnitSpec, caseCount int) error {
	genRoot := filepath.Join(workDir, "gen")
	if err := generateAllUnits(repoRoot, schemaBin, "", "js", units, genRoot); err != nil {
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

	var imp strings.Builder
	var reg strings.Builder
	for i, u := range units {
		upPkg := toUpperFirst(u.Pkg)
		fmt.Fprintf(&imp, "import * as P%d from \"./gen/%s/%s.js\";\n", i, u.Pkg, u.FileBase)
		fmt.Fprintf(&imp, "import * as UT%d from \"./gen/%s/%sTable.js\";\n", i, u.Pkg, upPkg)
		fmt.Fprintf(&imp, "import * as FT%d from \"./gen/%s/%sTable.js\";\n", i, u.Pkg, u.FileBase)
		fmt.Fprintf(&imp, "import * as V%d from \"./gen/%s/%sVariable.js\";\n", i, u.Pkg, upPkg)
		if fileExists(filepath.Join(genRoot, u.Pkg, u.FileBase+"Block.js")) {
			fmt.Fprintf(&imp, "import * as B%d from \"./gen/%s/%sBlock.js\"; void B%d;\n", i, u.Pkg, u.FileBase, i)
		}
		if fileExists(filepath.Join(genRoot, u.Pkg, u.FileBase+"Cook.js")) {
			fmt.Fprintf(&imp, "import * as C%d from \"./gen/%s/%sCook.js\"; void C%d;\n", i, u.Pkg, u.FileBase, i)
		}
		fmt.Fprintf(&reg, "units.set(%q, { P: P%d, UT: UT%d, FT: FT%d, V: V%d });\n", u.Pkg, i, i, i, i)
	}

	jsSrc := fmt.Sprintf(`import fs from "node:fs";
%s
const units = new Map();
%s

function die(id, why) {
  console.error("FAILED: " + id + ": " + why);
  process.exit(1);
}
function checkBytes(id, got, want) {
  if (!got || got.length !== want.length) {
    die(id, "len got=" + (got ? got.length : -1) + " want=" + want.length);
  }
  for (let i = 0; i < want.length; i++) {
    if (got[i] !== want[i]) {
      die(id, "byte[" + i + "] got=" + got[i].toString(16) + " want=" + want[i].toString(16));
    }
  }
}

const raw = fs.readFileSync(process.argv[2]);
const dv = new DataView(raw.buffer, raw.byteOffset, raw.byteLength);
let at = 0;
const dec = new TextDecoder("utf-8", { fatal: true });
const enc = new TextEncoder();
function ru32() { const v = dv.getUint32(at, true); at += 4; return v; }
function rstr() { const n = dv.getUint16(at, true); at += 2; const s = dec.decode(raw.subarray(at, at + n)); at += n; return s; }
function rvec() { const n = ru32(); const b = new Uint8Array(n); b.set(raw.subarray(at, at + n)); at += n; return b; }

const total = ru32();
for (let idx = 0; idx < total; idx++) {
  const id = rstr(), pkg = rstr(), root = rstr();
  const op = raw[at++];
  const aux = rvec(), inp = rvec(), want1 = rvec(), want2 = rvec();
  const u = units.get(pkg);
  if (!u) die(id, "unknown pkg " + pkg);

  if (op === 1) {
    const rep = new u.V.TableVariableReport();
    const val = u.V[root + "Load"](inp, rep);
    const out = u.V[root + "Save"](val);
    checkBytes(id, out, want1);
  } else if (op === 2) {
    const ret = new u.V.TableVariableRetain(new Uint8Array(262144), new Array(8192));
    const rep = new u.V.TableVariableReport();
    const val = u.V[root + "LoadRetain"](inp, ret, rep);
    const srep = new u.V.TableVariableReport();
    const out = u.V[root + "SaveRetain"](val, ret, srep);
    checkBytes(id, out, want1);
  } else if (op === 3) {
    const vocab = new u.UT.TableVocabulary();
    vocab.Init(new Array(4096));
    const arep = new u.UT.TableFixedReport();
    const ann = aux.length > 0 ? aux : u.UT.TableMessageAnnouncement;
    if (!u.UT.AnnounceRead(vocab, ann, arep)) die(id, "AnnounceRead");
    const newRoot = () => (u.UT[root] ? new u.UT[root]() : new u.P[root]());
    const batch = Array.from({ length: 16 }, newRoot);
    const lrep = new u.UT.TableFixedReport();
    const cnt = u.FT[root + "LoadMessages"](batch, vocab, inp, lrep);
    if (cnt < 0 || lrep.Damaged || (lrep.Verdict && lrep.Verdict !== 0)) die(id, "LoadMessages");
    const slice = batch.slice(0, cnt);
    const mrep = new u.UT.TableFixedReport();
    const mlen = u.FT[root + "MeasureMessages"](slice, mrep);
    if (mlen < 0 || mrep.Damaged || (mrep.Verdict && mrep.Verdict !== 0)) die(id, "MeasureMessages");
    const out = new Uint8Array(mlen);
    const srep = new u.UT.TableFixedReport();
    const wn = u.FT[root + "SaveMessages"](slice, out, srep);
    if (wn !== mlen || srep.Damaged || (srep.Verdict && srep.Verdict !== 0)) die(id, "SaveMessages");
    checkBytes(id, out, want1);
  } else if (op === 4) {
    const batch = [u.UT[root] ? new u.UT[root]() : new u.P[root]()];
    const plan = u.FT[root + "FixedNewPlan"](256, 256);
    const rep = new u.UT.TableFixedReport();
    const cnt = u.FT[root + "FixedLoad"](batch, 1, inp, inp.length, plan, rep);
    const flen = u.FT[root + "FixedMeasure"](1);
    const out = new Uint8Array(flen);
    const wn = u.FT[root + "FixedSave"](batch, 1, out);
    if (wn !== flen) die(id, "FixedSave");
    checkBytes(id, out, want1);
  } else if (op === 5) {
    const batch = [u.UT[root] ? new u.UT[root]() : new u.P[root]()];
    const plan = u.FT[root + "FixedNewPlan"](256, 256);
    const rep = new u.UT.TableFixedReport();
    const cnt = u.FT[root + "FixedLoad"](batch, 1, inp, inp.length, plan, rep);
    if (cnt >= 0 && !rep.damaged && !rep.refused && !rep.Damaged && (!rep.Verdict || rep.Verdict === 0)) {
      die(id, "FixedLoad accepted writer-bound breach");
    }
  } else if (op === 6) {
    const rep = new u.V.TableVariableReport();
    const txt = dec.decode(inp);
    const val = u.V[root + "FromJson"](txt, rep);
    const jtxt = u.V[root + "ToJson"](val);
    if (jtxt === null || jtxt === undefined) die(id, "ToJson");
    checkBytes(id, enc.encode(jtxt), want1);
    if (want2.length > 0) {
      const wout = u.V[root + "Save"](val);
      checkBytes(id + ":save", wout, want2);
    }
  } else {
    die(id, "unknown op " + op);
  }
}
console.log("TABLEGEN_OK js " + total);
`, imp.String(), reg.String())

	probe := filepath.Join(workDir, "main.mjs")
	if err := os.WriteFile(probe, []byte(jsSrc), 0o644); err != nil {
		return err
	}
	node := resolveTool(repoRoot, "NODE", "dist/node-v25.4.0/bin/node", "node")
	if out, err := runChecked(workDir, nil, node, probe, packFile); err != nil {
		return fmt.Errorf("js checked: %w\n%s", err, out)
	}
	out, err := runChecked(workDir, []string{"NODE_ENV=production"}, node, probe, packFile)
	return requireToken(out, err, "js", caseCount)
}

// ============================================================================
// 11. Python (python3 -W error -I -B + block/cook module load check)
// ============================================================================

func verifyPython(repoRoot, schemaBin, packFile, workDir string, units []UnitSpec, caseCount int) error {
	genRoot := filepath.Join(workDir, "gen")
	if err := generateAllUnits(repoRoot, schemaBin, "", "python", units, genRoot); err != nil {
		return err
	}

	var unitPaths strings.Builder
	for _, u := range units {
		fmt.Fprintf(&unitPaths, "    %q: %q,\n", u.Pkg, filepath.Join(genRoot, u.Pkg))
	}

	pySrc := fmt.Sprintf(`import importlib
import struct
import sys

UNIT_DIRS = {
%s}
for p in UNIT_DIRS.values():
    if p not in sys.path:
        sys.path.insert(0, p)

mods = {}
for pkg in UNIT_DIRS:
    m = importlib.import_module(pkg)
    importlib.import_module(pkg + "._schema.block")
    importlib.import_module(pkg + "._schema.cook")
    mods[pkg] = m

def die(cid: str, why: str) -> None:
    sys.stderr.write(f"FAILED: {cid}: {why}\n")
    sys.exit(1)

def check_bytes(cid: str, got: bytes, want: bytes) -> None:
    if got != want:
        die(cid, f"bytes mismatch len got={len(got)} want={len(want)}")

def is_bad(rep: object) -> bool:
    return bool(getattr(rep, "damaged", False) or getattr(rep, "refused", False))

with open(sys.argv[1], "rb") as _f:
    raw = _f.read()
at = 0

def ru32() -> int:
    global at
    v = struct.unpack_from("<I", raw, at)[0]
    at += 4
    return v

def rstr() -> str:
    global at
    n = struct.unpack_from("<H", raw, at)[0]
    at += 2
    s = raw[at : at + n].decode("utf-8")
    at += n
    return s

def rvec() -> bytes:
    global at
    n = ru32()
    b = raw[at : at + n]
    at += n
    return b

total = ru32()
for _ in range(total):
    cid = rstr()
    pkg = rstr()
    root = rstr()
    op = raw[at]
    at += 1
    aux = rvec()
    inp = rvec()
    want1 = rvec()
    want2 = rvec()

    m = mods[pkg]
    cls = getattr(m, root)

    if op == 1:
        rep = m.LoadReport()
        try:
            v = cls.load(inp, rep)
        except Exception as exc:
            die(cid, f"load raised {exc!r}")
        if is_bad(rep):
            die(cid, "load refused")
        out = v.save()
        check_bytes(cid, out, want1)
    elif op == 2:
        ret = m.TableRetain(bytearray(262144), bytearray(24 * 8192))
        rep = m.LoadReport()
        try:
            v = cls.load_retain(inp, retain=ret, report=rep)
        except TypeError:
            v = cls.load_retain(inp, ret, rep)
        except Exception as exc:
            die(cid, f"load_retain raised {exc!r}")
        if is_bad(rep):
            die(cid, "load_retain refused")
        srep = m.LoadReport()
        out = v.save_retain(ret, srep)
        if is_bad(srep):
            die(cid, "save_retain refused")
        check_bytes(cid, out, want1)
    elif op == 3:
        vocab = m.MessageVocabulary()
        ann = aux if len(aux) > 0 else m.ANNOUNCEMENT
        try:
            m.announce_read(vocab, ann)
            vals = m.load_messages(cls, vocab, inp, 16)
            out = m.save_messages(cls, vals)
        except Exception as exc:
            die(cid, f"message raised {exc!r}")
        check_bytes(cid, out, want1)
    elif op == 4:
        try:
            vals = cls.fixed_load(inp, 1)
            if len(vals) != 1:
                die(cid, f"fixed_load count={len(vals)}")
            out = cls.fixed_save(vals)
        except Exception as exc:
            die(cid, f"fixed raised {exc!r}")
        check_bytes(cid, out, want1)
    elif op == 5:
        refused = False
        try:
            vals = cls.fixed_load(inp, 1)
            if vals is None or len(vals) != 1:
                refused = True
        except Exception:
            refused = True
        if not refused:
            die(cid, "fixed_load accepted writer-bound breach")
    elif op == 6:
        rep = m.LoadReport()
        try:
            v = cls.from_json(inp.decode("utf-8"), rep)
            if is_bad(rep):
                die(cid, "from_json refused")
            jtxt = v.to_json().encode("utf-8")
        except Exception as exc:
            die(cid, f"json raised {exc!r}")
        check_bytes(cid, jtxt, want1)
        if len(want2) > 0:
            check_bytes(cid + ":save", v.save(), want2)
    elif op == 7:
        rep = m.LoadReport()
        try:
            v = cls.from_json(inp.decode("utf-8"), rep)
            if is_bad(rep):
                die(cid, "from_json refused")
            jtxt = v.to_json().encode("utf-8")
        except Exception as exc:
            die(cid, f"json raised {exc!r}")
        check_bytes(cid, jtxt, want1)
        try:
            out = m.save_messages(cls, [v])
        except Exception as exc:
            die(cid, f"save_messages after from_json raised {exc!r}")
        check_bytes(cid + ":save_messages", out, want2)
    else:
        die(cid, f"unknown op {op}")

print(f"TABLEGEN_OK python {total}")
`, unitPaths.String())

	probe := filepath.Join(workDir, "main.py")
	if err := os.WriteFile(probe, []byte(pySrc), 0o644); err != nil {
		return err
	}
	py := resolvePythonTool(repoRoot)
	out, err := runChecked(workDir, nil, py, "-W", "error", "-I", "-B", probe, packFile)
	return requireToken(out, err, "python", caseCount)
}

// ============================================================================
// 12. Ruby (ruby --disable-gems -w + zero-warning assertion)
// ============================================================================

func verifyRuby(repoRoot, schemaBin, packFile, workDir string, units []UnitSpec, caseCount int) error {
	genRoot := filepath.Join(workDir, "gen")
	if err := generateAllUnits(repoRoot, schemaBin, "", "ruby", units, genRoot); err != nil {
		return err
	}

	var loads strings.Builder
	for _, u := range units {
		fmt.Fprintf(&loads, "$LOAD_PATH.unshift(%q)\nrequire %q\nMODS[%q] = Object.const_get(%q)\n",
			filepath.Join(genRoot, u.Pkg), u.Pkg, u.Pkg, toUpperFirst(u.Pkg))
	}

	rbSrc := fmt.Sprintf(`# frozen_string_literal: true
MODS = {}
%s

def die(cid, why)
  $stderr.puts("FAILED: #{cid}: #{why}")
  exit(1)
end

def check_bytes(cid, got, want)
  g = got.b
  w = want.b
  die(cid, "bytes mismatch len got=#{g.bytesize} want=#{w.bytesize}") unless g == w
end

raw = File.binread(ARGV[0])
at = 0
ru32 = lambda do
  v = raw.byteslice(at, 4).unpack1("V")
  at += 4
  v
end
rstr = lambda do
  n = raw.byteslice(at, 2).unpack1("v")
  at += 2
  s = raw.byteslice(at, n).force_encoding("UTF-8")
  at += n
  s
end
rvec = lambda do
  n = ru32.call
  b = raw.byteslice(at, n)
  at += n
  b
end

total = ru32.call
total.times do
  cid = rstr.call
  pkg = rstr.call
  root = rstr.call
  op = raw.getbyte(at)
  at += 1
  aux = rvec.call
  inp = rvec.call
  want1 = rvec.call
  want2 = rvec.call

  mod = MODS.fetch(pkg)
  cls = mod.const_get(root)

  case op
  when 1
    begin
      rep = mod::LoadReport.new
      v = cls.load(inp, rep)
      out = v.save
    rescue StandardError => e
      die(cid, "load raised #{e.class}: #{e.message}")
    end
    check_bytes(cid, out, want1)
  when 2
    begin
      ret = mod::TableRetain.new
      rep = mod::LoadReport.new
      v = cls.load_retain(inp, ret, rep)
      srep = mod::LoadReport.new
      out = v.save_retain(ret, srep)
    rescue StandardError => e
      die(cid, "retain raised #{e.class}: #{e.message}")
    end
    check_bytes(cid, out, want1)
  when 3
    begin
      vocab = mod::MessageVocabulary.new
      ann = aux.bytesize > 0 ? aux : mod::ANNOUNCEMENT
      mod.announce_read(vocab, ann)
      vals = mod.load_messages(cls, vocab, inp, 16)
      out = mod.save_messages(cls, vals)
    rescue StandardError => e
      die(cid, "message raised #{e.class}: #{e.message}")
    end
    check_bytes(cid, out, want1)
  when 4
    begin
      vals = cls.fixed_load(inp, 1)
      die(cid, "fixed_load count=#{vals.length}") unless vals.length == 1
      out = cls.fixed_save(vals)
    rescue StandardError => e
      die(cid, "fixed raised #{e.class}: #{e.message}")
    end
    check_bytes(cid, out, want1)
  when 5
    refused = false
    begin
      vals = cls.fixed_load(inp, 1)
      refused = true if vals.nil? || vals.length != 1
    rescue mod::Refused
      refused = true
    end
    die(cid, "fixed_load accepted writer-bound breach") unless refused
  when 6
    begin
      rep = mod::LoadReport.new
      v = cls.from_json(inp.dup.force_encoding("UTF-8"), rep)
      jtxt = v.to_json
    rescue StandardError => e
      die(cid, "json raised #{e.class}: #{e.message}")
    end
    check_bytes(cid, jtxt, want1)
    check_bytes("#{cid}:save", v.save, want2) if want2.bytesize > 0
  when 7
    begin
      rep = mod::LoadReport.new
      v = cls.from_json(inp.dup.force_encoding("UTF-8"), rep)
      jtxt = v.to_json
    rescue StandardError => e
      die(cid, "json raised #{e.class}: #{e.message}")
    end
    check_bytes(cid, jtxt, want1)
    begin
      out = mod.save_messages(cls, [v])
    rescue StandardError => e
      die(cid, "save_messages after from_json raised #{e.class}: #{e.message}")
    end
    check_bytes("#{cid}:save_messages", out, want2)
  else
    die(cid, "unknown op #{op}")
  end
end

puts "TABLEGEN_OK ruby #{total}"
`, loads.String())

	probe := filepath.Join(workDir, "main.rb")
	if err := os.WriteFile(probe, []byte(rbSrc), 0o644); err != nil {
		return err
	}
	rb := resolveRubyTool(repoRoot)
	out, err := runChecked(workDir, nil, rb, "--disable-gems", "-w", probe, packFile)
	if strings.Contains(out, "warning:") {
		return fmt.Errorf("ruby emitted warnings under -w:\n%s", out)
	}
	return requireToken(out, err, "ruby", caseCount)
}

// ============================================================================
// 13. Lua (luac -p syntax/bytecode check on all emitted files + isolated per-unit run)
// ============================================================================

func verifyLua(repoRoot, schemaBin, packFile, workDir string, units []UnitSpec, caseCount int) error {
	genRoot := filepath.Join(workDir, "gen")
	if err := generateAllUnits(repoRoot, schemaBin, "", "lua", units, genRoot); err != nil {
		return err
	}

	luacBin := resolveGlobTool(repoRoot, "LUAC", "dist/lua-*/bin/luac", "luac")
	if toolUsable(luacBin) {
		var luaFiles []string
		for _, u := range units {
			entries, err := os.ReadDir(filepath.Join(genRoot, u.Pkg))
			if err != nil {
				return err
			}
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".lua") {
					luaFiles = append(luaFiles, filepath.Join(genRoot, u.Pkg, e.Name()))
				}
			}
		}
		args := append([]string{"-p"}, luaFiles...)
		if out, err := runChecked(workDir, nil, luacBin, args...); err != nil {
			return fmt.Errorf("luac -p check failed: %w\n%s", err, out)
		}
	}

	var umap strings.Builder
	for _, u := range units {
		fmt.Fprintf(&umap, "  [%q] = { dir = %q, mod = %q, fb = %q },\n",
			u.Pkg, filepath.Join(genRoot, u.Pkg), toUpperFirst(u.Pkg)+"Tables", u.FileBase)
	}

	luaSrc := fmt.Sprintf(`local UNIT_INFO = {
%s}

local base_loaded = {}
for k in pairs(package.loaded) do base_loaded[k] = true end

local loaded_units = {}
local function get_unit(pkg)
  if loaded_units[pkg] then return loaded_units[pkg] end
  local info = assert(UNIT_INFO[pkg], "unknown pkg " .. tostring(pkg))
  for k in pairs(package.loaded) do
    if not base_loaded[k] then package.loaded[k] = nil end
  end
  package.path = info.dir .. "/?.lua"
  package.cpath = ""
  local P = require(info.fb)
  local T = require(info.mod)
  local L = require("load")
  local S = require("save")
  local R = require("retain")
  local J = require("json")
  local B = require("block")
  local C = require("cook")
  local own_voc = {}
  assert(T.AnnounceRead(own_voc, T.Announcement, {}), "own AnnounceRead " .. pkg)
  local u = { P = P, T = T, L = L, S = S, R = R, J = J, B = B, C = C, own_voc = own_voc }
  loaded_units[pkg] = u
  return u
end

local function die(cid, why)
  io.stderr:write("FAILED: " .. cid .. ": " .. tostring(why) .. "\n")
  os.exit(1)
end

local function is_bad(rep)
  if not rep then return false end
  if rep.damaged then return true end
  if rep.refused and rep.refused ~= 0 then return true end
  return false
end

local function check_bytes(cid, got, want)
  if got ~= want then
    die(cid, string.format("bytes mismatch len got=%%d want=%%d", got and #got or -1, #want))
  end
end

local f = assert(io.open(arg[1], "rb"))
local raw = f:read("*a")
f:close()

local at = 1
local function ru32()
  local v = string.unpack("<I4", raw, at)
  at = at + 4
  return v
end
local function rstr()
  local n = string.unpack("<I2", raw, at)
  at = at + 2
  local s = string.sub(raw, at, at + n - 1)
  at = at + n
  return s
end
local function rvec()
  local n = ru32()
  local s = string.sub(raw, at, at + n - 1)
  at = at + n
  return s
end

local total = ru32()
for _ = 1, total do
  local cid = rstr()
  local pkg = rstr()
  local root = rstr()
  local op = string.byte(raw, at)
  at = at + 1
  local aux = rvec()
  local inp = rvec()
  local want1 = rvec()
  local want2 = rvec()

  local u = get_unit(pkg)
  if op == 1 then
    local rep = {}
    local v = u.L.Load(u.T, root, inp, rep)
    if v == nil or is_bad(rep) then die(cid, "Load") end
    local out = u.S.Save(u.T, root, v)
    check_bytes(cid, out, want1)
  elseif op == 2 then
    local store = { cap = 262144, id_cap = 8192 }
    local rep = {}
    local v = u.R.LoadRetain(u.T, root, inp, store, rep)
    if v == nil or is_bad(rep) then die(cid, "LoadRetain") end
    local srep = {}
    local out = u.R.SaveRetain(u.T, root, v, store, srep)
    if out == nil or is_bad(srep) then die(cid, "SaveRetain") end
    check_bytes(cid, out, want1)
  elseif op == 3 then
    local voc = u.own_voc
    if #aux > 0 then
      voc = {}
      local arep = {}
      if not u.T.AnnounceRead(voc, aux, arep) then die(cid, "peer AnnounceRead") end
    end
    local rep = {}
    local vals = u.T.LoadMessages(root, voc, inp, rep)
    if vals == nil or is_bad(rep) then die(cid, "LoadMessages") end
    local out = u.T.SaveMessages(root, u.own_voc, vals)
    if out == nil then die(cid, "SaveMessages") end
    check_bytes(cid, out, want1)
  elseif op == 4 then
    local ok, vals = pcall(u.T.FixedLoad, root, inp, 1)
    if not ok or vals == nil or #vals ~= 1 then die(cid, "FixedLoad: " .. tostring(vals)) end
    local out = u.T.FixedSave(root, vals)
    check_bytes(cid, out, want1)
  elseif op == 5 then
    local rep = {}
    local ok, vals = pcall(u.T.FixedLoad, root, inp, 1, rep)
    if ok and vals ~= nil and #vals == 1 and not is_bad(rep) then
      die(cid, "FixedLoad accepted writer-bound breach")
    end
  elseif op == 6 then
    local rep = {}
    local v = u.J.FromJson(u.T, root, inp, rep)
    if v == nil or is_bad(rep) then die(cid, "FromJson") end
    local jtxt, jwhy = u.J.ToJson(u.T, root, v)
    if jtxt == nil then die(cid, "ToJson: " .. tostring(jwhy)) end
    check_bytes(cid, jtxt, want1)
    if #want2 > 0 then
      local wout = u.S.Save(u.T, root, v)
      check_bytes(cid .. ":save", wout, want2)
    end
  elseif op == 7 then
    local rep = {}
    local v = u.J.FromJson(u.T, root, inp, rep)
    if v == nil or is_bad(rep) then die(cid, "FromJson") end
    local jtxt, jwhy = u.J.ToJson(u.T, root, v)
    if jtxt == nil then die(cid, "ToJson: " .. tostring(jwhy)) end
    check_bytes(cid, jtxt, want1)
    local out, owhy = u.T.SaveMessages(root, u.own_voc, { v })
    if out == nil then die(cid, "SaveMessages after FromJson: " .. tostring(owhy)) end
    check_bytes(cid .. ":save_messages", out, want2)
  else
    die(cid, "unknown op " .. tostring(op))
  end
end

print("TABLEGEN_OK lua " .. tostring(total))
`, umap.String())

	probe := filepath.Join(workDir, "main.lua")
	if err := os.WriteFile(probe, []byte(luaSrc), 0o644); err != nil {
		return err
	}
	luaBin := resolveGlobTool(repoRoot, "LUA", "dist/lua-*/bin/lua", "lua")
	out, err := runChecked(workDir, nil, luaBin, probe, packFile)
	return requireToken(out, err, "lua", caseCount)
}

func writeLuaCaseFixtures(cases []CorpusCase, isMoo bool) string {
	var b strings.Builder
	b.WriteString("--!strict\nlocal M = {}\n")
	if isMoo {
		b.WriteString("local function bytes(t: {number}): buffer\n\tlocal buf = buffer.create(#t)\n\tfor i = 1, #t do buffer.writeu8(buf, i - 1, t[i]) end\n\treturn buf\nend\n")
	} else {
		b.WriteString("local function bytes(t: {number}): buffer\n\tlocal buf = buffer.create(#t)\n\tfor i, v in ipairs(t) do buffer.writeu8(buf, i - 1, v) end\n\treturn buf\nend\n")
	}
	writeBuf := func(raw []byte) {
		b.WriteString("bytes({")
		for i, by := range raw {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "%d", by)
		}
		b.WriteString("})")
	}
	b.WriteString("M.cases = {\n")
	for _, c := range cases {
		fmt.Fprintf(&b, "\t{ id = %q, root = %q, op = %d, aux = ", c.ID, c.Root, c.Op)
		writeBuf(c.Aux)
		b.WriteString(", inp = ")
		writeBuf(c.Input)
		b.WriteString(", want1 = ")
		writeBuf(c.Want1)
		b.WriteString(", want2 = ")
		writeBuf(c.Want2)
		b.WriteString(" },\n")
	}
	b.WriteString("}\nreturn M\n")
	return b.String()
}

// ============================================================================
// 14. Luau (--!strict per-unit execution + Block/Cook require)
// ============================================================================

func verifyLuau(repoRoot, schemaBin, workDir string, units []UnitSpec, cases []CorpusCase) error {
	genRoot := filepath.Join(workDir, "gen")
	if err := generateAllUnits(repoRoot, schemaBin, "", "luau", units, genRoot); err != nil {
		return err
	}
	luauBin := resolveGlobTool(repoRoot, "LUAU", "dist/luau-*/luau", "luau")

	var ran atomic.Int64
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	errs := make(chan error, len(units))

	for _, u := range units {
		var ucases []CorpusCase
		for _, c := range cases {
			if c.Pkg == u.Pkg {
				ucases = append(ucases, c)
			}
		}
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			udir := filepath.Join(genRoot, u.Pkg)
			if err := os.WriteFile(filepath.Join(udir, "cases_fix.luau"), []byte(writeLuaCaseFixtures(ucases, false)), 0o644); err != nil {
				errs <- err
				return
			}
			blockReq := ""
			if fileExists(filepath.Join(udir, u.FileBase+"Block.luau")) {
				blockReq = fmt.Sprintf("local _RB = require(\"./%sBlock\")\n", u.FileBase)
			}
			src := fmt.Sprintf(`--!strict
local P = require("./%s")
local T = require("./%sTable")
%slocal L = require("./schema_load")
local S = require("./schema_save")
local R = require("./schema_retain")
local M = require("./schema_message")
local FX = require("./schema_fixed")
local J = require("./schema_json")
local F = require("./cases_fix")
local _ = P

local function die(cid: string, why: string)
	error("FAILED: " .. cid .. ": " .. why)
end
local function isBad(rep: any): boolean
	if rep == nil then return false end
	if rep.damaged == true or (type(rep.damaged) == "number" and rep.damaged ~= 0) then return true end
	if rep.refused == true or (type(rep.refused) == "number" and rep.refused ~= 0) then return true end
	return false
end
local function bufEq(a: buffer?, b: buffer): boolean
	if a == nil or buffer.len(a :: buffer) ~= buffer.len(b) then return false end
	local ab = a :: buffer
	for i = 0, buffer.len(ab) - 1 do
		if buffer.readu8(ab, i) ~= buffer.readu8(b, i) then return false end
	end
	return true
end

local ownVocab = M.NewVocabulary()
if not M.AnnounceRead(ownVocab, T.TableMessageAnnouncement, {}) then
	die("init", "own AnnounceRead")
end

for _, c in ipairs(F.cases) do
	local root: string = c.root
	local desc: any = (T :: any)[root .. "Descriptor"]
	if c.op == 1 then
		local rep: {[string]: any} = {}
		local v = L.load(root, c.inp, rep)
		if v == nil or isBad(rep) then die(c.id, "load") end
		local out = S.save(desc, v)
		if not bufEq(out, c.want1) then die(c.id, "save mismatch") end
	elseif c.op == 2 then
		local rep: {[string]: any} = {}
		local v, store = R.LoadRetain(root, c.inp, rep)
		if v == nil or isBad(rep) then die(c.id, "LoadRetain") end
		local srep: {[string]: any} = {}
		local out, _ = R.SaveRetain(root, v, store, srep)
		if out == nil or isBad(srep) or not bufEq(out, c.want1) then die(c.id, "SaveRetain") end
	elseif c.op == 3 then
		local voc = ownVocab
		if buffer.len(c.aux) > 0 then
			voc = M.NewVocabulary()
			local arep: {[string]: any} = {}
			if not M.AnnounceRead(voc, c.aux, arep) then die(c.id, "peer AnnounceRead") end
		end
		local lrep: {[string]: any} = {}
		local cnt, vals = M.LoadMessages(voc, root, c.inp, 16, lrep)
		if cnt < 0 or isBad(lrep) then die(c.id, "LoadMessages") end
		local srep: {[string]: any} = {}
		local out, _ = M.SaveMessages(ownVocab, root, vals, srep)
		if out == nil or isBad(srep) or not bufEq(out, c.want1) then die(c.id, "SaveMessages") end
	elseif c.op == 4 then
		local fxSpec = {
			hash = (T :: any)[root .. "FixedHash"],
			recordBytes = (T :: any)[root .. "FixedRecordBytes"],
			layout = (T :: any)[root .. "FixedLayout"],
			lineage = (T :: any)[root .. "FixedLineage"],
			floor = (T :: any)[root .. "FixedFloor"],
		}
		local rep: {[string]: any} = {}
		local vals = FX.read(desc, fxSpec, c.inp, rep, 1)
		if vals == nil or #(vals :: {any}) ~= 1 or isBad(rep) then die(c.id, "FX.read") end
		local out = FX.save(desc, fxSpec, vals :: {any})
		if not bufEq(out, c.want1) then die(c.id, "FX.save") end
	elseif c.op == 5 then
		local fxSpec = {
			hash = (T :: any)[root .. "FixedHash"],
			recordBytes = (T :: any)[root .. "FixedRecordBytes"],
			layout = (T :: any)[root .. "FixedLayout"],
			lineage = (T :: any)[root .. "FixedLineage"],
			floor = (T :: any)[root .. "FixedFloor"],
		}
		local rep: {[string]: any} = {}
		local vals = FX.read(desc, fxSpec, c.inp, rep, 1)
		if vals ~= nil and #(vals :: {any}) == 1 and not isBad(rep) then
			die(c.id, "FX.read accepted writer-bound breach")
		end
	elseif c.op == 6 then
		local rep: {[string]: any} = {}
		local v = J.FromJson(root, c.inp, rep)
		if v == nil or isBad(rep) then die(c.id, "FromJson") end
		local jrep: {[string]: any} = {}
		local jtxt = J.ToJson(root, v, jrep)
		if jtxt == nil or isBad(jrep) then die(c.id, "ToJson") end
		local jbuf = if type(jtxt) == "string" then buffer.fromstring(jtxt :: string) else (jtxt :: buffer)
		if not bufEq(jbuf, c.want1) then die(c.id, "ToJson mismatch") end
		if buffer.len(c.want2) > 0 then
			local wout = S.save(desc, v)
			if not bufEq(wout, c.want2) then die(c.id, "Save after FromJson") end
		end
	elseif c.op == 7 then
		local rep: {[string]: any} = {}
		local v = J.FromJson(root, c.inp, rep)
		if v == nil or isBad(rep) then die(c.id, "FromJson") end
		local jrep: {[string]: any} = {}
		local jtxt = J.ToJson(root, v, jrep)
		if jtxt == nil or isBad(jrep) then die(c.id, "ToJson") end
		local jbuf = if type(jtxt) == "string" then buffer.fromstring(jtxt :: string) else (jtxt :: buffer)
		if not bufEq(jbuf, c.want1) then die(c.id, "ToJson mismatch") end
		local srep: {[string]: any} = {}
		local out, _ = M.SaveMessages(ownVocab, root, { v }, srep)
		if out == nil or isBad(srep) or not bufEq(out, c.want2) then die(c.id, "SaveMessages after FromJson") end
	else
		die(c.id, "unknown op")
	end
end
print("UNIT_OK " .. tostring(#F.cases))
`, u.FileBase, u.FileBase, blockReq)
			if err := os.WriteFile(filepath.Join(udir, "probe.luau"), []byte(src), 0o644); err != nil {
				errs <- err
				return
			}
			out, err := runChecked(udir, nil, luauBin, "probe.luau")
			wantTok := fmt.Sprintf("UNIT_OK %d", len(ucases))
			if err != nil || strings.Contains(out, "FAILED:") || !strings.Contains(out, wantTok) {
				errs <- fmt.Errorf("luau (%s): %w (%s)", u.Pkg, err, out)
				return
			}
			ran.Add(int64(len(ucases)))
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	if int(ran.Load()) != len(cases) {
		return fmt.Errorf("luau ran %d cases, want %d", ran.Load(), len(cases))
	}
	return nil
}

// ============================================================================
// 15. Squirrel (64-bit double sq + isolated schema_module.nut per unit)
// ============================================================================

func verifySquirrel(repoRoot, schemaBin, packFile, workDir string, units []UnitSpec, caseCount int) error {
	genRoot := filepath.Join(workDir, "gen")
	if err := generateAllUnits(repoRoot, schemaBin, "", "squirrel", units, genRoot); err != nil {
		return err
	}

	var umap strings.Builder
	for _, u := range units {
		fmt.Fprintf(&umap, "  %s = { dir = %q, mod = %q, fb = %q },\n",
			u.Pkg, filepath.Join(genRoot, u.Pkg), toUpperFirst(u.Pkg)+"Tables", u.FileBase)
	}

	sqSrc := fmt.Sprintf(`if (_floatsize_ != 8) throw "squirrel requires 8-byte float build";
local serialize = dofile(%q, true);
local UNIT_INFO = {
%s};

local loaded = {};
local function get_unit(pkg) {
  if (pkg in loaded) return loaded[pkg];
  local info = UNIT_INFO[pkg];
  local loader = dofile(info.dir + "/schema_module.nut", true);
  local P = loader.require(info.fb);
  local T = loader.require(info.mod);
  local J = loader.require("schema_table_json");
  local B = loader.require("schema_table_block");
  local C = loader.require("schema_table_cook");
  local asz = T.AnnounceMeasure();
  local ann = blob(asz);
  if (T.Announce(ann) != asz) throw "own Announce failed " + pkg;
  local own_voc = {};
  if (!T.AnnounceRead(own_voc, ann, {})) throw "own AnnounceRead failed " + pkg;
  local u = { P = P, T = T, J = J, B = B, C = C, own_voc = own_voc };
  loaded[pkg] <- u;
  return u;
}

local function die(cid, why) {
  print("FAILED: " + cid + ": " + why + "\n");
  throw "FAILED: " + cid + ": " + why;
}
local function is_bad(rep) {
  if (rep == null) return false;
  if (("damaged" in rep) && rep.damaged) return true;
  if (("bodyDamaged" in rep) && rep.bodyDamaged) return true;
  if (("refused" in rep) && rep.refused) return true;
  return false;
}
local function check_blob(cid, got, got_len, want) {
  if (got == null || got_len != want.len()) die(cid, "len got=" + (got == null ? -1 : got_len) + " want=" + want.len());
  for (local i = 0; i < got_len; i++) {
    if ((got[i] & 0xff) != (want[i] & 0xff)) die(cid, "byte[" + i + "] mismatch");
  }
}
local function check_str_blob(cid, s, want) {
  if (s == null || s.len() != want.len()) die(cid, "str len got=" + (s == null ? -1 : s.len()) + " want=" + want.len());
  for (local i = 0; i < s.len(); i++) {
    if ((s[i] & 0xff) != (want[i] & 0xff)) die(cid, "str byte[" + i + "] mismatch");
  }
}

local f = file(%q, "rb");
local flen = f.len();
local raw = flen > 0 ? f.readblob(flen) : blob(0);
f.close();
local at = 0;
local function ru32() {
  local v = (raw[at] & 0xff) | ((raw[at+1] & 0xff) << 8) | ((raw[at+2] & 0xff) << 16) | ((raw[at+3] & 0xff) << 24);
  at += 4;
  return v;
}
local function rstr() {
  local n = (raw[at] & 0xff) | ((raw[at+1] & 0xff) << 8);
  at += 2;
  local s = "";
  for (local i = 0; i < n; i++) s += format("%%c", raw[at + i] & 0xff);
  at += n;
  return s;
}
local function rvec() {
  local n = ru32();
  local b = blob(n);
  if (n > 0) {
    raw.seek(at);
    b.writeblob(raw.readblob(n));
    b.seek(0);
  }
  at += n;
  return b;
}

local total = ru32();
for (local idx = 0; idx < total; idx++) {
  local cid = rstr();
  local pkg = rstr();
  local root = rstr();
  local op = raw[at] & 0xff;
  at += 1;
  local aux = rvec();
  local inp = rvec();
  local want1 = rvec();
  local want2 = rvec();

  local u = get_unit(pkg);
  local T = u.T;
  if (op == 1) {
    local rep = {};
    local v = T[root + "Load"](inp, rep);
    if (v == null || is_bad(rep)) die(cid, "Load");
    local out = T[root + "Save"](v);
    if (out == null) die(cid, "Save null");
    check_blob(cid, out, out.len(), want1);
  } else if (op == 2) {
    local store = { capacity = 262144, idCapacity = 8192 };
    local rep = {};
    local v = T[root + "LoadRetain"](inp, store, rep);
    if (v == null || is_bad(rep)) die(cid, "LoadRetain");
    local mlen = T[root + "MeasureRetain"](v, store);
    if (mlen < 0) die(cid, "MeasureRetain");
    local out = blob(mlen);
    local srep = {};
    local sn = T[root + "SaveRetain"](v, store, out, srep);
    if (sn != mlen || is_bad(srep)) die(cid, "SaveRetain");
    check_blob(cid, out, sn, want1);
  } else if (op == 3) {
    local voc = u.own_voc;
    if (aux.len() > 0) {
      voc = {};
      local arep = {};
      if (!T.AnnounceRead(voc, aux, arep)) die(cid, "peer AnnounceRead");
    }
    local batch = [];
    for (local k = 0; k < 16; k++) batch.append(T[root]());
    local lrep = {};
    local cnt = T[root + "LoadMessages"](batch, voc, inp, lrep);
    if (cnt < 0 || is_bad(lrep)) die(cid, "LoadMessages");
    local slice = batch.slice(0, cnt);
    local mrep = {};
    local mlen = T[root + "MeasureMessages"](slice, mrep);
    if (mlen < 0 || is_bad(mrep)) die(cid, "MeasureMessages");
    local out = blob(mlen);
    local srep = {};
    local sn = T[root + "SaveMessages"](slice, out, srep);
    if (sn != mlen || is_bad(srep)) die(cid, "SaveMessages");
    check_blob(cid, out, sn, want1);
  } else if (op == 4) {
    local batch = [T[root]()];
    local cnt = T[root + "FixedLoad"](inp, batch, 1);
    if (cnt != 1) die(cid, "FixedLoad cnt=" + cnt);
    local flen = T[root + "FixedMeasure"](1);
    local out = blob(flen);
    local sn = T[root + "FixedSave"](out, batch);
    if (sn != flen) die(cid, "FixedSave");
    check_blob(cid, out, sn, want1);
  } else if (op == 5) {
    local batch = [T[root]()];
    local cnt = T[root + "FixedLoad"](inp, batch, 1);
    if (cnt >= 0) die(cid, "FixedLoad accepted writer-bound breach");
  } else if (op == 6) {
    local rep = {};
    local v = u.J.FromJson(T, root, inp, rep);
    if (v == null || is_bad(rep)) die(cid, "FromJson");
    local jtxt = T[root + "ToJson"](v);
    check_str_blob(cid, jtxt, want1);
    if (want2.len() > 0) {
      local wout = T[root + "Save"](v);
      check_blob(cid + ":save", wout, wout.len(), want2);
    }
  } else {
    die(cid, "unknown op " + op);
  }
}
print("TABLEGEN_OK squirrel " + total + "\n");
`, filepath.Join(repoRoot, "internal/codegen/squirrel/serialize/serialize.nut"), umap.String(), packFile)

	probe := filepath.Join(workDir, "probe.nut")
	if err := os.WriteFile(probe, []byte(sqSrc), 0o644); err != nil {
		return err
	}
	sqBin := resolveSquirrelTool(repoRoot)
	out, err := runChecked(workDir, nil, sqBin, probe)
	return requireToken(out, err, "squirrel", caseCount)
}

// ============================================================================
// 16. Moo (--!strict checked Moo per-unit execution)
// ============================================================================

func verifyMoo(repoRoot, schemaMooBin, workDir string, units []UnitSpec, cases []CorpusCase) error {
	genRoot := filepath.Join(workDir, "gen")
	if err := generateAllUnits(repoRoot, "", schemaMooBin, "moo", units, genRoot); err != nil {
		return err
	}
	mooBin := resolveMooTool(repoRoot)

	var ran atomic.Int64
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	errs := make(chan error, len(units))

	for _, u := range units {
		var ucases []CorpusCase
		for _, c := range cases {
			if c.Pkg == u.Pkg {
				ucases = append(ucases, c)
			}
		}
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			udir := filepath.Join(genRoot, u.Pkg)
			if err := os.WriteFile(filepath.Join(udir, "cases_fix.moo"), []byte(writeLuaCaseFixtures(ucases, true)), 0o644); err != nil {
				errs <- err
				return
			}
			src := fmt.Sprintf(`--!strict
local P = require("./%s")
local T = require("./%sTable")
local ST = require("./schema_tables")
local L = require("./table_load")
local S = require("./table_save")
local R = require("./table_retain")
local MS = require("./table_message")
local FX = require("./table_fixed")
local J = require("./json")
local _BR = require("./table_blockread")
local _CR = require("./table_cookread")
local F = require("./cases_fix")
local _ = P

local function die(cid: string, why: string)
	error("FAILED: " .. cid .. ": " .. why)
end
local function isBad(rep: any): boolean
	if rep == nil then return false end
	if rep.damaged == true or (type(rep.damaged) == "number" and rep.damaged ~= 0) then return true end
	if rep.refused == true or (type(rep.refused) == "number" and rep.refused ~= 0) then return true end
	return false
end
local function bufEq(a: buffer?, b: buffer): boolean
	if a == nil or buffer.len(a :: buffer) ~= buffer.len(b) then return false end
	local ab = a :: buffer
	for i = 0, buffer.len(ab) - 1 do
		if buffer.readu8(ab, i) ~= buffer.readu8(b, i) then return false end
	end
	return true
end

local ownAnnRaw: any = (T :: any).TableMessageAnnouncement
local ownAnnBuf: buffer = if type(ownAnnRaw) == "string" then buffer.fromstring(ownAnnRaw :: string) else (ownAnnRaw :: buffer)
local ownVocab = MS.NewVocabulary()
if not MS.AnnounceRead(ownVocab, ownAnnBuf, L.newReport()) then
	die("init", "own AnnounceRead")
end

for idx = 1, #F.cases do
	local c = F.cases[idx]
	local root: string = c.root
	local desc: any = (T :: any)[root .. "Descriptor"]
	if c.op == 1 then
		local rep = L.newReport()
		local v = L.load(root, c.inp, rep)
		if v == nil or isBad(rep) then die(c.id, "load") end
		local out, _ = S.save(desc, v)
		if not bufEq(out, c.want1) then die(c.id, "save mismatch") end
	elseif c.op == 2 then
		local store = R.newStore(262144, 8192)
		local rep = L.newReport()
		local v = R.LoadRetain(root, c.inp, rep, store)
		if v == nil or isBad(rep) then die(c.id, "LoadRetain") end
		local srep = L.newReport()
		local out, _ = R.SaveRetain(root, v, store, srep)
		if out == nil or isBad(srep) or not bufEq(out, c.want1) then die(c.id, "SaveRetain") end
	elseif c.op == 3 then
		local voc = ownVocab
		if buffer.len(c.aux) > 0 then
			voc = MS.NewVocabulary()
			local arep = L.newReport()
			if not MS.AnnounceRead(voc, c.aux, arep) then die(c.id, "peer AnnounceRead") end
		end
		local lrep = L.newReport()
		local cnt, vals = MS.LoadMessages(voc, root, c.inp, 16, lrep)
		if cnt < 0 or isBad(lrep) then die(c.id, "LoadMessages") end
		local srep = L.newReport()
		local out, _ = MS.SaveMessages(root, vals, srep)
		if out == nil or isBad(srep) or not bufEq(out, c.want1) then die(c.id, "SaveMessages") end
	elseif c.op == 4 then
		local rep = L.newReport()
		local fxInfo: any = (T :: any)[root .. "Fixed"]
		local vals = FX.load(desc, fxInfo, c.inp, rep, 1)
		if vals == nil or #(vals :: {any}) ~= 1 or isBad(rep) then die(c.id, "FX.load") end
		local out, _ = FX.save(desc, fxInfo, vals :: {any})
		if not bufEq(out, c.want1) then die(c.id, "FX.save") end
	elseif c.op == 5 then
		local rep = L.newReport()
		local fxInfo: any = (T :: any)[root .. "Fixed"]
		local vals = FX.load(desc, fxInfo, c.inp, rep, 1)
		if vals ~= nil and #(vals :: {any}) == 1 and not isBad(rep) then
			die(c.id, "FX.load accepted writer-bound breach")
		end
	elseif c.op == 6 then
		local d: any = (ST :: any).lookup(root)
		local rec = d.new()
		local rep: {[string]: any} = {}
		if not (J :: any).fromJson(d, rec, buffer.tostring(c.inp), rep) or isBad(rep) then
			die(c.id, "fromJson")
		end
		local jtxt: string? = (J :: any).toJson(d, rec)
		if jtxt == nil or not bufEq(buffer.fromstring(jtxt :: string), c.want1) then
			die(c.id, "toJson")
		end
		if buffer.len(c.want2) > 0 then
			local wout, _ = S.save(desc, rec)
			if not bufEq(wout, c.want2) then die(c.id, "save after fromJson") end
		end
	else
		die(c.id, "unknown op")
	end
end
print("UNIT_OK " .. tostring(#F.cases))
`, u.FileBase, u.FileBase)
			if err := os.WriteFile(filepath.Join(udir, "probe.moo"), []byte(src), 0o644); err != nil {
				errs <- err
				return
			}
			out, err := runChecked(udir, nil, mooBin, "probe.moo")
			wantTok := fmt.Sprintf("UNIT_OK %d", len(ucases))
			if err != nil || strings.Contains(out, "FAILED:") || !strings.Contains(out, wantTok) {
				errs <- fmt.Errorf("moo (%s): %w (%s)", u.Pkg, err, out)
				return
			}
			ran.Add(int64(len(ucases)))
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	if int(ran.Load()) != len(cases) {
		return fmt.Errorf("moo ran %d cases, want %d", ran.Load(), len(cases))
	}
	return nil
}

// ============================================================================
// 17. Goose (goose build -O2 native compile + run per unit)
// ============================================================================

func verifyGoose(repoRoot, schemaBin, packFile, workDir string, units []UnitSpec, caseCount int) error {
	genRoot := filepath.Join(workDir, "gen")
	if err := generateAllUnits(repoRoot, schemaBin, "", "goose", units, genRoot); err != nil {
		return err
	}
	gooseBin := resolveGooseTool(repoRoot)

	var ran atomic.Int64
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	errs := make(chan error, len(units))

	for _, u := range units {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			udir := filepath.Join(genRoot, u.Pkg)
			hasLock := fileExists(filepath.Join(u.SchemaDir, "schema.lock"))
			roots := []string{u.FixRoot}
			if u.BlkRoot != "" && u.BlkRoot != u.FixRoot {
				roots = append(roots, u.BlkRoot)
			}

			var fixDisp strings.Builder
			for _, r := range roots {
				loadCall := fmt.Sprintf(`var frep = schema_rt::FixedReport { .. };
            let cnt = %s::%sFixedLoad(fbatch, c_in, frep);
            let ok_fix = (cnt == 1 && !frep.damaged && frep.reason == 0);`, u.Pkg, r)
				if hasLock && r == u.FixRoot {
					loadCall = fmt.Sprintf(`var erep = schema_rt::FixedEvolveReport { .. };
            let cnt = %s::%sFixedEvolveLoad(fbatch, c_in, erep);
            let ok_fix = (cnt == 1 && !erep.damaged && erep.reason == 0);`, u.Pkg, r)
				}
				jsonFixedBlock := ""
				if r == u.BlkRoot {
					jsonFixedBlock = fmt.Sprintf(`        if op == 6 {
            var v = %s::New%s();
            var jrep = schema_rt::json_report();
            if !%s::%sFromJson(v, c_in, jrep) || jrep.damaged { die(c_id, "FromJson fixed"); }
            var jout: u8[>..] = [];
            if %s::%sToJson(v, jout) < 0 { die(c_id, "ToJson fixed"); }
            check_bytes(c_id, jout, c_w1);
            return;
        }
`, u.Pkg, r, u.Pkg, r, u.Pkg, r)
				}
				fmt.Fprintf(&fixDisp, `    if same(c_root, %q) {
        if op == 3 {
            var voc = schema_rt::MsgVocabulary { .. };
            var arep = schema_rt::MsgReport { .. };
            let ann: const u8[:] = if c_aux.len > 0 { c_aux } else { %s::MessageAnnouncement };
            if !schema_rt::announce_read(voc, ann, arep) { die(c_id, "announce_read"); }
            var mbatch: %s::%s[16] = [
                %s::New%s(), %s::New%s(), %s::New%s(), %s::New%s(),
                %s::New%s(), %s::New%s(), %s::New%s(), %s::New%s(),
                %s::New%s(), %s::New%s(), %s::New%s(), %s::New%s(),
                %s::New%s(), %s::New%s(), %s::New%s(), %s::New%s()
            ];
            var mrep = schema_rt::MsgReport { .. };
            let cnt = %s::%sLoadMessages(mbatch, voc, c_in, mrep);
            if cnt < 0 || mrep.damaged || mrep.reason != 0 { die(c_id, "LoadMessages"); }
            var mout: u8[>..] = [];
            var srep = schema_rt::MsgReport { .. };
            let sn = %s::%sSaveMessages(mbatch[0 .. cnt], mout, srep);
            if sn != mout.len || srep.damaged || srep.reason != 0 { die(c_id, "SaveMessages"); }
            check_bytes(c_id, mout, c_w1);
            return;
        }
        if op == 4 || op == 5 {
            var fbatch: %s::%s[1] = [%s::New%s()];
            %s
            if op == 5 {
                if ok_fix { die(c_id, "FixedLoad accepted writer-bound breach"); }
                return;
            }
            if !ok_fix { die(c_id, "FixedLoad"); }
            var fout: u8[>..] = [];
            let sn = %s::%sFixedSave(fbatch[0 .. 1], fout);
            if sn != fout.len { die(c_id, "FixedSave"); }
            check_bytes(c_id, fout, c_w1);
            return;
        }
%s    }
`,
					r,
					u.Pkg,
					u.Pkg, r,
					u.Pkg, r, u.Pkg, r, u.Pkg, r, u.Pkg, r,
					u.Pkg, r, u.Pkg, r, u.Pkg, r, u.Pkg, r,
					u.Pkg, r, u.Pkg, r, u.Pkg, r, u.Pkg, r,
					u.Pkg, r, u.Pkg, r, u.Pkg, r, u.Pkg, r,
					u.Pkg, r,
					u.Pkg, r,
					u.Pkg, r, u.Pkg, r,
					loadCall,
					u.Pkg, r,
					jsonFixedBlock,
				)
			}

			optImports := ""
			if fileExists(filepath.Join(udir, u.FileBase+"Block.goose")) {
				optImports += fmt.Sprintf("import .%sBlock;\n", u.FileBase)
			}
			if fileExists(filepath.Join(udir, u.FileBase+"Cook.goose")) {
				optImports += fmt.Sprintf("import .%sCook;\n", u.FileBase)
			}

			gooseSrc := fmt.Sprintf(`import os;
import .%s;
import .%sTable;
%simport .%s_unit;
import .%s_message;
import .%s_json;
import .schema_rt;
import .wire_load;
import .wire_save;
import .wire_retain;
import .json_value;

fn die(cid: const u8[:], why: const u8[:]) {
    write_stderr(str("FAILED: ", cid, ": ", why, "\n"));
    exit(1);
}
fn same(a: const u8[:], b: const u8[:]) -> bool {
    if a.len != b.len { return false; }
    for i in a.len { if a[i] != b[i] { return false; } }
    return true;
}
fn check_bytes(cid: const u8[:], got: const u8[:], want: const u8[:]) {
    if !same(got, want) { die(cid, "bytes mismatch"); }
}
fn ru32(raw: const u8[:], at: i64&) -> i64 {
    let v: i64 = (raw[at] as i64) | ((raw[at + 1] as i64) << 8) | ((raw[at + 2] as i64) << 16) | ((raw[at + 3] as i64) << 24);
    at += 4;
    return v;
}
fn ru16(raw: const u8[:], at: i64&) -> i64 {
    let v = (raw[at] as i64) | ((raw[at + 1] as i64) << 8);
    at += 2;
    return v;
}

fn run_one(c_id: u8[>..], c_root: u8[>..], op: u8, c_aux: u8[>..], c_in: u8[>..], c_w1: u8[>..], c_w2: u8[>..]) {
%s
    if op == 1 {
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var rep = schema_rt::LoadReport { .. };
        let r = schema_rt::load_table(%s::TableUnit, c_root, c_in, vals, nodes, rep);
        if r < 0 || rep.stopped || rep.body_damaged || rep.refused || rep.non_finite { die(c_id, "load_table"); }
        var out: u8[>..<] = [];
        if schema_rt::save_table(%s::TableUnit, c_root, c_in, vals, nodes, r, out) != 0 { die(c_id, "save_table"); }
        check_bytes(c_id, out, c_w1);
        return;
    }
    if op == 2 {
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var rbytes: u8[>..<] = [];
        var rrecs: schema_rt::RetainRec[>..<] = [];
        var rids: u64[>..<] = [];
        var store = schema_rt::RetainStore { capacity: 262144, id_capacity: 8192, used: 0, bytes: rbytes, recs: rrecs, ids: rids };
        var rep = schema_rt::LoadReport { .. };
        let r = schema_rt::load_table_retain(%s::TableUnit, c_root, c_in, vals, nodes, rep, store);
        if r < 0 || rep.stopped || rep.body_damaged || rep.refused || rep.non_finite { die(c_id, "load_table_retain"); }
        var srep = schema_rt::LoadReport { .. };
        var out: u8[>..<] = [];
        if schema_rt::save_table_retain(%s::TableUnit, c_root, c_in, vals, nodes, r, store, srep, out) != 0 {
            die(c_id, "save_table_retain");
        }
        check_bytes(c_id, out, c_w1);
        return;
    }
    if op == 6 {
        var data: u8[>..] = [];
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var jrep = schema_rt::json_report();
        let r = schema_rt::json_load_table(%s::TableUnit, %s::TextUnit, c_root, c_in, data, vals, nodes, jrep);
        if r < 0 || jrep.damaged { die(c_id, "json_load_table"); }
        var jout: u8[>..] = [];
        if schema_rt::json_save_table(%s::TableUnit, %s::TextUnit, c_root, data, vals, nodes, r, jout) < 0 {
            die(c_id, "json_save_table");
        }
        check_bytes(c_id, jout, c_w1);
        if c_w2.len > 0 {
            var wout: u8[>..<] = [];
            if schema_rt::save_table(%s::TableUnit, c_root, data, vals, nodes, r, wout) != 0 {
                die(c_id, "save_table after json_load_table");
            }
            check_bytes(c_id, wout, c_w2);
        }
        return;
    }
    die(c_id, "unhandled op");
}

fn dup_bytes(s: const u8[:]) -> u8[>..] {
    var b: u8[>..] = [];
    for x in s { b.push(x); }
    return b;
}

fn read_pack(path: const u8[:]) -> u8[>..] {
    var b: u8[>..] = [];
    if !read_file(path, b) { write_stderr("FAILED: read pack\n"); exit(1); }
    return b;
}

fn main() {
    let raw = read_pack(%q);
    var at: i64 = 0;
    let total = ru32(raw, at);
    var ran: i64 = 0;
    for _ in total {
        let id_n = ru16(raw, at); let c_id = raw[at .. at + id_n]; at += id_n;
        let pk_n = ru16(raw, at); let c_pk = raw[at .. at + pk_n]; at += pk_n;
        let rt_n = ru16(raw, at); let c_rt = raw[at .. at + rt_n]; at += rt_n;
        let op = raw[at]; at += 1;
        let ax_n = ru32(raw, at); let c_ax = raw[at .. at + ax_n]; at += ax_n;
        let in_n = ru32(raw, at); let c_in = raw[at .. at + in_n]; at += in_n;
        let w1_n = ru32(raw, at); let c_w1 = raw[at .. at + w1_n]; at += w1_n;
        let w2_n = ru32(raw, at); let c_w2 = raw[at .. at + w2_n]; at += w2_n;
        if same(c_pk, %q) {
            run_one(dup_bytes(c_id), dup_bytes(c_rt), op, dup_bytes(c_ax), dup_bytes(c_in), dup_bytes(c_w1), dup_bytes(c_w2));
            ran += 1;
        }
    }
    print(str("UNIT_OK ", ran, "\n"));
}
`,
				u.FileBase, u.FileBase, optImports, u.Pkg, u.Pkg, u.Pkg,
				fixDisp.String(),
				u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg,
				packFile, u.Pkg,
			)
			srcFile := filepath.Join(udir, "probe.goose")
			exeFile := filepath.Join(udir, "probe_bin")
			if err := os.WriteFile(srcFile, []byte(gooseSrc), 0o644); err != nil {
				errs <- err
				return
			}
			if out, err := runChecked(udir, nil, gooseBin, "build", "-O2", "-o", exeFile, srcFile); err != nil {
				errs <- fmt.Errorf("goose build (%s): %w\n%s", u.Pkg, err, out)
				return
			}
			out, err := runChecked(udir, nil, exeFile)
			if err != nil || strings.Contains(out, "FAILED:") || !strings.Contains(out, "UNIT_OK ") {
				errs <- fmt.Errorf("goose run (%s): %w (%s)", u.Pkg, err, out)
				return
			}
			var n int64
			for line := range strings.SplitSeq(out, "\n") {
				if rest, ok := strings.CutPrefix(line, "UNIT_OK "); ok {
					_, _ = fmt.Sscanf(rest, "%d", &n)
				}
			}
			ran.Add(n)
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	if int(ran.Load()) != caseCount {
		return fmt.Errorf("goose ran %d cases, want %d", ran.Load(), caseCount)
	}
	return nil
}

// ============================================================================
// 18. Lobster (per-unit compile + execute)
// ============================================================================

func verifyLobster(repoRoot, schemaBin, packFile, workDir string, units []UnitSpec, caseCount int) error {
	genRoot := filepath.Join(workDir, "gen")
	if err := generateAllUnits(repoRoot, schemaBin, "", "lobster", units, genRoot); err != nil {
		return err
	}
	lobsterBin := resolveGlobTool(repoRoot, "LOBSTER", "dist/lobster-*/bin/lobster", "lobster")

	var ran atomic.Int64
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	errs := make(chan error, len(units))

	for _, u := range units {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			udir := filepath.Join(genRoot, u.Pkg)
			roots := []string{u.FixRoot}
			if u.BlkRoot != "" && u.BlkRoot != u.FixRoot {
				roots = append(roots, u.BlkRoot)
			}
			var fixDisp strings.Builder
			for _, r := range roots {
				fmt.Fprintf(&fixDisp, `    if c_rt == %q and (op == 4 or op == 5):
        let fbatch = [%s.New%s()]
        let fplan: [schema_rt.TableFixedEntry] = []
        for(256): push(fplan, schema_rt.TableFixedEntry {})
        let frep = schema_rt.TableReport {}
        let cnt = %s.%sFixedLoad(fbatch, c_in, fplan, frep)
        let ok_fix = cnt == 1 and not frep.malformed and frep.verdict == 0 and frep.reason == ""
        if op == 5:
            if ok_fix: die(c_id, "FixedLoad accepted writer-bound breach")
            return
        if not ok_fix: die(c_id, "FixedLoad: " + frep.reason)
        let flen = %s.%sFixedMeasure(1)
        let sn, fout, why = %s.%sFixedSaveChecked(fbatch, schema_rt.zeros(flen))
        if why != "" or sn != flen or fout != c_w1: die(c_id, "FixedSaveChecked: " + why)
        return
`, r, u.Pkg, r, u.Pkg, r, u.Pkg, r, u.Pkg, r)
			}

			lobSrc := fmt.Sprintf(`import .%s
import .%sTable
import .block_form
import .json
import .load
import .message
import .retain_unknown
import .schema_runtime
import .schema_save

def die(cid:string, why:string):
    print("FAILED: " + cid + ": " + why)
    assert false

let raw_opt = read_file(%q)
if not raw_opt: die("init", "read pack")
let raw = if raw_opt: raw_opt else: ""
var at = 0

def ru32() -> int:
    let b0 = raw[at] & 0xFF
    let b1 = raw[at + 1] & 0xFF
    let b2 = raw[at + 2] & 0xFF
    let b3 = raw[at + 3] & 0xFF
    at += 4
    return b0 | (b1 << 8) | (b2 << 16) | (b3 << 24)

def ru16() -> int:
    let b0 = raw[at] & 0xFF
    let b1 = raw[at + 1] & 0xFF
    at += 2
    return b0 | (b1 << 8)

def rstr() -> string:
    let n = ru16()
    let s = substring(raw, at, n)
    at += n
    return s

def rvec() -> string:
    let n = ru32()
    let s = substring(raw, at, n)
    at += n
    return s

let own_vocab = schema_rt.new_vocabulary(4096)
let own_arep = schema_rt.LoadReport {}
if not schema_rt.announce_read(own_vocab, schema_rt.announce(), own_arep):
    die("init", "own announce_read")

def run_one(c_id:string, c_rt:string, op:int, c_ax:string, c_in:string, c_w1:string, c_w2:string):
%s
    if op == 1:
        let rep = schema_rt.LoadReport {}
        let rec = schema_rt.load(c_rt, c_in, rep)
        if rec == nil or rep.damaged or rep.refused: die(c_id, "load")
        if rec:
            let out, why = schema_rt.save_reasoned(rec)
            if why != "" or out != c_w1: die(c_id, "save_reasoned: " + why)
        return
    if op == 2:
        let retain = schema_rt.Retain { capacity: 262144, id_capacity: 8192 }
        let rep = schema_rt.LoadReport {}
        let rec = schema_rt.load_retain(c_rt, c_in, retain, rep)
        if rec == nil or rep.damaged or rep.refused: die(c_id, "load_retain")
        if rec:
            let srep = schema_rt.LoadReport {}
            let out = schema_rt.save_retain(rec, retain, srep)
            if srep.damaged or srep.refused or out != c_w1: die(c_id, "save_retain")
        return
    if op == 3:
        var voc = own_vocab
        if length(c_ax) > 0:
            voc = schema_rt.new_vocabulary(4096)
            let arep = schema_rt.LoadReport {}
            if not schema_rt.announce_read(voc, c_ax, arep): die(c_id, "peer announce_read")
        let mbatch: [schema_rt.Record] = []
        let lrep = schema_rt.LoadReport {}
        let cnt, ok = schema_rt.load_messages_into(c_rt, voc, c_in, lrep, 16, mbatch)
        if not ok or cnt < 0 or lrep.damaged or lrep.refused: die(c_id, "load_messages_into")
        let sl = slice(mbatch, 0, cnt)
        let srep = schema_rt.LoadReport {}
        let out, sn = schema_rt.save_messages(sl, srep)
        if srep.damaged or srep.refused or sn != length(c_w1) or out != c_w1: die(c_id, "save_messages")
        return
    if op == 6:
        let rep = schema_rt.LoadReport {}
        let rec = schema_rt.json_read(c_rt, c_in, rep)
        if rec == nil or rep.damaged or rep.refused: die(c_id, "json_read")
        if rec:
            let jtxt, jwhy = schema_rt.json_write(rec)
            if jwhy != "" or jtxt != c_w1: die(c_id, "json_write: " + jwhy)
            if length(c_w2) > 0:
                let wout, wwhy = schema_rt.save_reasoned(rec)
                if wwhy != "" or wout != c_w2: die(c_id, "save after json_read: " + wwhy)
        return
    if op == 7:
        let rep = schema_rt.LoadReport {}
        let rec = schema_rt.json_read(c_rt, c_in, rep)
        if rec == nil or rep.damaged or rep.refused: die(c_id, "json_read fix")
        if rec:
            let jtxt, jwhy = schema_rt.json_write(rec)
            if jwhy != "" or jtxt != c_w1: die(c_id, "json_write fix: " + jwhy)
            let srep = schema_rt.LoadReport {}
            let out, sn = schema_rt.save_messages([rec], srep)
            if srep.damaged or srep.refused or sn != length(c_w2) or out != c_w2: die(c_id, "save_messages after json_read")
        return
    die(c_id, "unknown op")

let total = ru32()
var ran = 0
for(total):
    let c_id = rstr()
    let c_pk = rstr()
    let c_rt = rstr()
    let op = raw[at] & 0xFF
    at += 1
    let c_ax = rvec()
    let c_in = rvec()
    let c_w1 = rvec()
    let c_w2 = rvec()
    if c_pk == %q:
        run_one(c_id, c_rt, op, c_ax, c_in, c_w1, c_w2)
        ran += 1

print("UNIT_OK " + ran)
`, u.FileBase, u.FileBase, packFile, fixDisp.String(), u.Pkg)
			probe := filepath.Join(udir, "probe.lobster")
			if err := os.WriteFile(probe, []byte(lobSrc), 0o644); err != nil {
				errs <- err
				return
			}
			out, err := runChecked(udir, nil, lobsterBin, probe)
			if err != nil || strings.Contains(out, "FAILED:") || !strings.Contains(out, "UNIT_OK ") {
				errs <- fmt.Errorf("lobster (%s): %w (%s)", u.Pkg, err, out)
				return
			}
			var n int64
			for line := range strings.SplitSeq(out, "\n") {
				if rest, ok := strings.CutPrefix(line, "UNIT_OK "); ok {
					_, _ = fmt.Sscanf(rest, "%d", &n)
				}
			}
			ran.Add(n)
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	if int(ran.Load()) != caseCount {
		return fmt.Errorf("lobster ran %d cases, want %d", ran.Load(), caseCount)
	}
	return nil
}
