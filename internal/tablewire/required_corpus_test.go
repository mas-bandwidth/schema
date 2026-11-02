package tablewire_test

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const requiredCorpus = "../../testdata/conformance/feature-witnesses/required"

func TestRequiredCommittedCorpus(t *testing.T) {
	u, err := compiler.New().Load([]string{filepath.Join(requiredCorpus, "Probe.schema")})
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	for _, tc := range []struct{ name, reason string }{{"zero", ""}, {"populated", ""}, {"missing", "missing_required: b, c"}, {"missing-b", "missing_required: b"}} {
		t.Run(tc.name, func(t *testing.T) {
			text, err := os.ReadFile(filepath.Join(requiredCorpus, tc.name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			wire, err := os.ReadFile(filepath.Join(requiredCorpus, tc.name+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			v := m.New(m.Lookup("Root"))
			var rep tabletext.Report
			if ok := m.Read(v, text, &rep); ok != (tc.reason == "") || rep.Reason != tc.reason || rep.Malformed {
				t.Fatalf("JSON: %v %+v", ok, rep)
			}
			if tc.reason == "" {
				got, err := tablewire.Encode(m, v)
				if err != nil || !bytes.Equal(got, wire) {
					t.Fatalf("write %x want %x: %v", got, wire, err)
				}
			}
			v = m.New(m.Lookup("Root"))
			rep = tabletext.Report{}
			if ok, err := tablewire.Decode(m, v, wire, &rep); ok != (tc.reason == "") || rep.Reason != tc.reason || rep.Malformed || (err != nil) != (tc.reason != "") {
				t.Fatalf("wire: %v %v %+v", ok, err, rep)
			}
		})
	}
}

// The committed bytes are hand-framed; this independent generated runtime
// reads and writes them without calling the neutral oracle.
func TestRequiredCorpusGeneratedGo(t *testing.T) {
	slowtest.Gate(t, "Go (it checks independent required-field corpus bytes)")
	u, err := compiler.New().Load([]string{filepath.Join(requiredCorpus, "Probe.schema")})
	if err != nil {
		t.Fatal(err)
	}
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	runtime := slowtest.SerializeGo(t, "../..")
	corpus, err := filepath.Abs(requiredCorpus)
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["oracle_test.go"] = []byte(fmt.Sprintf(`package probe
import("bytes";"os";"path/filepath";"testing")
func TestRequiredPins(t *testing.T) {
 for _,tc:=range []struct{name,reason string;a,b,c int32}{
  {"zero","",0,0,0},{"populated","",3,7,9},{"missing","missing_required: b, c",0,0,0},{"missing-b","missing_required: b",0,0,9},
 } {
  pin,err:=os.ReadFile(filepath.Join(%q,tc.name+".bin"));if err!=nil {t.Fatal(err)}
  if tc.reason=="" {
   value:=Root{A:tc.a,B:tc.b,C:tc.c};wire:=make([]byte,RootMeasure(&value));if RootSave(&value,wire)!=int64(len(wire)) || !bytes.Equal(wire,pin){t.Fatalf("independent write %%s: %%x want %%x",tc.name,wire,pin)}
  }
  var b RootBuilder;if !b.Init(){t.Fatal("builder init")};var r TableReport
  ok:=RootLoadBuilderTolerant(&b,pin,&r);b.Shutdown()
  if ok!=(tc.reason=="") || r.Reason!=tc.reason || r.Malformed || (r.MissingRequired!=0)!=(tc.reason!=""){t.Fatalf("independent read %%s: %%v %%+v",tc.name,ok,r)}
 }
}
`, corpus))
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-timeout=0", "-count=1", "-run", "^TestRequiredPins$", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Go: %v\n%s", err, out)
	}
}
