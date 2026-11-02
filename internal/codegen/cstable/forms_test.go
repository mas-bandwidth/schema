package cstable

import (
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"strconv"
	"strings"
	"testing"
)

func TestDeclaredFormsCSharp(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	for mask := range 8 {
		forms := []string{"wire"}
		for i, name := range []string{"json", "cook", "block"} {
			if mask&(1<<i) != 0 {
				forms = append(forms, name)
			}
		}
		t.Run(strings.Join(forms, "-"), func(t *testing.T) {
			src := fmt.Sprintf("package probe\ntable Root | forms = %q { value int32 }\nfixed table FixedRow | forms = %q { value int32 }\n", strings.Join(forms, ","), strings.Join(forms, ","))
			u := unitFrom(t, src)
			m := tabletext.NewModel(u)
			value := m.New(u.Tables["Root"])
			var r tabletext.Report
			if !m.Read(value, []byte(`{"value":7}`), &r) {
				t.Fatal(r)
			}
			expected, err := tablewire.Encode(m, value)
			if err != nil {
				t.Fatal(err)
			}
			body := strings.NewReplacer("WANT_JSON", strconv.FormatBool(mask&1 != 0), "WANT_COOK", strconv.FormatBool(mask&2 != 0), "WANT_BLOCK", strconv.FormatBool(mask&4 != 0), "WIRE_HEX", hex.EncodeToString(expected)).Replace(`unsafe {
 bool Has(string name) { return Array.Exists(typeof(Probe.Schema).GetMethods(), m => m.Name == name); }
 var assembly = typeof(Probe.Schema).Assembly;
 if ((typeof(Probe.Schema).GetNestedType("TableJson") != null) != WANT_JSON) throw new Exception("unit-wide JSON walk retained without a JSON form");
 if (Has("RootFromJson") != WANT_JSON || Has("RootToJson") != WANT_JSON || Has("RootToJsonMeasure") != WANT_JSON) throw new Exception("declared JSON surface");
 if ((assembly.GetType("Probe.RootCook") != null) != WANT_COOK || (assembly.GetType("Probe.FixedRowCook") != null) != WANT_COOK || Has("RootCook") != WANT_COOK || Has("RootCookMeasure") != WANT_COOK) throw new Exception("declared cook surface");
 if ((typeof(Probe.RootBuilder).GetMethod("Cook") != null) != WANT_COOK || (typeof(Probe.RootBuilder).GetMethod("CookMeasure") != null) != WANT_COOK) throw new Exception("declared builder cook surface");
 if ((assembly.GetType("Probe.FixedRowBlock") != null) != WANT_BLOCK || (assembly.GetType("Probe.FixedRowBlockStorage") != null) != WANT_BLOCK) throw new Exception("declared block surface");
 if (!Has("FixedRowFixedSave") || !Has("FixedRowSaveMessages")) throw new Exception("forms removed independent fixed/message surface");
 var value = new Probe.Root { Value = 7 }; byte[] bytes = new byte[Probe.Schema.RootMeasure(value)];
 if (Probe.Schema.RootSave(value,bytes) != bytes.Length || !bytes.AsSpan().SequenceEqual(Convert.FromHexString("WIRE_HEX"))) throw new Exception("forms changed populated wire bytes");
 var back = new Probe.Root(); var report = new Probe.TableReport();
 if (!Probe.Schema.RootLoad(back,bytes,report) || back.Value != 7 || report.Malformed || report.Refused) throw new Exception("declared wire load");
 byte[] fixedBytes = new byte[Probe.Schema.FixedRowFixedMeasure(1)];
 if (Probe.Schema.FixedRowFixedSave(new Probe.FixedRow {Value=7},fixedBytes)!=fixedBytes.Length) throw new Exception("fixed save after forms trim");
 var fixedBack = new Probe.FixedRow();
 if (Probe.Schema.FixedRowFixedLoad(fixedBack,fixedBytes,new Probe.TableFixedEntry[8],new Probe.TableReport())!=1 || fixedBack.Value!=7) throw new Exception("fixed load after forms trim");
`)
			if mask&1 != 0 {
				body += `var text = System.Text.Encoding.UTF8.GetBytes("""{"value":17}""");
 if (!Probe.Schema.RootFromJson(back,text,new Probe.TableReport()) || back.Value!=17 || Probe.Schema.RootToJsonMeasure(back)<=0) throw new Exception("declared JSON populated read");
 if (Probe.Schema.RootFromJson(back,System.Text.Encoding.UTF8.GetBytes("{"),new Probe.TableReport())) throw new Exception("declared JSON damaged read");
`
			}
			if mask&2 != 0 {
				body += `using (var builder = new Probe.RootBuilder()) {
 builder.GetRoot()->Value = 19; byte[] cooked = new byte[builder.CookMeasure()];
 if (!builder.Cook(cooked)) throw new Exception("declared cook write");
 fixed (byte* ptr = cooked) {
 if (!Probe.RootCook.Open(out var opened,(IntPtr)ptr,cooked.Length) || opened.Root.Value != 19) throw new Exception("declared cook populated read");
 cooked[0]^=1;
 if (Probe.RootCook.Open(out _,(IntPtr)ptr,cooked.Length)) throw new Exception("declared cook damaged read");
 }
 }
`
			}
			if mask&4 != 0 {
				body += `using (var storage = new Probe.FixedRowBlockStorage()) {
 var counts = new Probe.FixedRowCounts();
 if (!Probe.FixedRowBlock.Begin(out var block,storage,in counts,out _)) throw new Exception("declared block begin");
 block.WritableProjection.Value=23;
 if (!Probe.FixedRowBlock.Open(out var opened,storage.Pointer,block.Bytes) || opened.Projection.Value!=23) throw new Exception("declared block populated read");
 *(byte*)storage.Pointer^=1;
 if (Probe.FixedRowBlock.Open(out _,storage.Pointer,block.Bytes)) throw new Exception("declared block damaged read");
 }
`
			}
			t15RunCS(t, src, body+"}")
		})
	}
}

