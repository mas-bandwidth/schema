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

// nestedUnionBody is a root body whose one unknown field (reference 1) is a
// union: `unions` unions, each holding the next as its arm (kind 15), around
// the innermost union's own content inner ([0] for None, or an arm reference,
// kind and framed payload).
func nestedUnionBody(unions int, inner []byte) []byte {
	payload := inner
	for range unions {
		next := binary.AppendUvarint([]byte{1, byte(ir.TableKindUnion)}, uint64(len(payload)))
		payload = append(next, payload...)
	}
	body := append([]byte{1, byte(ir.TableKindUnion)}, payload...)
	return append(body, 0)
}

func TestDartRetainKeepsDepth128(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadDirUnit(t, filepath.Join(root, "tables", "pointers"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	var probe strings.Builder
	probe.WriteString(strings.Replace(dartClosurePrelude, "ProbeWire.dart", "GraphdemoWire.dart", 1))
	for _, name := range []string{"depth_tables_128", "depth_arrays_128", "depth_union_tables_64", "depth_keyed_128"} {
		wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "framing", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&probe, `{final data=bytes(%q);final plain=w.TableWireReport();w.sceneWireLoad(data,plain);check(!plain.refused,'plain refused %s: ${plain.refusal}');final store=w.TableRetain(65536,List.filled(1024,0));final r=w.TableWireReport();final v=w.sceneWireLoadRetain(data,store,r);
check(!r.refused&&r.retainLost==0,'retained %s: refused=${r.refused} reason=${r.refusal} lost=${r.retainLost}');
final saved=w.sceneWireSaveRetain(v,store,w.TableWireReport());
check(hex(saved)==%q,'retained %s: saved bytes changed');}
`, hex.EncodeToString(wire), name, name, hex.EncodeToString(wire), name)
	}
	for _, name := range []string{"depth_tables_129", "depth_arrays_129", "depth_union_tables_65", "depth_keyed_129"} {
		wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "framing", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&probe, `{final r=w.TableWireReport();w.sceneWireLoadRetain(bytes(%q),w.TableRetain(65536,List.filled(1024,0)),r);check(r.refused,'retaining load accepted %s');}
`, hex.EncodeToString(wire), name)
	}
	// §4 COUNTS A UNION AS A LEVEL ONLY WHEN ITS ARM IS STRUCTURAL: the
	// files carry the pinned vectors' id table (one unknown id) and count, and
	// each verdict is the reference engine's on the same bytes.
	pinned, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "framing", "depth_tables_128.bin"))
	if err != nil {
		t.Fatal(err)
	}
	tail := pinned[len(pinned)-16:]
	m := tabletext.NewModel(u)
	scene := m.Lookup("Scene")
	if scene == nil {
		t.Fatal("no Scene in the graphdemo unit")
	}
	scalarArm := []byte{1, byte(ir.TableKindI32), 4, 7, 0, 0, 0}
	tableArm := []byte{1, byte(ir.TableKindTable), 1, 0}
	for _, c := range []struct {
		name string
		body []byte
		want bool
	}{
		{"128 unions around None", nestedUnionBody(ir.TableBinaryMaxDepth, []byte{0}), true},
		{"129 unions around None", nestedUnionBody(ir.TableBinaryMaxDepth+1, []byte{0}), false},
		{"a scalar arm under 128 unions", nestedUnionBody(ir.TableBinaryMaxDepth, scalarArm), true},
		{"a table arm under 127 unions", nestedUnionBody(ir.TableBinaryMaxDepth-1, tableArm), false},
		{"a table arm under 126 unions", nestedUnionBody(ir.TableBinaryMaxDepth-2, tableArm), true},
	} {
		file := append(append([]byte{ir.TableWireForm}, c.body...), tail...)
		var plain tabletext.Report
		if ok, err := tablewire.Decode(m, m.New(scene), file, &plain); ok != c.want {
			t.Fatalf("%s: the reference load answered ok=%v err=%v, want ok=%v", c.name, ok, err, c.want)
		}
		inst := m.New(scene)
		store := &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
		var kept tabletext.Report
		if ok, err := tablewire.DecodeRetain(m, inst, file, store, &kept); ok != c.want {
			t.Fatalf("%s: the reference retaining load answered ok=%v err=%v, want ok=%v", c.name, ok, err, c.want)
		}
		if !c.want {
			fmt.Fprintf(&probe, `{final data=bytes(%q);final plain=w.TableWireReport();w.sceneWireLoad(data,plain);check(plain.refused,'plain load accepted %s');
final r=w.TableWireReport();w.sceneWireLoadRetain(data,w.TableRetain(65536,List.filled(1024,0)),r);check(r.refused,'retaining load accepted %s');}
`, hex.EncodeToString(file), c.name, c.name)
			continue
		}
		var saved tabletext.Report
		out, err := tablewire.EncodeRetain(m, inst, store, &saved)
		if err != nil || kept.Retained != 1 || kept.RetainLost != 0 || string(out) != string(file) {
			t.Fatalf("%s: the reference retaining round trip: err=%v %+v\n got %x\nwant %x", c.name, err, kept, out, file)
		}
		fmt.Fprintf(&probe, `{final data=bytes(%q);final plain=w.TableWireReport();w.sceneWireLoad(data,plain);check(!plain.refused,'plain refused %s: ${plain.refusal}');final store=w.TableRetain(65536,List.filled(1024,0));final r=w.TableWireReport();final v=w.sceneWireLoadRetain(data,store,r);
check(!r.refused&&r.retainLost==0,'retained %s: refused=${r.refused} reason=${r.refusal} lost=${r.retainLost}');
final saved=w.sceneWireSaveRetain(v,store,w.TableWireReport());
check(hex(saved)==%q,'retained %s: saved bytes changed');}
`, hex.EncodeToString(file), c.name, c.name, hex.EncodeToString(file), c.name)
	}
	probe.WriteString("}\n")
	runFormsProbe(t, files, probe.String())
}
