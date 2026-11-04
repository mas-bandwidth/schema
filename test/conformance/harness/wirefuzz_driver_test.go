package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Compile the actual pipe driver and message binding against tiny codecs. This
// isolates protocol behavior from generated code while keeping the production
// control flow: a missing LoadMeasure must not turn false into true, and no
// failed read may invoke a writer or publish a default payload.
func TestGoWireFuzzDriverReadOutcomes(t *testing.T) {
	repo := conformanceRoot(t)
	dir := t.TempDir()
	source, err := os.ReadFile(filepath.Join(repo, "test/conformance/go/wire_fuzz.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wire_fuzz.go"), source, 0600); err != nil {
		t.Fatal(err)
	}
	messages, err := os.ReadFile(filepath.Join(repo, "test/conformance/go/messages.go"))
	if err != nil {
		t.Fatal(err)
	}
	a, b := strings.Index(string(messages), "type messageCodec struct"), strings.Index(string(messages), "func findMessageCodec")
	if a < 0 || b <= a {
		t.Fatal("message binding source not found")
	}
	stub := goWireFuzzFixture + string(messages[a:b])
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(stub), 0600); err != nil {
		t.Fatal(err)
	}
	driver := filepath.Join(dir, "driver")
	cmd := exec.Command("go", "build", "-o", driver, filepath.Join(dir, "wire_fuzz.go"), filepath.Join(dir, "fixture.go"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build driver: %v\n%s", err, out)
	}
	for _, builder := range []bool{false, true} {
		roots := []*wireRoot{{unit: "probe", root: "plain"}, {unit: "probe", root: "measured"}, {unit: "probe", root: "message", message: true}, {unit: "probe", root: "retain", variable: true, retain: true}}
		command := driver
		if builder {
			command += " builder"
			roots = []*wireRoot{{unit: "probe", root: "builder", variable: true, builder: true}}
		}
		leg, err := startWireLeg(command, roots)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(leg.kill)
		for i, root := range roots {
			if !leg.known[i] {
				t.Fatalf("codec unavailable: %s", root.root)
			}
			cases := []struct {
				wire, want []byte
				accepted   bool
			}{
				{[]byte{0}, nil, false}, {[]byte{2}, nil, false}, {[]byte{3}, nil, false}, {[]byte{7}, []byte{7}, true},
			}
			if root.message {
				cases = []struct {
					wire, want []byte
					accepted   bool
				}{{[]byte{0}, nil, false}, {[]byte{2, 7, 41}, []byte{7, 41}, true}}
			}
			for _, tc := range cases {
				if err := leg.send(i, tc.wire); err != nil {
					t.Fatal(err)
				}
				reply, err := leg.receiveWithDeadline(realClock{})
				if err != nil {
					t.Fatalf("%s: %v\n%s", root.root, err, leg.stderr.String())
				}
				if reply.accepted() != tc.accepted || !bytes.Equal(reply.saved, tc.want) {
					t.Fatalf("%s wire %x: accepted=%t saved=%x, want %t %x", root.root, tc.wire, reply.accepted(), reply.saved, tc.accepted, tc.want)
				}
				if !tc.accepted && !reply.saveFail {
					t.Fatalf("%s refusal advertised a saved value", root.root)
				}
			}
		}
		if err := leg.close(); err != nil {
			t.Fatal(err)
		}
	}
}

const goWireFuzzFixture = `package main
import "os"
type report struct { unknown,kindMismatch,widened,clamped,duplicate int32; malformed,refused bool }
type codec struct { fresh func()any; load func(any,[]byte,*report)bool; loadMeasure func([]byte)int64; measure func(any)int64; save func(any,[]byte)int64 }
type builderCodec struct { run func([]byte)([]byte,report,bool) }
type retentionCodec struct { run func([]byte)([]byte,report,int32,int32,bool,int64) }
func read(w []byte)(report,bool) { r:=report{}; switch w[0] { case 0:return r,false; case 2:r.refused=true; case 3:r.malformed=true; default:r.unknown=9;r.kindMismatch=5;r.clamped=4;r.duplicate=3 };return r,true }
func findCodec(_,root string)*codec {
 if root!="plain" && root!="measured" { return nil }
 c:=&codec{fresh:func()any{return new(byte)},load:func(v any,w []byte,r *report)bool{*v.(*byte)=w[0];p,ok:=read(w);*r=p;return ok},measure:func(v any)int64{if *v.(*byte)<4 {panic("writer called after refusal")};return 1},save:func(v any,b []byte)int64{b[0]=*v.(*byte);return 1}}
 if root=="measured" { c.loadMeasure=func([]byte)int64{return 9} };return c
}
func findBuilderCodec(_,root string)*builderCodec { if root!="builder"{return nil};return &builderCodec{func(w []byte)([]byte,report,bool){r,ok:=read(w);return []byte{w[0]},r,ok}} }
func findRetentionCodec(_,root string)*retentionCodec { if root!="retain"{return nil};return &retentionCodec{func(w []byte)([]byte,report,int32,int32,bool,int64){r,ok:=read(w);return []byte{w[0]},r,17,2,ok,999}} }
func findMessageCodec(_,root string)*messageCodec {
 if root!="message"{return nil}
 c:=messageRow("probe","message",func(v *byte){*v=0},func()*byte{return new(byte)},func()int64{return 0},func([]byte)int64{return 0},func(*byte,[]byte,*report)bool{return true},func(v []byte,_ *byte,w []byte,r *report)(int64,bool){if w[0]==0{return 0,false};n:=int(w[0]);copy(v,w[1:]);return int64(n),true},func(v []*byte,_ *report)int64{if *v[0]==0{panic("message writer called after refusal")};return int64(len(v))},func(v []*byte,b []byte,_ *report)int64{for i,p:=range v{b[i]=*p};return int64(len(v))},func(r *report)report{return *r})
 return &c
}
func main(){ if err:=wireFuzz(len(os.Args)>1);err!=nil{panic(err)} }
`
