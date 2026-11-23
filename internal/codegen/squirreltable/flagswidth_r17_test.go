package squirreltable_test

// A TABLE LOAD HOLDS A MASK TO ITS WIDTH (docs/SPEC-TABLES.md §4; R17, Glenn
// 2026-11-22: "The newer reader can read old data, but never the other way").
// W is the flags declaration wire width (the variant count, or K under
// `| max = K`). Every table read refuses a mask with a bit at or above W as
// out_of_range: the variable load, the fixed load (the runtime walk and the
// generated FixedLoad), and the message read of a newer writer that announces
// a wider mask. Every table writer refuses to write such a mask, in every
// build (docs/SPEC.md §5.1): Squirrel has no assert.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

const sqR17Schema = `package rseventeen

flags Perms { %s }
flags Wide | max = 40 { A }

table Doc
{
    lead  uint32
    perms Perms
    mask  Wide
}

fixed table Rec
{
    lead  uint32
    perms Perms
    mask  Wide
    trail uint32
}
`

// sqR17Run generates the unit into a fresh directory, runs the probe body
// there and answers its stdout.
func sqR17Run(t *testing.T, perms, body string, inputs map[string][]byte) string {
	t.Helper()
	u := fixedSynthetic(t, fmt.Sprintf(sqR17Schema, perms))
	dir := t.TempDir()
	writeGenerated(t, dir, u)
	head := fmt.Sprintf(`local I=dofile("inputs.nut",true);
local S=dofile("schema_module.nut",true);
local T=S.require(%q);
local L=S.require("schema_table_load");
local hex=function(b){local s="";for(local i=0;i<b.len();i++)s+=format("%%02x",b[i]&0xff);return s;};
`, squirreltable.ModuleName(u))
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(head+body), 0o644); err != nil {
		t.Fatal(err)
	}
	return string(sqrun.Run(t, dir, "main.nut", inputs))
}

func sqR17Wires(t *testing.T, out string) map[string][]byte {
	t.Helper()
	kv := map[string][]byte{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			b, err := hex.DecodeString(v)
			if err != nil {
				t.Fatalf("%s: %v", k, err)
			}
			kv[k] = b
		}
	}
	return kv
}

