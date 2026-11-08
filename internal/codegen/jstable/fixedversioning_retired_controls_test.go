package jstable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// These probes replace the still-printed historical controls in fixedform.mjs.
// The read uses the writer's locked entry, never a plan compiled from wire data.
// Values come from the C++ writer; faults are applied only after its clean file
// has passed. No retired forward-read or run-time-layout-walk behavior is revived.
func TestJSFixedVersioningBytesAndDefaults(t *testing.T) {
	slowtest.Gate(t, "native JavaScript fixed lineage controls")
	corpus, node := jsFixedCorpus(t), jsNode(t)
	older, newer := jsReadSchema(t, "FX1"), jsReadSchema(t, "FX2")
	for _, compiled := range []bool{false, true} {
		t.Run(fmt.Sprintf("compiled=%v", compiled), func(t *testing.T) {
			reader, lineage := older, []string(nil)
			extra := ""
			widened, unknown := 0, 0
			if compiled {
				reader, lineage = newer, []string{older}
				extra = `if (v.added !== 11 || v.extra.x !== 0 || v.extra.y !== 0) { fail("missing fields lost their defaults: " + show(v)); }`
				widened, unknown = 2, 1
			}
			body := fmt.Sprintf(`
const data = readFileSync(%q);
const plan = TNewPlan(4096, 4096);
const back = [new T(), new T()];
for (const poison of [0x5A, 0xFF]) {
  plan.image.fill(poison);
  const r = new TableFixedReport();
  const n = TLoad(back, 2, data, data.length, plan, r);
  if (n !== 2 || r.refused || r.damaged || r.unknown !== %d || r.widened !== %d ||
      r.mismatch || r.oor || r.repeated) { fail("FX1 C++ read: " + n + " " + reason(r)); }
  const v = back[0];
  if (v.keep !== 4242 || v.narrow !== 40000 || v.nested.a !== 111 || v.nested.b !== 222 ||
      v.blobLength !== 4 || [222,173,190,239,0,0].some((b,i) => v.blob[i] !== b)) {
    fail("partly-used bytes or neighbours: " + show(v));
  }
  if (back[1].blobLength !== 0 || Array.from(back[1].blob).some(b => b !== 0)) {
    fail("empty bytes/slack after populated record: " + show(back[1]));
  }
  %s
}
`, filepath.Join(corpus, "fx1.bin"), unknown, widened, extra)
			if out, err := jsRunVersionProbe(t, node, reader, lineage, 0, "FxRoot", body); err != nil {
				t.Fatalf("C++ bytes/defaults: %v\n%s", err, out)
			}
		})
	}
}

func TestJSFixedVersioningArmTextControls(t *testing.T) {
	slowtest.Gate(t, "native JavaScript fixed lineage controls")
	corpus, node := jsFixedCorpus(t), jsNode(t)
	older, newer := jsReadSchema(t, "FU1"), jsReadSchema(t, "FU2")
	for _, compiled := range []bool{false, true} {
		t.Run(fmt.Sprintf("compiled=%v", compiled), func(t *testing.T) {
			reader, lineage, widened := older, []string(nil), 0
			extra := ""
			if compiled {
				reader, lineage, widened = newer, []string{older}, 6
				extra = `if (back.some(v => v.extra !== 11)) { fail("appended defaults"); }`
			}
			body := fmt.Sprintf(`
const data = new Uint8Array(readFileSync(%q));
const at = 20 + new DataView(data.buffer).getUint32(16, true) + 8;
const back = [new T(), new T(), new T()];
const plan = TNewPlan(4096, 4096);
for (const [flag, present] of [[1,1],[2,7],[255,128],[0,0]]) {
  data[at] = flag; data[at+1] = present;
  plan.image.fill(0x5A);
  const r = new TableFixedReport();
  const n = TLoad(back, 3, data, data.length, plan, r);
  if (n !== 3 || r.refused || r.damaged || r.unknown || r.mismatch || r.oor ||
      r.repeated || r.widened !== %d) { fail("arm/flag report: " + n + " " + reason(r)); }
  const a = back[0], b = back[1], c = back[2];
  if (a.flag !== (flag !== 0) || a.notePresent !== (present !== 0) ||
      a.note !== (present ? 44 : 0)) { fail("noncanonical/zero flags: " + show(a)); }
  if (a.pick.type !== 2 || a.pick.labelled.lead !== 101 || a.pick.labelled.trail !== 202 ||
      a.pick.labelled.labelLength !== 5 || [104,101,108,108,111,0,0,0].some((v,i) => a.pick.labelled.label[i] !== v) ||
      a.tail !== 11 || a.mark !== -1 || a.heat !== 2 ** -149) { fail("second arm text and neighbours: " + show(a)); }
  if (b.flag || b.notePresent || b.pick.type !== 1 || b.pick.plain.n !== 303 ||
      b.tail !== 12 || b.mark !== -32768 || !Object.is(b.heat,-0)) { fail("first arm overwritten by sibling text: " + show(b)); }
  if (c.pick.type !== 2 || c.pick.labelled.labelLength !== 0 || c.pick.labelled.lead !== -5 ||
      c.pick.labelled.trail !== 606 || c.tail !== 13) { fail("empty text arm: " + show(c)); }
  %s
}
`, filepath.Join(corpus, "fu1.bin"), widened, extra)
			if out, err := jsRunVersionProbe(t, node, reader, lineage, 0, "FuRoot", body); err != nil {
				t.Fatalf("C++ union text controls: %v\n%s", err, out)
			}
		})
	}
}

