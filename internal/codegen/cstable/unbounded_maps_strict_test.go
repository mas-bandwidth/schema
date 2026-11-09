package cstable

// C# UNBOUNDED TEXT AND MAPS, STRICT-100 (docs/roadmap-evidence/
// zzz-cs-strict-100.sexp: vt/text-form-variable/cs, vt/maps/cs,
// vt/unbounded-arrays/cs). The strict read is the one read
// (docs/SPEC-TABLES.md §4, §6.5): a read returns the whole value or
// refuses the whole load and lands nothing. The probes below are written
// against the strict report only (Refused/Reason/Verdict) — no retired
// tolerant counters — and run the generated C# through the .NET SDK.
//
// The positive probe holds: a 5000-byte unbounded text element and
// 5000-byte map keys survive JSON -> wire -> load -> wire byte-for-byte;
// a repeated map key refuses in JSON (by name) and on the wire;
// ill-formed UTF-8 in JSON refuses by name.
//
// The negative control plants its fault in a copy of the real positive
// artifact (the wire the positive value saved): one text byte becomes
// 0xff, and the same strict load must refuse it and land nothing. A
// gate that accepts the sabotaged wire proves nothing about the real one.

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const unboundedMapsStrictProbe = `
{
 string longText = new string('x', 5000);
 string json = "{\"names\":[\"\",\"" + longText + "\"],\"by_name\":{\"" + longText + "a\":1,\"" + longText + "b\":2}}";
 var value = new Probe.Root(); var report = new Probe.TableReport();
 if (!Probe.Schema.RootFromJson(value, System.Text.Encoding.UTF8.GetBytes(json), report) || report.Refused)
  throw new System.Exception("unbounded JSON read refused: " + report.Reason);
 if (value.NamesCount != 2 || value.ByNameCount != 2)
  throw new System.Exception("unbounded counts names=" + value.NamesCount + " by_name=" + value.ByNameCount);
 byte[] wire = new byte[Probe.Schema.RootMeasure(value)];
 if (Probe.Schema.RootSave(value, wire) != wire.Length) throw new System.Exception("unbounded save");
 var back = new Probe.Root(); var reread = new Probe.TableReport();
 if (!Probe.Schema.RootLoad(back, wire, reread) || reread.Refused)
  throw new System.Exception("unbounded wire read refused: " + reread.Reason);
 byte[] again = new byte[Probe.Schema.RootMeasure(back)];
 if (Probe.Schema.RootSave(back, again) != again.Length || !again.AsSpan().SequenceEqual(wire))
  throw new System.Exception("unbounded wire changed on load/save");
 if (back.NamesCount != 2 || back.ByNameCount != 2)
  throw new System.Exception("unbounded reread counts");

 // A repeated map key is a duplicate: the whole JSON read refuses.
 var dup = new Probe.Root(); var dupReport = new Probe.TableReport();
 if (Probe.Schema.RootFromJson(dup, System.Text.Encoding.UTF8.GetBytes("{\"by_name\":{\"a\":1,\"a\":2}}"), dupReport) || dupReport.Reason == null || !dupReport.Reason.StartsWith("duplicate"))
  throw new System.Exception("repeated map key in JSON was not refused reason=" + dupReport.Reason + " count=" + dup.ByNameCount);
 if (dup.ByNameCount != 0) throw new System.Exception("refused duplicate map read landed entries");

 // Ill-formed UTF-8 in a text value refuses the JSON read.
 byte[] badJson = System.Text.Encoding.UTF8.GetBytes("{\"names\":[\"!\"]}");
 badJson[System.Array.IndexOf(badJson, (byte)'!')] = 0xff;
 var badValue = new Probe.Root(); var badReport = new Probe.TableReport();
 if (Probe.Schema.RootFromJson(badValue, badJson, badReport) || badReport.Reason == null || !badReport.Reason.StartsWith("string_not_utf8"))
  throw new System.Exception("ill-formed UTF-8 in JSON text was not refused reason=" + badReport.Reason);
 if (badValue.NamesCount != 0) throw new System.Exception("refused JSON text read landed entries");

 // A repeated map key on the wire refuses the whole load. The wire is
 // built by the neutral Go writer from a value holding one key twice;
 // the C# authoring save refuses such a value, so this hand-built wire
 // is the only way the read side can be held to the same rule.
 byte[] dupWire = System.Convert.FromHexString("DUPWIRE");
 var dupRead = new Probe.Root(); var dupWireReport = new Probe.TableReport();
 if (Probe.Schema.RootLoad(dupRead, dupWire, dupWireReport) || !dupWireReport.Refused)
  throw new System.Exception("repeated map key on the wire was not refused reason=" + dupWireReport.Reason);
 if (dupRead.ByNameCount != 0) throw new System.Exception("refused duplicate wire read landed entries");
}
`

