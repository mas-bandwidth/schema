package main

import (
	"encoding/hex"
	"testing"
)

// The file-form size oracle must count raw blob bodies as well as table
// records. C++ CatalogLoadMeasure independently answers 400 for this vector.
func TestFileWireSizeIncludesBlobRecords(t *testing.T) {
	_, _, units := corpus(t)
	root, err := newWireRoot(units, "blobdemo", "Catalog", false, false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := hex.DecodeString("010c0c03617274021102031106041107050c43060618010c05627269636b0708070000000811030911040a1105000b030102030c036361700608010c047461696c000b05deadbeef7f0c0b68656c6c6f20626c6f627300861b638ebaadbcc4030c9a5fcc128f0a03b2f40f72193b61dd7c58d1bafbf83bfffffffffffffffffb645ffecc6d4e8e43326721d7969cef054aa33067555b85a5024c86eb28aa5d28f025a0ba6c31e5e44f1c4f47c02e2f58fcaffad8e04b700c00000000000000")
	if err != nil {
		t.Fatal(err)
	}
	got, err := root.oracle(data)
	if err != nil {
		t.Fatal(err)
	}
	if !got.exact || got.bytes != 400 {
		t.Fatalf("region size = %d (exact %v), want exactly 400", got.bytes, got.exact)
	}
}
