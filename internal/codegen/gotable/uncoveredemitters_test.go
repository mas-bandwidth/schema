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
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestGoUncoveredEmitters covers the emitter surface no other test reaches.
// GenerateOmitting is the omit path; the comment-only stubs emitRegionMessage
// and emitRetainMessageSurface are removed (statement-less, so no call can
// cover them), and the message and retain forms below keep the behaviour their
// call sites frame under test.
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
	t.Run("variable_retained_message_round_trips", func(t *testing.T) {
		t.Parallel()
		// Retention is a region round trip through the VARIABLE form (§6.6):
		// load with retention and save the retained value back to the same bytes.
		runGenerated(t, `package probe
table Root { b int32 }
`, `package probe
import("testing";"bytes";"unsafe")
func TestRetainRoundTrip(t *testing.T){
	var v Root; v.B=5
	wire:=make([]byte,RootMeasure(&v));if RootSave(&v,wire)!=int64(len(wire)){t.Fatal("save")}
	n:=RootLoadMeasure(wire);if n<0{t.Fatal("measure")};raw:=make([]byte,n+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(n)]
	retain:=TableRetain{Bytes:make([]byte,1<<12),Ids:make([]TableRetainId,64)};var report TableReport
	kept:=RootLoadRetain(region,wire,&retain,&report);if kept==nil||report.Malformed||report.RetainLost!=0{t.Fatalf("retain load %+v",report)}
	out:=make([]byte,len(wire)+64);if got:=RootSaveRetain(kept,&retain,out,&report);got<0||!bytes.Equal(out[:got],wire){t.Fatalf("retain round trip %d",got)}
}`)
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
	runtime, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
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
