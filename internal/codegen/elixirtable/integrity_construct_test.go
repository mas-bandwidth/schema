package elixirtable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestIntegrityConstructElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir CRC-32C file fixed message and retaining surfaces")
	const src = `package probe
 fixed table Reading | integrity="crc32c" { x int32
 y int16
 label string(8) }
 fixed table PlainReading { x int32
 y int16
 label string(8) }
 table Leaf | integrity="crc32c" { x int32 }
 table Root | integrity="crc32c" { n int32
 child *Leaf }
 table PlainRoot { n int32
 child *Leaf }
 `
	u := unitFrom(t, src)
	plain := unitFrom(t, strings.ReplaceAll(src, ` | integrity="crc32c"`, ""))
	m := tabletext.NewModel(plain)
	seal := func(w []byte) string {
		return hex.EncodeToString(binary.LittleEndian.AppendUint32(w, crc32.Checksum(w, crc32.MakeTable(crc32.Castagnoli))))
	}
	var p strings.Builder
	p.WriteString(`defmodule ProbeCheck do
 def ok(true,_),do: :ok
 def ok(false,why),do: raise(why)
 def flip(data,i) do
  <<a::binary-size(^i),b,tail::binary>>=data
  <<a::binary,Bitwise.bxor(b,1),tail::binary>>
 end
end
alias ProbeCheck,as: P
alias Probe.Table,as: T
alias Probe.ProbeFixed,as: F
alias Probe.TableMessage,as: M
store=%{records: [],used: 0,cap: 4096,ids: [],id_cap: 256}
zero=%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
`)
	for _, input := range []string{`{}`, `{"n":7,"child":{"x":9}}`} {
		v := m.New(plain.Tables["Root"])
		var r tabletext.Report
		if !m.Read(v, []byte(input), &r) {
			t.Fatal(r)
		}
		w, e := tablewire.Encode(m, v)
		if e != nil {
			t.Fatal(e)
		}
		fmt.Fprintf(&p, `wire=Base.decode16!(%q,case: :lower)
plain=Base.decode16!(%q,case: :lower)
{:ok,v,r}=T.load("Root",wire)
P.ok(true and r.reason=="","clean integrity file")
{:ok,^wire}=T.save("Root",v)
{:ok,^plain}=T.save("PlainRoot",v)
P.ok(T.load_measure("Root",wire)>0,"integrity load measure")
{:ok,rv,rr,ret}=T.load_retain("Root",wire,store,zero)
{:ok,^wire,_,_}=T.save_retain("Root",rv,ret,rr)
P.ok(T.measure_retain("Root",rv,ret)==byte_size(wire),"retain measure includes CRC")
for i<-1..(byte_size(wire)-1) do
 bad=P.flip(wire,i)
 {:error,:integrity_mismatch,r}=T.load("Root",bad)
 P.ok(r.malformed and r.reason=="integrity_mismatch" and r.unknown==0 and r.clamped==0,"integrity before fields")
 P.ok(T.load_measure("Root",bad)==-1,"bad integrity measure")
 {:error,:integrity_mismatch,r,ret}=T.load_retain("Root",bad,store,zero)
 P.ok(r.malformed and ret.records==[] and ret.used==0,"retain CRC refusal")
end
{:error,_,_}=T.load("Root",plain)
{:error,_,_}=T.load("PlainRoot",wire)
`, seal(w), hex.EncodeToString(w))
	}
	var values []*tabletext.Instance
	for i, label := range []string{"ok", "", "eight ch"} {
		v := m.New(plain.Tables["Reading"])
		var r tabletext.Report
		if !m.Read(v, []byte(fmt.Sprintf(`{"x":%d,"y":%d,"label":%q}`, 1000+i, -7*i, label)), &r) {
			t.Fatal(r)
		}
		values = append(values, v)
	}
	message, e := tablewire.EncodeMessages(m, values)
	if e != nil {
		t.Fatal(e)
	}
	fmt.Fprintf(&p, `fixed=Base.decode16!(%q,case: :lower)
plain_fixed=Base.decode16!(%q,case: :lower)
values=Enum.with_index(["ok","","eight ch"],fn label,i->%%Probe.Reading{x: 1000+i,y: -7*i,label: label} end)
P.ok(F.reading_fixed_save(values)==fixed,"independent Go fixed CRC pin")
P.ok(F.reading_fixed_measure(3)==byte_size(fixed),"fixed CRC measure")
{:ok,^values,r}=F.reading_fixed_load(fixed)
P.ok(true,"fixed load")
for i<-1..(byte_size(fixed)-1) do
 {:error,:integrity_mismatch,r}=F.reading_fixed_load(P.flip(fixed,i))
 P.ok(r.malformed and r.reason=="integrity_mismatch" and r.clamped==0,"fixed CRC before output")
end
{:error,_,_}=F.reading_fixed_load(plain_fixed)
{:error,_,_}=F.plain_reading_fixed_load(fixed)
{:ok,vocab}=M.announce_read(M.announce(),M.new())
{:error,:unknown_root}=M.for_root(vocab,"unknown")
{:error,:unknown_root}=M.for_root(vocab,"Root")
{:ok,checked}=M.for_root(vocab,"Reading")
{:ok,plain_vocab}=M.for_root(vocab,"PlainReading")
message=Base.decode16!(%q,case: :lower)
plain_message=Base.decode16!(%q,case: :lower)
{:ok,bodies}=M.reading_load_messages(vocab,message)
{:error,:no_root}=M.load_messages(vocab,message)
{:error,:no_root}=M.save_messages(vocab,bodies)
{:error,:no_root}=M.measure_messages(vocab,bodies)
{:ok,^plain_message}=M.raw_save_messages(vocab,bodies)
{:ok,^bodies}=M.raw_load_messages(vocab,plain_message)
{:ok,raw_size}=M.raw_measure_messages(vocab,bodies)
P.ok(raw_size==byte_size(plain_message),"explicit raw measure")
{:ok,^message}=M.reading_save_messages(vocab,bodies)
{:ok,^message}=M.save_messages(checked,bodies)
{:ok,^plain_message}=M.save_messages(plain_vocab,bodies)
{:ok,n}=M.reading_measure_messages(vocab,bodies)
P.ok(n==byte_size(message),"message CRC measure")
for i<-1..(byte_size(message)-1) do
 {:error,:integrity_mismatch}=M.reading_load_messages(vocab,P.flip(message,i),0)
end
{:error,:newer_form}=M.reading_load_messages(vocab,<<1>>)
{:error,:integrity_mismatch}=M.reading_load_messages(vocab,<<2>>)
{:error,:newer_form,r}=T.load("Root",<<2>>)
P.ok(true,"file form precedence")
{:error,:previous_form,r}=F.reading_fixed_load(<<1>>)
P.ok(true,"fixed form precedence")
{:error,:integrity_mismatch,_}=T.load("Root",<<1>>)
{:error,:integrity_mismatch,_}=F.reading_fixed_load(<<3>>)
P.ok(Probe.FixedRuntime.integrity_seal("123456789")==<<"123456789",0xE3069283::little-32>>,"CRC check value")
bad_format=Enum.filter(Path.wildcard("*.ex"),fn path->
 raw=File.read!(path)
 formatted=IO.iodata_to_binary(Code.format_string!(raw))<>"\n"
 if raw != formatted, do: IO.inspect(Enum.find(Enum.zip(String.split(raw,"\n"),String.split(formatted,"\n")),fn {a,b}->a != b end),label: path)
 raw != formatted
end)
P.ok(bad_format==[],"raw formatting: "<>inspect(bad_format))
IO.puts("INTEGRITY_OK")
`, elixirIntegrityFixedGolden, elixirIntegrityPlainFixedGolden, seal(message), hex.EncodeToString(message))
	out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), p.String())
	if e != nil || !strings.Contains(out, "INTEGRITY_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("native integrity: %v\n%s", e, out)
	}
}

