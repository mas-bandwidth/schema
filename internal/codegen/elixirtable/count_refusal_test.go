package elixirtable

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const countRefusalSchema = `package countprobe
union Choice { value Child }
table Child { items []int32 }
table Root {
 marker int32 = 7
 items []int32
 entries map[int32]int32
 children map[int32]Child
 child Child
 pick Choice
 ptr *Child
 bounded [..2]int32
 boundedChildren [..2]Child
 boundedPointers [..2]*Child
}
`

// Tiny announced counts exercise the storage cap without allocating that many
// elements. The Go engine independently supplies both verdicts and all event
// counters; retaining loads start with nonzero history and an occupied store.
func TestWireCountRefusalElixir(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	files, probe := countRefusalProbe(t)
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err != nil {
		t.Fatalf("count refusal probe: %v\n%s", err, out)
	}
}

// Removing only the cap check must turn the same tiny payloads into body-short
// damage, and the native oracle must reject that loss of the named refusal.
func TestWireCountRefusalElixirNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	files, probe := countRefusalProbe(t)
	elixirPatch(t, files, "throw({:count_over_extent_cap, report})", "report")
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err == nil {
		t.Fatalf("disabled count cap stayed green\n%s", out)
	}
	for _, label := range []string{"list_2147483648 named refusal", "map_2147483648 named refusal"} {
		if !strings.Contains(out, label) {
			t.Errorf("negative control missed %s:\n%s", label, out)
		}
	}
}

