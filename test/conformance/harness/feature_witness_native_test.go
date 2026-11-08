package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

var updateFeatureWitnesses = flag.Bool("update-feature-witnesses", false, "rewrite populated feature pins from the independently generated Go runtime")

// Pins come from compiled generated Go, independently of the neutral codec
// used by implementationAudit. Normal runs compare and never rewrite them.
func TestPopulatedFeatureWitnessNativePins(t *testing.T) {
	slowtest.Gate(t, "generated Go (independent populated implementation witnesses)")
	t.Chdir("../../..")
	c := compiler.New()
	u, err := c.Load([]string{"testdata/conformance/feature-witnesses/full/Features.schema"})
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(u, "go", compiler.Options{"runtime": "standalone"})
	if err != nil {
		t.Fatal(err)
	}
	runtime := slowtest.SerializeGo(t, ".")
	files["go.mod"] = []byte(fmt.Sprintf("module featurewitnesses\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	dir := t.TempDir()
	source := "package featurewitnesses\nimport(\"os\";\"testing\")\n"
	cases := []struct{ root, name string }{{"Root", "feature_full"}, {"ArrayRoot", "feature_array_root"}}
	for _, tc := range cases {
		input, err := os.ReadFile("testdata/conformance/feature-witnesses/full/" + tc.root + ".input.json")
		if err != nil {
			t.Fatal(err)
		}
		files[tc.root+".input.json"] = input
		source += strings.ReplaceAll(featureWitnessGoProbe, "ROOT", tc.root)
	}
	files["witness_test.go"] = []byte(source)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout=0", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native feature pins: %v\n%s", err, out)
	}
	for _, tc := range cases {
		for _, ext := range []string{".bin", ".json"} {
			got, err := os.ReadFile(filepath.Join(dir, tc.root+ext))
			if err != nil {
				t.Fatal(err)
			}
			dest := "testdata/conformance/feature-witnesses/full/" + tc.root + ext
			if ext == ".json" {
				got = append(bytes.TrimRight(got, "\n"), '\n')
				dest = filepath.Join(defaultJSONDir, tc.name+".json")
			}
			if *updateFeatureWitnesses {
				if err := os.WriteFile(dest, got, 0644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(dest)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s native Go differs from committed pin; investigate before regenerating", dest)
			}
		}
	}
}

const featureWitnessGoProbe = `
func TestROOT(t *testing.T){
 input,err:=os.ReadFile("ROOT.input.json");if err!=nil{t.Fatal(err)}
 var b ROOTBuilder;if !b.Init(){t.Fatal("builder init")};defer b.Shutdown()
 var r TableReport
 if !ROOTFromJSONTolerant(&b,input,&r)||r!=(TableReport{}){t.Fatalf("populated JSON: %+v",r)}
 v:=b.GetRoot();n:=ROOTMeasure(v,&b.Arena);if n<=10{t.Fatal("fixture is empty or refused")}
 wire:=make([]byte,n);if ROOTSave(v,wire,&b.Arena)!=n{t.Fatal("wire save")}
 if !b.Lock(){t.Fatal("lock")}
 cv:=b.AsConst();tn:=ROOTToJSONMeasure(cv);if tn<=0{t.Fatal("JSON measure")}
 text:=make([]byte,tn);if ROOTToJSON(cv,text)!=tn{t.Fatal("JSON write")}
 if err=os.WriteFile("ROOT.bin",wire,0600);err!=nil{t.Fatal(err)}
 if err=os.WriteFile("ROOT.json",text,0600);err!=nil{t.Fatal(err)}
}
`

// This consumes the committed native pin directly, so the neutral integrity
// implementation cannot bless its own polynomial, extent or trailer order.
func TestPopulatedFeatureNeutralFileIntegrity(t *testing.T) {
	t.Chdir("../../..")
	u, err := compiler.New().Load([]string{"testdata/conformance/feature-witnesses/full/Features.schema"})
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	root := u.Tables["Root"]
	wire, err := os.ReadFile("testdata/conformance/feature-witnesses/full/Root.bin")
	if err != nil {
		t.Fatal(err)
	}
	for _, region := range []bool{false, true} {
		v := m.New(root)
		var r tabletext.Report
		var ok bool
		retain := &tablewire.Retain{Capacity: 4096}
		if region {
			ok, err = tablewire.DecodeRegionRetain(m, v, wire, retain, &r)
		} else {
			ok, err = tablewire.DecodeRetain(m, v, wire, retain, &r)
		}
		if err != nil || !ok || !r.Silent() {
			t.Fatalf("decode region=%v: %+v %v", region, r, err)
		}
		got, err := tablewire.EncodeRetain(m, v, retain, &r)
		if err != nil || !bytes.Equal(got, wire) {
			t.Fatalf("retained encode region=%v: %x %v", region, got, err)
		}
	}
	if n, whole, refused := tablewire.FileRegionMeasure(u, root, wire); n <= 0 || !whole || refused {
		t.Fatalf("sealed framing measure: %d %v %v", n, whole, refused)
	}
	for i := range wire {
		bad := append([]byte(nil), wire...)
		bad[i] ^= 1
		v := m.New(root)
		var r tabletext.Report
		ok, err := tablewire.Decode(m, v, bad, &r)
		if ok {
			t.Fatalf("changed byte %d accepted", i)
		}
		if i == 0 {
			if !tablewire.Refused(err) || !r.Refused {
				t.Fatalf("form precedence: %+v %v", r, err)
			}
		} else if tablewire.Refused(err) || tablewire.RefusalReason(err) != tablewire.ReasonIntegrityMismatch || !r.Refused || r.Unknown != 0 {
			t.Fatalf("byte %d decoded before integrity: %+v %v", i, r, err)
		}
		if _, whole, _ := tablewire.FileRegionMeasure(u, root, bad); whole {
			t.Fatalf("changed byte %d measured", i)
		}
		if v.Fields[0].Cell.I != 0 {
			t.Fatalf("damaged value published at %d", i)
		}
	}
}
