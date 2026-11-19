package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		for _, m := range matches {
			if fileExists(m) {
				return m
			}
		}
	}
	return "goose"
}

// ---------------------------------------------------------------------------
// 10. JavaScript (Stream + Flat DataView at byteOffset=7 + Variable/Retain + Message + Fixed + Block + Cook)
// ---------------------------------------------------------------------------

func verifyJS(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateReuse(repoRoot, schemaBin, "", "js", genDir); err != nil {
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
import * as P from "./gen/Reuse.js";
import * as PF from "./gen/ReuseFlat.js";
import * as V from "./gen/ReuseVariable.js";
import * as T from "./gen/ReuseTable.js";
import { BlockDocBlock } from "./gen/ReuseBlock.js";
import { BlockDocCook, BlockDocRow } from "./gen/ReuseCook.js";

const dir = process.argv[2];
const rd = (n) => new Uint8Array(fs.readFileSync(path.join(dir, n)));
const ri = (n) => parseInt(fs.readFileSync(path.join(dir, n), "utf8").trim(), 10);

function check(ok, msg) {
  if (!ok) throw new Error("FAILED: " + msg);
}
function bytesEq(a, b) {
  if (!a || a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}
function makePRead(src, lead = 7, tail = 13, slack = 0) {
  const full = new Uint8Array(lead + src.length + slack + tail);
  full.fill(0xFF);
  full.set(src, lead);
  if (slack > 0) full.fill(0, lead + src.length, lead + src.length + slack);
  return { full, view: full.subarray(lead, lead + src.length), slack };
}
function checkPRead(pr, lead = 7, tail = 13) {
  for (let i = 0; i < lead; i++) check(pr.full[i] === 0xFF, "pread lead canary");
  const end = pr.full.length - tail;
  for (let i = end; i < pr.full.length; i++) check(pr.full[i] === 0xFF, "pread tail canary");
}
function makePWrite(cap, lead = 7, tail = 13) {
  const full = new Uint8Array(lead + cap + tail);
  full.fill(0xA5);
  return { full, view: full.subarray(lead, lead + cap), lead, cap, tail };
}
function checkPWrite(pw) {
  for (let i = 0; i < pw.lead; i++) check(pw.full[i] === 0xA5, "pwrite lead canary");
  for (let i = pw.lead + pw.cap; i < pw.full.length; i++) check(pw.full[i] === 0xA5, "pwrite tail canary");
}

const pktA = rd("pkt_a.bin"), bitsA = ri("pkt_a_bits.txt");
const pktB = rd("pkt_b.bin"), bitsB = ri("pkt_b_bits.txt");
const pktC = rd("pkt_c_bad.bin"), bitsC = ri("pkt_c_bad_bits.txt");
const varA = rd("var_a.bin"), varAR = rd("var_a_retain.bin"), varARRT = rd("var_a_retain_rt.bin");
const varB = rd("var_b.bin"), varC = rd("var_c_bad.bin");
const ann = rd("announce.bin"), msgA = rd("msg_a.bin"), msgB = rd("msg_b.bin"), msgC = rd("msg_c_bad.bin");
const fixA = rd("fixed_a.bin"), fixB = rd("fixed_b.bin"), fixC = rd("fixed_c_bad.bin");
const blkA = rd("block_a.bin"), cookB = rd("cook_block_a.bin");

// 1a. Packet Stream dirty reuse + poison-bracketed subarray at byteOffset=7
const boxS = new P.PacketBox();
for (let s = 0; s < 5; s++) {
  if (s === 2) {
    const pr = makePRead(pktC);
    const rs = new ReadStream(pr.view, bitsC);
    check(!P.ReadPacketBox(rs, boxS), "js stream pktC refuse");
    checkPRead(pr);
    continue;
  }
  const isA = (s === 0 || s === 3);
  const w = isA ? pktA : pktB;
  const bits = isA ? bitsA : bitsB;
  const pr = makePRead(w);
  const rs = new ReadStream(pr.view, bits);
  check(P.ReadPacketBox(rs, boxS), "js stream read " + s);
  checkPRead(pr);
  const pw = makePWrite(P.PacketBoxMaxBytes);
  const ws = new WriteStream(pw.view);
  check(P.WritePacketBox(ws, boxS), "js stream write " + s);
  ws.flush();
  checkPWrite(pw);
  check(bytesEq(ws.data(), w), "js stream bytes " + s);
}

// 1b. Packet Flat DataView dirty reuse + non-zero byteOffset=7
const boxF = new P.PacketBox();
for (let s = 0; s < 5; s++) {
  if (s === 2) {
    const pr = makePRead(pktC, 7, 13, PF.FlatReadSlack);
    const rdv = new DataView(pr.full.buffer, 7, pktC.length + PF.FlatReadSlack);
    check(!PF.ReadPacketBoxFlat(rdv, bitsC, boxF), "js flat pktC refuse");
    checkPRead(pr);
    continue;
  }
  const isA = (s === 0 || s === 3);
  const w = isA ? pktA : pktB;
  const bits = isA ? bitsA : bitsB;
  const pr = makePRead(w, 7, 13, PF.FlatReadSlack);
  const rdv = new DataView(pr.full.buffer, 7, w.length + PF.FlatReadSlack);
  check(PF.ReadPacketBoxFlat(rdv, bits, boxF), "js flat read " + s);
  checkPRead(pr);
  const pw = makePWrite(P.PacketBoxMaxBytes);
  const wdv = new DataView(pw.full.buffer, 7, P.PacketBoxMaxBytes);
  const wn = PF.WritePacketBoxFlat(wdv, boxF);
  checkPWrite(pw);
  check(wn === w.length && bytesEq(pw.view.subarray(0, wn), w), "js flat bytes " + s);
}

// 2. Form 1 Variable + Retain reuse (reusing TableVariableRetain across A -> B -> C -> A -> B)
for (const w of [varA, varB]) {
  const pr = makePRead(w);
  const rep = new V.TableVariableReport();
  const val = V.VarDocLoad(pr.view, rep);
  check(val && !rep.damaged && !rep.refused, "js var load");
  checkPRead(pr);
  check(bytesEq(V.VarDocSave(val), w), "js var save");
}
{
  const pr = makePRead(varC);
  const rep = new V.TableVariableReport();
  const val = V.VarDocLoad(pr.view, rep);
  check(!val || rep.damaged || rep.refused, "js varC refuse");
  checkPRead(pr);
}
const retain = new V.TableVariableRetain(new Uint8Array(65536), new Array(4096));
for (let s = 0; s < 5; s++) {
  if (s === 2) {
    const pr = makePRead(varC);
    const rep = new V.TableVariableReport();
    const val = V.VarDocLoadRetain(pr.view, retain, rep);
    check(!val || rep.damaged || rep.refused, "js retC refuse");
    checkPRead(pr);
    continue;
  }
  const isA = (s === 0 || s === 3);
  const wIn = isA ? varAR : varB;
  const wWant = isA ? varARRT : varB;
  const pr = makePRead(wIn);
  const rep = new V.TableVariableReport();
  const val = V.VarDocLoadRetain(pr.view, retain, rep);
  check(val && !rep.damaged && !rep.refused, "js ret load " + s);
  check(isA ? rep.retained > 0 : rep.retained === 0, "js ret retained count " + s);
  checkPRead(pr);
  const srep = new V.TableVariableReport();
  const out = V.VarDocSaveRetain(val, retain, srep);
  check(bytesEq(out, wWant), "js ret bytes " + s);
}

// 3. Form 2 Message reuse (reusing vocab + mbatch[2] + poison-bracketed SaveMessages)
const vocab = new T.TableVocabulary(); vocab.Init(new Array(4096));
{
  const pr = makePRead(ann);
  const rep = new T.TableFixedReport();
  check(T.AnnounceRead(vocab, pr.view, rep), "js AnnounceRead");
  checkPRead(pr);
}
const mbatch = [new T.FixedDoc(), new T.FixedDoc()];
for (let s = 0; s < 5; s++) {
  if (s === 2) {
    const pr = makePRead(msgC);
    const rep = new T.TableFixedReport();
    check(T.FixedDocLoadMessages(mbatch, vocab, pr.view, rep) <= 0 && (rep.Damaged || rep.Verdict !== 0), "js msgC refuse");
    checkPRead(pr);
    continue;
  }
  const isA = (s === 0 || s === 3);
  const w = isA ? msgA : msgB;
  const wantCnt = isA ? 2 : 1;
  const pr = makePRead(w);
  const rep = new T.TableFixedReport();
  const cnt = T.FixedDocLoadMessages(mbatch, vocab, pr.view, rep);
  check(cnt === wantCnt && !rep.Damaged && (!rep.Verdict || rep.Verdict === 0), "js LoadMessages " + s);
  checkPRead(pr);
  const slice = mbatch.slice(0, cnt);
  const mlen = T.FixedDocMeasureMessages(slice, new T.TableFixedReport());
  check(mlen === w.length, "js MeasureMessages " + s);
  const pw = makePWrite(mlen);
  const wn = T.FixedDocSaveMessages(slice, pw.view, new T.TableFixedReport());
  checkPWrite(pw);
  check(wn === w.length && bytesEq(pw.view, w), "js SaveMessages " + s);
}

// 4. Form 3 Fixed reuse (reusing fbatch[2] + plan + poison-bracketed FixedSave)
const fbatch = [new T.FixedDoc(), new T.FixedDoc()];
const fplan = T.FixedDocFixedNewPlan(64, 64);
for (let s = 0; s < 5; s++) {
  if (s === 2) {
    const pr = makePRead(fixC);
    const rep = new T.TableFixedReport();
    check(T.FixedDocFixedLoad(fbatch, 2, pr.view, pr.view.length, fplan, rep) < 0, "js fixC refuse");
    checkPRead(pr);
    continue;
  }
  const isA = (s === 0 || s === 3);
  const w = isA ? fixA : fixB;
  const wantCnt = isA ? 2 : 1;
  const pr = makePRead(w);
  const rep = new T.TableFixedReport();
  const cnt = T.FixedDocFixedLoad(fbatch, 2, pr.view, pr.view.length, fplan, rep);
  check(cnt === wantCnt && !rep.damaged && !rep.refused, "js FixedLoad " + s);
  checkPRead(pr);
  const flen = T.FixedDocFixedMeasure(cnt);
  check(flen === w.length, "js FixedMeasure " + s);
  const pw = makePWrite(flen);
  const wn = T.FixedDocFixedSave(fbatch, cnt, pw.view);
  checkPWrite(pw);
  check(wn === w.length && bytesEq(pw.view, w), "js FixedSave " + s);
}

// 5. Form 4 Block (+64 vs +7 unaligned) & Cook (+8 vs +7 unaligned)
{
  const prBad = makePRead(blkA, 7, 13);
  check(BlockDocBlock.OpenReason(prBad.view) === "unaligned_base", "js Block +7 refuse");
  const pr64 = makePRead(blkA, 64, 13);
  const blk = BlockDocBlock.Open(pr64.view);
  check(BlockDocBlock.A(blk) === -42 && BlockDocBlock.B(blk) === 0x123456789ABCDEF0n, "js Block +64");
  checkPRead(pr64, 64, 13);

  const crBad = makePRead(cookB, 7, 13);
  check(BlockDocCook.OpenReason(crBad.view) === "unaligned_base", "js Cook +7 refuse");
  const cr8 = makePRead(cookB, 8, 13);
  const cook = BlockDocCook.Open(cr8.view);
  check(BlockDocRow.A(cook.View, cook.Region) === -42 && BlockDocRow.B(cook.View, cook.Region) === 0x123456789ABCDEF0n && BlockDocRow.NumsCount(cook.View, cook.Region) === 3, "js Cook +8");
  checkPRead(cr8, 8, 13);
}
console.log("REUSE_OK js 10");
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

// ---------------------------------------------------------------------------
// 11. Python (memoryview[7:7+n] + PacketBox._s_read reuse + TableRetain reuse + Message + Fixed + Block + Cook)
// ---------------------------------------------------------------------------

func verifyPython(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateReuse(repoRoot, schemaBin, "", "python", genDir); err != nil {
		return err
	}
	pySrc := `import pathlib, sys
sys.path.insert(0, "./gen")
import reuse as r
from reuse._schema.serialize import ReadStream, WriteStream
from reuse._schema import block as rblock, cook as rcook

d = pathlib.Path(sys.argv[1])
def check(ok: bool, msg: str) -> None:
    if not ok:
        raise AssertionError(f"FAILED: {msg}")

def make_pread(raw: bytes, lead: int = 7, tail: int = 13) -> tuple[bytearray, memoryview]:
    full = bytearray(b"\xff" * lead + raw + b"\xff" * tail)
    return full, memoryview(full)[lead : lead + len(raw)]

def check_pread(full: bytearray, lead: int = 7, tail: int = 13) -> None:
    check(all(b == 0xFF for b in full[:lead]), "py pread lead canary")
    check(all(b == 0xFF for b in full[len(full) - tail:]), "py pread tail canary")

pkt_a = (d / "pkt_a.bin").read_bytes()
pkt_b = (d / "pkt_b.bin").read_bytes()
pkt_c = (d / "pkt_c_bad.bin").read_bytes()
var_a = (d / "var_a.bin").read_bytes()
var_ar = (d / "var_a_retain.bin").read_bytes()
var_arrt = (d / "var_a_retain_rt.bin").read_bytes()
var_b = (d / "var_b.bin").read_bytes()
var_c = (d / "var_c_bad.bin").read_bytes()
ann = (d / "announce.bin").read_bytes()
msg_a = (d / "msg_a.bin").read_bytes()
msg_b = (d / "msg_b.bin").read_bytes()
msg_c = (d / "msg_c_bad.bin").read_bytes()
fix_a = (d / "fixed_a.bin").read_bytes()
fix_b = (d / "fixed_b.bin").read_bytes()
fix_c = (d / "fixed_c_bad.bin").read_bytes()
blk_a = (d / "block_a.bin").read_bytes()
cook_b = (d / "cook_block_a.bin").read_bytes()

# 1. Packet dirty-instance reuse (_s_read into same PacketBox) + memoryview[7:7+n]
box = r.PacketBox()
for s in range(5):
    if s == 2:
        full, mv = make_pread(pkt_c)
        rs = ReadStream(mv)
        ok = box._s_read(rs) and rs.serialize_end()
        check(not ok, "py pktC refuse")
        check_pread(full)
        continue
    w = pkt_a if s in (0, 3) else pkt_b
    full, mv = make_pread(w)
    rs = ReadStream(mv)
    check(box._s_read(rs) and rs.serialize_end(), f"py pkt read {s}")
    check_pread(full)
    check(box.write() == w, f"py pkt write {s}")

# 2. Form 1 Variable + Retain reuse (reusing TableRetain across A -> B -> C -> A -> B)
for w in (var_a, var_b):
    full, mv = make_pread(w)
    val = r.VarDoc.load(mv)
    check_pread(full)
    check(val.save() == w, "py var roundtrip")

full_vc, mv_vc = make_pread(var_c)
refused = False
try:
    r.VarDoc.load(mv_vc)
except r.Refused:
    refused = True
check(refused, "py varC refuse")
check_pread(full_vc)

retain = r.TableRetain(bytearray(65536), bytearray(24 * 4096))
for s in range(5):
    if s == 2:
        full, mv = make_pread(var_c)
        ok_ref = False
        try:
            r.VarDoc.load_retain(mv, retain)
        except r.Refused:
            ok_ref = True
        check(ok_ref, "py retC refuse")
        check_pread(full)
        continue
    is_a = s in (0, 3)
    w_in = var_ar if is_a else var_b
    w_want = var_arrt if is_a else var_b
    full, mv = make_pread(w_in)
    rep = r.LoadReport()
    val = r.VarDoc.load_retain(mv, retain, rep)
    check(rep.retained > 0 if is_a else rep.retained == 0, f"py ret count {s}")
    check_pread(full)
    srep = r.LoadReport()
    out = val.save_retain(retain, srep)
    check(out == w_want, f"py ret bytes {s}")

# 3. Form 2 Message reuse (reusing MessageVocabulary across A -> B -> C -> A -> B)
vocab = r.MessageVocabulary()
full_ann, mv_ann = make_pread(ann)
r.announce_read(vocab, mv_ann)
check_pread(full_ann)
for s in range(5):
    if s == 2:
        full, mv = make_pread(msg_c)
        ok_ref = False
        try:
            r.load_messages(r.FixedDoc, vocab, mv, 2)
        except r.Refused:
            ok_ref = True
        check(ok_ref, "py msgC refuse")
        check_pread(full)
        continue
    is_a = s in (0, 3)
    w = msg_a if is_a else msg_b
    want_cnt = 2 if is_a else 1
    full, mv = make_pread(w)
    vals = r.load_messages(r.FixedDoc, vocab, mv, 2)
    check(len(vals) == want_cnt, f"py msg count {s}")
    check_pread(full)
    check(r.save_messages(r.FixedDoc, vals) == w, f"py msg save {s}")

# 4. Form 3 Fixed memoryview[7:7+n]
for s in range(5):
    if s == 2:
        full, mv = make_pread(fix_c)
        ok_ref = False
        try:
            r.FixedDoc.fixed_load(mv, 2)
        except r.Refused:
            ok_ref = True
        check(ok_ref, "py fixC refuse")
        check_pread(full)
        continue
    is_a = s in (0, 3)
    w = fix_a if is_a else fix_b
    want_cnt = 2 if is_a else 1
    full, mv = make_pread(w)
    vals = r.FixedDoc.fixed_load(mv, 2)
    check(len(vals) == want_cnt, f"py fix count {s}")
    check_pread(full)
    check(r.FixedDoc.fixed_save(vals) == w, f"py fix save {s}")

# 5. Form 4 Block (+64 vs +7 unaligned) & Cook (+8 vs +7 unaligned)
full_blk, mv_blk = make_pread(blk_a, 64, 13)
blk_ref = False
try:
    rblock.BlockOpen(r.Reuse, "BlockDoc", mv_blk, build_version=r.BUILD_VERSION, base_offset=7)
except r.Refused:
    blk_ref = True
check(blk_ref, "py Block +7 refuse")
blk = rblock.BlockOpen(r.Reuse, "BlockDoc", mv_blk, build_version=r.BUILD_VERSION, base_offset=64)
check("a = -42" in rblock.Dump(blk), "py Block +64")
check_pread(full_blk, 64, 13)

full_ck, mv_ck = make_pread(cook_b, 8, 13)
ck_ref = False
try:
    rcook.open_cook(r.BlockDoc, mv_ck, base_offset=7)
except r.Refused:
    ck_ref = True
check(ck_ref, "py Cook +7 refuse")
ck = rcook.open_cook(r.BlockDoc, mv_ck, base_offset=8)
check("a = -42" in rcook.dump_cook(ck), "py Cook +8")
check_pread(full_ck, 8, 13)

print("REUSE_OK python 10")
`
	probe := filepath.Join(workDir, "main.py")
	if err := os.WriteFile(probe, []byte(pySrc), 0o644); err != nil {
		return err
	}
	py := resolvePythonTool(repoRoot)
	out, err := runChecked(workDir, nil, py, probe, corpusDir)
	return requireToken(out, err, "python")
}

// ---------------------------------------------------------------------------
// 12. Ruby (PacketBox._s_read reuse + read_bits bit_offset: 56 + TableRetain reuse + Message + Fixed + Block + Cook)
// ---------------------------------------------------------------------------

func verifyRuby(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateReuse(repoRoot, schemaBin, "", "ruby", genDir); err != nil {
		return err
	}
	rbSrc := `$LOAD_PATH.unshift(File.expand_path("./gen", __dir__))
require "reuse"

dir = ARGV[0]
def check(ok, msg)
  raise "FAILED: #{msg}" unless ok
end

def sub7(raw, lead = 7, tail = 13)
  full = ("\xFF".b * lead) + raw + ("\xFF".b * tail)
  full.byteslice(lead, raw.bytesize)
end

pkt_a = File.binread(File.join(dir, "pkt_a.bin"))
bits_a = File.read(File.join(dir, "pkt_a_bits.txt")).strip.to_i
pkt_b = File.binread(File.join(dir, "pkt_b.bin"))
bits_b = File.read(File.join(dir, "pkt_b_bits.txt")).strip.to_i
pkt_c = File.binread(File.join(dir, "pkt_c_bad.bin"))
var_a = File.binread(File.join(dir, "var_a.bin"))
var_ar = File.binread(File.join(dir, "var_a_retain.bin"))
var_arrt = File.binread(File.join(dir, "var_a_retain_rt.bin"))
var_b = File.binread(File.join(dir, "var_b.bin"))
var_c = File.binread(File.join(dir, "var_c_bad.bin"))
ann = File.binread(File.join(dir, "announce.bin"))
msg_a = File.binread(File.join(dir, "msg_a.bin"))
msg_b = File.binread(File.join(dir, "msg_b.bin"))
msg_c = File.binread(File.join(dir, "msg_c_bad.bin"))
fix_a = File.binread(File.join(dir, "fixed_a.bin"))
fix_b = File.binread(File.join(dir, "fixed_b.bin"))
fix_c = File.binread(File.join(dir, "fixed_c_bad.bin"))
blk_a = File.binread(File.join(dir, "block_a.bin"))
cook_b = File.binread(File.join(dir, "cook_block_a.bin"))

# 1. Packet dirty-instance reuse (_s_read into same PacketBox) + non-zero bit_offset: 56
box = Reuse::PacketBox.new
5.times do |s|
  if s == 2
    stream, = Reuse._s_open_read_stream(sub7(pkt_c), 0)
    ok = box._s_read(stream) && stream.serialize_end
    check(!ok, "rb pktC refuse")
    next
  end
  is_a = (s == 0 || s == 3)
  w = is_a ? pkt_a : pkt_b
  stream, = Reuse._s_open_read_stream(sub7(w), 0)
  check(box._s_read(stream) && stream.serialize_end, "rb pkt read #{s}")
  check(box.write == w, "rb pkt write #{s}")
  # Also read at non-zero bit_offset: 56 inside poison-bracketed buffer
  padded = ("\xFF".b * 7) + w + ("\xFF".b * 13)
  val_off, used_bits = Reuse::PacketBox.read_bits(padded, bit_offset: 56)
  check(val_off.write == w && used_bits > 0, "rb pkt read_bits +56 #{s}")
end

# 2. Form 1 Variable + Retain reuse
[var_a, var_b].each do |w|
  v = Reuse::VarDoc.load(sub7(w))
  check(v.save == w, "rb var roundtrip")
end
refused = false
begin
  Reuse::VarDoc.load(sub7(var_c))
rescue Reuse::Refused
  refused = true
end
check(refused, "rb varC refuse")

retain = Reuse::TableRetain.new
5.times do |s|
  if s == 2
    ok_ref = false
    begin
      Reuse::VarDoc.load_retain(sub7(var_c), retain)
    rescue Reuse::Refused
      ok_ref = true
    end
    check(ok_ref, "rb retC refuse")
    next
  end
  is_a = (s == 0 || s == 3)
  w_in = is_a ? var_ar : var_b
  w_want = is_a ? var_arrt : var_b
  rep = Reuse::LoadReport.new
  v = Reuse::VarDoc.load_retain(sub7(w_in), retain, rep)
  check(is_a ? rep.retained > 0 : rep.retained == 0, "rb ret count #{s}")
  srep = Reuse::LoadReport.new
  out = v.save_retain(retain, srep)
  check(out == w_want, "rb ret bytes #{s}")
end

# 3. Form 2 Message reuse (reusing MessageVocabulary)
vocab = Reuse::MessageVocabulary.new
Reuse.announce_read(vocab, sub7(ann))
5.times do |s|
  if s == 2
    ok_ref = false
    begin
      Reuse.load_messages(Reuse::FixedDoc, vocab, sub7(msg_c), 2)
    rescue Reuse::Refused
      ok_ref = true
    end
    check(ok_ref, "rb msgC refuse")
    next
  end
  is_a = (s == 0 || s == 3)
  w = is_a ? msg_a : msg_b
  want_cnt = is_a ? 2 : 1
  vals = Reuse.load_messages(Reuse::FixedDoc, vocab, sub7(w), 2)
  check(vals.length == want_cnt, "rb msg count #{s}")
  check(Reuse.save_messages(Reuse::FixedDoc, vals) == w, "rb msg save #{s}")
end

# 4. Form 3 Fixed reuse
5.times do |s|
  if s == 2
    ok_ref = false
    begin
      Reuse::FixedDoc.fixed_load(sub7(fix_c), 2)
    rescue Reuse::Refused
      ok_ref = true
    end
    check(ok_ref, "rb fixC refuse")
    next
  end
  is_a = (s == 0 || s == 3)
  w = is_a ? fix_a : fix_b
  want_cnt = is_a ? 2 : 1
  vals = Reuse::FixedDoc.fixed_load(sub7(w), 2)
  check(vals.length == want_cnt, "rb fix count #{s}")
  check(Reuse::FixedDoc.fixed_save(vals) == w, "rb fix save #{s}")
end

# 5. Form 4 Block (+64 vs +7 unaligned) & Cook (+8 vs +7 unaligned)
blk_ref = false
begin
  Reuse::BlockDoc.block_open(sub7(blk_a, 64, 13), base_offset: 7)
rescue Reuse::Refused
  blk_ref = true
end
check(blk_ref, "rb Block +7 refuse")
blk = Reuse::BlockDoc.block_open(sub7(blk_a, 64, 13), base_offset: 64)
check(!blk.nil?, "rb Block +64")

ck_ref = false
begin
  Reuse::BlockDoc.cook_open(sub7(cook_b, 8, 13), base_offset: 7)
rescue Reuse::Refused
  ck_ref = true
end
check(ck_ref, "rb Cook +7 refuse")
ck = Reuse::BlockDoc.cook_open(sub7(cook_b, 8, 13), base_offset: 8)
check(!ck.nil?, "rb Cook +8")

puts "REUSE_OK ruby 10"
`
	probe := filepath.Join(workDir, "main.rb")
	if err := os.WriteFile(probe, []byte(rbSrc), 0o644); err != nil {
		return err
	}
	rb := resolveRubyTool(repoRoot)
	out, err := runChecked(workDir, nil, rb, probe, corpusDir)
	return requireToken(out, err, "ruby")
}

// ---------------------------------------------------------------------------
// 13. Lua (PacketBox reuse + L.Load(..., into) & R.LoadRetain(..., store, rep, into) reuse + Message + Fixed + Block + Cook)
// ---------------------------------------------------------------------------

func verifyLua(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateReuse(repoRoot, schemaBin, "", "lua", genDir); err != nil {
		return err
	}
	luaSrc := `package.path = "./gen/?.lua;" .. package.path
local serialize = require("serialize")
local P = require("Reuse")
local T = require("ReuseTables")
local L = require("load")
local S = require("save")
local R = require("retain")
local B = require("block")
local C = require("cook")

local dir = arg[1]
local function check(ok, msg)
  if not ok then error("FAILED: " .. msg) end
end
local function read_all(n)
  local f = assert(io.open(dir .. "/" .. n, "rb"))
  local d = f:read("*a")
  f:close()
  return d
end
local function sub7(raw, lead, tail)
  lead = lead or 7
  tail = tail or 13
  local full = string.rep("\255", lead) .. raw .. string.rep("\255", tail)
  return string.sub(full, lead + 1, lead + #raw)
end

local pktA = read_all("pkt_a.bin")
local pktB = read_all("pkt_b.bin")
local pktC = read_all("pkt_c_bad.bin")
local varA = read_all("var_a.bin")
local varAR = read_all("var_a_retain.bin")
local varARRT = read_all("var_a_retain_rt.bin")
local varB = read_all("var_b.bin")
local varC = read_all("var_c_bad.bin")
local ann = read_all("announce.bin")
local msgA = read_all("msg_a.bin")
local msgB = read_all("msg_b.bin")
local msgC = read_all("msg_c_bad.bin")
local fixA = read_all("fixed_a.bin")
local fixB = read_all("fixed_b.bin")
local fixC = read_all("fixed_c_bad.bin")
local blkA = read_all("block_a.bin")
local cookB = read_all("cook_block_a.bin")

-- 1. Packet dirty-instance reuse into same PacketBox
local box = P.NewPacketBox()
for s = 0, 4 do
  if s == 2 then
    local rs = serialize.ReadStream.new(sub7(pktC))
    check(not P.ReadPacketBox(rs, box), "lua pktC refuse")
  else
    local w = (s == 0 or s == 3) and pktA or pktB
    local rs = serialize.ReadStream.new(sub7(w))
    check(P.ReadPacketBox(rs, box), "lua pkt read " .. s)
    local ws = serialize.WriteStream.new(512)
    check(P.WritePacketBox(ws, box), "lua pkt write " .. s)
    ws:flush()
    check(ws:data() == w, "lua pkt bytes " .. s)
  end
end

-- 2a. Form 1 Variable dirty-instance reuse (into = same VarDoc table + reused state)
local state = L.NewState()
local intoVar = T.NewVarDoc()
for s = 0, 4 do
  if s == 2 then
    local rep = {}
    local v = L.Load(T, "VarDoc", sub7(varC), rep, state, intoVar)
    check(v == nil or rep.damaged or (rep.refused and rep.refused ~= 0), "lua varC refuse")
  else
    local w = (s == 0 or s == 3) and varA or varB
    local rep = {}
    local v = L.Load(T, "VarDoc", sub7(w), rep, state, intoVar)
    check(v ~= nil and not rep.damaged and (not rep.refused or rep.refused == 0), "lua var load " .. s)
    check(S.Save(T, "VarDoc", v) == w, "lua var save " .. s)
  end
end

-- 2b. Form 1 Retain reuse (same store + same intoRet across A -> B -> C -> A -> B)
local store = { cap = 65536, id_cap = 4096 }
local intoRet = T.NewVarDoc()
for s = 0, 4 do
  if s == 2 then
    local rep = {}
    local v = R.LoadRetain(T, "VarDoc", sub7(varC), store, rep, intoRet)
    check(v == nil or rep.damaged or (rep.refused and rep.refused ~= 0), "lua retC refuse")
  else
    local isA = (s == 0 or s == 3)
    local wIn = isA and varAR or varB
    local wWant = isA and varARRT or varB
    local rep = {}
    local v = R.LoadRetain(T, "VarDoc", sub7(wIn), store, rep, intoRet)
    check(v ~= nil and not rep.damaged and (not rep.refused or rep.refused == 0), "lua ret load " .. s)
    check(isA and (rep.retained and rep.retained > 0) or ((rep.retained or 0) == 0), "lua ret count " .. s)
    local srep = {}
    local out = R.SaveRetain(T, "VarDoc", v, store, srep)
    check(out == wWant, "lua ret bytes " .. s)
  end
end

-- 3. Form 2 Message reuse (reusing vocab table)
local vocab = {}
local arep = {}
check(T.AnnounceRead(vocab, sub7(ann), arep), "lua AnnounceRead")
for s = 0, 4 do
  if s == 2 then
    local rep = {}
    local vals = T.LoadMessages("FixedDoc", vocab, sub7(msgC), rep)
    check(vals == nil or #vals == 0 or rep.damaged or ((rep.refused or 0) ~= 0), "lua msgC refuse")
  else
    local isA = (s == 0 or s == 3)
    local w = isA and msgA or msgB
    local wantCnt = isA and 2 or 1
    local rep = {}
    local vals = T.LoadMessages("FixedDoc", vocab, sub7(w), rep)
    check(vals ~= nil and #vals == wantCnt and not rep.damaged and ((rep.refused or 0) == 0), "lua msg load " .. s)
    local out = T.SaveMessages("FixedDoc", vocab, vals)
    check(out == w, "lua msg save " .. s)
  end
end

-- 4. Form 3 Fixed reuse
for s = 0, 4 do
  if s == 2 then
    local ok_fc, vals = pcall(T.FixedLoad, "FixedDoc", sub7(fixC), 2)
    check(not ok_fc or vals == nil, "lua fixC refuse")
  else
    local isA = (s == 0 or s == 3)
    local w = isA and fixA or fixB
    local wantCnt = isA and 2 or 1
    local vals = T.FixedLoad("FixedDoc", sub7(w), 2)
    check(vals ~= nil and #vals == wantCnt, "lua fix load " .. s)
    check(T.FixedSave("FixedDoc", vals) == w, "lua fix save " .. s)
  end
end

-- 5. Form 4 Block (+64 vs +7 unaligned) & Cook (+8 vs +7 unaligned)
local b7, berr7 = B.BlockOpen(T, "BlockDoc", sub7(blkA, 64, 13), nil, 7)
check(b7 == nil and berr7 ~= nil, "lua Block +7 refuse")
local b64, berr64 = B.BlockOpen(T, "BlockDoc", sub7(blkA, 64, 13), nil, 64)
check(b64 ~= nil and berr64 == nil and string.find(B.Dump(b64), "a = -42", 1, true) ~= nil, "lua Block +64")

local c7, cerr7 = C.Open(T, "BlockDoc", sub7(cookB, 8, 13), 7)
check(c7 == nil and cerr7 ~= nil, "lua Cook +7 refuse")
local c8, cerr8 = C.Open(T, "BlockDoc", sub7(cookB, 8, 13), 8)
check(c8 ~= nil and cerr8 == nil, "lua Cook +8")

print("REUSE_OK lua 10")
`
	probe := filepath.Join(workDir, "main.lua")
	if err := os.WriteFile(probe, []byte(luaSrc), 0o644); err != nil {
		return err
	}
	luaBin := resolveGlobTool(repoRoot, "LUA", "dist/lua-*/bin/lua", "lua")
	out, err := runChecked(workDir, nil, luaBin, probe, corpusDir)
	return requireToken(out, err, "lua")
}

// ---------------------------------------------------------------------------
// Shared embedded fixture generator for Luau & Moo (zero host file-I/O needed)
// ---------------------------------------------------------------------------

func renderLuaReuseFixtures(corpusDir string, isMoo bool) (string, error) {
	files := []struct{ key, name string }{
		{"pkt_a", "pkt_a.bin"},
		{"pkt_b", "pkt_b.bin"},
		{"pkt_c", "pkt_c_bad.bin"},
		{"var_a", "var_a.bin"},
		{"var_ar", "var_a_retain.bin"},
		{"var_arrt", "var_a_retain_rt.bin"},
		{"var_b", "var_b.bin"},
		{"var_c", "var_c_bad.bin"},
		{"ann", "announce.bin"},
		{"msg_a", "msg_a.bin"},
		{"msg_b", "msg_b.bin"},
		{"msg_c", "msg_c_bad.bin"},
		{"fix_a", "fixed_a.bin"},
		{"fix_b", "fixed_b.bin"},
		{"fix_c", "fixed_c_bad.bin"},
		{"blk_a", "block_a.bin"},
		{"cook_b", "cook_block_a.bin"},
	}
	var b strings.Builder
	b.WriteString("local M = {}\n")
	if isMoo {
		b.WriteString("local function bytes(t: {number}): buffer\n\tlocal buf = buffer.create(#t)\n\tfor i = 1, #t do buffer.writeu8(buf, i - 1, t[i]) end\n\treturn buf\nend\n")
	} else {
		b.WriteString("local function bytes(t: {number}): buffer\n\tlocal buf = buffer.create(#t)\n\tfor i, v in ipairs(t) do buffer.writeu8(buf, i - 1, v) end\n\treturn buf\nend\n")
	}
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(corpusDir, f.name))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "M.%s = bytes({", f.key)
		for i, by := range raw {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "%d", by)
		}
		b.WriteString("})\n")
	}
	for _, bf := range []struct{ key, name string }{
		{"bits_a", "pkt_a_bits.txt"},
		{"bits_b", "pkt_b_bits.txt"},
		{"bits_c", "pkt_c_bad_bits.txt"},
	} {
		raw, err := os.ReadFile(filepath.Join(corpusDir, bf.name))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "M.%s = %s\n", bf.key, strings.TrimSpace(string(raw)))
	}
	b.WriteString("return M\n")
	return b.String(), nil
}