// Build the existing independent optional corpus in a temporary directory. The
// ordinary native gate runs before make's external fixtures, so this must not
// depend on a prior build/js-fixed-optional or silently skip it in required mode.
func jsFixedCPPFiles(t *testing.T, schemas map[string]string, driver string, outputs []string) string {
	t.Helper()
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	tool, err := exec.LookPath(cxx)
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("C++ optional oracle requires %s: %v", cxx, err)
		}
		t.Skip("C++ optional oracle requires a C++ compiler")
	}
	dir := t.TempDir()
	args := []string{"-std=c++17", "-O0"}
	for name, source := range schemas {
		file := name + ".schema"
		ast, errs := parser.Parse(file, []byte(source))
		if len(errs) != 0 {
			t.Fatal(errs)
		}
		unit, errs := check.Unit([]check.SourceFile{{Path: file, Name: file, Base: name, Bytes: []byte(source), AST: ast}})
		if len(errs) != 0 {
			t.Fatal(errs)
		}
		files, err := cpp.Generate(unit)
		if err != nil {
			t.Fatal(err)
		}
		tables, err := cpptable.Generate(unit)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, tables)
		out := filepath.Join(dir, name)
		if err := os.MkdirAll(out, 0700); err != nil {
			t.Fatal(err)
		}
		for leaf, data := range files {
			if strings.Contains(leaf, "/") {
				continue
			}
			if err := os.WriteFile(filepath.Join(out, leaf), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		args = append(args, "-I", out)
	}
	serialize := os.Getenv("SERIALIZE")
	if serialize == "" {
		serialize = "../../../../serialize"
	}
	driverPath := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(driverPath, []byte(driver), 0600); err != nil {
		t.Fatal(err)
	}
	args = append(args, "-I", serialize, driverPath, "-o", filepath.Join(dir, "corpus"))
	if out, err := exec.Command(tool, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile C++ optional oracle: %v\n%s", err, out)
	}
	var paths []string
	for _, output := range outputs {
		paths = append(paths, filepath.Join(dir, output))
	}
	if out, err := exec.Command(filepath.Join(dir, "corpus"), paths...).CombinedOutput(); err != nil {
		t.Fatalf("run C++ optional oracle: %v\n%s", err, out)
	}
	return dir
}