func TestSquirrelFlagsMaskHeldToItsWidth(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)

	// The narrow build writes lawful files carrying a sentinel in the mask
	// slot (W = 40), and each of its writers answers a mask past W.
	wrote := sqR17Run(t, "Read, Write", `local d=T.Doc(); d.Lead=7; d.Perms=3; d.Mask=0x123456789a;
local r=T.Rec(); r.Lead=7; r.Perms=3; r.Mask=0x123456789a; r.Trail=9;
local db=T.DocSave(d);
if(db==null) throw "the lawful Doc was refused";
print("doc="+hex(db)+"\n");
local fsz=T.RecFixedMeasure(1);
local fb=blob(fsz);
if(T.RecFixedSave(fb,[r])!=fsz) throw "the lawful Rec was refused: "+T.FixedReason;
print("rec="+hex(fb)+"\n");
foreach(c in [["Perms",4,"perms:4"],["Mask",1<<40,"mask:bit40"],["Mask",1<<63,"mask:bit63"]]) {
  local d2=T.Doc(); d2[c[0]]=c[1];
  local r2=T.Rec(); r2[c[0]]=c[1];
  local v="";
  try { v=T.DocSave(d2)==null ? "refused" : "wrote"; } catch(e) { v="raised "+e; }
  print("Save "+c[2]+" "+v+"\n");
  try { v=T.RecSave(r2)==null ? "refused" : "wrote"; } catch(e) { v="raised "+e; }
  print("SaveFixedRoot "+c[2]+" "+v+"\n");
  try { local o=blob(fsz); v=T.RecFixedSave(o,[r2])<0 ? "refused" : "wrote"; } catch(e) { v="raised "+e; }
  print("FixedSave "+c[2]+" "+v+"\n");
  try {
    local mr={}; local ms=T.RecMeasureMessages([r2],mr);
    if(ms<0) v="refused";
    else { local o=blob(ms); local sr={}; v=T.RecSaveMessages([r2],o,sr)<0 ? "refused" : "wrote"; }
  } catch(e) { v="raised "+e; }
  print("SaveMessages "+c[2]+" "+v+"\n");
}
`, nil)
	for _, tag := range []string{"perms:4", "mask:bit40", "mask:bit63"} {
		for _, w := range []string{"Save", "SaveFixedRoot", "FixedSave", "SaveMessages"} {
			line := w + " " + tag + " refused\n"
			if !strings.Contains(wrote, line) {
				t.Errorf("want %q (a writer writes no mask its reader refuses); the probe printed:\n%s", line, wrote)
			}
		}
	}
	files := sqR17Wires(t, wrote)

	// The newer build (Perms has a third flag) writes a message batch with the
	// new bit set and one without it, and hands over its announcement.
	maps.Copy(files, sqR17Wires(t, sqR17Run(t, "Read, Write, Exec", `print("announce="+hex(T.TableMessageAnnouncement)+"\n");
foreach(c in [["msg_inside",3],["msg_past",5]]) {
  local x=T.Rec(); x.Perms=c[1];
  local mr={}; local ms=T.RecMeasureMessages([x],mr);
  if(ms<0) throw "the newer build refused its own batch";
  local o=blob(ms); local sr={};
  if(T.RecSaveMessages([x],o,sr)!=ms) throw "the newer build refused its own batch";
  print(c[0]+"="+hex(o)+"\n");
}
`, nil)))

	var slot [8]byte
	binary.LittleEndian.PutUint64(slot[:], 0x123456789a)
	inputs := map[string][]byte{}
	for _, name := range []string{"doc", "rec"} {
		b := files[name]
		if n := bytes.Count(b, slot[:]); n != 1 {
			t.Fatalf("%s: the mask slot appears %d times, want 1", name, n)
		}
		at := bytes.Index(b, slot[:])
		for suffix, value := range map[string]uint64{"inside": 0x123456789a, "past40": 0x123456789a | 1<<40, "bit63": 1 << 63} {
			out := append([]byte(nil), b...)
			binary.LittleEndian.PutUint64(out[at:at+8], value)
			inputs[name+"_"+suffix] = out
		}
	}
	for _, k := range []string{"announce", "msg_inside", "msg_past"} {
		if len(files[k]) == 0 {
			t.Fatalf("the newer build printed no %s wire", k)
		}
		inputs[k] = files[k]
	}

	read := sqR17Run(t, "Read, Write", `foreach(n in ["inside","past40","bit63"]) {
  foreach(root in ["Doc","Rec"]) {
    local rep={unknown=0,mismatch=0,widened=0,oor=0,repeated=0,bodyDamaged=false,refused=false};
    local x=L.load(T, root, I[root.tolower()+"_"+n], rep);
    print("Load "+root+" "+n+" "+(x==null ? "refused "+rep.reason : (x.Mask==0x123456789a ? "read sentinel" : "read other"))+"\n");
  }
  local back=T.Rec();
  local got=T.RecFixedLoad(I["rec_"+n],[back],null);
  print("FixedLoad "+n+" "+(got==1 ? (back.Mask==0x123456789a ? "read sentinel" : "read other") : "refused "+T.FixedReason)+"\n");
}
local v={}; local ar={};
if(!T.AnnounceRead(v,I["announce"],ar)) throw "announce refused: "+ar.reason;
foreach(n in ["msg_inside","msg_past"]) {
  local out=[T.Rec()]; local rr={};
  local got=T.RecLoadMessages(out,v,I[n],rr);
  print("LoadMessages "+n+" "+(got==1 ? "read" : "refused "+rr.reason)+"\n");
}
`, inputs)
	want := []string{
		"Load Doc inside read sentinel",
		"Load Doc past40 refused out_of_range",
		"Load Doc bit63 refused out_of_range",
		"Load Rec inside read sentinel",
		"Load Rec past40 refused out_of_range",
		"Load Rec bit63 refused out_of_range",
		"FixedLoad inside read sentinel",
		"FixedLoad past40 refused out_of_range",
		"FixedLoad bit63 refused out_of_range",
		"LoadMessages msg_inside read",
		"LoadMessages msg_past refused out_of_range",
	}
	for _, line := range want {
		if !strings.Contains(read, line+"\n") {
			t.Errorf("want %q; the probe printed:\n%s", line, read)
		}
	}
}
