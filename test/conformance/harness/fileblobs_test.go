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
	for _, record := range records {
		if record.TypeId == ir.BytesWireTypeId || record.TypeId == ir.StringWireTypeId {
			extra := int64(0)
			if record.TypeId == ir.StringWireTypeId {
				extra = 1
			}
			blobBytes += alignUp8(8 + record.Length + extra)
		}
	}
	if blobBytes != 72 {
		t.Fatalf("blob storage = %d, want 72", blobBytes)
	}
	answer, err := root.oracle(data)
	if err != nil {
		t.Fatal(err)
	}
	if !answer.exact || answer.bytes != 400 {
		t.Fatalf("region = %d exact=%v, want 400 exact", answer.bytes, answer.exact)
	}
}