// ---------------------------------------------------------------------------
// 14. Luau (PacketBox reuse + Variable/Retain + Message + Fixed + Block +64 + Cook +8)
// ---------------------------------------------------------------------------

func verifyLuau(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateReuse(repoRoot, schemaBin, "", "luau", genDir); err != nil {
		return err
	}
	fixSrc, err := renderLuaReuseFixtures(corpusDir, false)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(genDir, "fixtures.luau"), []byte(fixSrc), 0o644); err != nil {
		return err
	}
	src := `local serialize = require("./serialize")
local P = require("./Reuse")
local T = require("./ReuseTable")
local RB = require("./ReuseBlock")
local L = require("./schema_load")
local S = require("./schema_save")
local R = require("./schema_retain")
local M = require("./schema_message")
local FX = require("./schema_fixed")
local BR = require("./schema_blockread")
local CR = require("./schema_cookread")
local F = require("./fixtures")

local function isBad(rep: any): boolean
	if rep == nil then return false end
	if rep.damaged == true or (type(rep.damaged) == "number" and rep.damaged ~= 0) then return true end
	if rep.refused == true or (type(rep.refused) == "number" and rep.refused ~= 0) then return true end
	return false
end
local function check(ok: boolean, msg: string)
	if not ok then error("FAILED: " .. msg) end
end
local function bufEq(a: buffer?, b: buffer): boolean
	if a == nil or buffer.len(a :: buffer) ~= buffer.len(b) then return false end
	local ab = a :: buffer
	for i = 0, buffer.len(ab) - 1 do
		if buffer.readu8(ab, i) ~= buffer.readu8(b, i) then return false end
	end
	return true
end
local function makePRead(src: buffer, lead: number, tail: number): (buffer, buffer)
	local n = buffer.len(src)
	local full = buffer.create(lead + n + tail)
	buffer.fill(full, 0, 0xFF, lead + n + tail)
	buffer.copy(full, lead, src, 0, n)
	local sub = buffer.create(n)
	buffer.copy(sub, 0, full, lead, n)
	return full, sub
end
local function checkCanaries(full: buffer, lead: number, tail: number, val: number)
	local tot = buffer.len(full)
	for i = 0, lead - 1 do check(buffer.readu8(full, i) == val, "luau lead canary") end
	for i = tot - tail, tot - 1 do check(buffer.readu8(full, i) == val, "luau tail canary") end
end

-- 1. Packet dirty-instance reuse into same PacketBox
local box = P.NewPacketBox()
for s = 0, 4 do
	if s == 2 then
		local full, sub = makePRead(F.pkt_c, 7, 13)
		local rs = serialize.ReadStream.new(sub, F.bits_c)
		check(not P.ReadPacketBox(rs, box), "luau pktC refuse")
		checkCanaries(full, 7, 13, 0xFF)
	else
		local isA = (s == 0 or s == 3)
		local w = if isA then F.pkt_a else F.pkt_b
		local bits = if isA then F.bits_a else F.bits_b
		local full, sub = makePRead(w, 7, 13)
		local rs = serialize.ReadStream.new(sub, bits)
		check(P.ReadPacketBox(rs, box), "luau pkt read " .. tostring(s))
		checkCanaries(full, 7, 13, 0xFF)
		local ws = serialize.WriteStream.new(buffer.create(512))
		check(P.WritePacketBox(ws, box), "luau pkt write " .. tostring(s))
		ws:flush()
		check(bufEq(ws:data(), w), "luau pkt bytes " .. tostring(s))
	end
end

-- 2. Form 1 Variable + Retain
for _, w in ipairs({F.var_a, F.var_b}) do
	local rep: {[string]: any} = {}
	local v = L.load("VarDoc", w, rep)
	check(v ~= nil and not isBad(rep), "luau var load")
	check(bufEq(S.save(T.VarDocDescriptor, v), w), "luau var save")
end
do
	local rep: {[string]: any} = {}
	local v = L.load("VarDoc", F.var_c, rep)
	check(v == nil or isBad(rep), "luau varC refuse")
end
for s = 0, 4 do
	if s == 2 then
		local rep: {[string]: any} = {}
		local v, _ = R.LoadRetain("VarDoc", F.var_c, rep)
		check(v == nil or isBad(rep), "luau retC refuse")
	else
		local isA = (s == 0 or s == 3)
		local wIn = if isA then F.var_ar else F.var_b
		local wWant = if isA then F.var_arrt else F.var_b
		local rep: {[string]: any} = {}
		local v, store = R.LoadRetain("VarDoc", wIn, rep)
		check(v ~= nil and not isBad(rep), "luau ret load " .. tostring(s))
		check(if isA then (rep.retained or 0) > 0 else (rep.retained or 0) == 0, "luau ret count " .. tostring(s))
		local srep: {[string]: any} = {}
		local out, _ = R.SaveRetain("VarDoc", v, store, srep)
		check(bufEq(out, wWant), "luau ret bytes " .. tostring(s))
	end
end

-- 3. Form 2 Message reuse (reusing vocab across A -> B -> C -> A -> B)
local vocab = M.NewVocabulary()
local arep: {[string]: any} = {}
check(M.AnnounceRead(vocab, F.ann, arep), "luau AnnounceRead")
for s = 0, 4 do
	if s == 2 then
		local rep: {[string]: any} = {}
		local cnt, _ = M.LoadMessages(vocab, "FixedDoc", F.msg_c, 2, rep)
		check(cnt <= 0 or isBad(rep), "luau msgC refuse")
	else
		local isA = (s == 0 or s == 3)
		local w = if isA then F.msg_a else F.msg_b
		local wantCnt = if isA then 2 else 1
		local rep: {[string]: any} = {}
		local cnt, vals = M.LoadMessages(vocab, "FixedDoc", w, 2, rep)
		check(cnt == wantCnt and not isBad(rep), "luau msg load " .. tostring(s))
		local out, _ = M.SaveMessages(vocab, "FixedDoc", vals, {})
		check(bufEq(out, w), "luau msg save " .. tostring(s))
	end
end

-- 4. Form 3 Fixed reuse
local fxSpec = {
	hash = T.FixedDocFixedHash,
	recordBytes = T.FixedDocFixedRecordBytes,
	layout = T.FixedDocFixedLayout,
	lineage = T.FixedDocFixedLineage,
	floor = T.FixedDocFixedFloor,
}
for s = 0, 4 do
	if s == 2 then
		local rep: {[string]: any} = {}
		local vals = FX.read(T.FixedDocDescriptor, fxSpec, F.fix_c, rep, 2)
		check(vals == nil or isBad(rep), "luau fixC refuse")
	else
		local isA = (s == 0 or s == 3)
		local w = if isA then F.fix_a else F.fix_b
		local wantCnt = if isA then 2 else 1
		local rep: {[string]: any} = {}
		local vals = FX.read(T.FixedDocDescriptor, fxSpec, w, rep, 2)
		check(vals ~= nil and #(vals :: {any}) == wantCnt and not isBad(rep), "luau fix load " .. tostring(s))
		local out = FX.save(T.FixedDocDescriptor, fxSpec, vals :: {any})
		check(bufEq(out, w), "luau fix save " .. tostring(s))
	end
end

-- 5. Form 4 Block (base=64 inside poison-bracketed buffer vs base=7 unaligned) & Cook (base=8 vs base=7)
do
	local fullBlk7, _ = makePRead(F.blk_a, 7, 13)
	local b7, bwhy7 = BR.BlockOpen(RB.BlockDocBlock, fullBlk7, 7, buffer.len(F.blk_a))
	check(b7 == nil and bwhy7 == "unaligned_base", "luau Block +7 refuse")
	checkCanaries(fullBlk7, 7, 13, 0xFF)

	local fullBlk64, _ = makePRead(F.blk_a, 64, 13)
	local b64, bwhy64 = BR.BlockOpen(RB.BlockDocBlock, fullBlk64, 64, buffer.len(F.blk_a))
	check(b64 ~= nil and bwhy64 == nil, "luau Block +64 open")
	local bval, rwhy = BR.Read(RB.BlockDocBlock, b64 :: any)
	check(bval ~= nil and rwhy == nil and (bval :: any).a == -42, "luau Block +64 read")
	checkCanaries(fullBlk64, 64, 13, 0xFF)

	local fullCk7, _ = makePRead(F.cook_b, 7, 13)
	check(CR.Check("BlockDoc", fullCk7, T.TableMessageBuildVersion, 7, buffer.len(F.cook_b)) == "unaligned_base", "luau Cook +7 refuse")
	local fullCk8, _ = makePRead(F.cook_b, 8, 13)
	check(CR.Check("BlockDoc", fullCk8, T.TableMessageBuildVersion, 8, buffer.len(F.cook_b)) == nil, "luau Cook +8 open")
	checkCanaries(fullCk8, 8, 13, 0xFF)
end

print("REUSE_OK luau 10")
`
	if err := os.WriteFile(filepath.Join(genDir, "probe.luau"), []byte(src), 0o644); err != nil {
		return err
	}
	luauBin := resolveGlobTool(repoRoot, "LUAU", "dist/luau-*/luau", "luau")
	out, err := runChecked(genDir, nil, luauBin, "probe.luau")
	return requireToken(out, err, "luau")
}

