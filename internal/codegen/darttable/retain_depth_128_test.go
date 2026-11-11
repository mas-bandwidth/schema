package darttable

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

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
	probe.WriteString("}\n")
	runFormsProbe(t, files, probe.String())
}
