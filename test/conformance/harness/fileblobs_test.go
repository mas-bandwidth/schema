package main

import (
	"os"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// File blobs now run through the same sizing oracle as message blobs. Their
// records have variable storage even though their reserved type ID is fixed.
func TestFileBlobSizingIncludesPayloads(t *testing.T) {
	_, _, units := corpus(t)
	root, err := newWireRoot(units, "blobdemo", "Catalog", false, false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/wire/tables/blob_small.bin")
	if err != nil {
		t.Fatal(err)
	}
	records, whole := tablewire.FileNodeRecords(data)
	if !whole {
		t.Fatal("pinned blob file did not frame")
	}
	blobBytes := int64(0)
	align := ir.TableRegionAlign(root.model.Unit)
	for _, record := range records {
		if record.TypeId == ir.BytesWireTypeId || record.TypeId == ir.StringWireTypeId {
			extra := int64(0)
			if record.TypeId == ir.StringWireTypeId {
				extra = 1
			}
			blobBytes += (8 + record.Length + extra + align - 1) & -align
		}
	}
	if blobBytes != 72 {
		t.Fatalf("blob storage = %d, want 72", blobBytes)
	}
	measured, whole, refused := tablewire.FileRegionMeasure(root.model.Unit, root.def, data)
	if refused || !whole || measured != 400 {
		t.Fatalf("region = %d whole=%v refused=%v, want 400 whole", measured, whole, refused)
	}
}
