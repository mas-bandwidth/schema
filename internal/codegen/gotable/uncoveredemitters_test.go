package gotable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestGoUncoveredEmitters covers the emitter surface no other test reaches.
// GenerateOmitting is the omit path, and the message and retain forms below
// keep the behaviour their call sites frame under test.
//
// Retention on the message form does not exist: retain.go emits LoadRetain,
// MeasureRetain and SaveRetain only for a variable table, which has no message
// form, and a fixed table (the only class the message form carries, §3.3) gets
// LoadRetainMessages and SaveRetainMessages as refused-by-name constants. The
// retention witness below is therefore the region form's (§6.6), over a field
// this build cannot name.
func TestGoUncoveredEmitters(t *testing.T) {
	t.Parallel()
	t.Run("GenerateOmitting_omits_only_the_named_form_and_still_compiles", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, `package probe
fixed table Root { x int32 }
`)
		full, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		hadCook := false
		for name := range full {
			if strings.HasSuffix(name, "Cook.go") {
				hadCook = true
			}
		}
		if !hadCook {
			t.Fatal("baseline unit emits no Cook surface; cannot prove omission")
		}
		out, err := GenerateOmitting(u, nil, FormCook)
		if err != nil {
			t.Fatal(err)
		}
		for name := range out {
			if strings.HasSuffix(name, "Cook.go") {
				t.Fatalf("FormCook omitted but %s still generated", name)
			}
		}
		if _, ok := out["ProbeTable.go"]; !ok {
			t.Fatal("the core ProbeTable.go is missing when the Cook form is omitted")
		}
		compileOmitted(t, u, `package probe
import "testing"
func TestCoreStillLives(t *testing.T){
	var r Root; r.X=9
	b:=make([]byte,RootMeasure(&r));if RootSave(&r,b)!=int64(len(b)){t.Fatal("save")}
	var got Root;var rep TableReport
	if !RootLoad(&got,b,&rep)||got.X!=9{t.Fatalf("load %+v",rep)}
}`, FormCook)
	})
	t.Run("GenerateOmitting_nothing_is_omitted", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, `package probe
fixed table Root { x int32 }
`)
		full, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		out, err := GenerateOmitting(u, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(full, out) {
			t.Fatalf("omitting no form changed the output: %d files vs %d", len(full), len(out))
		}
		compileOmitted(t, u, `package probe
import "testing"
func TestCoreStillLives(t *testing.T){
	var r Root; r.X=9
	b:=make([]byte,RootMeasure(&r));if RootSave(&r,b)!=int64(len(b)){t.Fatal("save")}
	var got Root;var rep TableReport
	if !RootLoad(&got,b,&rep)||got.X!=9{t.Fatalf("load %+v",rep)}
}`, 0)
	})
	t.Run("message_form_round_trips", func(t *testing.T) {
		t.Parallel()
		// A unit that declares a variable table is REGIONAL, so the region
		// runtime rides the message form; the fixed root carries the batch
		// verbs. Save a batch, then read it back and check the decoded value.
		runGenerated(t, `package probe
table V { a int32 }
fixed table Root { b int32 }
`, `package probe
import "testing"
func TestRegionMessageRoundTrip(t *testing.T){
	var v Root; v.B=7
	values:=[]*Root{&v}
	n:=RootMeasureMessages(values,nil)
	out:=make([]byte,n)
	var wrep TableReport
	if got:=RootSaveMessages(values,out,&wrep);got!=n||wrep!=(TableReport{}){t.Fatalf("save %d/%d %+v",got,n,wrep)}
	vocab:=new(TableVocabulary);vocab.Init(make([]TableMessageEntry,TableMessageEntriesHere))
	ann:=make([]byte,AnnounceMeasure());if Announce(ann)!=int64(len(ann)){t.Fatal("announce")}
	var ar TableReport;if !AnnounceRead(vocab,ann,&ar){t.Fatalf("announce read %+v",ar)}
	var got [1]Root
	count,ok:=RootLoadMessages(got[:],vocab,out,&ar);if !ok||count!=1||got[0].B!=7{t.Fatalf("load %d %v %+v",count,ok,ar)}
}`)
	})
	t.Run("variable_retain_keeps_an_unknown_field_round_trip", func(t *testing.T) {
		t.Parallel()
		// Retention is a region round trip through the VARIABLE form (§6.6). A
		// newer writer carries a field this build cannot name; the retaining
		// load must KEEP it (report.Retained, not retain_lost), and the plain
		// reload of the saved bytes must still meet it as unknown. A reader that
		// discarded every unknown record would answer Unknown=0 here.
		older := `package probe
table Root { b int32 }
`
		newer := `package probe
table Root { b int32
 future string(16) }
`
		model := tabletext.NewModel(unitFrom(t, newer))
		value := model.New(model.Lookup("Root"))
		var encoded tabletext.Report
		if !model.Read(value, []byte(`{"b":5,"future":"kept"}`), &encoded) {
			t.Fatalf("newer write: %+v", encoded)
		}
		wire, err := tablewire.Encode(model, value)
		if err != nil {
			t.Fatal(err)
		}
		runGenerated(t, older, fmt.Sprintf(`package probe
import("testing";"unsafe")
func TestRetainUnknownRoundTrip(t *testing.T){
 wire:=%#v
 n:=RootLoadMeasure(wire);if n<0{t.Fatal("measure")};raw:=make([]byte,n+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(n)]
 retain:=TableRetain{Bytes:make([]byte,1<<12),Ids:make([]TableRetainID,64)};var report TableReport
 kept:=RootLoadRetain(region,wire,&retain,&report);if kept==nil||report.Malformed||report.Unknown!=1||report.Retained!=1||report.RetainLost!=0||retain.Count!=1{t.Fatalf("retain load %%+v count %%d",report,retain.Count)}
 out:=make([]byte,RootMeasureRetain(kept,&retain));report=TableReport{};if got:=RootSaveRetain(kept,&retain,out,&report);got!=int64(len(out))||report.RetainLost!=0{t.Fatalf("retain save %%d %%+v",got,report)}
 size:=RootLoadMeasure(out);if size<0{t.Fatal("reload measure")};raw2:=make([]byte,size+63);off2:=(-uintptr(unsafe.Pointer(&raw2[0])))&63;report=TableReport{};again:=RootLoad(raw2[off2:off2+uintptr(size)],out,&report);if again==nil||again.B!=5||report.Unknown!=1{t.Fatalf("reload %%+v",report)}
}`, wire))
	})
}

// compileOmitted compiles the tree GenerateOmitting produces for a unit (merged
// with the packet emitter's files) and runs testSource against it, proving the
// omitted form's files are truly gone and what remains still compiles and runs.
func compileOmitted(t *testing.T, u *ir.Unit, testSource string, omit Form) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	files := map[string][]byte{}
	packet, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packet)
	tables, err := GenerateOmitting(u, nil, omit)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["values_test.go"] = []byte(testSource)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated runtime: %v\n%s", err, out)
	}
}