// ---------------------------------------------------------------------------
// 15. Squirrel (PacketBox reuse + Retain store reuse + Message mbatch/vocab reuse + Fixed fbatch reuse + Block +64 + Cook +8)
// ---------------------------------------------------------------------------

func verifySquirrel(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateReuse(repoRoot, schemaBin, "", "squirrel", genDir); err != nil {
		return err
	}
	sqSrc := fmt.Sprintf(`local serialize = dofile(%q, true);
local loader = dofile("./gen/schema_module.nut", true);
local P = loader.require("Reuse");
local T = loader.require("ReuseTables");
local B = loader.require("schema_table_block");
local C = loader.require("schema_table_cook");
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
local function load_int(fname) {
  local b = load_blob(fname);
  local s = "";
  for (local i = 0; i < b.len(); i++) {
    local ch = b[i] & 0xff;
    if (ch >= '0' && ch <= '9') s += ch.tochar();
  }
  return s.tointeger();
}
local function sub7(src) {
  local full = blob(7 + src.len() + 13);
  for (local i = 0; i < 7; i++) full.writen(0xff, 'b');
  src.seek(0);
  if (src.len() > 0) full.writeblob(src.readblob(src.len()));
  for (local i = 0; i < 13; i++) full.writen(0xff, 'b');
  full.seek(7);
  local sub = blob(src.len());
  if (src.len() > 0) sub.writeblob(full.readblob(src.len()));
  sub.seek(0);
  src.seek(0);
  return sub;
}
local function blob_eq_prefix(a, b, len) {
  if (b.len() != len) return false;
  for (local i = 0; i < len; i++) if ((a[i] & 0xff) != (b[i] & 0xff)) return false;
  return true;
}

local pktA = load_blob("pkt_a.bin"), bitsA = load_int("pkt_a_bits.txt");
local pktB = load_blob("pkt_b.bin"), bitsB = load_int("pkt_b_bits.txt");
local pktC = load_blob("pkt_c_bad.bin"), bitsC = load_int("pkt_c_bad_bits.txt");
local varA = load_blob("var_a.bin"), varAR = load_blob("var_a_retain.bin"), varARRT = load_blob("var_a_retain_rt.bin");
local varB = load_blob("var_b.bin"), varC = load_blob("var_c_bad.bin");
local ann = load_blob("announce.bin"), msgA = load_blob("msg_a.bin"), msgB = load_blob("msg_b.bin"), msgC = load_blob("msg_c_bad.bin");
local fixA = load_blob("fixed_a.bin"), fixB = load_blob("fixed_b.bin"), fixC = load_blob("fixed_c_bad.bin");
local blkA = load_blob("block_a.bin"), cookB = load_blob("cook_block_a.bin");

// 1. Packet dirty-instance reuse into same PacketBox
local box = P.PacketBox();
for (local s = 0; s < 5; s++) {
  if (s == 2) {
    local rs = serialize.ReadStream(sub7(pktC));
    check(!P.ReadPacketBox(rs, box), "sq pktC refuse");
    continue;
  }
  local isA = (s == 0 || s == 3);
  local w = isA ? pktA : pktB;
  local bits = isA ? bitsA : bitsB;
  local rs = serialize.ReadStream(sub7(w));
  check(P.ReadPacketBox(rs, box), "sq pkt read " + s);
  local wbuf = blob(512);
  local ws = serialize.WriteStream(wbuf);
  check(P.WritePacketBox(ws, box), "sq pkt write " + s);
  ws.flush();
  check(blob_eq_prefix(wbuf, w, ws.bytesProcessed()), "sq pkt bytes " + s);
}

// 2. Form 1 Variable + Retain reuse (reusing same store table across A -> B -> C -> A -> B)
foreach (w in [varA, varB]) {
  local rep = {};
  local v = T.VarDocLoad(sub7(w), rep);
  check(v != null && !("damaged" in rep && rep.damaged) && !("refused" in rep && rep.refused), "sq var load");
  local out = T.VarDocSave(v);
  check(out != null && blob_eq_prefix(out, w, out.len()), "sq var save");
}
{
  local rep = {};
  local v = T.VarDocLoad(sub7(varC), rep);
  check(v == null || ("damaged" in rep && rep.damaged) || ("refused" in rep && rep.refused), "sq varC refuse");
}
local store = { capacity = 65536, idCapacity = 4096 };
for (local s = 0; s < 5; s++) {
  if (s == 2) {
    local rep = {};
    local v = T.VarDocLoadRetain(sub7(varC), store, rep);
    check(v == null || ("damaged" in rep && rep.damaged) || ("refused" in rep && rep.refused), "sq retC refuse");
    continue;
  }
  local isA = (s == 0 || s == 3);
  local wIn = isA ? varAR : varB;
  local wWant = isA ? varARRT : varB;
  local rep = {};
  local v = T.VarDocLoadRetain(sub7(wIn), store, rep);
  check(v != null && !("damaged" in rep && rep.damaged) && !("refused" in rep && rep.refused), "sq ret load " + s);
  local retCnt = ("retained" in rep) ? rep.retained : 0;
  check(isA ? retCnt > 0 : retCnt == 0, "sq ret count " + s);
  local mlen = T.VarDocMeasureRetain(v, store);
  check(mlen == wWant.len(), "sq ret measure " + s);
  local out = blob(mlen);
  local srep = {};
  local sn = T.VarDocSaveRetain(v, store, out, srep);
  check(sn == wWant.len() && blob_eq_prefix(out, wWant, sn), "sq ret save " + s);
}

// 3. Form 2 Message reuse (reusing vocab + mbatch[2])
local vocab = {};
local arep = {};
check(T.AnnounceRead(vocab, sub7(ann), arep), "sq AnnounceRead");
local mbatch = [T.FixedDoc(), T.FixedDoc()];
for (local s = 0; s < 5; s++) {
  if (s == 2) {
    local rep = {};
    local cnt = T.FixedDocLoadMessages(mbatch, vocab, sub7(msgC), rep);
    check(cnt <= 0 && (("damaged" in rep && rep.damaged) || ("refused" in rep && rep.refused)), "sq msgC refuse");
    continue;
  }
  local isA = (s == 0 || s == 3);
  local w = isA ? msgA : msgB;
  local wantCnt = isA ? 2 : 1;
  local rep = {};
  local cnt = T.FixedDocLoadMessages(mbatch, vocab, sub7(w), rep);
  check(cnt == wantCnt && !("damaged" in rep && rep.damaged) && !("refused" in rep && rep.refused), "sq msg load " + s);
  local slice = mbatch.slice(0, cnt);
  local mlen = T.FixedDocMeasureMessages(slice, {});
  check(mlen == w.len(), "sq msg measure " + s);
  local out = blob(mlen);
  local sn = T.FixedDocSaveMessages(slice, out, {});
  check(sn == w.len() && blob_eq_prefix(out, w, sn), "sq msg save " + s);
}

// 4. Form 3 Fixed reuse (reusing fbatch[2])
local fbatch = [T.FixedDoc(), T.FixedDoc()];
for (local s = 0; s < 5; s++) {
  if (s == 2) {
    check(T.FixedDocFixedLoad(sub7(fixC), fbatch, 2) < 0, "sq fixC refuse");
    continue;
  }
  local isA = (s == 0 || s == 3);
  local w = isA ? fixA : fixB;
  local wantCnt = isA ? 2 : 1;
  local cnt = T.FixedDocFixedLoad(sub7(fixB.len() == w.len() ? w : w), fbatch, 2);
  check(cnt == wantCnt, "sq fix load " + s);
  local slice = fbatch.slice(0, cnt);
  local flen = T.FixedDocFixedMeasure(cnt);
  local out = blob(flen);
  local sn = T.FixedDocFixedSave(out, slice);
  check(sn == w.len() && blob_eq_prefix(out, w, sn), "sq fix save " + s);
}

// 5. Form 4 Block (lead=64 vs lead=7) & Cook (lead=8 vs lead=7)
{
  local brep7 = {};
  check(B.Block(T, "BlockDoc", sub7(blkA), brep7, 7) == null, "sq Block +7 refuse");
  local brep64 = {};
  local blk = B.Block(T, "BlockDoc", sub7(blkA), brep64, 64);
  check(blk != null, "sq Block +64");

  local crep7 = {};
  check(C.Cook(T, "BlockDoc", sub7(cookB), crep7, 7) == null, "sq Cook +7 refuse");
  local crep8 = {};
  local ck = C.Cook(T, "BlockDoc", sub7(cookB), crep8, 8);
  check(ck != null, "sq Cook +8");
}

print("REUSE_OK squirrel 10\n");
`, filepath.Join(repoRoot, "internal/codegen/squirrel/serialize/serialize.nut"), corpusDir)
	probe := filepath.Join(workDir, "probe.nut")
	if err := os.WriteFile(probe, []byte(sqSrc), 0o644); err != nil {
		return err
	}
	sqBin := resolveSquirrelTool(repoRoot)
	out, err := runChecked(workDir, nil, sqBin, probe)
	return requireToken(out, err, "squirrel")
}

