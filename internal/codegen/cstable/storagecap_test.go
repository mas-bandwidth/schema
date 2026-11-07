package cstable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The storage cap vectors (docs/SPEC-TABLES.md §11, §6.5) live in
// testdata/wire/tables/storage-cap beside the unit they are read under. Big's
// storage is 2^27 bytes and one element or record of it costs two wire bytes,
// so 8191 of them measure under the 2^40-byte cap and 8192 measure over it.
const csStorageCapDir = "../../../testdata/wire/tables/storage-cap"

// TestStorageCapRefusedAtMeasureCSharp: the C# leg refuses each over vector
// as storage_over_cap from RootLoadMeasure, from the managed RootLoadVerdict
// (before a single Big is built) and from the region RootLoad, and measures
// each at vector at a size at or under 2^40. No at vector is loaded: its
// region is about 1 TiB, and its managed value more. The probe runs under a
// 4 GiB GC heap, so a Load that builds before it measures fails fast.
func TestStorageCapRefusedAtMeasureCSharp(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	memceiling.Guard(t, memceiling.Default)
	schema, err := os.ReadFile(filepath.Join(csStorageCapDir, "Cap.schema"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tableLoadMeasureSource, fmt.Sprintf("StorageCap = %d;", ir.MaxSizeBytes)) {
		t.Fatal("the C# storage cap is not ir.MaxSizeBytes")
	}
	var body strings.Builder
	for _, v := range []struct {
		file string
		over bool
	}{{"list_at.bin", false}, {"list_over.bin", true}, {"nodes_at.bin", false}, {"nodes_over.bin", true}} {
		wire, err := os.ReadFile(filepath.Join(csStorageCapDir, v.file))
		if err != nil {
			t.Fatal(err)
		}
		nums := make([]string, len(wire))
		for i, b := range wire {
			nums[i] = fmt.Sprint(b)
		}
		fmt.Fprintf(&body, "{\n byte[] bytes = new byte[] {%s};\n long size = Capcheck.Schema.RootLoadMeasure(bytes, out Capcheck.TableRefuseReason why);\n", strings.Join(nums, ","))
		if v.over {
			fmt.Fprintf(&body, ` if (size != -1 || why != Capcheck.TableRefuseReason.storage_over_cap || Capcheck.Schema.RootLoadMeasure(bytes) != -1)
  bad += ProbeLog.Fail("%[1]s measure: " + size + " " + why);
 var report = new Capcheck.TableReport { Unknown = 3 };
 var verdict = Capcheck.Schema.RootLoadVerdict(new Capcheck.Root(), bytes, report);
 if (verdict != Capcheck.Schema.TableWire.Verdict.Refused || !report.Refused || report.Reason != "storage_over_cap" || report.Malformed || report.Unknown != 3)
  bad += ProbeLog.Fail("%[1]s managed load: " + verdict + " " + report.Reason);
 var regionReport = new Capcheck.TableReport();
 System.IntPtr region = System.Runtime.InteropServices.Marshal.AllocHGlobal(65536 + 64);
 System.IntPtr aligned = (System.IntPtr)(((long)region + 63) & ~63L);
 unsafe { if (Capcheck.Schema.RootLoad(aligned, 65536, bytes, regionReport) != null) bad += ProbeLog.Fail("%[1]s region load answered a root"); }
 System.Runtime.InteropServices.Marshal.FreeHGlobal(region);
 if (!regionReport.Refused || regionReport.Reason != "storage_over_cap" || regionReport.Malformed)
  bad += ProbeLog.Fail("%[1]s region load: " + regionReport.Reason);
}
`, v.file)
		} else {
			fmt.Fprintf(&body, ` if (size <= 0 || size > (1L << 40) || why != Capcheck.TableRefuseReason.ok || Capcheck.Schema.RootLoadMeasure(bytes) != size)
  bad += ProbeLog.Fail("%[1]s measure: " + size + " " + why);
}
`, v.file)
		}
	}
	// each list or map term is refused before it is summed: a term that ends
	// exactly at the cap reads, one element past it refuses
	body.WriteString(`const long capBytes = 1L << 40;
if (Capcheck.Schema.TableWire.StorageTermOverCap(capBytes - 16, 1, 16) || !Capcheck.Schema.TableWire.StorageTermOverCap(capBytes - 15, 1, 16))
 bad += ProbeLog.Fail("a term at the cap reads and one past it refuses");
if (Capcheck.Schema.TableWire.StorageTermOverCap(0, 65536, 16777216) || !Capcheck.Schema.TableWire.StorageTermOverCap(0, 65537, 16777216))
 bad += ProbeLog.Fail("a 2^40 term from zero reads and one element more refuses");
`)
	// a 4 GiB heap: a managed Load that built the over vectors fails fast
	// rather than committing a terabyte of zeroed Big arrays
	t15RunCSConfig(t, string(schema), body.String(), `"System.GC.HeapHardLimit": 4294967296`)
}

// TestStorageWireFloorBoundsTheMeasure: a managed Load skips the measure pass
// only for a wire too short to command the cap. Cap.schema's Big is 2^27
// bytes, so the floor is under 8192 bytes and both 16 KiB vectors are
// measured; a unit of small records has a floor in the gigabytes.
func TestStorageWireFloorBoundsTheMeasure(t *testing.T) {
	defer memceiling.Heap(memceiling.Default)()
	schema, err := os.ReadFile(filepath.Join(csStorageCapDir, "Cap.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	floor := storageWireFloor(u, u.Tables["Root"])
	if floor <= 0 || floor >= 8192 {
		t.Fatalf("Cap.schema floor %d is not under the vectors' 8192 elements", floor)
	}
	// 8192 records of two wire bytes reach 2^40; the floor admits none of them
	if floor*(ir.RecordLayout(u, u.Tables["Big"]).Size+16) >= ir.MaxSizeBytes {
		t.Fatalf("floor %d lets a skipped wire reach the cap", floor)
	}
	small := unitFrom(t, "package probe\ntable Leaf { a int64 }\ntable Root { items []Leaf\n next *Leaf }\n")
	if f := storageWireFloor(small, small.Tables["Root"]); f < 1<<30 {
		t.Fatalf("a small unit's floor is %d, under a gigabyte", f)
	}
	fixed := unitFrom(t, "package probe\nfixed table Root { a int64 }\n")
	if f := storageWireFloor(fixed, fixed.Tables["Root"]); f != 1<<63-1 {
		t.Fatalf("a fixed root measures at %d", f)
	}
}
