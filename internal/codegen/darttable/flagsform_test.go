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

func TestDartFlagsThroughGeneratedWire(t *testing.T) {
	names := make([]string, 64)
	for i := range names {
		names[i] = fmt.Sprintf("Bit%02d", i)
	}
	src := "package probe\nflags Mask {" + strings.Join(names, ",") + "}\nenum Key { A,B }\ntable Root { value Mask = {Bit01}\n opt ?Mask\n values []Mask\n positions [2]Mask\n slots [Key]Mask\n entries map[uint16]Mask\n after int32\n body *string }\n"
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if len(files["ProbeWire.dart"]) == 0 {
		t.Fatal("flags root must carry form1")
	}
	m := tabletext.NewModel(u)
	inputs := []string{`{}`, `{"value":["Bit63","Bit00"],"opt":[],"values":[[],["Bit63"]],"positions":[["Bit01"],["Bit63"]],"slots":{"A":["Bit63"]},"entries":{"1":["Bit63"]},"after":77}`, `{"value":["Bit63","Missing","Bit00","Bit00",9],"after":77}`, `{"value":["Bit63"],"value":7,"after":77}`, `{"value":["Bit63"],"value":[],"opt":["Bit63"],"opt":null,"after":77}`, `{"values":[["Bit63"],7,[],["Missing"]],"positions":[["Bit63"]],"slots":{"B":["Bit01"],"B":[]},"after":77}`}
	var checks strings.Builder
	for _, input := range inputs {
		v := m.New(u.Tables["Root"])
		r := tabletext.Report{}
		if !m.Read(v, []byte(input), &r) || r.Malformed {
			t.Fatalf("Go flags input %s: %+v", input, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		text, err := m.Write(v)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&checks, "{final r=TableTextReport();final v=rootFromJson(%q,r);check(!r.malformed&&r.unknown==%d&&r.kindMismatch==%d&&r.duplicate==%d,'Go text report');final wire=rootWireSave(v);check(hex(wire)==%q,'Go flags wire');check(rootToJson(v)==%q,'Go flags JSON');final wr=TableWireReport();final back=rootWireLoad(wire,wr);check(!wr.malformed&&wr.kindMismatch==0,'flags wire read');check(hex(rootWireSave(back))==hex(wire),'flags resave');}\n", input, r.Unknown, r.KindMismatch, r.Duplicate, hex.EncodeToString(wire), string(text)+"\n")
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
