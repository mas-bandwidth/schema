package elixirtable

import (
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormsConstructElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir declared form exports and parent closure helpers")
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprint(mixed), func(t *testing.T) {
			source := `package probe
 fixed table Hidden | forms="wire" { n int32 }
 table WireOnly | forms="wire" { n int32
 tags map[uint8]int32 }
 `
			if mixed {
				source += `
 fixed table Visible { n int32 }
 table JsonOnly | forms="wire,json" { n int32 }
 table CookOnly | forms="wire,cook" { n int32
 child Hidden }
 table All { n int32
 child WireOnly }
 `
			}
			u := unitFrom(t, source)
			m := tabletext.NewModel(u)
			files := generateElixir(t, u)
			if !mixed {
				for name := range files {
					if strings.HasSuffix(name, "Cook.ex") || strings.HasSuffix(name, "Block.ex") || name == CookRuntimeModule+".ex" || name == BlockRuntimeModule+".ex" {
						t.Errorf("wire-only emitted %s", name)
					}
				}
			}
			var probe strings.Builder
			probe.WriteString(`defmodule Probe do
 def check(true,_),do: :ok
 def check(false,why),do: raise(why)
end
alias Probe.Table,as: T
`)
			fmt.Fprintf(&probe, `Probe.check(function_exported?(T,:to_json,2)==%t,"unit JSON export")
Probe.check(function_exported?(T,:from_json,2)==%t,"unit JSON reader export")
Probe.check(function_exported?(T,:cook_from,4)==%t,"unit cook export")
`, mixed, mixed, mixed)
			for _, name := range []string{"Hidden", "WireOnly", "Visible", "JsonOnly", "CookOnly", "All"} {
				st := u.Tables[name]
				if st == nil {
					continue
				}
				value := m.New(st)
				var r tabletext.Report
				if !m.Read(value, []byte(`{"n":7,"child":{"n":11}}`), &r) {
					t.Fatal(r)
				}
				wire, err := tablewire.Encode(m, value)
				if err != nil {
					t.Fatal(err)
				}
				text, err := m.Write(value)
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&probe, `name=%q
wire=Base.decode16!(%q,case: :lower)
{:ok,value,r}=T.load(name,wire)
Probe.check(not r.malformed,"declared forms wire")
{:ok,^wire}=T.save(name,value)
`, name, hex.EncodeToString(wire))
				if mixed {
					if st.CarriesForm("json") {
						fmt.Fprintf(&probe, `{:ok,text}=T.to_json(name,value)
Probe.check(text==%q,"enabled parent JSON includes restricted child")
{:ok,again,r}=T.from_json(name,text)
Probe.check(not r.malformed,"enabled JSON read")
{:ok,^wire}=T.save(name,again)
`, string(text)+"\n")
					} else {
						probe.WriteString(`Probe.check(T.to_json(name,value)=={:error,:form_not_generated},"disabled JSON write")
Probe.check(T.from_json(name,"{}")=={:error,:form_not_generated},"disabled JSON read")
`)
					}
					if st.CarriesForm("cook") {
						probe.WriteString(`{size,:ok}=T.cook_measure_reason(name,value)
{true,cook}=T.cook_from(name,value,:binary.copy(<<0>>,size),:little)
Probe.check(byte_size(cook)==size,"enabled cook writer")
`)
					} else {
						probe.WriteString(`Probe.check(T.cook_measure_reason(name,value)=={-1,:form_not_generated},"disabled cook measure")
Probe.check(T.cook_from(name,value,<<0>>, :little)==false,"disabled cook write")
`)
					}
				}
				fmt.Fprintf(&probe, `Probe.check(function_exported?(Probe.ProbeCook,%s,1)==%t,"per-root Cook handle")
Probe.check(function_exported?(Probe.ProbeBlock,%s,1)==%t,"per-root Block handle")
`, ":"+"cook_open_"+ir.RustSnake(name), mixed && st.CarriesForm("cook"), ":"+"block_open_"+ir.RustSnake(name), name == "Visible")
			}
			probe.WriteString(`for path<-Path.wildcard("*.ex") do
 raw=File.read!(path)
 formatted=IO.iodata_to_binary(Code.format_string!(raw))<>"\n"
 if raw != formatted do
  IO.inspect(Enum.find(Enum.zip(String.split(raw,"\n"),String.split(formatted,"\n")),fn {a,b}->a != b end),label: path)
 end
 Probe.check(raw==formatted,"raw form module format: "<>path)
end
IO.puts("FORMS_OK")
`)
			out, err := runArmProbe(t, elixirBinary(t), files, probe.String())
			if err != nil || !strings.Contains(out, "FORMS_OK") || strings.Contains(out, "warning:") {
				t.Fatalf("native forms: %v\n%s", err, out)
			}
		})
	}
}

func TestDropFormFunctionIncludesExceptionClauses(t *testing.T) {
	for _, clause := range []string{"rescue", "catch", "after", "else"} {
		source := "  defp remove() do\n    work()\n  " + clause + "\n    recover()\n  end\n\n  defp keep(), do: 7\n"
		got := dropTopLevel(source, func(head string) bool { return strings.HasPrefix(head, "  defp remove(") })
		if got != "  defp keep(), do: 7\n" {
			t.Fatalf("%s left partial function: %q", clause, got)
		}
	}
}

func TestFormsCookClosureAcrossFilesElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir declared cook closure across source files")
	dir := t.TempDir()
	var paths []string
	for _, f := range []struct{ name, body string }{
		{"Empty.schema", `package probe
 table Empty | forms="wire" {}`},
		{"Hidden.schema", `package probe
 table Leaf | forms="wire" { n int32 }
 table Hidden | forms="wire" { leaf Leaf }`},
		{"Parent.schema", `package probe
 table Parent | forms="wire,cook" { hidden Hidden
 empty Empty }`},
	} {
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, []byte(f.body), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	u := loadUnit(t, paths...)
	probe := `
 defmodule Probe do
  def check(true,_),do: :ok
  def check(false,why),do: raise(why)
 end
 alias Probe.Table,as: T
 value=%{"hidden"=>%{"leaf"=>%{"n"=>19}},"empty"=>%{}}
 {size,:ok}=T.cook_measure_reason("Parent",value)
 {true,cook}=T.cook_from("Parent",value,:binary.copy(<<0>>,size),:little)
 {:ok,handle}=Probe.ParentCook.cook_open_parent(cook)
 hidden=Probe.ParentCook.parent_node_hidden(handle.region,0)
 leaf=Probe.HiddenCook.hidden_node_leaf(handle.region,hidden)
 Probe.check(Probe.HiddenCook.leaf_node_n(handle.region,leaf)==19,"enabled parent uses hidden child accessors")
 Probe.check(not function_exported?(Probe.HiddenCook,:cook_open_hidden,1),"hidden root handle omitted")
 Probe.check(not function_exported?(Probe.HiddenCook,:cook_open_leaf,1),"hidden leaf handle omitted")
 Probe.check(not function_exported?(Probe.EmptyCook,:cook_open_empty,1),"empty child handle omitted")
 Probe.check(function_exported?(Probe.EmptyCook,:cook_info_empty,0),"empty child descriptor retained")
 Probe.check(not function_exported?(T,:to_json,2),"no JSON root in split unit")
 IO.puts("SPLIT_FORMS_OK")
 `
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
	if err != nil || !strings.Contains(out, "SPLIT_FORMS_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("split forms: %v\n%s", err, out)
	}
}

func TestFormsAllSubsetsElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir all eight declared form subsets")
	for mask := range 8 {
		t.Run(fmt.Sprint(mask), func(t *testing.T) {
			t.Parallel()
			forms := []string{"wire"}
			for i, name := range []string{"json", "cook", "block"} {
				if mask&(1<<i) != 0 {
					forms = append(forms, name)
				}
			}
			u := unitFrom(t, fmt.Sprintf("package probe\nfixed table Root | forms=%q { n int32 }", strings.Join(forms, ",")))
			m := tabletext.NewModel(u)
			v := m.New(u.Tables["Root"])
			v.Fields[0].Cell.I = 7
			v.Fields[0].Cell.U = 7
			wire, err := tablewire.Encode(m, v)
			if err != nil {
				t.Fatal(err)
			}
			var probe strings.Builder
			probe.WriteString(`defmodule Probe do
 def check(true,_),do: :ok
 def check(false,why),do: raise(why)
end
alias Probe.Table,as: T
`)
			fmt.Fprintf(&probe, `Probe.check(function_exported?(T,:to_json,2)==%t,"JSON subset")
Probe.check(function_exported?(T,:cook_from,4)==%t,"cook subset")
Probe.check(function_exported?(Probe.ProbeCook,:cook_open_root,1)==%t,"cook reader subset")
Probe.check(function_exported?(Probe.ProbeBlock,:block_open_root,1)==%t,"block subset")
wire=Base.decode16!(%q,case: :lower)
{:ok,value,_}=T.load("Root",wire)
Probe.check(value["n"]==7,"wire value")
{:ok,^wire}=T.save("Root",value)
`, mask&1 != 0, mask&2 != 0, mask&2 != 0, mask&4 != 0, hex.EncodeToString(wire))
			if mask&1 != 0 {
				probe.WriteString(`{:ok,text}=T.to_json("Root",value)
{:ok,^value,_}=T.from_json("Root",text)
`)
			}
			if mask&2 != 0 {
				cook, err := tablecook.Cook(m, v, tablecook.Options{})
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&probe, `want=Base.decode16!(%q,case: :lower)
{size,:ok}=T.cook_measure_reason("Root",value)
{true,^want}=T.cook_from("Root",value,:binary.copy(<<0>>,size),:little)
{:ok,handle}=Probe.ProbeCook.cook_open_root(want)
Probe.check(Probe.ProbeCook.root_node_n(handle.region,0)==7,"cooked value")
`, hex.EncodeToString(cook))
			}
			if mask&4 != 0 {
				probe.WriteString(`{:ok,block}=Probe.ProbeBlock.block_begin_root(%{})
{:ok,_}=Probe.ProbeBlock.block_open_root(block)
`)
			}
			probe.WriteString(`for path<-Path.wildcard("*.ex") do
 raw=File.read!(path)
 Probe.check(raw==IO.iodata_to_binary(Code.format_string!(raw))<>"\n","subset raw format: "<>path)
end
IO.puts("SUBSET_OK")
`)
			out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
			if err != nil || !strings.Contains(out, "SUBSET_OK") || strings.Contains(out, "warning:") {
				t.Fatalf("subset %d: %v\n%s", mask, err, out)
			}
		})
	}
}
