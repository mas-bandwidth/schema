package cstable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

const csUnsortedMapSaveProbe = `
{
 // Sorted reference via FromJson
 var sorted = new Probe.Root(); var rep = new Probe.TableReport();
 byte[] srcJson = System.Text.Encoding.UTF8.GetBytes("{\"by_name\":{\"\":1,\"a\":2,\"\uff61\":3,\"\ud83d\ude00\":4}}");
 if (!Probe.Schema.RootFromJson(sorted, srcJson, rep) || rep.Refused)
  throw new System.Exception("FromJson failed: " + rep.Reason);
 byte[] wantWire = new byte[Probe.Schema.RootMeasure(sorted)];
 if (Probe.Schema.RootSave(sorted, wantWire) != wantWire.Length)
  throw new System.Exception("sorted RootSave failed");
 byte[] wantJson = new byte[Probe.Schema.RootToJsonMeasure(sorted)];
 if (Probe.Schema.RootToJson(sorted, wantJson) != wantJson.Length)
  throw new System.Exception("sorted RootToJson failed");

 // Reverse-sorted in-memory Entry[] must sort canonically on both Save and ToJson (SPEC-TABLES §22.2.1).
 Probe.RootByNameEntry Mk(string k, int v) {
  byte[] kb = System.Text.Encoding.UTF8.GetBytes(k);
  return new Probe.RootByNameEntry { Key = kb, KeyLength = kb.Length, Value = v };
 }
 var rev = new Probe.Root();
 rev.ByName = new[] { Mk("\U0001F600", 4), Mk("\uff61", 3), Mk("a", 2), Mk("", 1) };
 rev.ByNameCount = 4;
 byte[] gotJson = new byte[8192];
 long jn = Probe.Schema.RootToJson(rev, gotJson);
 if (jn != wantJson.Length || !gotJson.AsSpan(0, (int)jn).SequenceEqual(wantJson))
  throw new System.Exception("unsorted ToJson mismatch");

 var rev2 = new Probe.Root();
 rev2.ByName = new[] { Mk("\U0001F600", 4), Mk("a", 2), Mk("\uff61", 3), Mk("", 1) };
 rev2.ByNameCount = 4;
 byte[] gotWire = new byte[4096];
 long wn = Probe.Schema.RootSave(rev2, gotWire);
 if (wn != wantWire.Length || !gotWire.AsSpan(0, (int)wn).SequenceEqual(wantWire))
  throw new System.Exception("unsorted Save mismatch wn=" + wn);

 // Duplicate keys in an unsorted Entry[] must still be refused on both Save and ToJson.
 var dup = new Probe.Root();
 dup.ByName = new[] { Mk("b", 1), Mk("a", 2), Mk("b", 3) };
 dup.ByNameCount = 3;
 if (Probe.Schema.RootSave(dup, gotWire) >= 0)
  throw new System.Exception("duplicate unsorted key accepted by Save");
 if (Probe.Schema.RootToJson(dup, gotJson) >= 0)
  throw new System.Exception("duplicate unsorted key accepted by ToJson");
}
`

func TestCSharpUnsortedMapEntriesSortCanonicallyAndRefuseDuplicates(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	t15RunCS(t, unboundedTextSchema, csUnsortedMapSaveProbe)
}