func TestJSFixedVersioningOptionalControls(t *testing.T) {
	slowtest.Gate(t, "native JavaScript fixed lineage controls")
	node := jsNode(t)
	driver, err := os.ReadFile("../../../test/js-tables/fixedoptional_corpus.cpp")
	if err != nil {
		t.Fatal(err)
	}
	schemas := map[string]string{}
	for _, name := range []string{"P1", "P3", "FO1"} {
		schemas[name] = jsReadSchema(t, name)
	}
	file := filepath.Join(jsFixedCPPFiles(t, schemas, string(driver), []string{"p1.bin", "p3.bin", "fo1.bin"}), "fo1.bin")
	older, newer := jsReadSchema(t, "FO1"), jsReadSchema(t, "FO2")
	for _, compiled := range []bool{false, true} {
		t.Run(fmt.Sprintf("compiled=%v", compiled), func(t *testing.T) {
			reader, lineage, plain := older, []string(nil), "plain"
			extra := ""
			if compiled {
				reader, lineage, plain = newer, []string{older}, "plain2"
				extra = `if (back.some(v => v.added !== 77)) { fail("inserted default lost on reuse"); }`
			}
			body := fmt.Sprintf(`
const file = new Uint8Array(readFileSync(%q));
const head = 20 + new DataView(file.buffer).getUint32(16,true);
const rec = (file.length-head)/4;
if (rec !== 80) { fail("FO1 reference record extent changed"); }
const back = [new T(),new T(),new T(),new T()];
const plan = TNewPlan(4096,4096);
let loadIndex = 0;
function load(data, count=1, clamped=0) {
  loadIndex++;
  plan.image.fill(0x5A);
  const r = new TableFixedReport();
  const n = TLoad(back,count,data,data.length,plan,r);
  if (n !== count || r.refused || r.damaged || r.unknown || r.mismatch || r.widened || r.repeated || r.oor !== clamped) {
    fail("optional load " + loadIndex + " expected clamps=" + clamped + ": " + n + " " + reason(r));
  }
  return back[0];
}
function one(k) {
  const out = new Uint8Array(head+rec); out.set(file.subarray(0,head));
  out.set(file.subarray(head+k*rec,head+(k+1)*rec),head); return out;
}
for (let repeat=0;repeat<2;repeat++) {
  load(file,4);
  const [a,b,c,d] = back;
  if (a.%s !== 101 || !a.optPresent || a.opt.v !== 11 || a.opt.tagLength !== 2 ||
      !a.numberPresent || a.number !== 12 || !a.markPresent || a.mark !== 2 ||
      a.wrap.n !== 13 || !a.wrap.leafPresent || a.wrap.leaf.v !== 14 || a.effect.type !== 1 || a.effect.boost.power !== 15 || a.tail !== 16) { fail("all present relocated: " + show(a)); }
  if (b.optPresent || b.opt.v !== 7 || b.numberPresent || b.markPresent || b.wrap.leafPresent || b.wrap.leaf.v !== 7 ||
      b.effect.type !== 2 || b.effect.ward.charge !== 0.5 || b.tail !== 203) { fail("all absent defaults: " + show(b)); }
  if (c.optPresent || !c.numberPresent || c.number !== -302 || !c.wrap.leafPresent || c.wrap.leaf.v !== -304 || c.tail !== 305 ||
      !d.optPresent || d.opt.v !== 402 || d.wrap.leafPresent || d.tail !== 404) { fail("mixed nested presence: " + show(back)); }
  %s
}
const body = head+8;
{
  const data = one(1), v = new DataView(data.buffer);
  data[body+20]=7; v.setInt32(body+21,999,true);
  data[body+48]=255; v.setInt32(body+49,42,true);
  data[body+37]=2; data[body+42]=128;
  const got=load(data);
  if (!got.optPresent || got.opt.v!==999 || !got.wrap.leafPresent || got.wrap.leaf.v!==42 || !got.numberPresent || !got.markPresent) { fail("nonzero presence flags: "+show(got)); }
}
{
  const data = one(1), v = new DataView(data.buffer);
  v.setInt32(body+21,2147483647,true); v.setInt32(body+49,2147483647,true);
  v.setInt32(body+38,123,true); data[body+43]=9;
  const got=load(data);
  if (got.optPresent || got.opt.v!==7 || got.wrap.leafPresent || got.wrap.leaf.v!==7 ||
      got.numberPresent || got.number!==0 || got.markPresent || got.mark!==0) { fail("absent poison projected: "+show(got)); }
}
for (const [input,want,clamps] of [[9999,1000,1],[-1,0,1],[1000,1000,0],[0,0,0]]) {
  const data=one(1); new DataView(data.buffer).setInt32(body+16,input,true);
  if (load(data,1,clamps).%s !== want) { fail("integer clamp boundary"); }
}
{
  const data=one(0); new DataView(data.buffer).setInt32(body+66,9999,true);
  if (load(data,1,1).effect.boost.power!==1000) { fail("live union range clamp"); }
}
for (const [offset,kind] of [[65,"union"],[43,"enum"]]) {
  const data=one(0); data[body+offset]=9;
  const got=load(data,1,1);
  if (got.tail!==16 || (kind==="union" ? got.effect.type!==0 : !got.markPresent || got.mark!==0)) { fail("forged ordinal: "+show(got)); }
}
`, file, plain, extra, plain)
			if out, err := jsRunVersionProbe(t, node, reader, lineage, 0, "OptRoot", body); err != nil {
				t.Fatalf("C++ optional controls: %v\n%s", err, out)
			}
		})
	}
}

