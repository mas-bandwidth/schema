package cstable

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/idcapwire"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE RETAINING WRITER KEEPS THE 4096-ID BUDGET (docs/SPEC-TABLES.md §3 and
// §6.6, ruling R8 follow-up), for C#. The older build loads a lawful 4096-id
// file with retention (the newer build's field z is the one retained foreign
// id), sets x (the 4096th known id) and saves: 4097 ids if the retained id
// were written beside them. The save must refuse, or drop the retained id and
// count it lost; the file it writes, if any, has at most 4096 ids and loads.
func TestCSharpRetainWriterKeeps4096IdBudget(t *testing.T) {
	wire := base64.StdEncoding.EncodeToString(idcapwire.NewerFile(t))
	t15RunCS(t, tablewire.IdCapSchema(), fmt.Sprintf(`
unsafe
{
  byte[] wire = Convert.FromBase64String("%s");
  long need = Idcap.Schema.RootLoadMeasure(wire);
  byte* region = (byte*)System.Runtime.InteropServices.NativeMemory.AlignedAlloc((nuint)(Math.Max(0, need) + 64), 64);
  byte* store = (byte*)System.Runtime.InteropServices.NativeMemory.Alloc(1 << 20);
  void* ids = System.Runtime.InteropServices.NativeMemory.Alloc(1 << 20);
  var retain = new Idcap.TableRetain { Bytes = store, Capacity = 1 << 20, Ids = (Idcap.TableRetain.Id*)ids, IdCapacity = 1 << 14 };
  var report = new Idcap.TableReport();
  var root = Idcap.Schema.RootLoadRetain((IntPtr)region, need, wire, ref retain, report);
  if (root == null) { bad += ProbeLog.Fail("the lawful 4096-id file did not load with retention"); }
  else
  {
    if (report.Retained != 1) { bad += ProbeLog.Fail("z was not retained: " + report.Retained); }
    root->X = 1;
    long size = Idcap.Schema.RootMeasureRetain((IntPtr)root, ref retain);
    if (size >= 0)
    {
      byte[] saved = new byte[size];
      long w = Idcap.Schema.RootSaveRetain((IntPtr)root, ref retain, saved, report);
      if (w != size) { bad += ProbeLog.Fail("save wrote " + w + " of " + size); }
      long count = 0; for (int i = 0; i < 8; i++) count |= (long)saved[saved.Length - 8 + i] << (8 * i);
      if (count > 4096) { bad += ProbeLog.Fail("the retaining save wrote an id table of " + count + " entries"); }
      var back = new Idcap.TableReport();
      if (!Idcap.Schema.RootLoad(new Idcap.Root(), saved, back) || back.Damaged || back.Refused) { bad += ProbeLog.Fail("the file the retaining save wrote does not load"); }
    }
  }
}
`, wire))
}
