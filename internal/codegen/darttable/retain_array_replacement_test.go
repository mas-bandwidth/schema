package darttable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestDartRetainedArrayReplacementGlobalVector(t *testing.T) {
	slowtest.Gate(t, "dart")
	for _, tc := range []struct {
		dir, unit, root, vector                                  string
		unknown, mismatch, duplicate, retained, lost, savedBytes int
	}{
		{"maps", "Mapdemo", "Depth", "retain_array_replaced", 6, 1, 0, 3, 3, 113},
		{"lists", "Listdemo", "Save", "retain_list_replaced", 4, 0, 0, 4, 0, 47},
		{"lists", "Listdemo", "Army", "retain_list_maps_replaced", 8, 0, 4, 8, 0, 133},
		// STRICT VERDICT: the mismatch/duplicate counts above are the old
		// tolerant counters; under the strict read those rows refuse whole
		// (kind_mismatch / wire_damaged), handled by refusalFor below.
	} {
		t.Run(tc.vector, func(t *testing.T) {
			schemas, err := filepath.Glob("../../../tables/" + tc.dir + "/*.schema")
			if err != nil || len(schemas) == 0 {
				t.Fatalf("schemas: %v", err)
			}
			u := loadUnit(t, schemas...)
			m := tabletext.NewModel(u)
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(files, packetFiles(t, u))
			wire, err := os.ReadFile("../../../testdata/wire/tables/fuzz-vectors/" + tc.vector + ".bin")
			if err != nil {
				t.Fatal(err)
			}
			v := m.New(u.Tables[tc.root])
			var r tabletext.Report
			store := &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
			refusal := ""
			switch {
			case strings.Contains(tc.vector, "retain_list_replaced"):
				// The row's wire carries the replaced list field twice: damage.
				refusal = "wire_damaged"
			case tc.mismatch != 0:
				refusal = "kind_mismatch"
			case tc.duplicate != 0:
				refusal = "wire_damaged"
			}
			ok, derr := tablewire.DecodeRetain(m, v, wire, store, &r)
			if refusal != "" {
				if ok && !r.Refused {
					t.Fatalf("independent read must refuse: %+v %v", r, derr)
				}
			} else if !ok || derr != nil || r.Refused || r.Unknown != tc.unknown || r.Widened != 0 || r.Retained != tc.retained || r.RetainLost != tc.lost {
				t.Fatalf("independent read: %+v %v", r, derr)
			}
			var sr tabletext.Report
			saved, err := tablewire.EncodeRetain(m, v, store, &sr)
			if refusal == "" && (err != nil || sr.RetainLost != 0 || len(saved) != tc.savedBytes) {
				t.Fatalf("independent save: %+v %v bytes=%d", sr, err, len(saved))
			}
			text, err := m.Write(v)
			if err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			b.WriteString(strings.Replace(dartClosurePrelude, "ProbeWire.dart", tc.unit+"Wire.dart", 1))
			fmt.Fprintf(&b, `
 final data=bytes(%q);
 final store=w.TableRetain(65536,List.filled(1024,0));
 for(var load=0;load<2;load++){
  final r=w.TableWireReport();
  final v=w.rootWireLoadRetain(data,store,r);
  check(r.refused==%t,'array replacement verdict: ${r.reason}');
  if(r.refused){check(r.reason==%q,'array replacement refusal');check(store.used==0,'refused array load keeps no store');}
  if(!r.refused){
  check(r.unknown==%d&&r.widened==0&&r.retained==%d&&r.retainLost==%d,'array replacement read');
  check(w.rootToJson(v)==%q,'array replacement values');
  for(var save=0;save<2;save++){
   final sr=w.TableWireReport();
   final saved=w.rootWireSaveRetain(v,store,sr);
   check(sr.retainLost==0,'replaced array records are superseded, not lost');
   check(hex(saved)==%q,'array replacement retained bytes');
   check(w.rootWireMeasureRetain(v,store)==saved.length,'array replacement measure');
  }
  }
 }
}
`, hex.EncodeToString(wire), refusal != "", refusal, tc.unknown, tc.retained, tc.lost, string(text)+"\n", hex.EncodeToString(saved))
			runFormsProbe(t, files, strings.ReplaceAll(b.String(), "w.root", "w."+dartName(tc.root)))
		})
	}
}