func TestDeclaredFormsCSharpMixedClosure(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	t15RunCS(t, `package probe
table Child | forms = "wire" { value int32 }
table Root | forms = "wire,json,cook" {
 child Child
 items map[uint8]Child
}
fixed table FreeRow { value int32 }
`, `unsafe {
 var assembly = typeof(Probe.Schema).Assembly;
 bool Has(string name) { return Array.Exists(typeof(Probe.Schema).GetMethods(), m => m.Name == name); }
 if (Has("ChildFromJson") || Has("ChildCook") || assembly.GetType("Probe.ChildCook") != null || typeof(Probe.ChildBuilder).GetMethod("Cook") != null) throw new Exception("wire-only child gained forbidden surface");
 if (!Has("FreeRowFromJson") || assembly.GetType("Probe.FreeRowCook") == null || assembly.GetType("Probe.FreeRowBlock") == null) throw new Exception("undeclared neighbor lost default forms");
 if (typeof(Probe.RootBuilder).GetMethod("Cook") == null || !Has("RootFromJson")) throw new Exception("allowed mixed-root surface absent");
 var root = new Probe.Root(); var report = new Probe.TableReport();
 if (!Probe.Schema.RootFromJson(root,System.Text.Encoding.UTF8.GetBytes("""{"child":{"value":31},"items":{"1":{"value":22}}}"""),report)) throw new Exception("nested wire-only type lost JSON storage");
 byte[] wire = new byte[Probe.Schema.RootMeasure(root)];Probe.Schema.RootSave(root,wire);
 using (var builder = new Probe.RootBuilder()) {
 if (!builder.Load(wire,new Probe.TableReport())) throw new Exception("mixed-root native load");
 byte[] cooked = new byte[builder.CookMeasure()];if (!builder.Cook(cooked)) throw new Exception("mixed-root cook");
 fixed (byte* ptr = cooked) {
 if (!Probe.RootCook.Open(out var opened,(IntPtr)ptr,cooked.Length) || opened.Root.Child.Value!=31) throw new Exception("nested wire-only type lost cook storage");
 var items = Probe.RootCook.Items(opened.RootPointer);
 if (items.Length!=1 || items[0].Value.Value!=22) throw new Exception("nested map-entry storage lost");
 }
 }
 }`)
}
