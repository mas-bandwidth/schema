package jstable

import (
	"encoding/binary"
	"encoding/hex"
	"os"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestJavaScriptMapRepeatedKeyLastKindWins(t *testing.T) {
	t.Skip("layer 4 (js): tolerant-read test, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "node")
	type row struct {
		Name   string `json:"name"`
		Root   string `json:"root"`
		Wire   string `json:"wire"`
		Saved  string `json:"saved"`
		Report []int  `json:"report"`
	}
	run := func(u *ir.Unit, root string, wires map[string][]byte) {
		m := tabletext.NewModel(u)
		cases := []row{}
		for name, wire := range wires {
			value := m.New(m.Lookup(root))
			var rep tabletext.Report
			ok, err := tablewire.Decode(m, value, wire, &rep)
			if !ok || err != nil {
				t.Fatalf("%s oracle: %v %v %+v", name, ok, err, rep)
			}
			saved, err := tablewire.Encode(m, value)
			if err != nil {
				t.Fatal(err)
			}
			cases = append(cases, row{name, root, hex.EncodeToString(wire), hex.EncodeToString(saved), []int{int(rep.Unknown), 0, int(rep.Widened), 0, 0}})
		}
		files, err := generateVariableFiles(u, nil)
		if err != nil {
			t.Fatal(err)
		}
		runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", `
import {readFileSync} from "node:fs";
import {pathToFileURL} from "node:url";
const m=await import(pathToFileURL(process.argv[3]));
const rows=JSON.parse(readFileSync(process.argv[2],"utf8"));
for(const c of rows) for(const retaining of [false,true]) for(let repeat=0;repeat<2;repeat++){
 const wire=Uint8Array.from(Buffer.from(c.wire,"hex"));
 const report=new m.TableVariableReport();
 const store=new m.TableVariableRetain(new Uint8Array(1<<20),new Array((1<<20)/8));
 const value=retaining?m[c.root+"LoadRetain"](wire,store,report):m[c.root+"Load"](wire,report);
 const counters=[report.unknown,report.mismatch,report.widened,report.oor,report.repeated];
 if(value===null||report.damaged||report.refused||JSON.stringify(counters)!==JSON.stringify(c.report))
  throw Error(c.name+" retain="+retaining+" report="+JSON.stringify(report));
 const saved=m[c.root+"Save"](value);
 if(saved===null||Buffer.from(saved).toString("hex")!==c.saved)
  throw Error(c.name+" canonical value differs, retain="+retaining);
}
console.log("all repeated-key cases passed");
`, cases)
	}
	u := unitFrom(t, "package probe\ntable Root { entries map[uint32]int32 }\n")
	f := u.Tables["Root"].Fields[0]
	wires := map[string][]byte{}
	for _, tc := range []struct {
		name string
		keys []byte
	}{
		{"bad_then_widened", []byte{2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 6, 7}},
		{"widened_then_bad", []byte{2, 6, 7, 2, 9, 0, 0, 0, 0, 0, 0, 0, 0}},
		{"bad_then_exact", []byte{2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 8, 7, 0, 0, 0}},
	} {
		entry := append(append([]byte{}, tc.keys...), 0)
		payload := append([]byte{13, 1, byte(len(entry))}, entry...)
		wire := append([]byte{1, 1, 14, byte(len(payload))}, payload...)
		wire = append(wire, 0)
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(f))
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(f.MapEntry.Fields[0]))
		wire = binary.LittleEndian.AppendUint64(wire, 2)
		wires[tc.name] = wire
	}
	run(u, "Root", wires)
	maps := loadUnitPaths(t, []string{"../../../tables/maps"})
	vector, err := os.ReadFile("../../../testdata/wire/tables/fuzz-vectors/map_repeated_key_widen.bin")
	if err != nil {
		t.Fatal(err)
	}
	run(maps, "Pairs", map[string][]byte{"shared_repeated_key_widen": vector})
}