func TestJSFixedVersioningOptionalGuardChain(t *testing.T) {
	slowtest.Gate(t, "native JavaScript fixed lineage controls")
	node := jsNode(t)
	const older = `package probe

enum E { Bronze, Gold }
fixed table Inner { e ?E }
union Pick {
    data Inner
    empty
}
fixed table Holder { pick Pick }
fixed table Nested {
    head uint32
    opt ?Holder
    tail uint32
}
`
	newer := strings.Replace(older, "    opt ?Holder", "    inserted int32 = 77\n    opt ?Holder", 1)
	const writer = `#include "N1Table.h"
#include <cstdio>
#include <new>
#include <vector>
int main(int argc, char** argv) {
    if (argc != 2) return 1;
    probe::Nested v; probe::NestedReset(v);
    v.head=0x11223344; v.tail=0x55667788;
    v.opt_present=true;
    ::new ((void*)&v.opt.pick.data) probe::Inner{};
    v.opt.pick.type=probe::PickType::Data;
    v.opt.pick.data.e_present=true; v.opt.pick.data.e=probe::E::Gold;
    std::vector<uint8_t> out((size_t)probe::NestedFixedMeasure(1));
    if(probe::NestedFixedSave(&v,1,out.data(),(int64_t)out.size())!=(int64_t)out.size()) return 2;
    FILE* f=std::fopen(argv[1],"wb"); if(!f) return 3;
    const bool ok=std::fwrite(out.data(),1,out.size(),f)==out.size();
    return std::fclose(f)==0 && ok ? 0 : 4;
}
`
	dir := jsFixedCPPFiles(t, map[string]string{"N1": older}, writer, []string{"nested.bin"})
	for _, compiled := range []bool{false, true} {
		t.Run(fmt.Sprintf("compiled=%v", compiled), func(t *testing.T) {
			reader, lineage := older, []string(nil)
			extra := ""
			if compiled {
				reader, lineage = newer, []string{older}
				extra = `if (got.inserted!==77) { fail("inserted default"); }`
			}
			body := fmt.Sprintf(`
const data = new Uint8Array(readFileSync(%q));
const at=20+new DataView(data.buffer).getUint32(16,true)+8;
if ([1,1,1,2].some((v,i)=>data[at+4+i]!==v)) { fail("C++ optional/union fixture positions"); }
const back=[new T()], plan=TNewPlan(4096,4096);
for (const [outer,tag,inner,ordinal,clamps] of [[1,1,1,2,0],[0,9,7,9,0],[1,1,0,9,0],[2,1,7,9,1],[2,1,255,2,0],[1,9,1,2,1],[1,2,7,9,0]]) {
  data.set([outer,tag,inner,ordinal],at+4); plan.image.fill(0x5A);
  const r=new TableFixedReport(), n=TLoad(back,1,data,data.length,plan,r), got=back[0];
  if(n!==1 || r.refused || r.damaged || r.unknown || r.mismatch || r.widened || r.repeated || r.oor!==clamps) {
    fail("nested guards "+[outer,tag,inner,ordinal]+" expected clamps="+clamps+" "+reason(r));
  }
  if(got.head!==0x11223344 || got.tail!==0x55667788 || got.optPresent!==(outer!==0)) { fail("optional guard neighbours/presence"); }
  if(outer) {
    if(got.opt.pick.type!==(tag>2 ? 0 : tag)) { fail("selected optional union tag"); }
    if(tag===1 && (got.opt.pick.data.ePresent!==(inner!==0) || got.opt.pick.data.e!==(inner && ordinal<=2 ? ordinal : 0))) { fail("inner optional enum projection"); }
  }
  %s
}
`, filepath.Join(dir, "nested.bin"), extra)
			if out, err := jsRunVersionProbe(t, node, reader, lineage, 0, "Nested", body); err != nil {
				t.Fatalf("nested C++ guards: %v\n%s", err, out)
			}
		})
	}
}

func TestJSFixedVersioningReferenceForwardRefusal(t *testing.T) {
	slowtest.Gate(t, "native JavaScript fixed lineage controls")
	corpus, node := jsFixedCorpus(t), jsNode(t)
	body := fmt.Sprintf(`
const data=readFileSync(%q), back=[new T()], plan=TNewPlan(4096,4096);
const before=show(back); plan.image.fill(0x5A);
const r=new TableFixedReport(), n=TLoad(back,1,data,data.length,plan,r);
const hash=new DataView(data.buffer,data.byteOffset,data.length).getBigUint64(8,true);
if(n!==-1 || r.refused!==TableFixedRefusal.LayoutNewer || r.layoutHash!==hash || r.damaged ||
   r.unknown || r.mismatch || r.widened || r.oor || r.repeated) { fail("FX2 outside FX1 lineage: "+reason(r)); }
if(show(back)!==before || plan.image.some(b=>b!==0x5A)) { fail("unknown reference version wrote destination"); }
`, filepath.Join(corpus, "fx2.bin"))
	if out, err := jsRunVersionProbe(t, node, jsReadSchema(t, "FX1"), nil, 0, "FxRoot", body); err != nil {
		t.Fatalf("C++ forward refusal: %v\n%s", err, out)
	}
}