func countRefusalProbe(t *testing.T) (map[string][]byte, string) {
	t.Helper()
	schema := filepath.Join(t.TempDir(), "Count.schema")
	if err := os.WriteFile(schema, []byte(countRefusalSchema), 0600); err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, schema)
	files := generateElixir(t, u)
	// marker, unknown, items, entries, child, pick, value, nodes, Child,
	// bounded, children, key, UnknownNode
	ids := []uint64{ir.TableWireId("marker"), ir.TableWireId("unknown"), ir.TableWireId("items"), ir.TableWireId("entries"), ir.TableWireId("child"), ir.TableWireId("pick"), ir.TableWireId("value"), ir.TableNodeWireId, ir.TableWireId("Child"), ir.TableWireId("bounded"), ir.TableWireId("children"), ir.MapKeyWireId, ir.TableWireId("UnknownNode")}
	field := func(ref, kind byte, body []byte) []byte {
		b := []byte{ref, kind}
		if kind == 12 || kind == 13 || kind == 14 {
			b = binary.AppendUvarint(b, uint64(len(body)))
		}
		return append(b, body...)
	}
	wrap := func(body []byte) []byte {
		b := append([]byte{1}, body...)
		b = append(b, 0)
		for _, id := range ids {
			b = binary.LittleEndian.AppendUint64(b, id)
		}
		return binary.LittleEndian.AppendUint64(b, uint64(len(ids)))
	}
	prefix := []byte{1, 4, 99, 0, 0, 0, 2, 4, 5, 0, 0, 0}
	model := tabletext.NewModel(u)
	var rows strings.Builder
	appendCase := func(label string, wire []byte, cap bool) {
		var plain tabletext.Report
		ok, err := tablewire.Decode(model, model.New(model.Lookup("Root")), wire, &plain)
		var refusal *tablewire.CountRefusal
		if ok || errors.As(err, &refusal) != cap || !tablewire.ReadRefusal(err) {
			t.Fatalf("%s fixture: cap=%v ok=%v err=%v report=%+v", label, cap, ok, err, plain)
		}
		fmt.Fprintf(&rows, "Probe.plain(%q, %s, %t, %s)\n", label, elixirCountBytes(wire), cap, elixirCountReport(plain))
		for _, damage := range []bool{false, true} {
			report := tabletext.Report{Unknown: 3, Widened: 5, Retained: 8, RetainLost: 9, Refused: damage}
			retain := &tablewire.Retain{Capacity: 4096, IdCapacity: 512}
			ok, err = tablewire.DecodeRetain(model, model.New(model.Lookup("Root")), wire, retain, &report)
			refusal = nil
			if ok || errors.As(err, &refusal) != cap {
				t.Fatalf("%s retaining fixture: cap=%v ok=%v err=%v report=%+v", label, cap, ok, err, report)
			}
			fmt.Fprintf(&rows, "Probe.retaining(%q, %s, %t, %t, %s)\n", label, elixirCountBytes(wire), cap, damage, elixirCountReport(report))
		}
	}
	for _, count := range []uint64{1 << 31, 1 << 32, 1 << 63} {
		list := binary.AppendUvarint([]byte{4}, count)
		list = append(list, 23, 0, 0, 0)
		wide := binary.AppendUvarint([]byte{2}, count)
		wide = append(wide, 23)
		mp := binary.AppendUvarint([]byte{13}, count)
		mp = append(mp, 1, 0)
		nested := append(field(3, 14, list), 0)
		arm := append(binary.AppendUvarint([]byte{7, 13}, uint64(len(nested))), nested...)
		nodes := binary.AppendUvarint([]byte{1, 9}, uint64(len(nested)))
		nodes = append(nodes, nested...)
		for _, row := range []struct {
			name string
			body []byte
		}{
			{"list", field(3, 14, list)}, {"widened-list", field(3, 14, wide)},
			{"map", field(4, 14, mp)}, {"nested", field(5, 13, nested)},
			{"arm", field(6, 15, arm)}, {"unreached_node", field(8, 12, nodes)},
		} {
			appendCase(fmt.Sprintf("%s_%d", row.name, count), wrap(append(append([]byte{}, prefix...), row.body...)), true)
		}
		// Unknown node census is a framing/directory event even when root decoding
		// immediately refuses. Its retained counterpart is one excluded-class loss.
		body := append(append([]byte{}, prefix...), field(3, 14, list)...)
		body = append(body, field(8, 12, []byte{1, 13, 1, 0})...)
		appendCase(fmt.Sprintf("unknown-node-before-root-cap_%d", count), wrap(body), true)
	}
	// The second entry's value precedes its key on the wire. Descending key
	// admission must happen before that nested value can trigger a count refusal.
	huge := binary.AppendUvarint([]byte{4}, 1<<31)
	huge = append(huge, 23, 0, 0, 0)
	child := append(field(3, 14, huge), 0)
	entry := func(key byte, body []byte, last bool) []byte {
		k := []byte{12, 4, key, 0, 0, 0}
		v := field(7, 13, body)
		if last {
			return append(append(v, k...), 0)
		}
		return append(append(k, v...), 0)
	}
	first := entry(5, []byte{0}, false)
	for _, key := range []byte{3, 5, 7} {
		second := entry(key, child, true)
		mp := binary.AppendUvarint([]byte{13, 2}, uint64(len(first)))
		mp = append(mp, first...)
		mp = binary.AppendUvarint(mp, uint64(len(second)))
		mp = append(mp, second...)
		appendCase(fmt.Sprintf("map-key-%d", key), wrap(append(append([]byte{}, prefix...), field(11, 14, mp)...)), key >= 5)
	}
	appendCase("body-short", wrap(append(append([]byte{}, prefix...), field(3, 14, []byte{4, 2, 23, 0, 0, 0})...)), false)
	bounded := binary.AppendUvarint([]byte{4}, 1<<31)
	bounded = append(bounded, 23, 0, 0, 0, 24, 0, 0, 0)
	boundedWire := wrap(field(10, 14, bounded))
	var boundedReport tabletext.Report
	boundedValue := model.New(model.Lookup("Root"))
	ok, err := tablewire.Decode(model, boundedValue, boundedWire, &boundedReport)
	if !ok || err != nil || boundedReport.Refused {
		t.Fatalf("bounded fixture: ok=%v err=%v report=%+v", ok, err, boundedReport)
	}
	probe := strings.ReplaceAll(countRefusalElixir, "CASES", rows.String())
	probe = strings.ReplaceAll(probe, "BOUNDED", elixirCountBytes(boundedWire))
	return files, probe
}

func elixirCountBytes(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprint(v)
	}
	return "<<" + strings.Join(parts, ",") + ">>"
}
func elixirCountReport(r tabletext.Report) string {
	return fmt.Sprintf("{%d,%d,%d,%d,%d,%d,%d,%t}", r.Unknown, 0, r.Widened, 0, 0, r.Retained, r.RetainLost, r.Refused)
}

