package elixirtable

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// These defaults are pinned directly from the specified payload/framing rules,
// independently of a writer whose default-elision is the behavior under test.
func TestRequiredConstructElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir required fields, native presence and refusals")
	const src = `package probe
 enum Choice { A, B }
 table Leaf { x int32 }
 union U { Empty
 Data Leaf }
 table All {
 b bool | required
 i int32 | required
 f float32 | required
 s string(8) | required
 w wstring(8) | required
 buf bytes(8) | required
 choice Choice | required
 u U | required
 ptr *Leaf | required
 text *string | required
 blob *bytes | required
 leaf Leaf | required
 list []int32 | required
 slots [2]int32 | required
 tags map[uint8]int32 | required
 keyed [Choice]int32 | required
 }
 table Kid { x int32 | required }
 table Nest { child Kid
 items []Kid
 tags map[uint8]Kid
 ptr *Kid }
 table Guard { on bool
 if on { needed int32 | required } }
 table Range { n int32 | min=5, required }
 table GuardRange { on bool
 if on { n int32 | min=5, required } }
 table Branch { outer bool
 if outer { inner bool
 if inner { yes int32 | required } else { no int32 | required } } }
 `
	u := unitFrom(t, src)
	payloads := []string{"00", "00000000", "00000000", "00", "00", "020600", "00", "00", "00", "00", "00", "0100", "020400", "0a04020000000000000000", "020d00", "020400"}
	var body []byte
	for i, f := range u.Tables["All"].Fields {
		p, e := hex.DecodeString(payloads[i])
		if e != nil {
			t.Fatal(e)
		}
		body = append(body, byte(i+1), byte(ir.TableWireFieldKind(f)))
		body = append(body, p...)
	}
	pack := func(body []byte) string {
		wire := append([]byte{1}, body...)
		wire = append(wire, 0)
		for _, f := range u.Tables["All"].Fields {
			wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(f))
		}
		wire = binary.LittleEndian.AppendUint64(wire, uint64(len(payloads)))
		return hex.EncodeToString(wire)
	}
	var p strings.Builder
	p.WriteString(`defmodule Probe do
 def check(true,_),do: :ok
 def check(false,why),do: raise(why)
 def missing(result,name) do
  case result do
   {:error,:missing_required,r}->check(Map.get(r,:missing_required)==1 and r.reason=="missing_required: "<>name and true,"missing reason "<>inspect(r))
   other->raise("expected missing "<>name<>": "<>inspect(other))
  end
 end
end
alias Probe.Table,as: T
store=%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
`)
	fmt.Fprintf(&p, `wire=Base.decode16!(%q,case: :lower)
{:ok,value,r}=T.load("All",wire)
Probe.check(true and Map.get(r,:missing_required,0)==0,"defaults report")
Probe.check(T.save("All",value)=={:ok,wire},"required default writer: "<>inspect(T.save("All",value)))
{:ok,held,r,held_store}=T.load_retain("All",wire,store,zero)
{:ok,^wire,_,_}=T.save_retain("All",held,held_store,r)
{:ok,text}=T.to_json("All",value)
{:ok,back,_}=T.from_json("All",text)
{:ok,^wire}=T.save("All",back)
Probe.check(T.cook_measure("All",value)>0,"required default cook")
`, pack(body))
	const populated = `{"b":true,"i":7,"f":1.25,"s":"hello","w":"wide","buf":"AQI=","choice":"A","u":{"Data":{"x":4}},"ptr":{"x":3},"text":"text","blob":"Aw==","leaf":{"x":9},"list":[1,-2],"slots":[2,3],"tags":{"1":4},"keyed":{"A":5}}`
	old := unitFrom(t, strings.ReplaceAll(strings.ReplaceAll(src, " | required", ""), "min=5, required", "min=0"))
	model := tabletext.NewModel(old)
	pv := model.New(old.Tables["All"])
	var pr tabletext.Report
	if !model.Read(pv, []byte(populated), &pr) || !pr.Silent() {
		t.Fatalf("populated reference: %+v", pr)
	}
	pw, e := tablewire.Encode(model, pv)
	if e != nil {
		t.Fatal(e)
	}
	fmt.Fprintf(&p, `pop=Base.decode16!(%q,case: :lower)
{:ok,pv,_}=T.from_json("All",%q)
{:ok,^pop}=T.save("All",pv)
{:ok,pv,_}=T.load("All",pop)
{:ok,^pop}=T.save("All",pv)
{:ok,pv,r,rs}=T.load_retain("All",pop,store,zero)
{:ok,^pop,_,_}=T.save_retain("All",pv,rs,r)
`, hex.EncodeToString(pw), populated)
	var obj map[string]json.RawMessage
	if e := json.Unmarshal([]byte(populated), &obj); e != nil {
		t.Fatal(e)
	}
	for _, f := range u.Tables["All"].Fields {
		v := obj[f.Name]
		delete(obj, f.Name)
		raw, e := json.Marshal(obj)
		if e != nil {
			t.Fatal(e)
		}
		fmt.Fprintf(&p, "Probe.missing(T.from_json(\"All\",%q),%q)\n", string(raw), f.Name)
		obj[f.Name] = v
	}

	oldNest := unitFrom(t, strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(src, " | required", ""), "min=5, required", "min=0"), "table Kid { x int32 }", "table Kid { x int32\n keep int32 }"), "table Nest { child Kid", "table Nest { noise int32\n child Kid"))
	nm := tabletext.NewModel(oldNest)
	for _, input := range []string{`{"noise":1,"child":{"keep":1}}`, `{"noise":1,"items":[{"keep":1},{"keep":1}]}`, `{"noise":1,"tags":{"1":{"keep":1}}}`, `{"noise":1,"ptr":{"keep":1}}`} {
		nv := nm.New(oldNest.Tables["Nest"])
		var nr tabletext.Report
		if !nm.Read(nv, []byte(input), &nr) || !nr.Silent() {
			t.Fatalf("old nested fixture: %+v", nr)
		}
		nw, e := tablewire.Encode(nm, nv)
		if e != nil {
			t.Fatal(e)
		}
		fmt.Fprintf(&p, `nw=Base.decode16!(%q,case: :lower)
Probe.missing(T.load("Nest",nw),"x")
{:error,:missing_required,nr}=T.load("Nest",nw)
Probe.check(nr.unknown==%d,"nested diagnostic prefix: "<>inspect(nr))
{:error,:missing_required,rr,_}=T.load_retain("Nest",nw,store,zero)
Probe.missing({:error,:missing_required,rr},"x")
`, hex.EncodeToString(nw), func() int {
			if strings.Contains(input, "ptr") {
				return 1
			}
			return 2
		}())
	}
	at := 0
	for i, f := range u.Tables["All"].Fields {
		n := 2 + len(payloads[i])/2
		cut := append(append([]byte{}, body[:at]...), body[at+n:]...)
		fmt.Fprintf(&p, `bad=Base.decode16!(%q,case: :lower)
Probe.missing(T.load("All",bad),%q)
{:error,:missing_required,rr,rs}=T.load_retain("All",bad,store,zero)
Probe.missing({:error,:missing_required,rr},%q)
Probe.check(rs.records==[],"refused retention cleared")
`, pack(cut), f.Name, f.Name)
		if f.Name == "buf" || f.Name == "list" || f.Name == "slots" || f.Name == "tags" || f.Name == "keyed" {
			bad := append([]byte{}, body...)
			bad[at+3] = 1
			fmt.Fprintf(&p, `bad=Base.decode16!(%q,case: :lower)
Probe.missing(T.load("All",bad),%q)
{:error,:missing_required,rr,_}=T.load_retain("All",bad,store,zero)
Probe.missing({:error,:missing_required,rr},%q)
`, pack(bad), f.Name, f.Name)
		}
		at += n
	}
	p.WriteString(`for input<- ["{}","{\"on\":false}","{\"on\":true,\"needed\":0}"] do
 {:ok,v,_}=T.from_json("Guard",input)
 {:ok,w}=T.save("Guard",v)
 {:ok,_,_}=T.load("Guard",w)
end
Probe.missing(T.from_json("Guard","{\"on\":true}"),"needed")
{:ok,_,wrong}=T.from_json("Guard","{\"on\":true,\"needed\":\"wrong\"}")
Probe.check(wrong.kind_mismatch==1 and Map.get(wrong,:missing_required,0)==0,"JSON key presence")
for input<- ["{}","{\"outer\":false}","{\"outer\":true,\"inner\":false,\"no\":0}","{\"outer\":true,\"inner\":true,\"yes\":0}"] do
 {:ok,bv,_}=T.from_json("Branch",input)
 {:ok,bw}=T.save("Branch",bv)
 {:ok,_,_}=T.load("Branch",bw)
end
Probe.missing(T.from_json("Branch","{\"outer\":true,\"inner\":true}"),"yes")
Probe.missing(T.from_json("Branch","{\"outer\":true,\"inner\":false}"),"no")
Probe.check(T.save("Range",%{})=={:error,:invalid_value},"required invalid storage zero wire")
Probe.check(T.to_json("Range",%{})=={:error,:invalid_value},"required invalid storage zero JSON")
Probe.check(T.cook_measure("Range",%{})==-1,"required invalid storage zero cook")
{:ok,_}=T.save("GuardRange",%{"on"=>false,"n"=>0})
{:ok,_}=T.to_json("GuardRange",%{"on"=>false,"n"=>0})
Probe.check(T.cook_measure("GuardRange",%{"on"=>false,"n"=>0})>0,"inactive required range cook")
Probe.check(T.save("GuardRange",%{"on"=>true,"n"=>0})=={:error,:invalid_value},"active required range wire")
Probe.check(T.cook_measure("GuardRange",%{"on"=>true,"n"=>0})==-1,"active required range cook")
{:ok,_}=T.save("Range",%{"n"=>5})
for input<- ["{\"child\":{}}","{\"items\":[{}]}","{\"items\":[{},{}]}","{\"tags\":{\"1\":{}}}","{\"ptr\":{}}"] do
 Probe.missing(T.from_json("Nest",input),"x")
end
for path<-Path.wildcard("*.ex") do
 raw=File.read!(path)
 formatted=IO.iodata_to_binary(Code.format_string!(raw))<>"\n"
 if raw != formatted do
  IO.inspect(Enum.find(Enum.zip(String.split(raw,"\n"),String.split(formatted,"\n")),fn {a,b}->a != b end),label: path)
 end
 Probe.check(raw==formatted,"generated format: "<>path)
end
{:error,:missing_required,prefix}=T.from_json("Nest","{\"noise\":1,\"items\":[{},{}]}")
Probe.check(prefix.unknown==1 and prefix.missing_required==1,"JSON refusal prefix")
{:ok,_,reuse,_}=T.load_retain("All",wire,store,Map.put(zero,:missing_required,3))
Probe.check(reuse.missing_required==3,"reused clean report")
{:error,:missing_required,reused,_}=T.load_retain("All",bad,store,zero |> Map.put(:missing_required,3) |> Map.put(:reason,"old refusal"))
Probe.check(reused.missing_required==4 and reused.reason=="missing_required: keyed","reused failing report")
IO.puts("REQUIRED_OK")
`)
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), p.String())
	if e != nil || !strings.Contains(out, "REQUIRED_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native required: %v\n%s", e, out)
	}
}

func TestRequiredWidePresenceElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir required presence beyond 512 fields")
	src := "package probe\ntable Wide | forms=\"wire,json\" {\n"
	for i := range 513 {
		src += "v" + strconv.Itoa(i) + " int32"
		if i == 512 {
			src += " | required"
		}
		src += "\n"
	}
	src += "}\n"
	files := generateElixir(t, unitFrom(t, src))
	// The generic Table module is the actual API under test. The separate
	// packet/cook adapters are unrelated to JSON presence and need not compile.
	for path := range files {
		if !strings.HasSuffix(path, "/Table.ex") && path != "Table.ex" && !strings.HasSuffix(path, "/BuildVersion.ex") && path != "BuildVersion.ex" {
			delete(files, path)
		}
	}
	probe := `alias Probe.Table,as: T
{:error,:missing_required,r}=T.from_json("Wide","{}")
if r.reason != "missing_required: v512" or r.missing_required != 1 or r.malformed, do: raise("wide missing report")
{:ok,v,_}=T.from_json("Wide","{\"v512\":0}")
{:ok,w}=T.save("Wide",v)
{:ok,_,_}=T.load("Wide",w)
IO.puts("WIDE_REQUIRED_OK")
`
	out, e := runArmProbe(t, elixirBinary(t), files, probe)
	if e != nil || !strings.Contains(out, "WIDE_REQUIRED_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native wide required: %v\n%s", e, out)
	}
}

func TestRequiredRangeDomainsElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir required range preflight across storage domains")
	u := unitFrom(t, `package probe
 enum Choice { A, B }
 table R {
 ints []int32 | min=5, required
 wide [2]uint64 | min=9007199254740993, required
 keyed [Choice]int32 | min=5, required
 floating float32 | min=0.25, max=1.0, resolution=0.25, required
 floats [..2]float32 | min=0.25, max=1.0, resolution=0.25, required
 plain int32 | required
 mask bits(24) | required
 }
 `)
	probe := `defmodule Probe do
 def check(true,_),do: :ok
 def check(false,why),do: raise(why)
end
alias Probe.Table,as: T
valid=%{"ints"=>[5],"wide"=>[9007199254740993,9007199254740993],"keyed"=>%{"A"=>5,"B"=>5},"floating"=>0.5,"floats"=>[0.5],"mask"=>5,"plain"=>7}
{:ok,wire}=T.save("R",valid)
{:ok,_,_}=T.load("R",wire)
{:ok,text}=T.to_json("R",valid)
{:ok,back,_}=T.from_json("R",text)
{:ok,^wire}=T.save("R",back)
Probe.check(T.cook_measure("R",valid)>0,"valid required ranges cook")
for {key,bad}<- [{"ints",[4]},{"wide",[9007199254740992,9007199254740993]},{"keyed",%{"A"=>5}},{"floating",0.0},{"floats",[0.0]},{"mask",16777216},{"plain",2147483648}] do
 value=Map.put(valid,key,bad)
 Probe.check(T.save("R",value)=={:error,:invalid_value},key<>" required range wire")
 Probe.check(T.to_json("R",value)=={:error,:invalid_value},key<>" required range JSON")
 Probe.check(T.cook_measure("R",value)==-1,key<>" required range cook")
end
IO.puts("REQUIRED_RANGES_OK")
`
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
	if e != nil || !strings.Contains(out, "REQUIRED_RANGES_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native required range domains: %v\n%s", e, out)
	}
}

func TestRequiredFloatReadClampsElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir ranged required float scalar and array reads")
	u := unitFrom(t, `package probe
 table R {
 scalar float32 | min=0.25, max=1.0, resolution=0.25, required
 values [..2]float32 | min=0.25, max=1.0, resolution=0.25, required
 }
 `)
	wire := []byte{1, 1, 10, 0, 0, 0, 0, 2, 14, 6, 10, 1, 0, 0, 0, 0, 0}
	for _, f := range u.Tables["R"].Fields {
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(f))
	}
	wire = binary.LittleEndian.AppendUint64(wire, 2)
	probe := fmt.Sprintf(`alias Probe.Table,as: T
wire=Base.decode16!(%q,case: :lower)
{:ok,v,r}=T.load("R",wire)
if v["scalar"] != 0.25 or v["values"] != [0.25] or r.clamped != 2 or r.malformed, do: raise("wire required float clamps: "<>inspect({v,r}))
{:ok,v,r}=T.from_json("R","{\"scalar\":0,\"values\":[0]}")
if v["scalar"] != 0.25 or v["values"] != [0.25] or r.clamped != 2, do: raise("JSON required float clamps: "<>inspect({v,r}))
IO.puts("REQUIRED_FLOAT_CLAMP_OK")
`, hex.EncodeToString(wire))
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
	if e != nil || !strings.Contains(out, "REQUIRED_FLOAT_CLAMP_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native required float clamps: %v\n%s", e, out)
	}
}

func TestRequiredCookGuardStorageElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir physical guarded cook storage against native Go pins")
	const base = "../../../testdata/conformance/feature-witnesses/required/cook-guard/"
	src, e := os.ReadFile(base + "Probe.schema")
	if e != nil {
		t.Fatal(e)
	}
	pins, e := os.ReadFile(base + "pins.tsv")
	if e != nil {
		t.Fatal(e)
	}
	for _, required := range []bool{false, true} {
		t.Run(fmt.Sprint(required), func(t *testing.T) {
			source := string(src)
			if !required {
				source = strings.ReplaceAll(source, ", required", "")
			}
			u := unitFrom(t, source)
			m := tabletext.NewModel(u)
			var p strings.Builder
			p.WriteString("alias Probe.Table,as: T\n")
			count := 0
			for line := range strings.SplitSeq(string(pins), "\n") {
				if line == "" {
					continue
				}
				cols := strings.Fields(line)
				if len(cols) != 3 {
					t.Fatal(line)
				}
				n, e := strconv.Atoi(cols[0])
				if e != nil {
					t.Fatal(e)
				}
				order, e := strconv.Atoi(cols[1])
				if e != nil {
					t.Fatal(e)
				}
				pin, e := hex.DecodeString(cols[2])
				if e != nil {
					t.Fatal(e)
				}
				v := m.New(u.Tables["Guard"])
				field, ok := v.FieldByKey("n")
				if !ok {
					t.Fatal("n")
				}
				field.Cell.U = uint64(n)
				cooked, e := tablecook.Cook(m, v, tablecook.Options{Big: order == 2})
				if e != nil || !bytes.Equal(cooked, pin) {
					t.Fatalf("independent native Go pin mismatch: n=%d order=%d err=%v\n%x\n%x", n, order, e, cooked, pin)
				}
				exorder := "little"
				if order == 2 {
					exorder = "big"
				}
				fmt.Fprintf(&p, `want=Base.decode16!(%q,case: :lower)
v=%%{"on"=>false,"n"=>%d}
if T.cook_measure("Guard",v)!=byte_size(want), do: raise("guard measure")
{true,^want}=T.cook_from("Guard",v,:binary.copy(<<0>>,byte_size(want)),:%s)
`, cols[2], n, exorder)
				count++
			}
			if count != 4 {
				t.Fatalf("got %d pins", count)
			}
			p.WriteString("IO.puts(\"COOK_GUARD_STORAGE_OK\")\n")
			out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), p.String())
			if e != nil || !strings.Contains(out, "COOK_GUARD_STORAGE_OK") || strings.Contains(out, "warning:") {
				t.Fatalf("native guarded storage: %v\n%s", e, out)
			}
		})
	}
}

func TestRequiredCookInactiveEdgesElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir cook refuses unreached storage behind inactive guards")
	u := unitFrom(t, `package probe
 table Leaf { x int32 }
 table Child { items []int32 }
 table Graph { on bool
 if on { values []int32 | required
 child Child
 ptr *Leaf
 tags map[uint8]int32 } }
 `)
	probe := `alias Probe.Table,as: T
for v<-[%{"on"=>false,"values"=>[1]},%{"on"=>false,"child"=>%{"items"=>[1]}},%{"on"=>false,"ptr"=>%{"x"=>7}},%{"on"=>false,"tags"=>%{"1"=>1}}] do
 if T.cook_measure("Graph",v)!=-1, do: raise("unreached inactive storage accepted: "<>inspect(v))
 if T.cook_from("Graph",v,:binary.copy(<<0>>,1024),:little)!=false, do: raise("unreached inactive write accepted")
end
for v<-[%{"on"=>false,"values"=>[],"child"=>%{"items"=>[]},"tags"=>%{}},%{"on"=>true,"values"=>[1],"ptr"=>%{"x"=>7}}] do
 if T.cook_measure("Graph",v)<=0, do: raise("live or empty storage refused: "<>inspect(v))
end
IO.puts("COOK_INACTIVE_EDGES_OK")
`
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
	if e != nil || !strings.Contains(out, "COOK_INACTIVE_EDGES_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native inactive cook edges: %v\n%s", e, out)
	}
}

func TestRequiredNestedGuardCorpusElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir shared twelve-row nested required guard corpus")
	const base = "../../../testdata/conformance/feature-witnesses/required/nested-guards/"
	source, e := os.ReadFile(base + "Probe.schema")
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(base + "cases.json")
	if e != nil {
		t.Fatal(e)
	}
	var cases []struct {
		Root, Input, Missing string
		Fields               []string
		Value                uint32
	}
	if e = json.Unmarshal(raw, &cases); e != nil {
		t.Fatal(e)
	}
	if len(cases) != 12 {
		t.Fatalf("got %d corpus rows", len(cases))
	}
	u := unitFrom(t, string(source))
	var p strings.Builder
	p.WriteString("alias Probe.Table,as: T\n")
	for _, c := range cases {
		if c.Missing != "" {
			fmt.Fprintf(&p, `{:error,:missing_required,r}=T.from_json(%q,%q)
if r.missing_required != 1 or r.reason != %q,do: raise("nested guard missing: "<>inspect(r))
`, c.Root, c.Input, "missing_required: "+c.Missing)
			continue
		}
		wire := []byte{1}
		var ids []uint64
		for i, name := range c.Fields {
			var field *ir.Field
			for _, f := range u.Tables[c.Root].Fields {
				if f.Name == name {
					field = f
					break
				}
			}
			if field == nil {
				t.Fatal(name)
			}
			wire = append(wire, byte(i+1), byte(ir.TableWireFieldKind(field)))
			if field.Type.Kind == ir.TBool {
				wire = append(wire, 1)
			} else {
				wire = binary.LittleEndian.AppendUint32(wire, c.Value)
			}
			ids = append(ids, ir.TableFieldWireId(field))
		}
		wire = append(wire, 0)
		for _, id := range ids {
			wire = binary.LittleEndian.AppendUint64(wire, id)
		}
		wire = binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
		fmt.Fprintf(&p, `{:ok,v,r}=T.from_json(%q,%q)
if r.missing_required != 0 or r.malformed,do: raise("nested guard report")
want=Base.decode16!(%q,case: :lower)
{:ok,^want}=T.save(%q,v)
{:ok,back,_}=T.load(%q,want)
{:ok,^want}=T.save(%q,back)
`, c.Root, c.Input, hex.EncodeToString(wire), c.Root, c.Root, c.Root)
	}
	p.WriteString(`{:ok,<<1,0,0,0,0,0,0,0,0,0>>}=T.save("P",%{"outer"=>false,"inner"=>true,"child"=>%{"x"=>7}})
IO.puts("REQUIRED_NESTED_GUARDS_OK")
`)
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), p.String())
	if e != nil || !strings.Contains(out, "REQUIRED_NESTED_GUARDS_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native shared guard corpus: %v\n%s", e, out)
	}
}

func TestRequiredArrayRootRangeElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir required range on array-root JSON writer")
	u := unitFrom(t, `package probe
 table R | json="array" { values [..2]int32 | min=5, required }
 `)
	probe := `alias Probe.Table,as: T
if T.to_json("R",%{"values"=>[4]})!={:error,:invalid_value},do: raise("array-root required range JSON")
{:ok,wire}=T.save("R",%{"values"=>[5]})
{:ok,back,_}=T.from_json("R","[5]")
{:ok,^wire}=T.save("R",back)
IO.puts("REQUIRED_ARRAY_ROOT_OK")
`
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
	if e != nil || !strings.Contains(out, "REQUIRED_ARRAY_ROOT_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native required array root: %v\n%s", e, out)
	}
}
