package darttable

import (
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDartPlainTypeBodiesThroughGeneratedWire(t *testing.T) {
	src := `package probe
 flags Mask { Low,High }
 enum Key { A,B }
 type Value { n int32 = 7
 mask Mask }
 union Choice { value Value
 ping }
 table Root { value Value
 opt ?Value
 values []Value
 positions [2]Value
 slots [Key]Value
 entries map[uint16]Value
 choice Choice
 after int32
 body *string }
 `
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if len(files["ProbeWire.dart"]) == 0 {
		t.Fatal("plain type root must carry form1")
	}
	m := tabletext.NewModel(u)
	inputs := []string{`{"value":{"n":9},"value":7,"after":77}`, `{"value":{"n":9},"value":{},"after":77}`, `{"opt":{"n":9},"opt":7,"after":77}`, `{"opt":{"n":9},"opt":null,"after":77}`, `{}`, `{"value":{"n":9,"mask":["High"]},"opt":{},"values":[{}, {"n":10}],"positions":[{"n":11},{}],"slots":{"B":{"n":12}},"entries":{"1":{"n":13}},"choice":{"value":{}},"after":77}`, `{"choice":{"ping":null},"after":77}`, `{"value":7,"opt":null,"values":[{}],"after":77}`, `{"slots":{"B":{"n":9},"B":{}},"entries":{"1":{"n":9},"1":{}},"after":77}`}
	var checks strings.Builder
	for _, input := range inputs {
		v := m.New(u.Tables["Root"])
		r := tabletext.Report{}
		if !m.Read(v, []byte(input), &r) || r.Malformed {
			t.Fatalf("Go type input %s: %+v", input, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		text, err := m.Write(v)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&checks, "{final r=TableTextReport();final v=rootFromJson(%q,r);check(!r.malformed&&r.unknown==%d&&r.kindMismatch==%d&&r.duplicate==%d,'Go text report');final wire=rootWireSave(v);check(hex(wire)==%q,'Go type wire');check(rootToJson(v)==%q,'Go type JSON');final wr=TableWireReport();final back=rootWireLoad(wire,wr);check(!wr.malformed&&wr.kindMismatch==0,'type wire read');check(hex(rootWireSave(back))==hex(wire),'type resave');}\n", input, r.Unknown, r.KindMismatch, r.Duplicate, hex.EncodeToString(wire), string(text)+"\n")
	}
	t.Run("runtime-production-oracle", func(t *testing.T) {
		slowtest.Gate(t, "dart")
		dir := t.TempDir()
		for name, data := range files {
			if !strings.Contains(name, "/") {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		probe := "import 'ProbeWire.dart';\nString hex(List<int> b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();\nvoid check(bool b,String why){if(!b)throw StateError(why);}\nvoid main(){\n" + checks.String() + "}\n"
		if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"analyze", "--fatal-infos", "probe.dart"}, {"--enable-asserts", "probe.dart"}} {
			cmd := exec.Command(dartBinary(t), args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %v\n%s", args, err, out)
			}
		}
	})
}