const countRefusalElixir = `
defmodule Probe do
 def check(true,_), do: :ok
 def check(false,label) do
  IO.puts(:stderr,"FAIL: "<>label)
  Process.put(:count_failed,true)
 end
 def events(r), do: {r.unknown,r.kind_mismatch,r.widened,r.clamped,Map.get(r,:duplicate,0),Map.get(r,:retained,0),Map.get(r,:retain_lost,0),r.malformed}
 def plain(label,wire,cap,expected) do
  result=Countprobe.Table.load("Root",wire)
  case result do
   {:error,reason,r} ->
    want=if cap,do: :count_over_extent_cap,else: :malformed
    check(reason==want,label<>" named refusal")
    check(events(r)==expected,label<>" event history got="<>inspect(events(r))<>" expected="<>inspect(expected))
    if cap,do: check(r.reason=="count_over_extent_cap",label<>" reason")
   _ -> check(false,label<>" must discard partial value: "<>inspect(result))
  end
 end
 def retaining(label,wire,cap,damage,expected) do
  r=%{unknown: 3,kind_mismatch: 4,widened: 5,clamped: 6,duplicate: 7,retained: 8,retain_lost: 9,malformed: damage,reason: ""}
  store=%{records: [{[],1,4,<<7,0,0,0>>}],used: 13,cap: 4096,ids: [1],id_cap: 512}
  result=Countprobe.Table.load_retain("Root",wire,store,r)
  case result do
   {:error,reason,report,retain} ->
    want=if cap,do: :count_over_extent_cap,else: :malformed
    check(reason==want,label<>" named refusal retaining")
    check(events(report)==expected,label<>" retained event history got="<>inspect(events(report))<>" expected="<>inspect(expected))
    check(retain.records==[] and retain.used==0 and retain.ids==[],label<>" retained store clear")
    check(retain.cap==4096 and retain.id_cap==512,label<>" capacity preserved")
    if cap,do: check(report.reason=="count_over_extent_cap",label<>" retained reason")
   _ -> check(false,label<>" retaining must discard partial value: "<>inspect(result))
  end
 end
end
CASES
case Countprobe.Table.load("Root",BOUNDED) do
 {:ok,v,r} -> Probe.check(v["bounded"]==[23,24] and r.clamped==1 and true,"bounded count clamps independently")
 other -> Probe.check(false,"bounded array: "<>inspect(other))
end
if Process.get(:count_failed,false),do: System.halt(1)
IO.puts("count refusal, reports, retention, admission, body-short and bounded controls pass")
`

// A counted array's schema bound is independent of the builder storage cap.
// Retaining table/pointer specializations must clamp before visiting elements,
// just as the plain decoder does, so an absent discarded suffix is not damage.
func TestWireCountBoundedRetainElixir(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	schema := filepath.Join(t.TempDir(), "Count.schema")
	if err := os.WriteFile(schema, []byte(countRefusalSchema), 0600); err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, schema)
	model := tabletext.NewModel(u)
	files := generateElixir(t, u)
	var rows strings.Builder
	for _, c := range []struct {
		name  string
		kind  byte
		elems []byte
	}{
		{"boundedChildren", 13, []byte{1, 0, 1, 0}}, {"boundedPointers", 17, []byte{0, 0}},
	} {
		body := binary.AppendUvarint([]byte{c.kind}, 1<<31)
		body = append(body, c.elems...)
		wire := binary.AppendUvarint([]byte{1, 1, 14}, uint64(len(body)))
		wire = append(wire, body...)
		wire = append(wire, 0)
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId(c.name))
		wire = binary.LittleEndian.AppendUint64(wire, 1)
		for _, retaining := range []bool{false, true} {
			var report tabletext.Report
			v := model.New(model.Lookup("Root"))
			var ok bool
			var err error
			if retaining {
				ok, err = tablewire.DecodeRetain(model, v, wire, &tablewire.Retain{Capacity: 4096, IdCapacity: 512}, &report)
			} else {
				ok, err = tablewire.Decode(model, v, wire, &report)
			}
			if !ok || err != nil || report.Refused {
				t.Fatalf("%s retaining=%v oracle ok=%v err=%v report=%+v", c.name, retaining, ok, err, report)
			}
		}
		fmt.Fprintf(&rows, "BoundProbe.run(%q,%s)\n", c.name, elixirCountBytes(wire))
	}
	probe := `
defmodule BoundProbe do
 def run(name,wire) do
  empty=%{unknown: 0,kind_mismatch: 0,widened: 0,clamped: 0,malformed: false,retained: 0,retain_lost: 0}
  store=%{records: [],used: 0,cap: 4096,ids: [],id_cap: 512}
  for result <- [Countprobe.Table.load("Root",wire),Countprobe.Table.load_retain("Root",wire,store,empty)] do
   case result do
    {:ok,v,r} -> check(name,v,r)
    {:ok,v,r,_} -> check(name,v,r)
    other -> IO.puts(:stderr,"FAIL: "<>name<>" bounded load "<>inspect(other));Process.put(:failed,true)
   end
  end
 end
 def check(name,v,r) do
  if length(v[name]) != 2 or r.clamped != 1 or r.malformed do
   IO.puts(:stderr,"FAIL: "<>name<>" bounded prefix "<>inspect({v,r}));Process.put(:failed,true)
  end
 end
end
CASES
if Process.get(:failed,false),do: System.halt(1)
`
	out, err := runArmProbe(t, elixirBinary(t), files, strings.ReplaceAll(probe, "CASES", rows.String()))
	if err != nil {
		t.Fatalf("bounded retaining probe: %v\n%s", err, out)
	}
}