// ---------------------------------------------------------------------------
// 16. Moo (PacketBox reuse + R.newStore reuse + Message vocab reuse + Fixed + Block +64 + Cook +8)
// ---------------------------------------------------------------------------

func verifyMoo(repoRoot, schemaMooBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateReuse(repoRoot, "", schemaMooBin, "moo", genDir); err != nil {
		return err
	}
	fixSrc, err := renderLuaReuseFixtures(corpusDir, true)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(genDir, "fixtures.moo"), []byte(fixSrc), 0o644); err != nil {
		return err
	}
	src := `local serialize = require("./serialize")
local P = require("./Reuse")
local T = require("./ReuseTable")
local L = require("./table_load")
local S = require("./table_save")
local R = require("./table_retain")
local MS = require("./table_message")
local FX = require("./table_fixed")
local BR = require("./table_blockread")
local CR = require("./table_cookread")
local F = require("./fixtures")

local function isBad(rep: any): boolean
	if rep == nil then return false end
	if rep.damaged == true or (type(rep.damaged) == "number" and rep.damaged ~= 0) then return true end
	if rep.refused == true or (type(rep.refused) == "number" and rep.refused ~= 0) then return true end
	return false
end
local function check(ok: boolean, msg: string)
	if not ok then error("FAILED: " .. msg) end
end
local function bufEq(a: buffer?, b: buffer): boolean
	if a == nil or buffer.len(a :: buffer) ~= buffer.len(b) then return false end
	local ab = a :: buffer
	for i = 0, buffer.len(ab) - 1 do
		if buffer.readu8(ab, i) ~= buffer.readu8(b, i) then return false end
	end
	return true
end
local function makePRead(src: buffer, lead: number, tail: number): (buffer, buffer)
	local n = buffer.len(src)
	local full = buffer.create(lead + n + tail)
	buffer.fill(full, 0, 0xFF, lead + n + tail)
	buffer.copy(full, lead, src, 0, n)
	local sub = buffer.create(n)
	buffer.copy(sub, 0, full, lead, n)
	return full, sub
end
local function checkCanaries(full: buffer, lead: number, tail: number, val: number)
	local tot = buffer.len(full)
	for i = 0, lead - 1 do check(buffer.readu8(full, i) == val, "moo lead canary") end
	for i = tot - tail, tot - 1 do check(buffer.readu8(full, i) == val, "moo tail canary") end
end

-- 1. Packet dirty-instance reuse into same PacketBox
local box = P.NewPacketBox()
for s = 0, 4 do
	if s == 2 then
		local full, sub = makePRead(F.pkt_c, 7, 13)
		local rs = serialize.ReadStream.new(sub, F.bits_c)
		check(not P.ReadPacketBox(rs, box), "moo pktC refuse")
		checkCanaries(full, 7, 13, 0xFF)
	else
		local isA = (s == 0 or s == 3)
		local w = if isA then F.pkt_a else F.pkt_b
		local bits = if isA then F.bits_a else F.bits_b
		local full, sub = makePRead(w, 7, 13)
		local rs = serialize.ReadStream.new(sub, bits)
		check(P.ReadPacketBox(rs, box), "moo pkt read " .. tostring(s))
		checkCanaries(full, 7, 13, 0xFF)
		local ws = serialize.WriteStream.new(buffer.create(512))
		check(P.WritePacketBox(ws, box), "moo pkt write " .. tostring(s))
		ws:flush()
		check(bufEq(ws:data(), w), "moo pkt bytes " .. tostring(s))
	end
end

-- 2. Form 1 Variable + Retain reuse (reusing same R.newStore across A -> B -> C -> A -> B)
for idx = 1, 2 do
	local w = if idx == 1 then F.var_a else F.var_b
	local rep = L.newReport()
	local v = L.load("VarDoc", w, rep)
	check(v ~= nil and not isBad(rep), "moo var load")
	local out, _ = S.save(T.VarDocDescriptor, v)
	check(bufEq(out, w), "moo var save")
end
do
	local rep = L.newReport()
	local v = L.load("VarDoc", F.var_c, rep)
	check(v == nil or isBad(rep), "moo varC refuse")
end

local store = R.newStore(65536, 4096)
for s = 0, 4 do
	if s == 2 then
		local rep = L.newReport()
		local v = R.LoadRetain("VarDoc", F.var_c, rep, store)
		check(v == nil or isBad(rep), "moo retC refuse")
	else
		local isA = (s == 0 or s == 3)
		local wIn = if isA then F.var_ar else F.var_b
		local wWant = if isA then F.var_arrt else F.var_b
		local rep = L.newReport()
		local v = R.LoadRetain("VarDoc", wIn, rep, store)
		check(v ~= nil and not isBad(rep), "moo ret load " .. tostring(s))
		check(if isA then rep.retained > 0 else rep.retained == 0, "moo ret count " .. tostring(s))
		local srep = L.newReport()
		local out, _ = R.SaveRetain("VarDoc", v, store, srep)
		check(bufEq(out, wWant), "moo ret bytes " .. tostring(s))
	end
end

-- 3. Form 2 Message reuse (reusing vocab across A -> B -> C -> A -> B)
local vocab = MS.NewVocabulary()
local arep = L.newReport()
check(MS.AnnounceRead(vocab, F.ann, arep), "moo AnnounceRead")
for s = 0, 4 do
	if s == 2 then
		local rep = L.newReport()
		local cnt, _ = MS.LoadMessages(vocab, "FixedDoc", F.msg_c, 2, rep)
		check(cnt <= 0 or isBad(rep), "moo msgC refuse")
	else
		local isA = (s == 0 or s == 3)
		local w = if isA then F.msg_a else F.msg_b
		local wantCnt = if isA then 2 else 1
		local rep = L.newReport()
		local cnt, vals = MS.LoadMessages(vocab, "FixedDoc", w, 2, rep)
		check(cnt == wantCnt and not isBad(rep), "moo msg load " .. tostring(s))
		local out, _ = MS.SaveMessages("FixedDoc", vals, L.newReport())
		check(bufEq(out, w), "moo msg save " .. tostring(s))
	end
end

-- 4. Form 3 Fixed reuse
for s = 0, 4 do
	if s == 2 then
		local rep = L.newReport()
		local vals = FX.load(T.FixedDocDescriptor, T.FixedDocFixed, F.fix_c, rep, 2)
		check(vals == nil or isBad(rep), "moo fixC refuse")
	else
		local isA = (s == 0 or s == 3)
		local w = if isA then F.fix_a else F.fix_b
		local wantCnt = if isA then 2 else 1
		local rep = L.newReport()
		local vals = FX.load(T.FixedDocDescriptor, T.FixedDocFixed, w, rep, 2)
		check(vals ~= nil and #(vals :: {any}) == wantCnt and not isBad(rep), "moo fix load " .. tostring(s))
		local out, _ = FX.save(T.FixedDocDescriptor, T.FixedDocFixed, vals :: {any})
		check(bufEq(out, w), "moo fix save " .. tostring(s))
	end
end

-- 5. Form 4 Block (base=64 vs base=7) & Cook (base=8 vs base=7)
do
	local fullBlk7, _ = makePRead(F.blk_a, 7, 13)
	local b7, bwhy7 = BR.BlockOpen(T.BlockDocBlockInfo, fullBlk7, 7, buffer.len(F.blk_a))
	check(b7 == nil and bwhy7 == "unaligned_base", "moo Block +7 refuse")

	local fullBlk64, _ = makePRead(F.blk_a, 64, 13)
	local b64, bwhy64 = BR.BlockOpen(T.BlockDocBlockInfo, fullBlk64, 64, buffer.len(F.blk_a))
	check(b64 ~= nil and bwhy64 == nil, "moo Block +64 open")
	local bval, rwhy = BR.Read(b64)
	check(bval ~= nil and rwhy == nil and (bval.a == -42 or bval.A == -42), "moo Block +64 read")
	checkCanaries(fullBlk64, 64, 13, 0xFF)

	local fullCk7, _ = makePRead(F.cook_b, 7, 13)
	local ck7, cwhy7 = CR.open("BlockDoc", fullCk7, 7, buffer.len(F.cook_b))
	check(ck7 == nil and cwhy7 == "unaligned_base", "moo Cook +7 refuse")

	local fullCk8, _ = makePRead(F.cook_b, 8, 13)
	local ck8, cwhy8 = CR.open("BlockDoc", fullCk8, 8, buffer.len(F.cook_b))
	check(ck8 ~= nil and cwhy8 == nil, "moo Cook +8 open")
	local cval, crwhy = CR.read(ck8)
	check(cval ~= nil and crwhy == nil and (cval.a == -42 or cval.A == -42), "moo Cook +8 read")
	checkCanaries(fullCk8, 8, 13, 0xFF)
end

print("REUSE_OK moo 10")
`
	if err := os.WriteFile(filepath.Join(genDir, "probe.moo"), []byte(src), 0o644); err != nil {
		return err
	}
	mooBin := resolveGlobTool(repoRoot, "MOO", "dist/moo-*/moo", "moo")
	out, err := runChecked(genDir, nil, mooBin, "probe.moo")
	return requireToken(out, err, "moo")
}

