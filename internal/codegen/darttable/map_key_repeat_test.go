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

func dartMapKeyCase(t *testing.T, b *strings.Builder, m *tabletext.Model, def *ir.Struct, name string, data []byte, wantCount, wantUnknown, wantMismatch, wantWidened int) {
	t.Helper()
	for _, retaining := range []bool{false, true} {
		v := m.New(def)
		var r tabletext.Report
		store := &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
		var ok bool
		var err error
		if retaining {
			ok, err = tablewire.DecodeRetain(m, v, data, store, &r)
		} else {
			ok, err = tablewire.Decode(m, v, data, &r)
		}
		if !ok || err != nil || r.Malformed || len(v.Fields[0].Entries) != wantCount || r.Unknown != wantUnknown || r.KindMismatch != wantMismatch || r.Widened != wantWidened {
			t.Fatalf("%s oracle count%d report%+v err%v", name, len(v.Fields[0].Entries), r, err)
		}
		text, err := m.Write(v)
		if err != nil {
			t.Fatal(err)
		}
		saved, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		var sr tabletext.Report
		retained, err := tablewire.EncodeRetain(m, v, store, &sr)
		if err != nil {
			t.Fatal(err)
		}
		fn := dartName(def.Name)
		load := fmt.Sprintf("w.%sWireLoad(data,r)", fn)
		if retaining {
			load = fmt.Sprintf("w.%sWireLoadRetain(data,store,r)", fn)
		}
		fmt.Fprintf(b, `{final data=bytes(%q);final store=w.TableRetain(65536,List.filled(1024,0));
for(var repeat=0;repeat<2;repeat++){
 final r=w.TableWireReport();final v=%s;
 check(!r.malformed&&!r.refused&&r.unknown==%d&&r.kindMismatch==%d&&r.widened==%d&&r.clamped==%d&&r.duplicate==%d&&r.retained==%d&&r.retainLost==%d,%q);
 check(w.%sToJson(v)==%q,%q);
 check(hex(w.%sWireSave(v))==%q,%q);
`, hex.EncodeToString(data), load, r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Retained, r.RetainLost, name+" report", fn, string(text)+"\n", name+" values", fn, hex.EncodeToString(saved), name+" bytes")
		if retaining {
			fmt.Fprintf(b, ` final sr=w.TableWireReport();final saved=w.%sWireSaveRetain(v,store,sr);
 check(hex(saved)==%q&&sr.retainLost==%d,%q);
`, fn, hex.EncodeToString(retained), sr.RetainLost, name+" retained bytes")
		}
		b.WriteString("}}\n")
	}
}

func TestDartMapRepeatedKeyLastKindWins(t *testing.T) {
	slowtest.Gate(t, "dart")
	u := unitFrom(t, "package probe\ntable Root { entries map[uint32]int32 }\n")
	m := tabletext.NewModel(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	var b strings.Builder
	b.WriteString(dartClosurePrelude)
	for _, tc := range []struct {
		name           string
		keys           []byte
		count, widened int
	}{
		{"bad_then_widened", []byte{2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 6, 7}, 1, 1},
		{"widened_then_bad", []byte{2, 6, 7, 2, 9, 0, 0, 0, 0, 0, 0, 0, 0}, 0, 1},
		{"bad_then_exact", []byte{2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 8, 7, 0, 0, 0}, 1, 0},
	} {
		entry := append(append([]byte(nil), tc.keys...), 0)
		payload := append([]byte{13, 1, byte(len(entry))}, entry...)
		wire := append([]byte{1, 1, 14, byte(len(payload))}, payload...)
		wire = append(wire, 0)
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(u.Tables["Root"].Fields[0]))
		wire = binary.LittleEndian.AppendUint64(wire, ir.MapKeyWireId)
		wire = binary.LittleEndian.AppendUint64(wire, 2)
		dartMapKeyCase(t, &b, m, u.Tables["Root"], tc.name, wire, tc.count, 0, 1, tc.widened)
	}
	b.WriteString("}\n")
	runFormsProbe(t, files, b.String())
}

func TestDartMapRepeatedKeyWidenGlobalVector(t *testing.T) {
	slowtest.Gate(t, "dart")
	schemas, err := filepath.Glob("../../../tables/maps/*.schema")
	if err != nil || len(schemas) == 0 {
		t.Fatalf("map schemas %v", err)
	}
	u := loadUnit(t, schemas...)
	m := tabletext.NewModel(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	wire, err := os.ReadFile("../../../testdata/wire/tables/fuzz-vectors/map_repeated_key_widen.bin")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString(strings.Replace(dartClosurePrelude, "ProbeWire.dart", "MapdemoWire.dart", 1))
	dartMapKeyCase(t, &b, m, u.Tables["Pairs"], "map_repeated_key_widen", wire, 2, 2, 1, 1)
	b.WriteString("}\n")
	runFormsProbe(t, files, b.String())
}