// A list inside a pointered node record whose count exceeds the element slice's
// byte floor must be refused by load_measure (-1), matching FileRegionMeasure.
func TestNodeRecordListCountRefusalElixir(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	u := loadDirUnit(t, filepath.Join("..", "..", "..", "tables", "lists"))
	root := u.Tables["Sheet"]
	if root == nil {
		t.Fatal("Lists.schema declares no Sheet root")
	}

	const mutantHex = "01010e530d030e020e040d01010003040a000000001c020e120d020704046400000000070404650000000003040b0000000024020e1a0d03070404c800000000070404c900000000070404ca0000000003040c00000000051102060c1f01071c020e120d1007040401000000000704040200000000030463000000000038810af11f06a7a36f2c414fbf84783e3d62cb8fecfcf739090602864ceb63af0f62ad077747825ffffffffffffffffffb06c9fe19e113a00700000000000000"
	const mutantSHA256 = "7a084541b43ed4e19690ccea7b06142f03dcd305854bd3fe0acf845cf955ff26"

	mutant, err := hex.DecodeString(mutantHex)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(mutant)); got != mutantSHA256 {
		t.Fatalf("mutant sha256 = %s, want %s", got, mutantSHA256)
	}
	if size, whole, refused := tablewire.FileRegionMeasure(u, root, mutant); size != -1 || !whole || !refused {
		t.Fatalf("FileRegionMeasure(mutant) = (%d, %v, %v), want (-1, true, true)", size, whole, refused)
	}

	clean := append([]byte(nil), mutant...)
	if clean[100] != 0x10 {
		t.Fatalf("node record items count moved: byte 100 = 0x%02x, want 0x10", clean[100])
	}
	clean[100] = 0x02
	cleanSize, cleanWhole, cleanRefused := tablewire.FileRegionMeasure(u, root, clean)
	if cleanSize != 184 || !cleanWhole || cleanRefused {
		t.Fatalf("FileRegionMeasure(clean) = (%d, %v, %v), want (184, true, false)", cleanSize, cleanWhole, cleanRefused)
	}

	files := generateElixir(t, u)
	probe := fmt.Sprintf(`
mutant = Base.decode16!(%q, case: :lower)
clean = Base.decode16!(%q, case: :lower)
if Listdemo.Table.load_measure("Sheet", mutant) != -1 do
  raise("expected -1 for mutant load_measure, got " <> inspect(Listdemo.Table.load_measure("Sheet", mutant)))
end
if Listdemo.Table.load_measure("Sheet", clean) != %d do
  raise("expected %d for clean load_measure, got " <> inspect(Listdemo.Table.load_measure("Sheet", clean)))
end
case Listdemo.Table.load("Sheet", mutant) do
  {:error, :malformed, report} when report.malformed == true -> :ok
  other -> raise("expected malformed refusal on mutant load, got " <> inspect(other))
end
IO.puts("NODE_RECORD_LIST_COUNT_OK")
`, mutantHex, hex.EncodeToString(clean), cleanSize, cleanSize)

	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err != nil || !strings.Contains(out, "NODE_RECORD_LIST_COUNT_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("node record list count probe failed: %v\n%s", err, out)
	}

	// Negative control: swallowing :error from measure_node_table into {:ok, 0, 0}
	// sizes the root without the node table (136) instead of refusing (-1).
	const target = "  defp measure_found_nodes(payload, ids, desc), do: measure_node_table(payload, ids, desc)"
	const sabotage = `  defp measure_found_nodes(payload, ids, desc) do
    case measure_node_table(payload, ids, desc) do
      {:ok, count, blobs} -> {:ok, count, blobs}
      :error -> {:ok, 0, 0}
    end
  end`
	tableEx := string(files["Table.ex"])
	if !strings.Contains(tableEx, target) {
		t.Fatal("negative control target not found in generated Table.ex")
	}
	badFiles := make(map[string][]byte, len(files))
	maps.Copy(badFiles, files)
	badFiles["Table.ex"] = []byte(strings.Replace(tableEx, target, sabotage, 1))
	if badOut, badErr := runArmProbe(t, elixirBinary(t), badFiles, probe); badErr == nil || !strings.Contains(badOut, "expected -1 for mutant load_measure, got 136") {
		t.Fatalf("negative control did not expose the swallowed node extent: %v\n%s", badErr, badOut)
	}
}