// ---------------------------------------------------------------------------
// 17. Goose (Sub-sliced u8[7..7+n] + 0xA5 poison-bracketed writes + PacketBox/RetainStore/mbatch/fbatch reuse + Block + Cook)
// ---------------------------------------------------------------------------

func verifyGoose(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateReuse(repoRoot, schemaBin, "", "goose", genDir); err != nil {
		return err
	}
	bitsABytes, err := os.ReadFile(filepath.Join(corpusDir, "pkt_a_bits.txt"))
	if err != nil {
		return err
	}
	bitsBBytes, err := os.ReadFile(filepath.Join(corpusDir, "pkt_b_bits.txt"))
	if err != nil {
		return err
	}
	bitsCBytes, err := os.ReadFile(filepath.Join(corpusDir, "pkt_c_bad_bits.txt"))
	if err != nil {
		return err
	}
	gooseSrc := fmt.Sprintf(`import os;
import .Reuse;
import .ReuseTable;
import .ReuseBlock;
import .ReuseCook;
import .reuse_unit;
import .reuse_message;
import .schema_rt;
import .serialize;
import .wire_load;
import .wire_save;
import .wire_retain;

fn check(ok: bool, msg: const u8[:]) {
    if !ok { write_stderr(str("FAILED: ", msg, "\n")); exit(1); }
}
fn same(a: const u8[:], b: const u8[:]) -> bool {
    if a.len != b.len { return false; }
    for i in a.len { if a[i] != b[i] { return false; } }
    return true;
}
fn load_bin(dir: const u8[:], name: const u8[:], out: u8[>..]&) {
    out.clear();
    check(read_file(str(dir, "/", name), out), name);
}
fn make_pread(src: const u8[:], lead: i64, tail: i64, out: u8[>..]&) {
    out.clear();
    for _ in lead { out.push(0xff); }
    out.append(src);
    for _ in tail { out.push(0xff); }
}
fn check_pread(full: const u8[:], lead: i64, tail: i64) {
    for i in lead { check(full[i] == 0xff, "goose pread lead canary"); }
    for i in tail { check(full[full.len - tail + i] == 0xff, "goose pread tail canary"); }
}
fn make_pwrite(cap: i64, lead: i64, tail: i64, out: u8[>..]&) {
    out.clear();
    for _ in lead + cap + tail { out.push(0xa5); }
}
fn check_pwrite(full: const u8[:], lead: i64, cap: i64, tail: i64) {
    for i in lead { check(full[i] == 0xa5, "goose pwrite lead canary"); }
    for i in tail { check(full[lead + cap + i] == 0xa5, "goose pwrite tail canary"); }
}

fn main() {
    let dir = %q;
    let bits_a: i64 = %s;
    let bits_b: i64 = %s;
    let bits_c: i64 = %s;

    var pkt_a: u8[>..] = []; load_bin(dir, "pkt_a.bin", pkt_a);
    var pkt_b: u8[>..] = []; load_bin(dir, "pkt_b.bin", pkt_b);
    var pkt_c: u8[>..] = []; load_bin(dir, "pkt_c_bad.bin", pkt_c);
    var var_a: u8[>..] = []; load_bin(dir, "var_a.bin", var_a);
    var var_ar: u8[>..] = []; load_bin(dir, "var_a_retain.bin", var_ar);
    var var_arrt: u8[>..] = []; load_bin(dir, "var_a_retain_rt.bin", var_arrt);
    var var_b: u8[>..] = []; load_bin(dir, "var_b.bin", var_b);
    var var_c: u8[>..] = []; load_bin(dir, "var_c_bad.bin", var_c);
    var ann: u8[>..] = []; load_bin(dir, "announce.bin", ann);
    var msg_a: u8[>..] = []; load_bin(dir, "msg_a.bin", msg_a);
    var msg_b: u8[>..] = []; load_bin(dir, "msg_b.bin", msg_b);
    var msg_c: u8[>..] = []; load_bin(dir, "msg_c_bad.bin", msg_c);
    var fix_a: u8[>..] = []; load_bin(dir, "fixed_a.bin", fix_a);
    var fix_b: u8[>..] = []; load_bin(dir, "fixed_b.bin", fix_b);
    var fix_c: u8[>..] = []; load_bin(dir, "fixed_c_bad.bin", fix_c);
    var blk_a: u8[>..] = []; load_bin(dir, "block_a.bin", blk_a);
    var cook_b: u8[>..] = []; load_bin(dir, "cook_block_a.bin", cook_b);

    var pr: u8[>..] = [];
    var pw: u8[>..] = [];

    // 1. Packet dirty-instance reuse into same PacketBox + poison-bracketed read/write slices
    var box = reuse::NewPacketBox();
    for s in 5 {
        if s == 2 {
            make_pread(pkt_c, 7, 13, pr);
            var pad_c: u8[512] = [0; 512];
            let sub_c = pr[7 .. 7 + pkt_c.len];
            var rs = serialize_read_stream_init_padded(pad_c, sub_c);
            check(!reuse::ReadPacketBox(rs, box), "goose pktC refuse");
            check_pread(pr, 7, 13);
            let _ = bits_c;
        } else {
            let is_a = (s == 0 || s == 3);
            let w: const u8[:] = if is_a { pkt_a } else { pkt_b };
            let _ = if is_a { bits_a } else { bits_b };
            make_pread(w, 7, 13, pr);
            var pad_r: u8[512] = [0; 512];
            let sub_r = pr[7 .. 7 + w.len];
            var rs = serialize_read_stream_init_padded(pad_r, sub_r);
            check(reuse::ReadPacketBox(rs, box), "goose pkt read");
            check_pread(pr, 7, 13);

            make_pwrite(72, 7, 13, pw);
            for k in 72 { pw[7 + k] = 0; }
            var ws = serialize_write_stream_init(pw[7 .. 7 + 72]);
            check(reuse::WritePacketBox(ws, box) == SERIALIZE_ERROR_NONE, "goose pkt write");
            serialize_flush(ws);
            check_pwrite(pw, 7, 72, 13);
            let wn = serialize_bytes_processed(ws) as! i64;
            check(same(pw[7 .. 7 + wn], w), "goose pkt bytes");
        }
    }

    // 2. Form 1 Variable + Retain reuse (reusing vals, nodes, and RetainStore across A -> B -> C -> A -> B)
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var rbytes: u8[>..<] = [];
    var rrecs: schema_rt::RetainRec[>..<] = [];
    var rids: u64[>..<] = [];
    var store = schema_rt::RetainStore { capacity: 65536, id_capacity: 4096, used: 0, bytes: rbytes, recs: rrecs, ids: rids };
    for s in 5 {
        if s == 2 {
            make_pread(var_c, 7, 13, pr);
            var rep = schema_rt::LoadReport { .. };
            let root = schema_rt::load_table_retain(reuse::TableUnit, "VarDoc", pr[7 .. 7 + var_c.len], vals, nodes, rep, store);
            check(root < 0 || rep.damaged || rep.refused, "goose retC refuse");
            check_pread(pr, 7, 13);
        } else {
            let is_a = (s == 0 || s == 3);
            let w_in: const u8[:] = if is_a { var_ar } else { var_b };
            let w_want: const u8[:] = if is_a { var_arrt } else { var_b };
            make_pread(w_in, 7, 13, pr);
            let sub = pr[7 .. 7 + w_in.len];
            var rep = schema_rt::LoadReport { .. };
            let root = schema_rt::load_table_retain(reuse::TableUnit, "VarDoc", sub, vals, nodes, rep, store);
            check(root >= 0 && !rep.damaged && !rep.refused, "goose ret load");
            check(if is_a { rep.retained > 0 } else { rep.retained == 0 }, "goose ret count");
            check_pread(pr, 7, 13);
            var srep = schema_rt::LoadReport { .. };
            var wout: u8[>..<] = [];
            check(schema_rt::save_table_retain(reuse::TableUnit, "VarDoc", sub, vals, nodes, root, store, srep, wout) == 0, "goose ret save");
            check(same(wout, w_want), "goose ret bytes");
        }
    }

    // 3. Form 2 Message reuse (reusing voc + mbatch[2] across A -> B -> C -> A -> B)
    var voc = schema_rt::MsgVocabulary { .. };
    var arep = schema_rt::MsgReport { .. };
    make_pread(ann, 7, 13, pr);
    check(schema_rt::announce_read(voc, pr[7 .. 7 + ann.len], arep), "goose announce_read");
    check_pread(pr, 7, 13);
    var mbatch: reuse::FixedDoc[2] = [reuse::NewFixedDoc(), reuse::NewFixedDoc()];
    for s in 5 {
        if s == 2 {
            make_pread(msg_c, 7, 13, pr);
            var mrep = schema_rt::MsgReport { .. };
            let cnt = reuse::FixedDocLoadMessages(mbatch, voc, pr[7 .. 7 + msg_c.len], mrep);
            check(cnt <= 0 && (mrep.damaged || mrep.reason != 0), "goose msgC refuse");
            check_pread(pr, 7, 13);
        } else {
            let is_a = (s == 0 || s == 3);
            let w: const u8[:] = if is_a { msg_a } else { msg_b };
            let want_cnt: i64 = if is_a { 2 } else { 1 };
            make_pread(w, 7, 13, pr);
            var mrep = schema_rt::MsgReport { .. };
            let cnt = reuse::FixedDocLoadMessages(mbatch, voc, pr[7 .. 7 + w.len], mrep);
            check(cnt == want_cnt && !mrep.damaged && mrep.reason == 0, "goose msg load");
            check_pread(pr, 7, 13);
            var mout: u8[>..] = [];
            var srep = schema_rt::MsgReport { .. };
            let sn = reuse::FixedDocSaveMessages(mbatch[0 .. cnt], mout, srep);
            check(sn == w.len && same(mout, w), "goose msg save");
        }
    }

    // 4. Form 3 Fixed reuse (reusing fbatch[2] across A -> B -> C -> A -> B)
    var fbatch: reuse::FixedDoc[2] = [reuse::NewFixedDoc(), reuse::NewFixedDoc()];
    for s in 5 {
        if s == 2 {
            make_pread(fix_c, 7, 13, pr);
            var frep = schema_rt::FixedReport { .. };
            check(reuse::FixedDocFixedLoad(fbatch, pr[7 .. 7 + fix_c.len], frep) < 0, "goose fixC refuse");
            check_pread(pr, 7, 13);
        } else {
            let is_a = (s == 0 || s == 3);
            let w: const u8[:] = if is_a { fix_a } else { fix_b };
            let want_cnt: i64 = if is_a { 2 } else { 1 };
            make_pread(w, 7, 13, pr);
            var frep = schema_rt::FixedReport { .. };
            let cnt = reuse::FixedDocFixedLoad(fbatch, pr[7 .. 7 + w.len], frep);
            check(cnt == want_cnt && !frep.damaged && frep.reason == 0, "goose fix load");
            check_pread(pr, 7, 13);
            var fout: u8[>..] = [];
            let sn = reuse::FixedDocFixedSave(fbatch[0 .. cnt], fout);
            check(sn == w.len && same(fout, w), "goose fix save");
        }
    }

    // 5. Form 4 Block (lead=64 vs lead=7) & Cook (lead=8 vs lead=7)
    make_pread(blk_a, 64, 13, pr);
    let blk_sub = pr[64 .. 64 + blk_a.len];
    check(!reuse::BlockDocBlockOpen(blk_sub, 7), "goose Block +7 refuse");
    check(reuse::BlockDocBlockOpen(blk_sub, 64), "goose Block +64 open");
    check(reuse::BlockDocBlockA(blk_sub) == -42 && reuse::BlockDocBlockB(blk_sub) == 0x123456789abcdef0 && reuse::BlockDocBlockNumsCount(blk_sub) == 3, "goose Block +64 fields");
    check_pread(pr, 64, 13);

    make_pread(cook_b, 8, 13, pr);
    let ck_sub = pr[8 .. 8 + cook_b.len];
    check(!reuse::BlockDocCookOpen(ck_sub, 7), "goose Cook +7 refuse");
    check(reuse::BlockDocCookOpen(ck_sub, 8), "goose Cook +8 open");
    check_pread(pr, 8, 13);

    print("REUSE_OK goose 10\n");
}
`, corpusDir, strings.TrimSpace(string(bitsABytes)), strings.TrimSpace(string(bitsBBytes)), strings.TrimSpace(string(bitsCBytes)))
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

// ---------------------------------------------------------------------------
// 18. Lobster (PacketBox reuse + Retain reuse + load_messages_into reuse + FixedDocFixedLoad reuse + Block +64 + Cook +8)
// ---------------------------------------------------------------------------

func verifyLobster(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateReuse(repoRoot, schemaBin, "", "lobster", genDir); err != nil {
		return err
	}
	bitsABytes, err := os.ReadFile(filepath.Join(corpusDir, "pkt_a_bits.txt"))
	if err != nil {
		return err
	}
	bitsBBytes, err := os.ReadFile(filepath.Join(corpusDir, "pkt_b_bits.txt"))
	if err != nil {
		return err
	}
	bitsCBytes, err := os.ReadFile(filepath.Join(corpusDir, "pkt_c_bad_bits.txt"))
	if err != nil {
		return err
	}
	lobSrc := fmt.Sprintf(`import .Reuse
import .ReuseTable
import .block_form
import .load
import .message
import .retain_unknown
import .schema_runtime
import .schema_save
import .serialize

def check(ok:bool, msg:string):
    if not ok:
        print("FAILED: " + msg)
        assert false

def must_read(dir:string, n:string) -> string:
    let d = read_file(dir + "/" + n)
    check(d != nil, "read " + n)
    return if d: d else: ""

def sub7(raw:string, lead:int, tail:int) -> string:
    var p = ""
    for(lead): p += "\xFF"
    p += raw
    for(tail): p += "\xFF"
    return substring(p, lead, length(raw))

let dir = %q
// bits unused for padded stream (%s, %s, %s)

let pkt_a = must_read(dir, "pkt_a.bin")
let pkt_b = must_read(dir, "pkt_b.bin")
let pkt_c = must_read(dir, "pkt_c_bad.bin")
let var_a = must_read(dir, "var_a.bin")
let var_ar = must_read(dir, "var_a_retain.bin")
let var_arrt = must_read(dir, "var_a_retain_rt.bin")
let var_b = must_read(dir, "var_b.bin")
let var_c = must_read(dir, "var_c_bad.bin")
let ann = must_read(dir, "announce.bin")
let msg_a = must_read(dir, "msg_a.bin")
let msg_b = must_read(dir, "msg_b.bin")
let msg_c = must_read(dir, "msg_c_bad.bin")
let fix_a = must_read(dir, "fixed_a.bin")
let fix_b = must_read(dir, "fixed_b.bin")
let fix_c = must_read(dir, "fixed_c_bad.bin")
let blk_a = must_read(dir, "block_a.bin")
let cook_b = must_read(dir, "cook_block_a.bin")

// 1. Packet dirty-instance reuse into same PacketBox
let box = reuse.NewPacketBox()
for(5) s:
    if s == 2:
        let rs = serialize_read_stream_padded(sub7(pkt_c, 7, 13))
        check(not reuse.ReadPacketBox(rs, box), "lobster pktC refuse")
    else:
        let is_a = s == 0 or s == 3
        let w = if is_a: pkt_a else: pkt_b
        let rs = serialize_read_stream_padded(sub7(w, 7, 13))
        check(reuse.ReadPacketBox(rs, box), "lobster pkt read")
        let ws = serialize_write_stream(schema_rt.zeros(512))
        check(reuse.WritePacketBox(ws, box), "lobster pkt write")
        ws.flush()
        check(substring(ws.buffer, 0, ws.bytes_processed()) == w, "lobster pkt bytes")

// 2. Form 1 Variable + Retain reuse (reusing same Retain across A -> B -> C -> A -> B)
for([var_a, var_b]) w:
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load("VarDoc", sub7(w, 7, 13), rep)
    check(rec != nil and not rep.damaged and not rep.refused, "lobster var load")
    if rec:
        let out, why = schema_rt.save_reasoned(rec)
        check(why == "" and out == w, "lobster var save")

if true:
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load("VarDoc", sub7(var_c, 7, 13), rep)
    check(rec == nil or rep.damaged or rep.refused, "lobster varC refuse")

let retain = schema_rt.Retain { capacity: 65536, id_capacity: 4096 }
for(5) s:
    if s == 2:
        let rep = schema_rt.LoadReport {}
        let rec = schema_rt.load_retain("VarDoc", sub7(var_c, 7, 13), retain, rep)
        check(rec == nil or rep.damaged or rep.refused, "lobster retC refuse")
    else:
        let is_a = s == 0 or s == 3
        let w_in = if is_a: var_ar else: var_b
        let w_want = if is_a: var_arrt else: var_b
        let rep = schema_rt.LoadReport {}
        let rec = schema_rt.load_retain("VarDoc", sub7(w_in, 7, 13), retain, rep)
        check(rec != nil and not rep.damaged and not rep.refused, "lobster ret load")
        check(if is_a: rep.retained > 0 else: rep.retained == 0, "lobster ret count")
        if rec:
            let srep = schema_rt.LoadReport {}
            let out = schema_rt.save_retain(rec, retain, srep)
            check(out == w_want, "lobster ret bytes")

// 3. Form 2 Message reuse (reusing vocab + mbatch[2] via load_messages_into)
let vocab = schema_rt.new_vocabulary(4096)
let arep = schema_rt.LoadReport {}
check(schema_rt.announce_read(vocab, sub7(ann, 7, 13), arep), "lobster announce_read")
let mbatch: [schema_rt.Record] = []
for(5) s:
    if s == 2:
        let rep = schema_rt.LoadReport {}
        let cnt, ok = schema_rt.load_messages_into("FixedDoc", vocab, sub7(msg_c, 7, 13), rep, 2, mbatch)
        check(not ok or cnt <= 0 or rep.damaged or rep.refused, "lobster msgC refuse")
    else:
        let is_a = s == 0 or s == 3
        let w = if is_a: msg_a else: msg_b
        let want_cnt = if is_a: 2 else: 1
        let rep = schema_rt.LoadReport {}
        let cnt, ok = schema_rt.load_messages_into("FixedDoc", vocab, sub7(w, 7, 13), rep, 2, mbatch)
        check(ok and cnt == want_cnt and not rep.damaged and not rep.refused, "lobster msg load")
        let slice = slice(mbatch, 0, cnt)
        let srep = schema_rt.LoadReport {}
        let out, sn = schema_rt.save_messages(slice, srep)
        check(sn == length(w) and out == w, "lobster msg save")

// 4. Form 3 Fixed reuse (reusing fbatch[2] + plan)
let fbatch = [reuse.NewFixedDoc(), reuse.NewFixedDoc()]
let fplan: [schema_rt.TableFixedEntry] = []
for(64): push(fplan, schema_rt.TableFixedEntry {})
for(5) s:
    if s == 2:
        let rep = schema_rt.TableReport {}
        check(reuse.FixedDocFixedLoad(fbatch, sub7(fix_c, 7, 13), fplan, rep) < 0, "lobster fixC refuse")
    else:
        let is_a = s == 0 or s == 3
        let w = if is_a: fix_a else: fix_b
        let want_cnt = if is_a: 2 else: 1
        let rep = schema_rt.TableReport {}
        let cnt = reuse.FixedDocFixedLoad(fbatch, sub7(w, 7, 13), fplan, rep)
        check(cnt == want_cnt and not rep.malformed and rep.verdict == 0 and rep.reason == "", "lobster fix load")
        let slice = slice(fbatch, 0, cnt)
        let flen = reuse.FixedDocFixedMeasure(cnt)
        let sn, out, why = reuse.FixedDocFixedSaveChecked(slice, schema_rt.zeros(flen))
        check(why == "" and sn == length(w) and out == w, "lobster fix save")

// 5. Form 4 Block (lead=64 vs lead=7) & Cook (lead=8 vs lead=7)
check(schema_rt.block_open_reason("BlockDoc", sub7(blk_a, 64, 13), 7, false) == "unaligned_base", "lobster Block +7 refuse")
check(schema_rt.block_open_reason("BlockDoc", sub7(blk_a, 64, 13), 64, false) == "ok", "lobster Block +64 open")
check(reuse.BlockDocOpenReason(sub7(cook_b, 8, 13), 7, false) == "unaligned_base", "lobster Cook +7 refuse")
check(reuse.BlockDocOpenReason(sub7(cook_b, 8, 13), 8, false) == "ok", "lobster Cook +8 open")

print("REUSE_OK lobster 10")
`, corpusDir, strings.TrimSpace(string(bitsABytes)), strings.TrimSpace(string(bitsBBytes)), strings.TrimSpace(string(bitsCBytes)))
	probe := filepath.Join(genDir, "probe.lobster")
	if err := os.WriteFile(probe, []byte(lobSrc), 0o644); err != nil {
		return err
	}
	lobsterBin := resolveGlobTool(repoRoot, "LOBSTER", "dist/lobster-*/bin/lobster", "lobster")
	out, err := runChecked(genDir, nil, lobsterBin, probe)
	return requireToken(out, err, "lobster")
}