const unboundedMapsStrictNegativeProbe = `
{
 string longText = new string('x', 5000);
 string json = "{\"names\":[\"\",\"" + longText + "\"],\"by_name\":{\"" + longText + "a\":1,\"" + longText + "b\":2}}";
 var value = new Probe.Root(); var report = new Probe.TableReport();
 if (!Probe.Schema.RootFromJson(value, System.Text.Encoding.UTF8.GetBytes(json), report) || report.Refused)
  throw new System.Exception("negative-control setup: positive JSON read refused: " + report.Reason);
 byte[] wire = new byte[Probe.Schema.RootMeasure(value)];
 if (Probe.Schema.RootSave(value, wire) != wire.Length) throw new System.Exception("negative-control setup: save");
 var sane = new Probe.Root(); var saneReport = new Probe.TableReport();
 if (!Probe.Schema.RootLoad(sane, wire, saneReport) || saneReport.Refused)
  throw new System.Exception("negative-control setup: the real artifact does not load");

 // Plant the fault in a copy of the real artifact: one byte of the
 // 5000-byte text element becomes 0xff, which is not text.
 byte[] sabotaged = (byte[])wire.Clone();
 int at = System.Array.IndexOf(sabotaged, (byte)'x');
 if (at < 0) throw new System.Exception("negative-control setup: no text byte to sabotage");
 sabotaged[at] = 0xff;
 var planted = new Probe.Root(); var plantedReport = new Probe.TableReport();
 if (Probe.Schema.RootLoad(planted, sabotaged, plantedReport))
  throw new System.Exception("NEGATIVE CONTROL FAILED: wire with ill-formed text was accepted");
 if (planted.NamesCount != 0 || planted.ByNameCount != 0)
  throw new System.Exception("NEGATIVE CONTROL FAILED: refused sabotaged wire landed a partial value");
}
`

// unboundedMapsDupWire builds, through the neutral model and writer, a
// wire whose by_name map holds the key "a" twice (values 1 and 2).
func unboundedMapsDupWire(t *testing.T) string {
	t.Helper()
	u := unitFrom(t, unboundedTextSchema)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var byName *tabletext.Field
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == "by_name" {
			byName = &inst.Fields[i]
		}
	}
	if byName == nil {
		t.Fatal("by_name field not found")
	}
	for _, v := range []int32{1, 2} {
		entry := m.New(byName.Def.MapEntry)
		entry.Fields[0].Cell.Str = []byte("a")
		entry.Fields[0].Count = 1
		entry.Fields[1].Cell.I = int64(v)
		entry.Fields[1].Cell.U = uint64(int64(v))
		byName.Entries = append(byName.Entries, tabletext.Cell{Tab: entry})
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("neutral duplicate-key encode: %v", err)
	}
	return hex.EncodeToString(wire)
}

func TestCSharpUnboundedTextAndMaps(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	probe := strings.Replace(unboundedMapsStrictProbe, "DUPWIRE", unboundedMapsDupWire(t), 1)
	t15RunCS(t, unboundedTextSchema, probe)
}

func TestCSharpUnboundedTextAndMapsNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	t15RunCS(t, unboundedTextSchema, unboundedMapsStrictNegativeProbe)
}