// Existing independently compiled generated-Go pins from gotable/integrityfixed_test.go.
const elixirIntegrityFixedGolden = "03000000000000006938893b0968270148000000040000007b8c6cd3fc19b9b80d1200000003000000071702864cf563af040400000000000000541502864cf463af0302000000000000003d62cb8fecfcf7390c0c000000000000006938893b09682701e80300000000020000006f6b0000000000006938893b09682701e9030000f9ff0000000000000000000000006938893b09682701ea030000f2ff0800000065696768742063682854cdb3"
const elixirIntegrityPlainFixedGolden = "030000000000000064f3fa8daeaf844448000000040000000deeaf527dc1c0970d1200000003000000071702864cf563af040400000000000000541502864cf463af0302000000000000003d62cb8fecfcf7390c0c0000000000000064f3fa8daeaf8444e80300000000020000006f6b00000000000064f3fa8daeaf8444e9030000f9ff00000000000000000000000064f3fa8daeaf8444ea030000f2ff080000006569676874206368"

func TestIntegritySingleRootElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir integrity-only closure warning-clean generation")
	for _, decl := range []string{"table", "fixed table", "wire-only"} {
		t.Run(decl, func(t *testing.T) {
			source := "package probe\n" + decl + ` R | integrity="crc32c" { x int32 }`
			if decl == "wire-only" {
				source = `package probe
 table R | integrity="crc32c", forms="wire" { x int32 }`
			}
			u := unitFrom(t, source)
			probe := `alias Probe.Table,as: T
{:ok,wire}=T.save("R",%{"x"=>7})
{:ok,%{"x"=>7},_}=T.load("R",wire)
IO.puts("SINGLE_INTEGRITY_OK")
`
			out, e := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
			if e != nil || !strings.Contains(out, "SINGLE_INTEGRITY_OK") || strings.Contains(out, "warning:") {
				t.Fatalf("native single-root integrity: %v\n%s", e, out)
			}
		})
	}
}