func TestDartRetainedTableArrayReplacement(t *testing.T) {
	t.Skip("layer 4 (dart): replacement reads the same field id twice, damage under the strict read, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "dart")
	for _, union := range []bool{false, true} {
		t.Run(fmt.Sprintf("union_%v", union), func(t *testing.T) {
			elem := "Item"
			extra := ""
			if union {
				elem = "Choice"
				extra = "union Choice { item Item\n plain int32 }\n"
			}
			u := unitFrom(t, "package probe\ntable Node { known int32 }\ntable Item { known int32\n peer *Node }\n"+extra+"table Root { items [..3]"+elem+" }\n")
			m := tabletext.NewModel(u)
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(files, packetFiles(t, u))
			item := func(future, pointer bool) []byte {
				body := []byte{2, 4, 7, 0, 0, 0}
				if future {
					body = append(body, 3, 8, 9, 0, 0, 0)
				}
				if pointer {
					body = append(body, 4, 17, 2)
				}
				body = append(body, 0)
				frame := binary.AppendUvarint(nil, uint64(len(body)))
				frame = append(frame, body...)
				if union {
					frame = append([]byte{8, 13}, frame...)
				}
				return frame
			}
			kind := byte(13)
			if union {
				kind = 15
			}
			field := func(entries ...[]byte) []byte {
				payload := binary.AppendUvarint([]byte{kind}, uint64(len(entries)))
				for _, e := range entries {
					payload = append(payload, e...)
				}
				out := binary.AppendUvarint([]byte{1, 14}, uint64(len(payload)))
				return append(out, payload...)
			}
			var b strings.Builder
			b.WriteString(dartClosurePrelude)
			for _, tc := range []struct {
				name          string
				tail          []byte
				pointer       bool
				unknown, lost int
			}{
				{"empty", field(), false, 1, 0},
				{"populated", field(item(false, false)), false, 1, 0},
				{"incompatible_preserves", []byte{1, 14, 2, 8, 0}, false, 1, 0},
				{"detached_pointer_remains_lost", field(), true, 2, 1},
			} {
				wire := append([]byte{1}, field(item(true, tc.pointer))...)
				wire = append(wire, tc.tail...)
				if tc.pointer {
					wire = append(wire, 5, 12, 10, 1, 6, 7, 7, 8, 9, 0, 0, 0, 0)
				}
				wire = append(wire, 0)
				ids := []uint64{ir.TableWireId("items"), ir.TableWireId("known"), ir.TableWireId("future"), ir.TableWireId("peer"), ir.TableNodeWireId, ir.TableWireId("Node"), ir.TableWireId("futureNode"), ir.TableWireId("item")}
				for _, id := range ids {
					wire = binary.LittleEndian.AppendUint64(wire, id)
				}
				wire = binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
				v := m.New(u.Tables["Root"])
				var r tabletext.Report
				store := &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
				km := 0
				if tc.name == "incompatible_preserves" {
					km = 1
				}
				ok, derr := tablewire.DecodeRetain(m, v, wire, store, &r)
				if km != 0 {
					if ok && !r.Refused {
						t.Fatalf("%s neutral read must refuse: %+v %v", tc.name, r, derr)
					}
				} else if !ok || derr != nil || r.Refused || r.Unknown != tc.unknown || r.Retained != tc.unknown || r.RetainLost != 0 {
					t.Fatalf("%s neutral read: %+v %v", tc.name, r, derr)
				}
				var sr tabletext.Report
				saved, err := tablewire.EncodeRetain(m, v, store, &sr)
				if km == 0 && (err != nil || sr.RetainLost != tc.lost) {
					t.Fatalf("%s neutral save: %+v %v", tc.name, sr, err)
				}
				text, err := m.Write(v)
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&b, `
 {final data=bytes(%q);final store=w.TableRetain(65536,List.filled(1024,0));
 for(var load=0;load<2;load++){
  final r=w.TableWireReport();final v=w.rootWireLoadRetain(data,store,r);
  check(r.refused==%t,%q);
  if(r.refused){check(r.reason=='kind_mismatch',%q);check(store.used==0,%q);}
  if(!r.refused){
  check(r.unknown==%d&&r.retained==%d&&r.retainLost==0,%q);
  check(w.rootToJson(v)==%q,%q);
  for(var save=0;save<2;save++){
   final sr=w.TableWireReport();final saved=w.rootWireSaveRetain(v,store,sr);
   check(sr.retainLost==%d,%q);check(hex(saved)==%q,%q);
   check(w.rootWireMeasureRetain(v,store)==saved.length,%q);
  }
  }
 }}
`, hex.EncodeToString(wire), km != 0, tc.name+" verdict", tc.name+" refusal", tc.name+" refused store", tc.unknown, tc.unknown, tc.name+" read", string(text)+"\n", tc.name+" values", tc.lost, tc.name+" replacement loss", hex.EncodeToString(saved), tc.name+" saved bytes", tc.name+" measure")
			}
			b.WriteString("}\n")
			runFormsProbe(t, files, b.String())
		})
	}
}
