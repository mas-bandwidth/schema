package compiler

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Open is deliberately a header match, not a hostile-input validator (§7).
// This test exercises the supported trust boundary: CookCheck with the same
// unit and root, then generated consumers of those unchanged, owned bytes.
// Attribution is required for the check. A concurrent writer or mutation
// after checking invalidates that proof; copying alone is not validation.
func TestCheckedCookGoConsumers(t *testing.T) {
	slowtest.Gate(t, "the generated Go hostile cook consumer")
	const schema = `package probe
table Node { value int32
 next *Node
 other *Node
 name string(12)
 samples []uint16 }
table Child { tag uint8
 wides []uint128 }
table Root { head *Node
 alias *Node
 name string(12)
 values [..3]int32
 children []Child
 real float64 }
`
	c := New()
	u := unitFromSource(t, schema)
	m := tabletext.NewModel(u)
	root := m.Lookup("Root")
	seed := m.New(root)
	var report tabletext.Report
	if !m.Read(seed, []byte(`{"head":{"&node":1,"value":7,"next":{"value":9,"samples":[3,4]},"name":"node","samples":[1,2]},"alias":{"&node":1},"name":"root","values":[1,2],"children":[{"tag":3,"wides":[1,2]},{"tag":5,"wides":[3]}],"real":1.25}`), &report) {
		t.Fatal(report)
	}
	wire, err := tablewire.Encode(m, seed)
	if err != nil {
		t.Fatal(err)
	}
	valid, _, _, err := c.Cook(u, "Root", wire, CookOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := tablecook.ReadHeader(valid, ir.BuildVersion(u), ir.CookNodeAlign(u, root))
	if err != nil {
		t.Fatal(err)
	}
	directory, err := h.Directory(valid)
	if err != nil || len(directory) != 3 {
		t.Fatalf("directory: %v %+v", err, directory)
	}
	ord := binary.LittleEndian
	slot := func(record, field string, piece int) int {
		fl := ir.RecordLayout(u, m.Lookup(record)).FieldByName(field)
		return int(ir.FieldPieces(u, fl.Field, fl.Offset)[piece].Offset)
	}
	at := func(record, field string, piece int) int {
		return int(h.DataOffset()) + slot(record, field, piece)
	}
	head, alias := at("Root", "head", 0), at("Root", "alias", 0)
	node := int(h.DataOffset() + directory[1].Offset)
	next := node + slot("Node", "next", 0)
	children := at("Root", "children", 0)
	child := children + int(int64(ord.Uint64(valid[children:])))
	wides := child + slot("Child", "wides", 0)
	dir := int(h.AttribOffset())
	out := t.TempDir()
	accepted, refused := 0, 0
	check := func(name string, file []byte, mustRefuse bool) {
		t.Helper()
		original := bytes.Clone(file)
		_, err := c.CookCheck(u, "Root", file)
		if !bytes.Equal(original, file) {
			t.Fatalf("%s: checker mutated input", name)
		}
		if err != nil {
			refused++
			return
		}
		if mustRefuse {
			t.Fatalf("%s: strict checker accepted a directed forgery", name)
		}
		path := filepath.Join(out, fmt.Sprintf("%04d.cook", accepted))
		if err := os.WriteFile(path, file, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+".name", []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
		accepted++
	}
	edit64 := func(at int, value uint64) func([]byte) { return func(f []byte) { ord.PutUint64(f[at:], value) } }
	edit32 := func(at int, value uint32) func([]byte) { return func(f []byte) { ord.PutUint32(f[at:], value) } }
	cases := []struct {
		name string
		edit func([]byte)
	}{
		{"pointer-forward", edit64(head, uint64(h.DataLength)+64)},
		{"pointer-backward", edit64(head, 1<<63)},
		{"pointer-overflow", edit64(head, math.MaxInt64)},
		{"pointer-misaligned", edit64(head, ord.Uint64(valid[head:])+1)},
		{"pointer-type", edit64(alias, uint64(int(h.DataOffset())-alias))},
		{"self-cycle", edit64(next, uint64(-int64(slot("Node", "next", 0))))},
		{"count-negative", edit32(at("Root", "values", 1), math.MaxUint32)},
		{"count-over-bound", edit32(at("Root", "values", 1), 4)},
		{"text-over-bound", edit32(at("Root", "name", 1), 13)},
		{"list-count-overflow", edit32(children+8, math.MaxInt32)},
		{"list-count-negative", edit32(children+8, math.MaxUint32)},
		{"list-null-nonempty", edit64(children, 0)},
		{"list-delta-overflow", edit64(children, math.MaxInt64)},
		{"nested-wide-misaligned", edit64(wides, ord.Uint64(valid[wides:])+8)},
		{"nested-wide-count-overflow", edit32(wides+8, math.MaxInt32)},
		{"header-alignment", edit64(40, 3)},
		{"header-under-alignment", edit64(40, 8)},
		{"data-length-overflow", edit64(24, math.MaxUint64)},
		{"directory-overlap", edit64(dir+32, ord.Uint64(valid[dir+16:]))},
		{"directory-offset-forgery", edit64(dir+16, math.MaxUint64)},
		{"directory-type-forgery", edit64(dir+24, 0xdeadbeef)},
		{"unreachable-bad-count", func(f []byte) {
			ord.PutUint64(f[head:], 0)
			ord.PutUint64(f[alias:], 0)
			ord.PutUint32(f[node+slot("Node", "samples", 0)+8:], math.MaxInt32)
		}},
		{"unreachable-cycle", func(f []byte) {
			ord.PutUint64(f[head:], 0)
			ord.PutUint64(f[alias:], 0)
			ord.PutUint64(f[next:], uint64(-int64(slot("Node", "next", 0))))
		}},
	}
	mustAccept := func(name string, file []byte) {
		t.Helper()
		if _, err := c.CookCheck(u, "Root", file); err != nil {
			t.Fatalf("%s: legal cook refused: %v", name, err)
		}
		check(name, file, false)
	}
	mustAccept("valid", valid)
	orphan := bytes.Clone(valid)
	ord.PutUint64(orphan[head:], 0)
	ord.PutUint64(orphan[alias:], 0)
	mustAccept("legal-unreachable-nodes", orphan)
	for _, tc := range cases {
		f := bytes.Clone(valid)
		tc.edit(f)
		check(tc.name, f, true)
	}
	for i := range valid {
		check(fmt.Sprintf("truncate-%d", i), valid[:i], true)
	}
	noAttrib, _, err := CookSplitAttribution(valid)
	if err != nil {
		t.Fatal(err)
	}
	check("missing-attribution", noAttrib, true)
	if _, err := c.CookCheck(u, "Node", valid); err == nil {
		t.Fatal("wrong root accepted")
	}
	// Non-finite scalars must fail the strict check. Invalid text encodings
	// may instead safely refuse a later writer; count that outcome separately.
	for _, bits := range []uint64{0, 1, math.Float64bits(2.5), math.Float64bits(math.NaN()), math.Float64bits(math.Inf(1))} {
		f := bytes.Clone(valid)
		ord.PutUint64(f[at("Root", "real", 0):], bits)
		check(fmt.Sprintf("scalar-%x", bits), f, math.IsNaN(math.Float64frombits(bits)) || math.IsInf(math.Float64frombits(bits), 0))
	}
	rng := rand.New(rand.NewSource(20261102))
	for i := range 400 {
		f := bytes.Clone(valid)
		switch i % 3 {
		case 0:
			f[rng.Intn(len(f))] ^= byte(1 + rng.Intn(255))
		case 1:
			ord.PutUint32(f[rng.Intn(len(f)-3):], []uint32{0, 1, math.MaxInt32, math.MaxUint32}[rng.Intn(4)])
		case 2:
			ord.PutUint64(f[rng.Intn(len(f)-7):], []uint64{0, 1, math.MaxInt64, 1 << 63, math.MaxUint64}[rng.Intn(5)])
		}
		check(fmt.Sprintf("random-%d", i), f, false)
	}
	// Deep chains and shared diamonds bound graph work by stored nodes/edges,
	// not by recursion depth or the exponentially many possible root paths.
	setNode := func(inst *tabletext.Instance, name string, target *tabletext.Instance) {
		for i := range inst.Fields {
			if inst.Fields[i].Def.Name == name {
				inst.Fields[i].Cell.Node = target
				return
			}
		}
		t.Fatalf("missing pointer %s", name)
	}
	for _, shape := range []struct {
		name    string
		count   int
		diamond bool
	}{{"chain-2048", 2048, false}, {"diamond-48", 48, true}} {
		graph := m.New(root)
		var first *tabletext.Instance
		for i := 0; i < shape.count; i++ {
			node := m.New(m.Lookup("Node"))
			setNode(node, "next", first)
			if shape.diamond {
				setNode(node, "other", first)
			}
			first = node
		}
		setNode(graph, "head", first)
		setNode(graph, "alias", first)
		cooked, err := tablecook.Cook(m, graph, tablecook.Options{})
		if err != nil {
			t.Fatal(err)
		}
		mustAccept(shape.name, cooked)
		header, err := tablecook.ReadHeader(cooked, ir.BuildVersion(u), ir.CookNodeAlign(u, root))
		if err != nil {
			t.Fatal(err)
		}
		entries, err := header.Directory(cooked)
		if err != nil {
			t.Fatal(err)
		}
		lastNext := int(header.DataOffset()+entries[len(entries)-1].Offset) + slot("Node", "next", 0)
		target := int(header.DataOffset() + entries[1].Offset)
		ord.PutUint64(cooked[lastNext:], uint64(int64(target-lastNext)))
		check(shape.name+"-backedge", cooked, true)
		ord.PutUint64(cooked[head:], 0)
		ord.PutUint64(cooked[alias:], 0)
		check(shape.name+"-unreachable-backedge", cooked, true)
	}
	if accepted < 9 || refused < len(cases)+len(valid) {
		t.Fatalf("battery did not exercise both outcomes: accepted=%d refused=%d", accepted, refused)
	}
	t.Logf("strict checker: accepted=%d refused=%d; seed=20261102 random=400", accepted, refused)
	files, err := c.Generate(u, "go", Options{"runtime": "standalone"})
	if err != nil {
		t.Fatal(err)
	}
	runtime := slowtest.SerializeGo(t, "..")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["consumer_test.go"] = []byte(strings.ReplaceAll(checkedCookGoConsumer, "FIXTURE_DIRECTORY", fmt.Sprintf("%q", out)))
	dirNative := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dirNative, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, flags := range [][]string{nil, {"-gcflags=all=-N -l -d=checkptr=2"}} {
		args := append([]string{"test", "-count=1", "-v", "-timeout=60s"}, flags...)
		args = append(args, ".")
		cmd := exec.Command("go", args...)
		cmd.Dir = dirNative
		result, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("accepted cook consumer %v: %v\n%s", flags, err, result)
		}
		for line := range strings.SplitSeq(string(result), "\n") {
			if strings.Contains(line, "safe operation") || strings.Contains(line, "safe refusal") {
				t.Logf("generated Go %v: %s", flags, strings.TrimSpace(line))
			}
		}
	}
	recooks, err := filepath.Glob(filepath.Join(out, "*.recook"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recooks) == 0 {
		t.Fatal("no native recooks")
	}
	for _, path := range recooks {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.CookCheck(u, "Root", data); err != nil {
			t.Fatalf("native recook %s: %v", filepath.Base(path), err)
		}
	}
}

const checkedCookGoConsumer = `package probe
import("bytes";"fmt";"os";"path/filepath";"runtime";"testing";"unsafe")
func TestAcceptedCookConsumers(t *testing.T) {
 files,err:=filepath.Glob(filepath.Join(FIXTURE_DIRECTORY,"*.cook"));if err!=nil{t.Fatal(err)}
 semanticRefusals:=0
 bounded:=func(t *testing.T,n int64)int64{t.Helper();if n>1<<20{t.Fatalf("fixture expanded beyond test budget to %d bytes",n)};return n}
 for _,path:=range files {
  original,err:=os.ReadFile(path);if err!=nil{t.Fatal(err)}
  name,_:=os.ReadFile(path+".name")
  t.Run(fmt.Sprintf("%s-%s",filepath.Base(path),name),func(t *testing.T){
   // Own an aligned copy, keep it alive through every view, and never mutate
   // it after the parent checked its exact contents with the same unit/root.
   allocation:=make([]byte,len(original)+63)
   off:=(-uintptr(unsafe.Pointer(&allocation[0])))&63
   data:=allocation[off:off+uintptr(len(original))]
   copy(data,original)
   var cook RootCook
   if err:=cook.Open(unsafe.Pointer(&data[0]),int64(len(data)));err!=nil{t.Fatal(err)}
   root:=cook.Root()
   _=root.Name[:root.NameLength]
   _=root.Values[:root.ValuesCount]
   for i:=int32(0);i<root.Children.Count;i++ {
    child:=root.Children.At(i);_=child.Tag
    for j:=int32(0);j<child.Wides.Count;j++{_=*child.Wides.At(j)}
   }
   seen:=map[*Node]bool{}
   pending:=[]*Node{NodeAt(&root.Head),NodeAt(&root.Alias)}
   for len(pending)>0 {
    node:=pending[len(pending)-1];pending=pending[:len(pending)-1]
    if node==nil||seen[node]{continue};seen[node]=true
    if len(seen)>len(data){t.Fatal("unbounded node walk")}
    _=node.Value;_=node.Name[:node.NameLength]
    for j:=int32(0);j<node.Samples.Count;j++{_=*node.Samples.At(j)}
    pending=append(pending,NodeAt(&node.Next),NodeAt(&node.Other))
   }
   jsonSize,jsonErr:=RootToJSONMeasureReason(root);bounded(t,jsonSize)
   if string(name)=="valid"&&jsonSize<0{t.Fatal("valid fixture JSON refused")}
   if jsonSize<0{semanticRefusals++;if jsonErr==nil{t.Fatal("JSON refusal without reason")};if RootToJSON(root,make([]byte,1<<20))>=0{t.Fatal("JSON measure/write refusal differs")};t.Logf("safe refusal JSON %s: %v",name,jsonErr)}else{
    dst:=make([]byte,jsonSize);if RootToJSON(root,dst)!=jsonSize{t.Fatal("JSON write")}
   }
   wireSize,wireErr:=RootMeasureReason(root);bounded(t,wireSize)
   if string(name)=="valid"&&wireSize<0{t.Fatal("valid fixture wire refused")}
   if wireSize<0{semanticRefusals++;if wireErr==nil{t.Fatal("wire refusal without reason")};if RootSave(root,make([]byte,1<<20))>=0{t.Fatal("wire measure/write refusal differs")};t.Logf("safe refusal wire %s: %v",name,wireErr)}else{
    wire:=make([]byte,wireSize);if RootSave(root,wire)!=wireSize{t.Fatal("wire write")}
    size:=bounded(t,RootLoadMeasure(wire));if size<0{t.Fatal("saved wire measure")}
    buf:=make([]byte,size+63);start:=(-uintptr(unsafe.Pointer(&buf[0])))&63
    var report TableReport
    if RootLoad(buf[start:start+uintptr(size)],wire,&report)==nil||report!=(TableReport{}){t.Fatalf("saved wire load: %+v",report)}
   }
   size,cookErr:=RootCookMeasureReason(root);bounded(t,size)
   if string(name)=="valid"&&size<0{t.Fatal("valid fixture cook refused")}
   if size<0{semanticRefusals++;if cookErr==nil{t.Fatal("cook refusal without reason")};if RootCookFrom(root,make([]byte,1<<20),TableByteOrderLittle){t.Fatal("cook measure/write refusal differs")};t.Logf("safe refusal cook: %v",cookErr)}else{
    dst:=make([]byte,size);if !RootCookFrom(root,dst,TableByteOrderLittle){t.Fatal("recook")}
    if err:=os.WriteFile(path+".recook",dst,0600);err!=nil{t.Fatal(err)}
   }
   if !bytes.Equal(original,data){t.Fatal("consumer mutated validated input")}
   runtime.KeepAlive(allocation)
  })
 }
 t.Logf("accepted native consumers=%d safe operation refusals=%d",len(files),semanticRefusals)
}
`

// Two independent controls prove that both halves of the gate are live:
// weakened strict validation fails before Open; a broken native accessor
// fails in the child process after acceptance. No recovery masks a panic.
func TestCheckedCookGoConsumersNegativeControls(t *testing.T) {
	slowtest.Gate(t, "the generated Go checked-cook negative controls")
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, path, before, after, want string }{
		{"strict-companion", "internal/tablecook/check.go", "if n < 0 || n > bound {", "if false {", "count-negative: strict checker accepted a directed forgery"},
		{"native-accessor", "internal/codegen/gotable/lists.go", "return (*T)(c.At(index))", "_ = c; return nil", "panic: runtime error: invalid memory address"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(root, tc.path)
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(source), tc.before) != 1 {
				t.Fatal("negative control seam moved")
			}
			dir := t.TempDir()
			replacement := filepath.Join(dir, "replacement.txt")
			if err := os.WriteFile(replacement, []byte(strings.Replace(string(source), tc.before, tc.after, 1)), 0600); err != nil {
				t.Fatal(err)
			}
			overlay, err := json.Marshal(map[string]any{"Replace": map[string]string{path: replacement}})
			if err != nil {
				t.Fatal(err)
			}
			overlayPath := filepath.Join(dir, "overlay.json")
			if err := os.WriteFile(overlayPath, overlay, 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("go", "test", "-count=1", "-overlay="+overlayPath, "./compiler", "-run", "^TestCheckedCookGoConsumers$", "-timeout=90s")
			cmd.Dir = root
			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatal("negative control stayed green")
			}
			if !strings.Contains(string(output), tc.want) {
				t.Fatalf("wrong failure: %v\n%s", err, output)
			}
			t.Logf("negative control failed at intended gate: %s", tc.want)
		})
	}
}
