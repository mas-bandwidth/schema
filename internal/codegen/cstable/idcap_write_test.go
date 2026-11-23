package cstable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE WRITER REFUSES AN ID TABLE ITS READERS REFUSE (docs/SPEC-TABLES.md §3,
// ruling R8), for C#. The unit is tablewire.IdCapSchema, so the three values
// need 4095, 4096 and 4097 ids: the first two write (the count word says so
// and the strict read accepts them), and the third makes Measure and Save
// answer -1 and writes nothing. The id table the save interns into is sized by
// the unit's capacity, capped at the maximum every reader enforces.
func TestCSharpWriterRefusesMoreThan4096Ids(t *testing.T) {
	t15RunCS(t, tablewire.IdCapSchema(), `
Idcap.Root Make(byte x, byte y) { var v = new Idcap.Root { X = x, Y = y }; for (int i = 0; i < v.Keys.Slots.Length; i++) v.Keys.Slots[i] = 1; return v; }
foreach (var row in new[] { (x: (byte)0, y: (byte)0, ids: 4095), (x: (byte)1, y: (byte)0, ids: 4096) })
{
  var v = Make(row.x, row.y);
  long n = Idcap.Schema.RootMeasure(v);
  if (n <= 0) throw new Exception(row.ids + " ids: measure refused");
  byte[] buf = new byte[n];
  if (Idcap.Schema.RootSave(v, buf) != n) throw new Exception(row.ids + " ids: save refused");
  long count = 0; for (int i = 0; i < 8; i++) count |= (long)buf[buf.Length - 8 + i] << (8 * i);
  if (count != row.ids) throw new Exception("id count " + count + ", want " + row.ids);
  var report = new Idcap.TableReport();
  if (!Idcap.Schema.RootLoad(new Idcap.Root(), buf, report) || report.Damaged || report.Refused) throw new Exception(row.ids + " ids: load refused the file");
}
var over = Make(1, 1);
if (Idcap.Schema.RootMeasure(over) >= 0) throw new Exception("4097 ids: measure did not refuse");
byte[] big = new byte[1 << 20];
Array.Fill(big, (byte)0x5a);
if (Idcap.Schema.RootSave(over, big) >= 0) throw new Exception("4097 ids: save did not refuse");
`)
}
